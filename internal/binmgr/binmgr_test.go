package binmgr

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLocateRouter verifies the new ComponentFeivpnRouter switch arm:
// the locator should resolve and SHA-skip a placeholder entry for the
// current platform.
func TestLocateRouter(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binBase := "feivpn-router-" + runtime.GOOS + "-" + runtime.GOARCH
	binPath := filepath.Join(binDir, binBase)
	if err := os.WriteFile(binPath, []byte("placeholder"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{
	  "feivpn":         {"binaries": {}},
	  "feiapi":         {"binaries": {}},
	  "feivpn_router": {
	    "binaries": {
	      "` + runtime.GOOS + "-" + runtime.GOARCH + `": {
	        "path":   "bin/` + binBase + `",
	        "sha256": "0000000000000000000000000000000000000000000000000000000000000000",
	        "url":    "https://example/x"
	      }
	    }
	  }
	}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FEIVPN_BIN_DIR", binDir)
	loc := New(manifestPath)
	got, err := loc.Locate(ComponentFeivpnRouter)
	if err != nil {
		t.Fatalf("Locate(router): %v", err)
	}
	if got != binPath {
		t.Fatalf("got %q want %q", got, binPath)
	}
}

func TestLocateUnknownComponent(t *testing.T) {
	loc := New("/no/such/manifest.json")
	if _, err := loc.Locate("nope"); err == nil {
		t.Fatal("expected error for unknown component")
	}
}
