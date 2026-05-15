package store

import (
	"path/filepath"
	"testing"
	"time"
)

func setNodesPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.json")
	t.Setenv("FEIVPN_NODES_FILE", path)
	return path
}

func TestLoadNodes_MissingFile(t *testing.T) {
	setNodesPath(t)
	_, err := LoadNodes()
	if err != ErrNoNodeCache {
		t.Fatalf("expected ErrNoNodeCache, got %v", err)
	}
}

func TestUpsertNodes_FirstWrite(t *testing.T) {
	setNodesPath(t)
	now := time.Date(2026, 5, 15, 4, 0, 0, 0, time.UTC)
	nodes := []CachedNode{
		{Name: "🇭🇰 香港 02", AccessKey: "ss://aaa@1.1.1.1:443"},
		{Name: "🇯🇵 东京", AccessKey: "trojan://bbb@2.2.2.2:443"},
	}

	out, changed, err := UpsertNodes(nil, "https://api.example.com/subscribe?token=t1", nodes, now)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first write should report changed=true")
	}
	if !out.FetchedAt.Equal(now) || !out.LastChangedAt.Equal(now) {
		t.Errorf("timestamps wrong: fetched=%v changed=%v want %v", out.FetchedAt, out.LastChangedAt, now)
	}
	if out.SubscribeURLHash == "" || out.ContentHash == "" {
		t.Errorf("hashes not populated: %+v", out)
	}

	// Reload from disk, must be identical.
	got, err := LoadNodes()
	if err != nil {
		t.Fatal(err)
	}
	if got.SubscribeURLHash != out.SubscribeURLHash || got.ContentHash != out.ContentHash {
		t.Errorf("on-disk hashes diverged: %+v vs %+v", got, out)
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("nodes round-trip: got %d want 2", len(got.Nodes))
	}
}

func TestUpsertNodes_SameContent_PreservesLastChanged(t *testing.T) {
	setNodesPath(t)
	url := "https://api.example.com/subscribe?token=t1"
	nodes := []CachedNode{{Name: "HK 01", AccessKey: "ss://x@h:1"}}

	t0 := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	first, _, err := UpsertNodes(nil, url, nodes, t0)
	if err != nil {
		t.Fatal(err)
	}

	t1 := t0.Add(2 * time.Hour)
	// Same nodes, same URL → only FetchedAt should advance.
	again, changed, err := UpsertNodes(first, url, nodes, t1)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Errorf("identical content should report changed=false")
	}
	if !again.FetchedAt.Equal(t1) {
		t.Errorf("FetchedAt did not advance: %v", again.FetchedAt)
	}
	if !again.LastChangedAt.Equal(t0) {
		t.Errorf("LastChangedAt should have stayed at %v, got %v", t0, again.LastChangedAt)
	}
}

func TestUpsertNodes_NewContent_BumpsLastChanged(t *testing.T) {
	setNodesPath(t)
	url := "https://api.example.com/subscribe?token=t1"
	t0 := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)

	first, _, err := UpsertNodes(nil, url,
		[]CachedNode{{Name: "HK 01", AccessKey: "ss://x@h:1"}}, t0)
	if err != nil {
		t.Fatal(err)
	}

	t1 := t0.Add(time.Hour)
	updated, changed, err := UpsertNodes(first, url,
		[]CachedNode{
			{Name: "HK 01", AccessKey: "ss://x@h:1"},
			{Name: "JP 02", AccessKey: "trojan://y@h:2"},
		}, t1)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("content delta should report changed=true")
	}
	if !updated.LastChangedAt.Equal(t1) {
		t.Errorf("LastChangedAt should bump to %v, got %v", t1, updated.LastChangedAt)
	}
}

func TestUpsertNodes_URLChange_TreatedAsFreshCache(t *testing.T) {
	setNodesPath(t)
	t0 := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	first, _, err := UpsertNodes(nil, "https://a.example.com/sub?t=1",
		[]CachedNode{{Name: "HK 01", AccessKey: "ss://x@h:1"}}, t0)
	if err != nil {
		t.Fatal(err)
	}

	t1 := t0.Add(time.Hour)
	// Same node payload but different subscribe URL → must NOT inherit
	// LastChangedAt (different identity).
	updated, changed, err := UpsertNodes(first, "https://a.example.com/sub?t=DIFFERENT",
		[]CachedNode{{Name: "HK 01", AccessKey: "ss://x@h:1"}}, t1)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("URL change should always report changed=true")
	}
	if !updated.LastChangedAt.Equal(t1) {
		t.Errorf("LastChangedAt should reset to %v after URL switch, got %v", t1, updated.LastChangedAt)
	}
	if updated.SubscribeURLHash == first.SubscribeURLHash {
		t.Error("URL hashes should differ after URL switch")
	}
}

func TestNodeCache_IsFor(t *testing.T) {
	c := &NodeCache{SubscribeURLHash: HashSubscribeURL("u1")}
	if !c.IsFor("u1") {
		t.Error("IsFor should match the URL it was built for")
	}
	if c.IsFor("u2") {
		t.Error("IsFor should reject a different URL")
	}
	var nilc *NodeCache
	if nilc.IsFor("anything") {
		t.Error("nil cache should never match")
	}
}

func TestHashNodes_OrderInvariant(t *testing.T) {
	a := []CachedNode{{Name: "A", AccessKey: "ss://1"}, {Name: "B", AccessKey: "ss://2"}}
	b := []CachedNode{{Name: "B", AccessKey: "ss://2"}, {Name: "A", AccessKey: "ss://1"}}
	if HashNodes(a) != HashNodes(b) {
		t.Error("HashNodes should be order-invariant")
	}
}
