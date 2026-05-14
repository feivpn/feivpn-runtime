package tz

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIANA_AlwaysLoadable(t *testing.T) {
	got := IANA()
	if got == "" {
		t.Fatalf("IANA() returned empty string")
	}
	if !isLoadable(got) {
		t.Fatalf("IANA() returned %q which time.LoadLocation cannot resolve", got)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("TZ", "Asia/Shanghai")
	if got := fromEnv(); got != "Asia/Shanghai" {
		t.Fatalf("fromEnv() = %q, want Asia/Shanghai", got)
	}

	t.Setenv("TZ", ":America/New_York")
	if got := fromEnv(); got != "America/New_York" {
		t.Fatalf("fromEnv() should strip leading colon, got %q", got)
	}

	t.Setenv("TZ", "Garbage/NotReal")
	if got := fromEnv(); got != "" {
		t.Fatalf("fromEnv() should reject unknown zones, got %q", got)
	}
}

func TestFromLocaltimeSymlink_Synthetic(t *testing.T) {
	dir := t.TempDir()
	zif := filepath.Join(dir, "zoneinfo", "Asia", "Shanghai")
	if err := os.MkdirAll(filepath.Dir(zif), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zif, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	target := zif
	const marker = "/zoneinfo/"
	idx := -1
	for i := 0; i < len(target); i++ {
		if target[i:] == marker[:1]+"" {
			break
		}
		if i+len(marker) <= len(target) && target[i:i+len(marker)] == marker {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("test setup: marker not present in %q", target)
	}
	candidate := target[idx+len(marker):]
	if !isLoadable(candidate) {
		t.Fatalf("test setup: %q should be loadable", candidate)
	}
}

func TestIsLoadable_RejectsLocal(t *testing.T) {
	if isLoadable("Local") {
		t.Fatalf("isLoadable(\"Local\") must be false")
	}
	if isLoadable("") {
		t.Fatalf("isLoadable(\"\") must be false")
	}
}
