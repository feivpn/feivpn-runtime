package store

// NodeCache mirrors the TS web client's `servers_v1` + `feivpn-countries`
// design but collapses both into a single on-disk file. It exists so
// that `feivpnctl countries` and `ensure-ready` both have:
//
//  1. A *fast* country listing when the network is unreachable
//     (subscription fetch fails → fall back to last known nodes).
//  2. A way to detect whether the subscription actually changed since
//     the previous fetch — `LastChangedAt` only moves when the
//     server-returned node set differs.
//
// The country index is NOT persisted: it is cheap to re-derive from
// node names with feiapi.DetectCountry. Persisting it would add a
// consistency burden every time we add a new country keyword.
//
// Refresh policy is implemented by the caller (see action.refreshNodes).
// This package only stores / loads the bytes.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// CachedNode is the minimal shape of a subscription node we keep on
// disk. Mirrors feiapi.SubscriptionNode but lives here so the store
// package does not import feiapi (would create a cycle).
type CachedNode struct {
	Name      string `json:"name"`
	AccessKey string `json:"access_key"`
}

// NodeCache is the on-disk shape under /var/lib/feivpn/nodes.json.
//
// Hashes are SHA-256 hex strings; we use them as opaque change-detection
// tokens, never decoded. Time fields are RFC3339 in UTC.
type NodeCache struct {
	// SubscribeURLHash binds the cached node list to the subscription
	// URL it came from. When the user logs into a different account
	// (or the server rotates their subscribe_url), the URL changes →
	// the hash changes → the cache is treated as a miss and refetched
	// rather than silently returning stale nodes that belong to a
	// different identity. Mirrors the bug the TS client has where
	// `feivpn-subscription-hash` is a global key.
	SubscribeURLHash string `json:"subscribe_url_hash"`

	// ContentHash is the SHA-256 of canonical-JSON(nodes), recomputed
	// after every fetch. We use it to decide whether to bump
	// LastChangedAt and to suppress "noisy" downstream actions
	// (re-rendering daemon config, restarting services) when nothing
	// actually changed upstream.
	ContentHash string `json:"content_hash"`

	// FetchedAt advances on every successful fetch — even if the
	// content was identical. Lets `feivpnctl status` report "we
	// reached the API X seconds ago" independently of "the node set
	// last changed Y minutes ago".
	FetchedAt time.Time `json:"fetched_at"`

	// LastChangedAt only advances when ContentHash changes. Useful UX
	// for "is the node list moving?" without operators reading raw
	// hashes.
	LastChangedAt time.Time `json:"last_changed_at"`

	Nodes []CachedNode `json:"nodes"`
}

// IsFor reports whether this cache was built for the given subscribe
// URL. Callers should treat a false result as "cache miss" and refetch
// regardless of TTL.
func (c *NodeCache) IsFor(subscribeURL string) bool {
	if c == nil {
		return false
	}
	return c.SubscribeURLHash == HashSubscribeURL(subscribeURL)
}

// HashSubscribeURL returns the canonical SubscribeURLHash value for a
// given URL. Exported so callers can compare without round-tripping
// through NodeCache.
func HashSubscribeURL(url string) string {
	if url == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

// HashNodes returns the canonical ContentHash for a node list. Order
// matters in the canonical form (we sort by Name+AccessKey) so two
// fetches that return the same set in different order still hash equal
// and we don't false-positive a "subscription changed" event.
func HashNodes(nodes []CachedNode) string {
	if len(nodes) == 0 {
		return ""
	}
	canonical := make([]CachedNode, len(nodes))
	copy(canonical, nodes)
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].Name != canonical[j].Name {
			return canonical[i].Name < canonical[j].Name
		}
		return canonical[i].AccessKey < canonical[j].AccessKey
	})
	raw, err := json.Marshal(canonical)
	if err != nil {
		// json.Marshal on a struct of strings cannot fail; if it ever
		// does the safest behavior is to return an empty hash so the
		// caller treats the cache as definitely-different and refetches.
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// NodesPath is where the cache lives. Override via $FEIVPN_NODES_FILE
// (mirrors $FEIVPN_ACCOUNT_FILE for the adjacent account file).
func NodesPath() string {
	if env := os.Getenv("FEIVPN_NODES_FILE"); env != "" {
		return env
	}
	return "/var/lib/feivpn/nodes.json"
}

// ErrNoNodeCache is returned by LoadNodes when the file does not exist.
// Callers should treat this as a normal cold-start condition (fetch
// from API), not an error to surface.
var ErrNoNodeCache = errors.New("no node cache on disk: subscription has not been fetched yet")

// LoadNodes returns the on-disk cache, or ErrNoNodeCache if missing.
// A corrupt file (bad JSON) is reported as a real error so the operator
// notices — silently treating it as a miss would mask filesystem bugs.
func LoadNodes() (*NodeCache, error) {
	path := NodesPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoNodeCache
		}
		return nil, fmt.Errorf("store: read %s: %w", path, err)
	}
	var c NodeCache
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("store: parse %s: %w", path, err)
	}
	return &c, nil
}

// SaveNodes atomically (over)writes the cache file. Caller is
// responsible for filling in all fields; if you only have the new node
// list, use UpsertNodes instead.
func SaveNodes(c *NodeCache) error {
	if c == nil {
		return errors.New("store: SaveNodes(nil)")
	}
	if c.SubscribeURLHash == "" {
		return errors.New("store: SaveNodes called without subscribe_url_hash — refusing to write")
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(NodesPath(), raw)
}

// UpsertNodes is the high-level helper the action layer should use. It
// applies the documented refresh semantics in one place:
//
//   - If `previous` is nil OR was built for a different subscribe URL,
//     write a fresh cache. LastChangedAt = now.
//   - If the new content hash differs from `previous`, write the new
//     nodes and bump LastChangedAt = now.
//   - If everything matches, only the FetchedAt timestamp moves
//     forward; nodes are left untouched.
//
// Returns the cache that ultimately ended up on disk and a boolean
// telling the caller whether the node set actually changed (true) or
// the call was a no-op refresh (false).
func UpsertNodes(previous *NodeCache, subscribeURL string, fresh []CachedNode, now time.Time) (*NodeCache, bool, error) {
	urlHash := HashSubscribeURL(subscribeURL)
	contentHash := HashNodes(fresh)

	out := &NodeCache{
		SubscribeURLHash: urlHash,
		ContentHash:      contentHash,
		FetchedAt:        now.UTC(),
		Nodes:            fresh,
	}

	switch {
	case previous == nil:
		out.LastChangedAt = out.FetchedAt
	case previous.SubscribeURLHash != urlHash:
		// User switched account / subscribe URL rotated. Treat as a
		// brand-new cache — anything we knew before describes a
		// different identity.
		out.LastChangedAt = out.FetchedAt
	case previous.ContentHash != contentHash:
		out.LastChangedAt = out.FetchedAt
	default:
		// Same URL, same content. Preserve the historical
		// LastChangedAt so users can see "nodes haven't changed since
		// X" rather than the trivially-now value.
		out.LastChangedAt = previous.LastChangedAt
	}

	if err := SaveNodes(out); err != nil {
		return nil, false, err
	}
	changed := previous == nil || previous.ContentHash != contentHash || previous.SubscribeURLHash != urlHash
	return out, changed, nil
}
