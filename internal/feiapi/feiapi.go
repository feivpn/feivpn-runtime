// Package feiapi is a thin wrapper around the pinned `feiapi` CLI from
// feivpn/feivpn-apps. feivpnctl never imports the Go API client
// package directly — it spawns the pre-built feiapi binary so that the
// API secret stays baked into a single auditable artefact.
package feiapi

import (
	"encoding/json"
	"fmt"

	"github.com/feivpn/feivpn-runtime/internal/binmgr"
)

// Client is the feivpnctl-side façade for `feiapi`.
type Client struct {
	loc *binmgr.Locator
}

func New(loc *binmgr.Locator) *Client { return &Client{loc: loc} }

// Envelope mirrors the OpenAPI Envelope schema:
//
//	{ "code": 0, "message": "...", "data": { ... } }
type Envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// SubscriptionNode mirrors the parsed `getconfig` line item.
type SubscriptionNode struct {
	Name     string `json:"name"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Token    string `json:"token,omitempty"`
	Method   string `json:"method,omitempty"`
	Raw      string `json:"raw,omitempty"`
}

// VersionInfo mirrors the version/check response data block.
type VersionInfo struct {
	Latest      string `json:"latest"`
	Forced      bool   `json:"forced"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
}

// GetID calls `feiapi getid` and returns the raw envelope JSON.
func (c *Client) GetID(referrer string) (*Envelope, error) {
	args := []string{"getid"}
	if referrer != "" {
		args = append(args, "--referrer", referrer)
	}
	return c.runEnvelope(args)
}

// GetInfo calls `feiapi getinfo --token <token>`.
func (c *Client) GetInfo(token string) (*Envelope, error) {
	if token == "" {
		return nil, fmt.Errorf("feiapi: token is required for getinfo")
	}
	return c.runEnvelope([]string{"getinfo", "--token", token})
}

// GetConfig calls `feiapi getconfig --token <token>` and parses the
// returned subscription nodes.
func (c *Client) GetConfig(token string, tz string) ([]SubscriptionNode, error) {
	args := []string{"getconfig", "--token", token}
	if tz != "" {
		args = append(args, "--tz", tz)
	}
	stdout, err := c.runRaw(args)
	if err != nil {
		return nil, err
	}
	var nodes []SubscriptionNode
	if err := json.Unmarshal(stdout, &nodes); err != nil {
		return nil, fmt.Errorf("feiapi: parse getconfig output: %w", err)
	}
	return nodes, nil
}

// GetVersion calls `feiapi getversion`.
func (c *Client) GetVersion(platform string) (*VersionInfo, error) {
	args := []string{"getversion"}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	stdout, err := c.runRaw(args)
	if err != nil {
		return nil, err
	}
	var v VersionInfo
	if err := json.Unmarshal(stdout, &v); err != nil {
		return nil, fmt.Errorf("feiapi: parse getversion output: %w", err)
	}
	return &v, nil
}

// runRaw exec's feiapi and returns stdout, mapping non-zero exit codes
// to canonical error strings. The exit-code → name mapping is defined
// in client/go/api/cmd/feiapi/main.go on the upstream side.
func (c *Client) runRaw(args []string) ([]byte, error) {
	bin, err := c.loc.Locate(binmgr.ComponentFeiapi)
	if err != nil {
		return nil, err
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

func (c *Client) runEnvelope(args []string) (*Envelope, error) {
	stdout, err := c.runRaw(args)
	if err != nil {
		return nil, err
	}
	var env Envelope
	if err := json.Unmarshal(stdout, &env); err != nil {
		return nil, fmt.Errorf("feiapi: parse envelope: %w", err)
	}
	return &env, nil
}

// Exit-code contract — must stay in sync with feiapi/main.go upstream:
//   0   ok
//   1   generic
//   10  network failure (all domains unreachable)
//   11  signature / auth rejection
//   12  upstream returned code != 0
func mapExitCode(code int, stdout []byte) error {
	switch code {
	case 10:
		return fmt.Errorf("API_NETWORK_FAILURE: %s", trimErr(stdout))
	case 11:
		return fmt.Errorf("API_AUTH_REJECTED: %s", trimErr(stdout))
	case 12:
		return fmt.Errorf("API_LOGICAL_ERROR: %s", trimErr(stdout))
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
