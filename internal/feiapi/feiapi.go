// Package feiapi is a thin wrapper around the pinned `feiapi` CLI from
// feivpn/feivpn-apps. feivpnctl never imports the Go API client
// package directly — it spawns the pre-built feiapi binary so that the
// API secret stays baked into a single auditable artefact.
//
// The wire format and exit-code contract this wrapper relies on lives in
// client/go/api/cmd/feiapi/main.go in the upstream repo:
//
//	exit 0 → success; stdout is the JSON payload
//	exit 1 → usage / programmer error
//	exit 2 → network / API error (retriable in principle)
//	exit 3 → authentication / signature failure (do NOT retry)
//
// All payloads are emitted directly (no envelope wrapper) so callers
// json.Unmarshal straight into the type they expect.
package feiapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
	"github.com/feivpn/feivpn-runtime/internal/host"
)

// Client is the feivpnctl-side façade for `feiapi`.
//
// Every spawned `feiapi …` invocation has `--platform <skill tag>`
// prepended automatically, so the backend always sees the skill
// release-channel tag (e.g. "feivpn-runtime-linux") and never the
// desktop client's bare "linux"/"mac". This applies uniformly to
// /getid, /info, /register, /login, /version/check, /plans, /appconfig,
// etc. — the skill is one logical client and the server should be able
// to count, rate-limit, and dispatch it independently of the desktop app.
type Client struct {
	loc      *binmgr.Locator
	platform string // baked-in --platform value; "" disables injection
}

// New returns a Client backed by the given binary locator. The platform
// tag is auto-detected from the host (host.Info.SkillUpgradeTag) and
// will be injected as --platform on every feiapi invocation. On
// unsupported OSes (no skill tag) we fall back to letting feiapi pick
// its own default — those calls will fail at the network layer anyway.
func New(loc *binmgr.Locator) *Client {
	return &Client{
		loc:      loc,
		platform: host.Detect().SkillUpgradeTag,
	}
}

// NewWithPlatform is an escape hatch for tests / tools that need to
// pretend to be a different platform. Production code should use New.
func NewWithPlatform(loc *binmgr.Locator, platform string) *Client {
	return &Client{loc: loc, platform: platform}
}

// UserData is the unified payload returned by `feiapi getid`, `getinfo`,
// `login`, and `register`. The server reuses the same shape across all
// four endpoints, so we model it once.
//
// Two email-ish fields are intentional: /user/info returns `email`,
// while /getid, /login, /bind return `user_email`. Use ResolvedEmail()
// to get whichever is populated.
type UserData struct {
	UUID         string `json:"uuid,omitempty"`
	IsNew        bool   `json:"is_new,omitempty"`
	SubscribeURL string `json:"subscribe_url,omitempty"`
	Token        string `json:"token,omitempty"`
	AuthData     string `json:"auth_data,omitempty"`
	IsAdmin      bool   `json:"is_admin,omitempty"`
	InviteCode   string `json:"invite_code,omitempty"`
	ExpiredAt    *int64 `json:"expired_at,omitempty"`

	// Email is what /user/info uses.
	Email string `json:"email,omitempty"`
	// UserEmail is what /getid, /login, /bind use.
	UserEmail string `json:"user_email,omitempty"`

	// Per-day usage counters — read-only diagnostics; we never persist
	// these so the next refresh is always authoritative.
	TodayAvailableTime *int64 `json:"today_available_time,omitempty"`
	UsageTimeBalance   *int64 `json:"usage_time_balance,omitempty"`
	DailyUsageLimit    *int64 `json:"daily_usage_limit,omitempty"`
	SumUsageSeconds    *int64 `json:"sum_usage_seconds,omitempty"`
}

// ResolvedEmail returns whichever of UserEmail / Email is populated.
// UserEmail wins if both are set, since /getid is the canonical
// identity endpoint.
func (u *UserData) ResolvedEmail() string {
	if u == nil {
		return ""
	}
	if u.UserEmail != "" {
		return u.UserEmail
	}
	return u.Email
}

// SubscriptionNode is one entry from `feiapi getconfig`. The upstream
// feiapi outputs `name` + `access_key`; we additionally parse the
// access_key URL into structured Server / Port / Protocol so older
// callers (renderDaemonConfig) keep working.
type SubscriptionNode struct {
	Name      string `json:"name"`
	AccessKey string `json:"access_key"`

	// Derived (best-effort) — populated by parseAccessKey:
	Server   string `json:"server,omitempty"`
	Port     int    `json:"port,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Token    string `json:"token,omitempty"`
	Method   string `json:"method,omitempty"`
}

// configEnvelope mirrors what `feiapi getconfig` actually prints.
type configEnvelope struct {
	Nodes        []SubscriptionNode `json:"nodes"`
	NodeCount    int                `json:"node_count"`
	SubscribeURL string             `json:"subscribe_url"`
}

// VersionInfo mirrors `feiapi getversion` output.
type VersionInfo struct {
	Version     string `json:"version"`
	ForceUpdate bool   `json:"force_update,omitempty"`
	UpdateURL   string `json:"update_url"`
	Changelog   string `json:"changelog,omitempty"`
}

// Plan mirrors a single plan returned by `feiapi plans`.
type Plan struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Content       string `json:"content,omitempty"`
	MonthPrice    *int64 `json:"month_price,omitempty"`
	QuarterPrice  *int64 `json:"quarter_price,omitempty"`
	HalfYearPrice *int64 `json:"half_year_price,omitempty"`
	YearPrice     *int64 `json:"year_price,omitempty"`
	WeekPrice     *int64 `json:"week_price,omitempty"`
	OnetimePrice  *int64 `json:"onetime_price,omitempty"`
	Currency      string `json:"currency,omitempty"`
}

type plansEnvelope struct {
	Plans []Plan `json:"plans"`
	Count int    `json:"count"`
}

// AppConfig mirrors `feiapi appconfig` output.
type AppConfig struct {
	AppVersion       string `json:"app_version,omitempty"`
	RechargeURL      string `json:"recharge_url"`
	AddonRechargeURL string `json:"addon_recharge_url,omitempty"`
	TermsURL         string `json:"terms_url,omitempty"`
	PrivacyURL       string `json:"privacy_url,omitempty"`
	HelpURL          string `json:"help_url,omitempty"`
	SupportURL       string `json:"support_url,omitempty"`
	ShareURL         string `json:"share_url,omitempty"`
}

// ----- Identity endpoints (return UserData) -----

// GetID calls `feiapi getid --id <deviceID>`. Anonymous bootstrap: the
// server creates a device-only account on first call and returns the
// same UUID forever after for the same deviceID.
func (c *Client) GetID(deviceID, referrer string) (*UserData, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("feiapi: deviceID is required for getid")
	}
	args := []string{"getid", "--id", deviceID}
	if referrer != "" {
		args = append(args, "--referrer", referrer)
	}
	var u UserData
	if err := c.runJSON(args, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetInfo calls `feiapi getinfo --id <deviceID> --token <authData>`.
// authData is the `auth_data` (Authorization header value), not the raw
// `token` field.
func (c *Client) GetInfo(deviceID, authData string) (*UserData, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("feiapi: deviceID is required for getinfo")
	}
	if authData == "" {
		return nil, fmt.Errorf("feiapi: authData is required for getinfo")
	}
	args := []string{"getinfo", "--id", deviceID, "--token", authData}
	var u UserData
	if err := c.runJSON(args, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Login calls `feiapi login --email --password`. Server returns a fresh
// UserData (uuid + token + auth_data + subscribe_url + ...).
func (c *Client) Login(email, password string) (*UserData, error) {
	if email == "" || password == "" {
		return nil, fmt.Errorf("feiapi: email and password required for login")
	}
	args := []string{"login", "--email", email, "--password", password}
	var u UserData
	if err := c.runJSON(args, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Register calls `feiapi register --id <deviceID> --email --password`.
// The server "binds" the device's existing anonymous account to the
// email so the existing usage quota carries over.
func (c *Client) Register(deviceID, email, password string) (*UserData, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("feiapi: deviceID required for register")
	}
	if email == "" || password == "" {
		return nil, fmt.Errorf("feiapi: email and password required for register")
	}
	args := []string{"register", "--id", deviceID, "--email", email, "--password", password}
	var u UserData
	if err := c.runJSON(args, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// ChangePassword calls `feiapi changepw --auth-data <authData> --new-password`.
// Returns nil on success.
func (c *Client) ChangePassword(authData, newPassword string) error {
	if authData == "" {
		return fmt.Errorf("feiapi: authData required for changepw")
	}
	if newPassword == "" {
		return fmt.Errorf("feiapi: newPassword required for changepw")
	}
	args := []string{"changepw", "--auth-data", authData, "--new-password", newPassword}
	var ack map[string]string
	return c.runJSON(args, &ack)
}

// ----- Non-identity endpoints -----

// GetConfig fetches and decodes a subscription URL into a list of nodes.
func (c *Client) GetConfig(subscribeURL, timezone string) ([]SubscriptionNode, error) {
	if subscribeURL == "" {
		return nil, fmt.Errorf("feiapi: subscribeURL is required for getconfig")
	}
	args := []string{"getconfig", "--url", subscribeURL}
	if timezone != "" {
		args = append(args, "--timezone", timezone)
	}
	var env configEnvelope
	if err := c.runJSON(args, &env); err != nil {
		// Runtime-side subscription fallback (mirrors TS behavior):
		// if the original subscription host is unreachable, rewrite only
		// the host part to an API domain and retry once.
		//
		// Why here (runtime) instead of feiapi:
		// - feivpn-runtime treats feiapi as a pinned black-box binary.
		// - this keeps skill-specific resilience policy in the skill repo.
		if fallbackURL, ok := rewriteSubscriptionHost(subscribeURL, runtimeSubscriptionFallbackHost()); ok {
			fallbackArgs := []string{"getconfig", "--url", fallbackURL}
			if timezone != "" {
				fallbackArgs = append(fallbackArgs, "--timezone", timezone)
			}
			if retryErr := c.runJSON(fallbackArgs, &env); retryErr != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	for i := range env.Nodes {
		parseAccessKey(&env.Nodes[i])
	}
	return env.Nodes, nil
}

func runtimeSubscriptionFallbackHost() string {
	// Escape hatch for ops if backend migrates API domains.
	if v := strings.TrimSpace(os.Getenv("FEIVPN_SUBSCRIPTION_FALLBACK_HOST")); v != "" {
		return v
	}
	// Keep in sync with feivpn-apps/client/go/api/endpoint_manager.go.
	return "www.xfx365.com"
}

func rewriteSubscriptionHost(rawURL, fallbackHost string) (string, bool) {
	if rawURL == "" || fallbackHost == "" {
		return "", false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", false
	}
	if strings.EqualFold(u.Hostname(), fallbackHost) {
		return "", false
	}
	u.Host = fallbackHost
	return u.String(), true
}

// GetVersion calls `feiapi getversion --for <platform>`.
func (c *Client) GetVersion(platform string) (*VersionInfo, error) {
	args := []string{"getversion"}
	if platform != "" {
		args = append(args, "--for", platform)
	}
	var v VersionInfo
	if err := c.runJSON(args, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// FetchPlans calls `feiapi plans [--auth-data ...]`. authData empty ⇒
// guest mode (public default plans).
func (c *Client) FetchPlans(authData string) ([]Plan, error) {
	args := []string{"plans"}
	if authData != "" {
		args = append(args, "--auth-data", authData)
	}
	var env plansEnvelope
	if err := c.runJSON(args, &env); err != nil {
		return nil, err
	}
	return env.Plans, nil
}

// FetchAppConfig calls `feiapi appconfig`.
func (c *Client) FetchAppConfig() (*AppConfig, error) {
	var cfg AppConfig
	if err := c.runJSON([]string{"appconfig"}, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ----- runtime plumbing -----

func (c *Client) runJSON(args []string, out any) error {
	stdout, err := c.runRaw(args)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if len(stdout) == 0 {
		return fmt.Errorf("feiapi: empty stdout for %v", args)
	}
	if err := json.Unmarshal(stdout, out); err != nil {
		return fmt.Errorf("feiapi: parse %v output: %w", args[0], err)
	}
	return nil
}

func (c *Client) runRaw(args []string) ([]byte, error) {
	bin, err := c.loc.Locate(binmgr.ComponentFeiapi)
	if err != nil {
		return nil, err
	}
	// Inject --platform once, globally. We prepend rather than append
	// so the persistent flag sits before the subcommand (cobra accepts
	// either, but consistent ordering keeps logs grep-friendly).
	if c.platform != "" {
		args = append([]string{"--platform", c.platform}, args...)
	}
	res, err := binmgr.Spawn(bin, args, nil)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, mapExitCode(res.ExitCode, res.Stdout)
	}
	return res.Stdout, nil
}

// Exit-code contract — must stay in sync with feiapi/main.go upstream:
//
//	0 success
//	1 usage / programmer error
//	2 network / API error (retriable in principle)
//	3 authentication / signature failure (do NOT retry)
func mapExitCode(code int, stdout []byte) error {
	switch code {
	case 1:
		return fmt.Errorf("INVALID_ARGUMENT: %s", trimErr(stdout))
	case 2:
		return fmt.Errorf("API_UNREACHABLE: %s", trimErr(stdout))
	case 3:
		return fmt.Errorf("API_AUTH_FAILED: %s", trimErr(stdout))
	default:
		return fmt.Errorf("FEIAPI_FAILED (exit=%d): %s", code, trimErr(stdout))
	}
}

func trimErr(b []byte) string {
	if len(b) == 0 {
		return "<no output>"
	}
	if len(b) > 512 {
		b = b[:512]
	}
	return string(b)
}

// ParseAccessKey best-effort fills the legacy Server/Port/Protocol/Token/
// Method fields from a proxy URL. Supported schemes: ss, trojan, vless,
// vmess (partial — only host/port; encoded body is opaque), anytls.
//
// On parse failure the structured fields are left blank; callers should
// rely on AccessKey itself when shipping the node to the daemon.
//
// Exported so the action layer can re-hydrate nodes that came from the
// on-disk cache (which only persists Name + AccessKey, not the derived
// fields).
func ParseAccessKey(n *SubscriptionNode) { parseAccessKey(n) }

func parseAccessKey(n *SubscriptionNode) {
	ak := strings.TrimSpace(n.AccessKey)
	if ak == "" {
		return
	}
	scheme := ""
	for _, p := range []string{"ss://", "trojan://", "vless://", "vmess://", "anytls://"} {
		if strings.HasPrefix(ak, p) {
			scheme = strings.TrimSuffix(p, "://")
			break
		}
	}
	if scheme == "" {
		return
	}
	n.Protocol = scheme
	if scheme == "vmess" {
		parseVmessAccessKey(n, ak)
		return
	}

	u, err := url.Parse(ak)
	if err != nil {
		return
	}
	n.Server = u.Hostname()
	if portStr := u.Port(); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			n.Port = p
		}
	}
	if u.User != nil {
		n.Method = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			n.Token = pw
		}
	}
}

// parseVmessAccessKey populates Server/Port/Token (the user UUID) from
// the standard base64-encoded JSON body that follows `vmess://`. The
// fragment (`#name`) is stripped first because some subscription
// providers append it even though the canonical vmess URI uses the
// JSON `ps` field for the display name.
//
// Without this, ensure_ready's resolveProxyIP returns
// PROXY_IP_RESOLVE_FAILED for any vmess node, which prevents the C++
// router from installing the `<server>/32 via <gw>` bypass and leaves
// the daemon-to-server TCP socket subject to the same PBR loop the
// router exists to break.
func parseVmessAccessKey(n *SubscriptionNode, ak string) {
	body := strings.TrimPrefix(ak, "vmess://")
	if i := strings.Index(body, "#"); i >= 0 {
		body = body[:i]
	}
	raw, err := decodeBase64StdOrRaw(body)
	if err != nil {
		return
	}
	// Field types differ across vmess generators: `port` is sometimes a
	// JSON string ("443") and sometimes a number (443). json.Number
	// accepts both; we then try Int conversion.
	var cfg struct {
		Add  string      `json:"add"`
		Port json.Number `json:"port"`
		ID   string      `json:"id"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&cfg); err != nil {
		return
	}
	n.Server = strings.TrimSpace(cfg.Add)
	if cfg.Port != "" {
		if p, err := strconv.Atoi(cfg.Port.String()); err == nil {
			n.Port = p
		}
	}
	if cfg.ID != "" {
		n.Token = cfg.ID
	}
}

func decodeBase64StdOrRaw(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
