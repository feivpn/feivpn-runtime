package feiapi

import (
	"regexp"
	"sort"
	"strings"
)

const backupMarker = "备用"

// Keep this in sync with the TS client:
// country_manager.ts: /^(.+?)\s+([A-Z]{2,3})\s*-\s*(\d+)$/
var serverNameRegex = regexp.MustCompile(`^(.+?)\s+([A-Z]{2,3})\s*-\s*(\d+)$`)
var countryCodeRegex = regexp.MustCompile(`^[A-Z]{2,3}$`)

// ParsedServerName is the structured output of ParseServerName.
type ParsedServerName struct {
	Country string // raw country segment from the name (may include "备用")
	Code    string // 2-3 uppercase code, e.g. HK / KOR
	Number  string // trailing ordinal, e.g. 01
}

// ParseServerName mirrors TS CountryManager.parseServerName.
func ParseServerName(name string) (*ParsedServerName, bool) {
	s := strings.TrimSpace(name)
	if s == "" {
		return nil, false
	}
	m := serverNameRegex.FindStringSubmatch(s)
	if len(m) != 4 {
		return nil, false
	}
	return &ParsedServerName{
		Country: strings.TrimSpace(m[1]),
		Code:    strings.ToUpper(strings.TrimSpace(m[2])),
		Number:  strings.TrimSpace(m[3]),
	}, true
}

// IsValidServerName mirrors TS CountryManager.isValidServerName.
func IsValidServerName(name string) bool {
	_, ok := ParseServerName(name)
	return ok
}

// IsBackupServerName mirrors TS CountryManager.isBackupServerName.
// A node is considered backup when the parsed country segment contains "备用".
func IsBackupServerName(name string) bool {
	p, ok := ParseServerName(name)
	return ok && strings.Contains(p.Country, backupMarker)
}

// DisplayCountryName removes the backup marker from the raw country
// segment. Mirrors TS getDisplayCountryName().
func DisplayCountryName(raw string) string {
	return strings.TrimSpace(strings.ReplaceAll(raw, backupMarker, ""))
}

// DetectCountry returns the server-name code (2-3 uppercase) when the
// name matches the TS naming contract; otherwise "".
func DetectCountry(name string) string {
	if p, ok := ParseServerName(name); ok {
		return p.Code
	}
	return ""
}

// IsKnownCountry validates CLI input. TS accepts 2-3 uppercase codes in
// names (e.g. HK, KOR), so we do the same here instead of hard-whitelisting
// only ISO alpha-2 values.
func IsKnownCountry(code string) bool {
	cc := strings.ToUpper(strings.TrimSpace(code))
	return countryCodeRegex.MatchString(cc)
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
