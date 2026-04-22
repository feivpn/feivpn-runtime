package host

import (
	"runtime"
	"strings"
	"testing"
)

func TestDescribe_KnownPlatforms(t *testing.T) {
	cases := []struct {
		goos, goarch    string
		skillUpgradeTag string
		manifestKey     string
		friendly        string
		supported       bool
	}{
		{"linux", "amd64", SkillUpgradeTagLinux, "linux-amd64", "Linux x86_64", true},
		{"linux", "arm64", SkillUpgradeTagLinux, "linux-arm64", "Linux ARM64", true},
		{"darwin", "arm64", SkillUpgradeTagMac, "darwin-arm64", "macOS Apple Silicon", true},
		{"darwin", "amd64", SkillUpgradeTagMac, "darwin-amd64", "macOS Intel", true},
		{"windows", "amd64", "", "", "windows x86_64", false},
	}
	for _, c := range cases {
		t.Run(c.goos+"-"+c.goarch, func(t *testing.T) {
			got := describe(c.goos, c.goarch)
			if got.SkillUpgradeTag != c.skillUpgradeTag {
				t.Errorf("SkillUpgradeTag = %q want %q", got.SkillUpgradeTag, c.skillUpgradeTag)
			}
			if got.ManifestKey != c.manifestKey {
				t.Errorf("ManifestKey = %q want %q", got.ManifestKey, c.manifestKey)
			}
			if got.Friendly != c.friendly {
				t.Errorf("Friendly = %q want %q", got.Friendly, c.friendly)
			}
			if got.Supported() != c.supported {
				t.Errorf("Supported() = %v want %v", got.Supported(), c.supported)
			}
		})
	}
}

// TestSkillUpgradeTag_DistinctFromDesktop guards the architectural rule
// that the skill MUST NOT reuse the desktop client's platform tag set
// ("linux"/"mac"/"win"/"ios"/"android"/"web"). If anyone changes
// describe() to fall back on those, this test fails loudly.
func TestSkillUpgradeTag_DistinctFromDesktop(t *testing.T) {
	desktopTags := map[string]bool{
		"linux": true, "mac": true, "win": true,
		"ios": true, "android": true, "web": true, "darwin": true,
	}
	for _, goos := range []string{"linux", "darwin"} {
		got := describe(goos, "amd64").SkillUpgradeTag
		if desktopTags[got] {
			t.Errorf("SkillUpgradeTag for %s == %q, which collides with desktop client namespace", goos, got)
		}
		if got == "" {
			t.Errorf("SkillUpgradeTag for %s is empty; supported OSes must have a tag", goos)
		}
	}
}

func TestDetect_LiveHost(t *testing.T) {
	got := Detect()
	if got.GoOS != runtime.GOOS || got.GoArch != runtime.GOARCH {
		t.Fatalf("Detect did not pick up runtime values: %+v", got)
	}
	if (runtime.GOOS == "linux" || runtime.GOOS == "darwin") && got.SkillUpgradeTag == "" {
		t.Errorf("SkillUpgradeTag empty on a supposedly mapped OS %q", runtime.GOOS)
	}
	if !strings.Contains(got.Friendly, " ") {
		t.Errorf("Friendly missing space-separated arch: %q", got.Friendly)
	}
}

func TestAssertSupported(t *testing.T) {
	if err := (Info{}).AssertSupported(); err == nil {
		t.Errorf("zero-value Info should be unsupported")
	}
	live := Detect()
	if live.Supported() {
		if err := live.AssertSupported(); err != nil {
			t.Errorf("live host claims supported but AssertSupported failed: %v", err)
		}
	}
}
