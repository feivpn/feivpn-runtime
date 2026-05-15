package feiapi

import (
	"sort"
	"strings"
	"unicode"
)

// DetectCountry inspects a SubscriptionNode.Name and returns the ISO 3166-1
// alpha-2 country code (uppercase) for the egress location, or "" when the
// name is too unstructured to classify confidently.
//
// The upstream subscription does not expose a structured country field, so
// we have to reverse-engineer it from the human-readable name. We try, in
// order of decreasing reliability:
//
//  1. Regional-indicator emoji flag (🇭🇰 → HK) — least ambiguous.
//  2. A whitespace/punctuation-delimited bare ISO code ("[HK] 02", "JP-01").
//  3. Chinese country/region tokens ("香港", "日本", …).
//  4. English country/city tokens ("Hong Kong", "Tokyo", …).
//
// We return "" rather than guessing when no signal hits. Callers (e.g.
// `feivpnctl countries`) should bucket those nodes under "??" so the
// operator can see we couldn't classify them.
func DetectCountry(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return ""
	}

	if cc := detectFlagEmoji(s); cc != "" {
		return cc
	}
	if cc := detectBareISOCode(s); cc != "" {
		return cc
	}

	// Lowercase + Chinese-preserving comparison for token search.
	lc := strings.ToLower(s)
	for _, kw := range countryKeywords {
		if strings.Contains(lc, kw.token) {
			return kw.cc
		}
	}
	return ""
}

// IsKnownCountry reports whether code is a recognised ISO alpha-2 code we
// can ever produce from DetectCountry. Used to validate
// `--country` / `preferred_country` user input before we go fishing in the
// node list.
func IsKnownCountry(code string) bool {
	cc := strings.ToUpper(strings.TrimSpace(code))
	if len(cc) != 2 {
		return false
	}
	_, ok := knownCountries[cc]
	return ok
}

// CountryDisplayName returns a stable human-readable label for a known
// ISO code (Chinese for the locales we ship to). Returns the code itself
// if unknown so callers can always print *something*.
func CountryDisplayName(code string) string {
	cc := strings.ToUpper(strings.TrimSpace(code))
	if name, ok := knownCountries[cc]; ok {
		return name
	}
	return cc
}

// KnownCountryCodes returns all ISO codes feivpnctl knows how to detect,
// sorted alphabetically. Used by `feivpnctl countries --all` style
// callers and for help text.
func KnownCountryCodes() []string {
	codes := make([]string, 0, len(knownCountries))
	for cc := range knownCountries {
		codes = append(codes, cc)
	}
	sort.Strings(codes)
	return codes
}

// ----- internals -----

// detectFlagEmoji scans for two consecutive Unicode regional-indicator
// symbols (U+1F1E6 .. U+1F1FF). They encode an ISO alpha-2 pair where
// each glyph is the offset (A=0) into the alphabet.
func detectFlagEmoji(s string) string {
	const base = 0x1F1E6
	runes := []rune(s)
	for i := 0; i+1 < len(runes); i++ {
		a, b := runes[i], runes[i+1]
		if a >= base && a <= base+25 && b >= base && b <= base+25 {
			cc := string([]rune{rune('A') + (a - base), rune('A') + (b - base)})
			if _, ok := knownCountries[cc]; ok {
				return cc
			}
		}
	}
	return ""
}

// detectBareISOCode looks for an isolated 2-letter token that matches a
// known ISO code. We require the token to be surrounded by non-letter
// runes (or string boundaries) so substrings like "USA" don't match
// "SA", and the U.K. inside "ukraine" never claims to be "UK".
func detectBareISOCode(s string) string {
	upper := strings.ToUpper(s)
	for cc := range knownCountries {
		idx := 0
		for {
			pos := strings.Index(upper[idx:], cc)
			if pos < 0 {
				break
			}
			start := idx + pos
			end := start + 2
			if isLeftBoundary(upper, start) && isRightBoundary(upper, end) {
				return cc
			}
			idx = end
		}
	}
	return ""
}

func isLeftBoundary(s string, start int) bool {
	if start <= 0 {
		return true
	}
	// ISO codes are ASCII; inspect the immediately preceding byte.
	return !unicode.IsLetter(rune(s[start-1]))
}

func isRightBoundary(s string, end int) bool {
	if end >= len(s) {
		return true
	}
	return !unicode.IsLetter(rune(s[end]))
}

// countryKeywords are scanned with a lowercased substring match against
// the node name. Order matters — earlier entries win, so longer / more
// specific tokens sit ahead of shorter ones (e.g. "south korea" before
// "korea", "new zealand" before "zealand").
//
// Keep additions paired with an entry in knownCountries below.
type countryKeyword struct {
	token string
	cc    string
}

var countryKeywords = []countryKeyword{
	// East Asia
	{"香港", "HK"}, {"hong kong", "HK"}, {"hongkong", "HK"},
	{"台湾", "TW"}, {"臺灣", "TW"}, {"taiwan", "TW"},
	{"日本", "JP"}, {"东京", "JP"}, {"東京", "JP"}, {"大阪", "JP"}, {"japan", "JP"}, {"tokyo", "JP"}, {"osaka", "JP"},
	{"韩国", "KR"}, {"韓國", "KR"}, {"首尔", "KR"}, {"korea", "KR"}, {"seoul", "KR"},
	{"中国", "CN"}, {"china", "CN"},
	{"蒙古", "MN"}, {"mongolia", "MN"},

	// Southeast Asia
	{"新加坡", "SG"}, {"狮城", "SG"}, {"singapore", "SG"},
	{"马来西亚", "MY"}, {"马来", "MY"}, {"malaysia", "MY"}, {"kuala lumpur", "MY"},
	{"印度尼西亚", "ID"}, {"印尼", "ID"}, {"indonesia", "ID"}, {"jakarta", "ID"},
	{"菲律宾", "PH"}, {"philippines", "PH"}, {"manila", "PH"},
	{"越南", "VN"}, {"vietnam", "VN"},
	{"泰国", "TH"}, {"thailand", "TH"}, {"bangkok", "TH"},
	{"柬埔寨", "KH"}, {"cambodia", "KH"},
	{"老挝", "LA"}, {"laos", "LA"},
	{"缅甸", "MM"}, {"myanmar", "MM"},

	// South Asia
	{"印度", "IN"}, {"india", "IN"},
	{"巴基斯坦", "PK"}, {"pakistan", "PK"},
	{"孟加拉", "BD"}, {"bangladesh", "BD"},
	{"斯里兰卡", "LK"}, {"sri lanka", "LK"},
	{"尼泊尔", "NP"}, {"nepal", "NP"},

	// Middle East
	{"阿联酋", "AE"}, {"迪拜", "AE"}, {"emirates", "AE"}, {"dubai", "AE"},
	{"沙特", "SA"}, {"saudi", "SA"},
	{"土耳其", "TR"}, {"turkey", "TR"}, {"istanbul", "TR"},
	{"以色列", "IL"}, {"israel", "IL"},
	{"伊朗", "IR"}, {"iran", "IR"},
	{"卡塔尔", "QA"}, {"qatar", "QA"},

	// Europe
	{"英国", "GB"}, {"伦敦", "GB"}, {"united kingdom", "GB"}, {"britain", "GB"}, {"london", "GB"},
	{"德国", "DE"}, {"法兰克福", "DE"}, {"germany", "DE"}, {"frankfurt", "DE"},
	{"法国", "FR"}, {"巴黎", "FR"}, {"france", "FR"}, {"paris", "FR"},
	{"荷兰", "NL"}, {"阿姆斯特丹", "NL"}, {"netherlands", "NL"}, {"amsterdam", "NL"},
	{"意大利", "IT"}, {"italy", "IT"}, {"milan", "IT"},
	{"西班牙", "ES"}, {"spain", "ES"}, {"madrid", "ES"},
	{"葡萄牙", "PT"}, {"portugal", "PT"},
	{"瑞士", "CH"}, {"switzerland", "CH"}, {"zurich", "CH"},
	{"瑞典", "SE"}, {"sweden", "SE"},
	{"挪威", "NO"}, {"norway", "NO"},
	{"芬兰", "FI"}, {"finland", "FI"},
	{"丹麦", "DK"}, {"denmark", "DK"},
	{"波兰", "PL"}, {"poland", "PL"},
	{"奥地利", "AT"}, {"austria", "AT"},
	{"比利时", "BE"}, {"belgium", "BE"},
	{"爱尔兰", "IE"}, {"ireland", "IE"},
	{"希腊", "GR"}, {"greece", "GR"},
	{"匈牙利", "HU"}, {"hungary", "HU"},
	{"罗马尼亚", "RO"}, {"romania", "RO"},
	{"乌克兰", "UA"}, {"ukraine", "UA"},
	{"俄罗斯", "RU"}, {"莫斯科", "RU"}, {"russia", "RU"}, {"moscow", "RU"},

	// Americas
	{"美国", "US"}, {"洛杉矶", "US"}, {"硅谷", "US"}, {"圣何塞", "US"}, {"纽约", "US"}, {"西雅图", "US"},
	{"united states", "US"}, {"america", "US"}, {"usa", "US"}, {"los angeles", "US"}, {"new york", "US"}, {"silicon valley", "US"},
	{"加拿大", "CA"}, {"温哥华", "CA"}, {"多伦多", "CA"}, {"canada", "CA"}, {"toronto", "CA"}, {"vancouver", "CA"},
	{"墨西哥", "MX"}, {"mexico", "MX"},
	{"巴西", "BR"}, {"brazil", "BR"}, {"sao paulo", "BR"},
	{"阿根廷", "AR"}, {"argentina", "AR"},
	{"智利", "CL"}, {"chile", "CL"},

	// Oceania
	{"澳大利亚", "AU"}, {"澳洲", "AU"}, {"悉尼", "AU"}, {"墨尔本", "AU"}, {"australia", "AU"}, {"sydney", "AU"}, {"melbourne", "AU"},
	{"新西兰", "NZ"}, {"new zealand", "NZ"}, {"auckland", "NZ"},

	// Africa
	{"南非", "ZA"}, {"south africa", "ZA"}, {"johannesburg", "ZA"},
	{"埃及", "EG"}, {"egypt", "EG"},
	{"尼日利亚", "NG"}, {"nigeria", "NG"},
	{"肯尼亚", "KE"}, {"kenya", "KE"},
}

// knownCountries is the canonical set of ISO codes feivpnctl will accept
// from the user (--country, preferred_country) and ever emit from
// DetectCountry. Keep in sync with countryKeywords.
//
// Names are Chinese because the subscription product surface is
// Chinese-speaking; English consumers can fall back on the ISO code.
var knownCountries = map[string]string{
	// East Asia
	"HK": "香港", "TW": "台湾", "JP": "日本", "KR": "韩国", "CN": "中国", "MN": "蒙古",
	// Southeast Asia
	"SG": "新加坡", "MY": "马来西亚", "ID": "印度尼西亚", "PH": "菲律宾",
	"VN": "越南", "TH": "泰国", "KH": "柬埔寨", "LA": "老挝", "MM": "缅甸",
	// South Asia
	"IN": "印度", "PK": "巴基斯坦", "BD": "孟加拉", "LK": "斯里兰卡", "NP": "尼泊尔",
	// Middle East
	"AE": "阿联酋", "SA": "沙特", "TR": "土耳其", "IL": "以色列", "IR": "伊朗", "QA": "卡塔尔",
	// Europe
	"GB": "英国", "DE": "德国", "FR": "法国", "NL": "荷兰", "IT": "意大利",
	"ES": "西班牙", "PT": "葡萄牙", "CH": "瑞士", "SE": "瑞典", "NO": "挪威",
	"FI": "芬兰", "DK": "丹麦", "PL": "波兰", "AT": "奥地利", "BE": "比利时",
	"IE": "爱尔兰", "GR": "希腊", "HU": "匈牙利", "RO": "罗马尼亚",
	"UA": "乌克兰", "RU": "俄罗斯",
	// Americas
	"US": "美国", "CA": "加拿大", "MX": "墨西哥",
	"BR": "巴西", "AR": "阿根廷", "CL": "智利",
	// Oceania
	"AU": "澳大利亚", "NZ": "新西兰",
	// Africa
	"ZA": "南非", "EG": "埃及", "NG": "尼日利亚", "KE": "肯尼亚",
}
