package action

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/feivpn/feivpn-runtime/internal/logging"
)

// reachabilityTargets are small, geo-distributed HTTP endpoints we ping
// to confirm the egress is healthy. Each is expected to respond with a
// 2xx within a short timeout.
var reachabilityTargets = []string{
	"https://www.google.com/generate_204",
	"https://www.cloudflare.com/cdn-cgi/trace",
	"https://www.youtube.com/generate_204",
}

// egressProbeURLs is a list of services we ask "what's my IP". The
// first one to succeed wins.
var egressProbeURLs = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://icanhazip.com",
}

// dnsProbeHost is a hostname we resolve to confirm DNS is functional. It
// is intentionally unrelated to the VPN provider so a misconfigured DNS
// hijack shows up.
const dnsProbeHost = "www.cloudflare.com"

// Test runs the same set of probes a human ops engineer would run by
// hand to satisfy themselves that the tunnel is up: who is the egress
// IP, can we resolve DNS, is the daemon's TUN device alive, and can we
// reach a few canary URLs.
//
// All probes are best-effort; the Status field reports "ok" iff at
// least one reachability target succeeded AND DNS resolved.
func (r *Runner) Test() (*TestResult, error) {
	res := &TestResult{Status: "failed"}
	httpClient := &http.Client{Timeout: 5 * time.Second}

	// 1. Egress IP + latency
	if ip, via, lat, err := probeEgressIP(httpClient); err == nil {
		res.EgressIP = ip
		res.EgressIPVia = via
		res.LatencyMS = lat.Milliseconds()
	} else {
		res.Errors = append(res.Errors, "egress_ip: "+err.Error())
	}

	// 2. DNS
	res.DNS = probeDNS()
	if !res.DNS.OK {
		res.Errors = append(res.Errors, "dns: resolution failed for "+dnsProbeHost)
	}

	// 3. TUN (via daemon health if available)
	if h, err := r.Daemon.Health(); err == nil && h != nil {
		res.TUN.Up = h.Tun.Up
		res.TUN.Name = h.Tun.Name
	} else if err != nil {
		res.Errors = append(res.Errors, "tun: "+err.Error())
	}

	// 4. Reachability matrix
	reachOK := 0
	for _, target := range reachabilityTargets {
		probe := probeReach(httpClient, target)
		res.Reachability = append(res.Reachability, probe)
		if probe.OK {
			reachOK++
		}
	}

	if reachOK > 0 && res.DNS.OK {
		res.Status = "ok"
	} else if reachOK > 0 {
		res.Status = "partial"
	}
	logging.Info("test: done", "status", res.Status, "egress_ip", res.EgressIP, "reach_ok", reachOK)
	return res, nil
}

func probeEgressIP(c *http.Client) (ip string, via string, latency time.Duration, err error) {
	var lastErr error
	for _, u := range egressProbeURLs {
		start := time.Now()
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		resp, e := c.Do(req)
		if e != nil {
			lastErr = e
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = errors.New(u + ": HTTP " + resp.Status)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		ip := strings.TrimSpace(string(raw))
		// ifconfig.me sometimes returns trace-style key=value lines;
		// take only the first line and strip "ip=" prefix if present.
		if i := strings.IndexAny(ip, "\r\n"); i >= 0 {
			ip = ip[:i]
		}
		ip = strings.TrimPrefix(ip, "ip=")
		if ip != "" {
			return ip, u, time.Since(start), nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("all egress probes returned empty body")
	}
	return "", "", 0, lastErr
}

func probeDNS() DNSProbeResult {
	res := DNSProbeResult{}
	res.Servers = systemDNSServers()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, dnsProbeHost)
	if err != nil || len(addrs) == 0 {
		return res
	}
	res.Resolved = addrs[0]
	res.OK = true
	return res
}

func probeReach(c *http.Client, target string) ReachProbe {
	probe := ReachProbe{Target: target}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	resp, err := c.Do(req)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	defer resp.Body.Close()
	probe.Status = resp.StatusCode
	probe.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return probe
}

// systemDNSServers returns a best-effort list of resolver IPs the host
// is using. Implementation is intentionally simple — we only use this
// for diagnostic display, not for control flow.
func systemDNSServers() []string {
	// On most Linux/macOS hosts /etc/resolv.conf is the source of
	// truth. We don't parse it deeply; reading it is enough for human
	// diagnostics.
	if data, err := readFileLimited("/etc/resolv.conf", 8192); err == nil {
		var out []string
		for _, line := range strings.Split(data, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "nameserver ") {
				out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "nameserver ")))
			}
		}
		return out
	}
	return nil
}

func readFileLimited(path string, max int) (string, error) {
	f, err := openReadOnly(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, int64(max)))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
