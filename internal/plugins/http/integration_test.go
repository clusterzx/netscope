package http

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// testClient mirrors the production transport (InsecureSkipVerify, no keep-alives).
func testClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			TLSHandshakeTimeout: 3 * time.Second,
			DisableKeepAlives:   true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// TestRunIntegration fingerprints the router's web interfaces from the lab. Requires
// NETSCOPE_INTEGRATION=1.
func TestRunIntegration(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("set NETSCOPE_INTEGRATION=1 to run against the lab")
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"extra_ports": []string{"80", "443", "8080", "3000"}})
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{
		ID: 1, PrimaryIP: "192.168.8.1", IPs: []string{"192.168.8.1"},
		Ports: []plugin.PortRef{
			{IP: "192.168.8.1", Proto: "tcp", Port: 80, Service: "http"},
			{IP: "192.168.8.1", Proto: "tcp", Port: 8080, Service: "http"},
			{IP: "192.168.8.1", Proto: "tcp", Port: 443, Service: "http", Tunnel: "ssl"},
		},
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatalf("run: %v", err)
	}
	obs := sink.All()
	if len(obs) == 0 {
		t.Fatal("no observations")
	}
	for _, o := range obs {
		if o.HTTP == nil {
			continue
		}
		for _, s := range o.HTTP.Services {
			fav := int32(0)
			if s.FaviconHash != nil {
				fav = *s.FaviconHash
			}
			t.Logf("%s -> %d %q server=%q apps=%v favicon=%d", s.URL, s.StatusCode, s.Title, s.Server, appNames(s.Apps), fav)
		}
	}
}
