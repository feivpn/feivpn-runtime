package action

import (
	"errors"
	"time"

	"github.com/feivpn/feivpn-runtime/internal/feiapi"
	"github.com/feivpn/feivpn-runtime/internal/logging"
	"github.com/feivpn/feivpn-runtime/internal/store"
	"github.com/feivpn/feivpn-runtime/internal/tz"
)

// nodesView is the canonical "what does the world look like right now"
// snapshot the action layer hands to its callers (Countries(),
// renderDaemonConfig(), …). It always carries:
//
//   - The node list to actually operate on (Nodes).
//   - Provenance metadata (FetchedAt / LastChangedAt) so user-facing
//     output can surface "last refreshed N seconds ago".
//   - Source flag — "live" when we just pulled it from the API,
//     "stale" when we fell back to the on-disk cache because the API
//     was unreachable. This distinction matters because the operator
//     should know they're seeing an outdated picture.
type nodesView struct {
	Nodes         []feiapi.SubscriptionNode
	FetchedAt     time.Time
	LastChangedAt time.Time
	Source        string // "live" | "stale"
	Changed       bool   // set when this fetch produced a different node set than the previous on-disk cache
}

// refreshNodes is the single funnel through which every action layer
// caller obtains a node list. It implements the documented refresh
// policy:
//
//	1. Fetch the subscription via feiapi (always — TTL=0 by design).
//	2. On success, write/update /var/lib/feivpn/nodes.json:
//	   - URL hash mismatch  → treat as cold cache (LastChangedAt = now)
//	   - content hash same  → only FetchedAt advances, nodes untouched
//	   - content hash diff  → overwrite + bump LastChangedAt
//	3. On fetch failure, fall back to the on-disk cache so callers can
//	   still operate (e.g. user lost connectivity but wants to inspect
//	   the country list). If the cache is also missing, surface the
//	   underlying network error verbatim.
//
// We never return an empty node list with no error: callers can rely on
// "err == nil ⇒ len(view.Nodes) > 0".
func (r *Runner) refreshNodes(subscribeURL string) (*nodesView, error) {
	if subscribeURL == "" {
		return nil, errors.New("CONFIG_INCOMPLETE: subscribe_url is empty (run `feivpnctl whoami` or `feivpnctl login`)")
	}

	previous, loadErr := store.LoadNodes()
	if loadErr != nil && !errors.Is(loadErr, store.ErrNoNodeCache) {
		// Corrupt cache file. Don't refuse to operate — log it and
		// continue as if cold-start. UpsertNodes will overwrite the
		// bad bytes on the next successful fetch.
		logging.Warn("refresh_nodes: cache load failed; treating as cold-start", "err", loadErr)
		previous = nil
	}

	zone := tz.IANA()
	live, fetchErr := r.Feiapi.GetConfig(subscribeURL, zone)
	if fetchErr != nil {
		if previous != nil && previous.IsFor(subscribeURL) && len(previous.Nodes) > 0 {
			logging.Warn("refresh_nodes: subscription fetch failed; falling back to on-disk cache",
				"err", fetchErr,
				"cached_age", time.Since(previous.FetchedAt).Round(time.Second).String(),
				"cached_nodes", len(previous.Nodes),
			)
			return &nodesView{
				Nodes:         feiapiNodesFromCache(previous.Nodes),
				FetchedAt:     previous.FetchedAt,
				LastChangedAt: previous.LastChangedAt,
				Source:        "stale",
				Changed:       false,
			}, nil
		}
		return nil, fetchErr
	}

	cached, changed, saveErr := store.UpsertNodes(previous, subscribeURL, cacheNodesFromFeiapi(live), time.Now())
	if saveErr != nil {
		// Persistence failure is non-fatal — we still have the live
		// nodes in memory. Just log and carry on with a synthetic
		// view that mirrors what the cache *would* have looked like.
		logging.Warn("refresh_nodes: cache save failed; serving live data without persistence", "err", saveErr)
		now := time.Now().UTC()
		view := &nodesView{
			Nodes:         live,
			FetchedAt:     now,
			LastChangedAt: now,
			Source:        "live",
			Changed:       true,
		}
		if previous != nil && previous.IsFor(subscribeURL) && previous.ContentHash == store.HashNodes(cacheNodesFromFeiapi(live)) {
			view.LastChangedAt = previous.LastChangedAt
			view.Changed = false
		}
		return view, nil
	}

	return &nodesView{
		Nodes:         live,
		FetchedAt:     cached.FetchedAt,
		LastChangedAt: cached.LastChangedAt,
		Source:        "live",
		Changed:       changed,
	}, nil
}

// cacheNodesFromFeiapi narrows a feiapi.SubscriptionNode (which carries
// a bunch of derived fields like Server/Port/Protocol) to the minimal
// shape we store on disk. Keeping the cache small + protocol-agnostic
// means a router/protocol upgrade never invalidates persisted bytes.
func cacheNodesFromFeiapi(in []feiapi.SubscriptionNode) []store.CachedNode {
	out := make([]store.CachedNode, len(in))
	for i, n := range in {
		out[i] = store.CachedNode{Name: n.Name, AccessKey: n.AccessKey}
	}
	return out
}

// feiapiNodesFromCache is the inverse. We re-run feiapi.ParseAccessKey
// on the way back so downstream callers (resolveProxyIP, daemon config
// renderer) see the same structured Server/Port/Protocol fields they
// would have gotten from a live fetch — otherwise a stale-cache fallback
// would unconditionally fail at "subscription node has no server host".
func feiapiNodesFromCache(in []store.CachedNode) []feiapi.SubscriptionNode {
	out := make([]feiapi.SubscriptionNode, len(in))
	for i, n := range in {
		out[i] = feiapi.SubscriptionNode{Name: n.Name, AccessKey: n.AccessKey}
		feiapi.ParseAccessKey(&out[i])
	}
	return out
}
