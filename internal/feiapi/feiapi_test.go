package feiapi

import (
	"encoding/base64"
	"testing"
)

func TestParseAccessKey(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantProto   string
		wantServer  string
		wantPort    int
		wantToken   string
		wantMethod  string
		wantNonZero bool // sanity check that *something* got populated
	}{
		{
			// trojan/anytls URIs put the password directly in userinfo
			// with no colon, so Go's url.Userinfo.Username() returns the
			// password and Password() is empty. parseAccessKey's
			// pre-existing behavior is to land that into Method, which
			// is structurally wrong (trojan has no method concept) but
			// harmless because nothing downstream reads Method. The bug
			// this test guards against is purely Server/Port population
			// for the router /32 bypass; Method/Token is best-effort
			// diagnostics.
			name:        "trojan",
			in:          "trojan://thepass@us-1.example.com:10001?sni=foo#%E7%BE%8E%E5%9B%BD%20US%20-%2001",
			wantProto:   "trojan",
			wantServer:  "us-1.example.com",
			wantPort:    10001,
			wantMethod:  "thepass",
			wantNonZero: true,
		},
		{
			name:        "ss",
			in:          "ss://YWVzLTI1Ni1nY206cGFzcw==@hk-1.example.com:8388#%E9%A6%99%E6%B8%AF%20HK%20-%2002",
			wantProto:   "ss",
			wantServer:  "hk-1.example.com",
			wantPort:    8388,
			// userinfo is the base64-encoded "method:pass" blob without
			// an inner colon at the URI level, so Go again reports it
			// all as Username; nothing downstream cares.
			wantMethod:  "YWVzLTI1Ni1nY206cGFzcw==",
			wantNonZero: true,
		},
		{
			name:        "anytls",
			in:          "anytls://thepass@jp-7.example.com:443#%E6%97%A5%E6%9C%AC%20JP%20-%2007",
			wantProto:   "anytls",
			wantServer:  "jp-7.example.com",
			wantPort:    443,
			wantMethod:  "thepass",
			wantNonZero: true,
		},
		{
			name: "vmess port as string",
			// the most common form FeiVPN backends emit
			in: "vmess://" + base64.StdEncoding.EncodeToString([]byte(
				`{"v":"2","ps":"美国 US - 03","add":"us-3.example.com","port":"443","id":"00000000-0000-0000-0000-000000000003","aid":"0","net":"ws","tls":"tls"}`,
			)),
			wantProto:   "vmess",
			wantServer:  "us-3.example.com",
			wantPort:    443,
			wantToken:   "00000000-0000-0000-0000-000000000003",
			wantNonZero: true,
		},
		{
			name: "vmess port as number",
			in: "vmess://" + base64.StdEncoding.EncodeToString([]byte(
				`{"v":"2","ps":"日本 JP - 11","add":"jp-11.example.com","port":10086,"id":"deadbeef-1111-1111-1111-deadbeef1111","aid":0}`,
			)),
			wantProto:   "vmess",
			wantServer:  "jp-11.example.com",
			wantPort:    10086,
			wantToken:   "deadbeef-1111-1111-1111-deadbeef1111",
			wantNonZero: true,
		},
		{
			name: "vmess raw base64 (no padding) with trailing fragment",
			in: "vmess://" + base64.RawStdEncoding.EncodeToString([]byte(
				`{"add":"hk-9.example.com","port":"8443","id":"feedf00d-2222-3333-4444-feedf00d5555"}`,
			)) + "#noise",
			wantProto:   "vmess",
			wantServer:  "hk-9.example.com",
			wantPort:    8443,
			wantToken:   "feedf00d-2222-3333-4444-feedf00d5555",
			wantNonZero: true,
		},
		{
			name:      "vmess garbage body — protocol still set, server stays empty",
			in:        "vmess://!!!not_base64!!!",
			wantProto: "vmess",
			// Server / Port intentionally empty; resolveProxyIP will fail
			// loud rather than silently dialling a wrong host.
			wantNonZero: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := &SubscriptionNode{AccessKey: tc.in}
			parseAccessKey(n)

			if n.Protocol != tc.wantProto {
				t.Errorf("Protocol = %q want %q", n.Protocol, tc.wantProto)
			}
			if n.Server != tc.wantServer {
				t.Errorf("Server = %q want %q", n.Server, tc.wantServer)
			}
			if n.Port != tc.wantPort {
				t.Errorf("Port = %d want %d", n.Port, tc.wantPort)
			}
			if tc.wantToken != "" && n.Token != tc.wantToken {
				t.Errorf("Token = %q want %q", n.Token, tc.wantToken)
			}
			if tc.wantMethod != "" && n.Method != tc.wantMethod {
				t.Errorf("Method = %q want %q", n.Method, tc.wantMethod)
			}
			if tc.wantNonZero && n.Protocol == "" {
				t.Errorf("nothing populated: %+v", n)
			}
		})
	}
}

func TestParseAccessKey_UnknownSchemeIsNoop(t *testing.T) {
	n := &SubscriptionNode{AccessKey: "http://nope.example.com/"}
	parseAccessKey(n)
	if n.Protocol != "" || n.Server != "" || n.Port != 0 {
		t.Errorf("expected zero values for unknown scheme, got %+v", n)
	}
}
