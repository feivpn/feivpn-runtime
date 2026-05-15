package feiapi

import "testing"

func TestDetectCountry(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Valid TS format
		{"香港 HK - 01", "HK"},
		{"香港备用 HK - 02", "HK"},
		{"韩国 KOR - 04", "KOR"},
		{"United States US - 03", "US"},
		{"英国 GB - 03", "GB"},

		// Invalid names should not be classified
		{"🇭🇰 香港 02", ""},
		{"JP-01", ""},
		{"Tokyo Premium", ""},
		{"VIP 01", ""},
		{"", ""},
		{"???", ""},
		{"剩余流量：130.15 GB", ""},
	}

	for _, c := range cases {
		got := DetectCountry(c.in)
		if got != c.want {
			t.Errorf("DetectCountry(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsKnownCountry(t *testing.T) {
	for _, cc := range []string{"HK", "hk", " jp ", "US", "KOR"} {
		if !IsKnownCountry(cc) {
			t.Errorf("IsKnownCountry(%q) = false, want true", cc)
		}
	}
	for _, cc := range []string{"", "X", "ZZZZ", "H1"} {
		if IsKnownCountry(cc) {
			t.Errorf("IsKnownCountry(%q) = true, want false", cc)
		}
	}
}

func TestParseServerNameAndBackup(t *testing.T) {
	p, ok := ParseServerName("香港备用 HK - 02")
	if !ok {
		t.Fatalf("ParseServerName failed")
	}
	if p.Country != "香港备用" || p.Code != "HK" || p.Number != "02" {
		t.Fatalf("unexpected parse: %+v", p)
	}
	if !IsBackupServerName("香港备用 HK - 02") {
		t.Fatalf("expected backup node")
	}
	if IsBackupServerName("香港 HK - 02") {
		t.Fatalf("non-backup node misdetected")
	}
	if DisplayCountryName("香港备用") != "香港" {
		t.Fatalf("DisplayCountryName should strip 备用")
	}
}

func TestCountryDisplayName(t *testing.T) {
	if got := CountryDisplayName("HK"); got != "香港" {
		t.Errorf("CountryDisplayName(HK) = %q, want 香港", got)
	}
	if got := CountryDisplayName("ZZ"); got != "ZZ" {
		t.Errorf("CountryDisplayName(ZZ) = %q, want ZZ (passthrough)", got)
	}
}
