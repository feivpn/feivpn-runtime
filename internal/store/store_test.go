package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func setupTmp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FEIVPN_ACCOUNT_FILE", filepath.Join(dir, "account.json"))
	return dir
}

func TestLoadOnEmptyStore(t *testing.T) {
	setupTmp(t)
	_, err := Load()
	if !errors.Is(err, ErrNoAccount) {
		t.Errorf("Load on empty store = %v, want ErrNoAccount", err)
	}
	if Exists() {
		t.Error("Exists = true on empty store")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	setupTmp(t)

	in := &Account{
		UUID:         "413852962",
		Token:        "ccc7fb8c6f120efb94ff2356c6835bc5",
		AuthData:     "Bearer PTdxamokmiSFV2Q0kzAFlN7gVEPcT8MpJPO8mSmq5a815431",
		SubscribeURL: "https://msub.dubgcn.cn/xiaofeixia?token=ccc7fb8c6f120efb94ff2356c6835bc5",
		ExpiredAt:    1801583999,
		UserEmail:    "82569673@qq.com",
		InviteCode:   "RKkKy4Um",
		UpdatedAt:    1700000000,
	}
	if err := Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !Exists() {
		t.Error("Exists = false after Save")
	}

	out, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *out != *in {
		t.Errorf("roundtrip mismatch:\n  got:  %+v\n  want: %+v", out, in)
	}
	if !out.IsLoggedIn() {
		t.Error("IsLoggedIn = false despite non-empty AuthData")
	}
}

func TestIsLoggedInRequiresAuthData(t *testing.T) {
	a := &Account{UUID: "1", Token: "tok"}
	if a.IsLoggedIn() {
		t.Error("IsLoggedIn = true with empty AuthData (anonymous getid result)")
	}
	a.AuthData = "Bearer x"
	if !a.IsLoggedIn() {
		t.Error("IsLoggedIn = false with non-empty AuthData")
	}
}

func TestSaveRefusesEmptyUUID(t *testing.T) {
	setupTmp(t)
	if err := Save(&Account{Token: "x"}); err == nil {
		t.Error("Save with empty UUID succeeded; expected guard error")
	}
}

func TestAccountFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not meaningful on Windows")
	}
	setupTmp(t)
	if err := Save(&Account{UUID: "1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o want 0600", got)
	}
}
