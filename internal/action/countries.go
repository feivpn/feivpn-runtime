package action

import (
	"fmt"
	"sort"
	"time"

	"github.com/feivpn/feivpn-runtime/internal/feiapi"
	"github.com/feivpn/feivpn-runtime/internal/logging"
)

// Countries fetches the current subscription, classifies each node into
// an ISO alpha-2 country bucket, and returns a sorted summary. It is
// the read-only counterpart to `--country` / `preferred_country`: the
// operator runs `feivpnctl countries` to discover what codes their
// subscription actually supports.
//
// Node fetching is funnelled through r.refreshNodes() so the cache file
// (/var/lib/feivpn/nodes.json) and `ensure-ready`'s view of the world
// stay in sync. When the API is unreachable we transparently serve the
// last known list and tag the result with `source: "stale"` so the
// operator knows what they're looking at.
//
// The country index itself is NOT cached on disk — it is cheap to
// re-derive from node names with feiapi.DetectCountry, and persisting
// it would create a consistency burden every time we add a new keyword.
func (r *Runner) Countries() (*CountriesResult, error) {
	acc, err := r.refreshAccountForEnsureReady()
	if err != nil {
		return nil, err
	}
	if acc.SubscribeURL == "" {
		return nil, fmt.Errorf("CONFIG_INCOMPLETE: server returned no subscribe_url — try `feivpnctl whoami` or `feivpnctl login`")
	}

	view, err := r.refreshNodes(acc.SubscribeURL)
	if err != nil {
		return nil, err
	}

	buckets := map[string]*CountryBucket{}
	var unknown []string
	for _, n := range view.Nodes {
		cc := feiapi.DetectCountry(n.Name)
		if cc == "" {
			unknown = append(unknown, n.Name)
			continue
		}
		b, ok := buckets[cc]
		if !ok {
			b = &CountryBucket{
				Code:        cc,
				DisplayName: feiapi.CountryDisplayName(cc),
			}
			buckets[cc] = b
		}
		b.Count++
		if len(b.Sample) < 3 {
			b.Sample = append(b.Sample, n.Name)
		}
	}

	out := &CountriesResult{
		Status:        view.Source, // "live" | "stale"
		Total:         len(view.Nodes),
		Countries:     make([]CountryBucket, 0, len(buckets)),
		Unknown:       unknown,
		FetchedAt:     view.FetchedAt,
		LastChangedAt: view.LastChangedAt,
		AgeSeconds:    int64(time.Since(view.FetchedAt).Round(time.Second).Seconds()),
	}
	for _, b := range buckets {
		out.Countries = append(out.Countries, *b)
		out.Classified += b.Count
	}
	// Stable order: most-populated countries first, ties broken by ISO
	// code so the output is deterministic.
	sort.Slice(out.Countries, func(i, j int) bool {
		if out.Countries[i].Count != out.Countries[j].Count {
			return out.Countries[i].Count > out.Countries[j].Count
		}
		return out.Countries[i].Code < out.Countries[j].Code
	})

	logging.Info("countries: classified",
		"total", out.Total,
		"buckets", len(out.Countries),
		"unknown", len(unknown),
		"source", out.Status,
		"age_sec", out.AgeSeconds,
	)
	return out, nil
}
