package feiapi

import "testing"

func TestDetectCountry(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// emoji flag wins over everything else
		{"🇭🇰 香港 02", "HK"},
		{"🇯🇵 Tokyo Premium", "JP"},
		{"🇺🇸 LA-1", "US"},

		// bare ISO code, with various separators
		{"[HK] 02", "HK"},
		{"JP-01", "JP"},
		{"NODE_US_03", "US"},
		{"sg.premium", "SG"},

		// Chinese tokens
		{"香港 02 中转", "HK"},
		{"日本东京 03", "JP"},
		{"美国洛杉矶 高级", "US"},
		{"新加坡 BGP", "SG"},
		{"台湾 IEPL", "TW"},
		{"澳洲悉尼", "AU"},
		{"阿联酋迪拜", "AE"},
		{"南非约翰内斯堡", "ZA"},

		// English tokens
		{"Hong Kong 02", "HK"},
		{"Tokyo Premium", "JP"},
		{"Los Angeles BGP", "US"},
		{"New York 1", "US"},
		{"Frankfurt DE", "DE"},
		{"London UK", "GB"},

		// nothing recognisable
		{"VIP 01", ""},
		{"", ""},
		{"???", ""},

		// must NOT misclassify USA → "SA" via substring
		{"USA Premium", "US"},
	}

	for _, c := range cases {
		got := DetectCountry(c.in)
		if got != c.want {
			t.Errorf("DetectCountry(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsKnownCountry(t *testing.T) {
	for _, cc := range []string{"HK", "hk", " jp ", "US"} {
		if !IsKnownCountry(cc) {
			t.Errorf("IsKnownCountry(%q) = false, want true", cc)
		}
	}
	for _, cc := range []string{"", "X", "ZZZ", "foo"} {
		if IsKnownCountry(cc) {
			t.Errorf("IsKnownCountry(%q) = true, want false", cc)
		}
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
