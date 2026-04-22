package router

import (
	"runtime"
	"testing"
)

func TestSocketAddress(t *testing.T) {
	scheme, addr := SocketAddress()
	switch runtime.GOOS {
	case "darwin":
		if scheme != "tcp" || addr != "127.0.0.1:38964" {
			t.Fatalf("darwin: want tcp/127.0.0.1:38964, got %s/%s", scheme, addr)
		}
	case "linux":
		if scheme != "unix" || addr != "/var/run/feivpn_controller" {
			t.Fatalf("linux: want unix /var/run/feivpn_controller, got %s/%s", scheme, addr)
		}
	default:
		if scheme != "" || addr != "" {
			t.Fatalf("unsupported OS should return empty pair, got %s/%s", scheme, addr)
		}
	}
}
