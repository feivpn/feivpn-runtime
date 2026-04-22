// Package binmgr locates, verifies, and execs the pinned feivpn / feiapi
// binaries shipped under bin/ inside this repo (and copied to
// /opt/feivpn/bin/ at install time).
//
// Locator priority:
//
//  1. $FEIVPN_BIN_DIR (developer override)
//  2. /opt/feivpn/bin/   (default install layout)
//  3. <feivpnctl-dir>/bin/  (run-from-source fallback for `make build`)
//  4. /usr/local/bin/   (fallback if someone manually placed the binaries)
//
// Verifier: SHA256 of the resolved binary is checked against the matching
// entry in manifest/binaries.manifest.json (loaded once and cached).
//
// Exec: a small wrapper around os/exec that captures stdout (parsed by
// callers as JSON) and forwards stderr unchanged.
package binmgr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Component identifies one of the two pinned binaries.
type Component string

const (
	ComponentFeivpn       Component = "feivpn"
	ComponentFeiapi       Component = "feiapi"
	ComponentFeivpnRouter Component = "feivpn-router"
)

// Locator finds and verifies pinned binaries.
type Locator struct {
	manifestPath string
	manifestOnce sync.Once
	manifest     *Manifest
	manifestErr  error
}

// New returns a Locator. manifestPath defaults to
// /opt/feivpn/manifest.json, falling back to manifest/binaries.manifest.json
// next to the running feivpnctl binary.
func New(manifestPath string) *Locator {
	if manifestPath == "" {
		manifestPath = defaultManifestPath()
	}
	return &Locator{manifestPath: manifestPath}
}

// Manifest mirrors the on-disk schema. Only the fields binmgr needs are
// modelled here; unknown fields are ignored.
type Manifest struct {
	Feivpn       ComponentEntry `json:"feivpn"`
	Feiapi       ComponentEntry `json:"feiapi"`
	FeivpnRouter ComponentEntry `json:"feivpn_router"`
}

type ComponentEntry struct {
	Tag      string                       `json:"tag"`
	Version  string                       `json:"version"`
	Binaries map[string]BinaryEntry       `json:"binaries"`
}

type BinaryEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

// Manifest returns the parsed manifest, loading it once.
func (l *Locator) Manifest() (*Manifest, error) {
	l.manifestOnce.Do(func() {
		raw, err := os.ReadFile(l.manifestPath)
		if err != nil {
			l.manifestErr = fmt.Errorf("binmgr: read manifest %s: %w", l.manifestPath, err)
			return
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			l.manifestErr = fmt.Errorf("binmgr: parse manifest: %w", err)
			return
		}
		l.manifest = &m
	})
	return l.manifest, l.manifestErr
}

// PlatformKey returns the manifest key for the current host (e.g. "linux-amd64").
func PlatformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// Locate returns the verified absolute path to the requested component
// for the current platform. Errors are wrapped with one of the
// well-known string codes BINARY_MISSING / BINARY_CHECKSUM_MISMATCH /
// UNSUPPORTED_PLATFORM so the CLI can surface them.
func (l *Locator) Locate(c Component) (string, error) {
	m, err := l.Manifest()
	if err != nil {
		return "", err
	}
	var entry ComponentEntry
	switch c {
	case ComponentFeivpn:
		entry = m.Feivpn
	case ComponentFeiapi:
		entry = m.Feiapi
	case ComponentFeivpnRouter:
		entry = m.FeivpnRouter
	default:
		return "", fmt.Errorf("UNSUPPORTED_PLATFORM: unknown component %q", c)
	}
	bin, ok := entry.Binaries[PlatformKey()]
	if !ok {
		return "", fmt.Errorf("UNSUPPORTED_PLATFORM: %s/%s not in manifest", c, PlatformKey())
	}

	resolved, err := l.resolveOnDisk(c, bin.Path)
	if err != nil {
		return "", err
	}
	if isPlaceholder(bin.SHA256) {
		return resolved, nil
	}
	if err := verifySHA(resolved, bin.SHA256); err != nil {
		return "", err
	}
	return resolved, nil
}

// resolveOnDisk searches the locator priority list for the requested
// relative path, returning the first match.
func (l *Locator) resolveOnDisk(c Component, relPath string) (string, error) {
	base := filepath.Base(relPath) // e.g. "feivpn-linux-amd64"

	candidates := []string{}
	if env := os.Getenv("FEIVPN_BIN_DIR"); env != "" {
		candidates = append(candidates,
			filepath.Join(env, base),
			filepath.Join(env, string(c)),
		)
	}
	candidates = append(candidates,
		filepath.Join("/opt/feivpn/bin", base),
		filepath.Join("/opt/feivpn/bin", string(c)),
	)
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "..", relPath),
			filepath.Join(dir, "bin", base),
		)
	}
	candidates = append(candidates,
		filepath.Join("/usr/local/bin", base),
		filepath.Join("/usr/local/bin", string(c)),
	)

	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			abs, _ := filepath.Abs(p)
			return abs, nil
		}
	}
	return "", fmt.Errorf("BINARY_MISSING: %s not found in any of: %s", c, strings.Join(candidates, ", "))
}

// SpawnResult captures the output of a child process.
type SpawnResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Spawn runs the binary at path with args, capturing stdout for
// machine-readable parsing. Stderr is copied to feivpnctl's own stderr
// in real time. Honours $FEIVPN_RUNTIME_DRYRUN by returning a zero-value
// result without execing (used by unit tests).
func Spawn(path string, args []string, env []string) (*SpawnResult, error) {
	if os.Getenv("FEIVPN_RUNTIME_DRYRUN") == "1" {
		return &SpawnResult{ExitCode: 0}, nil
	}
	cmd := exec.Command(path, args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}

	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn %s: %w", path, err)
	}

	captured, _ := io.ReadAll(stdout)
	err := cmd.Wait()

	res := &SpawnResult{Stdout: captured, ExitCode: 0}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			return nil, err
		}
	}
	return res, nil
}

// SpawnDetached starts the binary as a long-lived background daemon.
// Stdout/stderr are routed to the given log file. Returns the spawned PID.
func SpawnDetached(path string, args []string, env []string, logFile string) (int, error) {
	if os.Getenv("FEIVPN_RUNTIME_DRYRUN") == "1" {
		return 0, nil
	}
	cmd := exec.Command(path, args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	if logFile != "" {
		_ = os.MkdirAll(filepath.Dir(logFile), 0o755)
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return 0, err
		}
		cmd.Stdout = f
		cmd.Stderr = f
	}
	cmd.SysProcAttr = sysProcAttrDetached()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// ----- helpers -----

func verifySHA(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("BINARY_CHECKSUM_MISMATCH: %s\n  want: %s\n  got:  %s", path, want, got)
	}
	return nil
}

func isPlaceholder(sha string) bool {
	return strings.Trim(sha, "0") == "" || sha == ""
}

func defaultManifestPath() string {
	for _, p := range []string{
		"/opt/feivpn/manifest.json",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, rel := range []string{
			"../manifest/binaries.manifest.json",
			"manifest/binaries.manifest.json",
			"../../manifest/binaries.manifest.json",
		} {
			candidate := filepath.Join(dir, rel)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return "manifest/binaries.manifest.json"
}
