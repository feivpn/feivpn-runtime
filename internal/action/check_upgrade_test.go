package action

import (
	"os"
	"strings"
	"testing"
)

func TestBuildUpgradePlan_NormalisesTagAndComposesCommand(t *testing.T) {
	cases := []struct {
		name     string
		remote   string
		wantTag  string
		wantTagInCmd bool
	}{
		{"server-omits-v-prefix", "0.2.0", "v0.2.0", true},
		{"server-includes-v-prefix", "v0.2.0", "v0.2.0", true},
		{"empty-remote-version", "", "", false},
		{"whitespace-around-version", "  v1.0.0 ", "v1.0.0", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := buildUpgradePlan(c.remote)
			if plan.TargetTag != c.wantTag {
				t.Errorf("TargetTag = %q want %q", plan.TargetTag, c.wantTag)
			}
			if !plan.RequiresRoot {
				t.Errorf("RequiresRoot must be true")
			}
			if !strings.HasSuffix(plan.Command, "&& sudo feivpnctl upgrade") {
				t.Errorf("Command must end with the restart step, got: %s", plan.Command)
			}
			if c.wantTagInCmd && !strings.Contains(plan.Command, "--tag "+c.wantTag) {
				t.Errorf("Command should contain --tag %s, got: %s", c.wantTag, plan.Command)
			}
			if !c.wantTagInCmd && strings.Contains(plan.Command, "--tag") {
				t.Errorf("Command must not contain --tag when remote version unknown, got: %s", plan.Command)
			}
			if len(plan.Steps) != 2 {
				t.Errorf("Steps must split install + restart, got %d", len(plan.Steps))
			}
		})
	}
}

func TestBuildUpgradePlan_RespectsInstallerOverride(t *testing.T) {
	t.Setenv("FEIVPN_INSTALLER_URL", "https://example.test/install.sh")
	plan := buildUpgradePlan("0.3.0")
	if plan.InstallerURL != "https://example.test/install.sh" {
		t.Fatalf("InstallerURL = %q want override", plan.InstallerURL)
	}
	if !strings.Contains(plan.Command, "https://example.test/install.sh") {
		t.Errorf("Command must use overridden URL, got: %s", plan.Command)
	}
	_ = os.Unsetenv("FEIVPN_INSTALLER_URL")
}
