// Package tz returns the host's IANA time zone name (e.g.
// "Asia/Shanghai", "America/New_York", "UTC"). The FeiVPN backend's
// /xiaofeixia subscription endpoint expects the IANA form — Go's
// time.Now().Zone() returns only the abbreviation ("UTC", "CST",
// "PST") which the server treats as garbage and which is itself
// ambiguous (CST = China Standard Time = US Central Standard Time).
//
// Resolution order (matches what most Linux/macOS distros agree on):
//
//  1. $TZ env var, if it names a real zone
//  2. /etc/timezone (Debian/Ubuntu/Alpine; Docker images inherit this)
//  3. /etc/localtime symlink → ".../zoneinfo/Asia/Shanghai"
//     (Arch, Fedora, RHEL, macOS)
//  4. fall back to "UTC" so we never send the abbreviation
//
// All steps verify the candidate via time.LoadLocation so we never
// emit a garbage value.
package tz

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// IANA returns the host's IANA time zone name, or "UTC" if it cannot
// be detected. The result is always a value that time.LoadLocation
// recognises.
func IANA() string {
	if name := fromEnv(); name != "" {
		return name
	}
	if name := fromEtcTimezone(); name != "" {
		return name
	}
	if name := fromLocaltimeSymlink(); name != "" {
		return name
	}
	return "UTC"
}

func fromEnv() string {
	v := strings.TrimSpace(os.Getenv("TZ"))
	if v == "" {
		return ""
	}
	// $TZ is sometimes set to ":Asia/Shanghai" (POSIX leading colon).
	v = strings.TrimPrefix(v, ":")
	if isLoadable(v) {
		return v
	}
	return ""
}

func fromEtcTimezone() string {
	raw, err := os.ReadFile("/etc/timezone")
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(raw))
	if isLoadable(v) {
		return v
	}
	return ""
}

func fromLocaltimeSymlink() string {
	const path = "/etc/localtime"
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	// On macOS the link is relative, e.g. "/var/db/timezone/zoneinfo/Asia/Shanghai".
	// On Linux it's typically "../usr/share/zoneinfo/Asia/Shanghai".
	if !filepath.IsAbs(target) {
		target = filepath.Clean(filepath.Join(filepath.Dir(path), target))
	}
	const marker = "/zoneinfo/"
	idx := strings.LastIndex(target, marker)
	if idx < 0 {
		return ""
	}
	candidate := target[idx+len(marker):]
	if isLoadable(candidate) {
		return candidate
	}
	return ""
}

func isLoadable(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// ErrNoZone is reserved for callers that want a hard failure instead
// of the "UTC" fallback. Unused by IANA() but exported so callers can
// build wrappers like `Strict() (string, error)` later.
var ErrNoZone = errors.New("tz: cannot determine host IANA time zone")
