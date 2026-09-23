package tls

import (
	"context"
	"os"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestRunIntegration probes the router's TLS ports from the lab. Requires
// NETSCOPE_INTEGRATION=1.
func TestRunIntegration(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("set NETSCOPE_INTEGRATION=1 to run against the lab")
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"extra_ports": []string{"443", "8443"}, "check_weak": true})
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{
		ID: 1, PrimaryIP: "192.168.8.1", IPs: []string{"192.168.8.1"},
		Ports: []plugin.PortRef{
			{IP: "192.168.8.1", Proto: "tcp", Port: 443, Service: "http", Tunnel: "ssl"},
			{IP: "192.168.8.1", Proto: "tcp", Port: 8443, Service: "https-alt", Tunnel: "ssl"},
		},
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatalf("run: %v", err)
	}
	obs := sink.All()
	if len(obs) == 0 || obs[0].TLS == nil || len(obs[0].TLS.Certs) == 0 {
		t.Fatalf("no certs: %+v", obs)
	}
	for _, c := range obs[0].TLS.Certs {
		t.Logf("port %d: CN=%q issuer=%q self=%v versions=%v weakProto=%v weakCipher=%v",
			c.Port, c.SubjectCN, c.IssuerCN, c.SelfSigned, c.Versions, c.WeakProtocols, c.WeakCiphers)
	}
}
