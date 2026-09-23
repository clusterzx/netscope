package api

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"netscope/internal/inventory"
	"netscope/internal/wgconf"
)

func TestSubnetTunnelAPI(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	priv, _ := wgtypes.GeneratePrivateKey()
	server, _ := wgtypes.GeneratePrivateKey()
	config := "[Interface]\nPrivateKey = " + priv.String() + "\nAddress = 10.10.10.3/32\nPostUp = rm -rf /\n\n[Peer]\nPublicKey = " +
		server.PublicKey().String() + "\nEndpoint = 203.0.113.9:51820\nAllowedIPs = 0.0.0.0/0\n"

	// an invalid configuration is rejected at the field
	resp, body := h.do(t, "POST", "/api/v1/credentials", map[string]any{"name": "Kaputt", "type": "wireguard",
		"values": map[string]any{"config": "[Interface]\nPrivateKey = x\n"}}, csrf, "1")
	if resp.StatusCode != 400 || !strings.Contains(string(body), `"field":"config"`) {
		t.Fatalf("invalid config: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, "POST", "/api/v1/credentials", map[string]any{"name": "RZ", "type": "wireguard",
		"values": map[string]any{"config": config}}, csrf, "1")
	if resp.StatusCode != 201 || strings.Contains(string(body), priv.String()) {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	var cred credentialView
	_ = json.Unmarshal(body, &cred)

	// inspect: public data and warnings only
	resp, body = h.do(t, "POST", "/api/v1/tunnels/inspect", map[string]any{"credentialId": cred.ID, "subnets": []string{"10.99.0.0/24"}}, csrf, "1")
	var sum wgconf.Summary
	_ = json.Unmarshal(body, &sum)
	if resp.StatusCode != 200 || sum.PublicKey != priv.PublicKey().String() || strings.Contains(string(body), priv.String()) ||
		!strings.Contains(strings.Join(sum.Warnings, " "), "PostUp wird ignoriert") {
		t.Fatalf("inspect: %d %s", resp.StatusCode, body)
	}

	// subnet through the tunnel
	resp, body = h.do(t, "POST", "/api/v1/subnets", map[string]any{"cidr": "10.99.0.0/24", "name": "Rechenzentrum", "access": "wireguard",
		"interface": "eth9"}, csrf, "1")
	if resp.StatusCode != 400 || !strings.Contains(string(body), "tunnelCredentialId") {
		t.Fatalf("missing tunnel: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, "POST", "/api/v1/subnets", map[string]any{"cidr": "10.99.0.0/24", "name": "Rechenzentrum", "access": "wireguard",
		"tunnelCredentialId": cred.ID, "interface": "eth9", "enabled": true}, csrf, "1")
	var sn subnetView
	_ = json.Unmarshal(body, &sn)
	if resp.StatusCode != 201 || sn.Access != inventory.AccessWireGuard || sn.TunnelCredentialID == nil || *sn.TunnelCredentialID != cred.ID || sn.Interface != "" {
		t.Fatalf("create subnet: %d %s", resp.StatusCode, body)
	}
	targets, err := h.inv.Subnets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range targets {
		if tg.CIDR.String() == "10.99.0.0/24" && !tg.Routed {
			t.Error("tunnel subnet must be routed for the scanners")
		}
	}

	// a non-WireGuard credential is no tunnel
	resp, body = h.do(t, "POST", "/api/v1/credentials", map[string]any{"name": "ssh", "type": "ssh",
		"values": map[string]any{"username": "root", "password": "x"}}, csrf, "1")
	if resp.StatusCode != 201 {
		t.Fatalf("create ssh credential: %d %s", resp.StatusCode, body)
	}
	var sshCred credentialView
	_ = json.Unmarshal(body, &sshCred)
	resp, body = h.do(t, "PUT", "/api/v1/subnets/"+strconv.FormatInt(sn.ID, 10), map[string]any{"cidr": "10.99.0.0/24", "access": "wireguard",
		"tunnelCredentialId": sshCred.ID, "enabled": true}, csrf, "1")
	if resp.StatusCode != 400 || !strings.Contains(string(body), "tunnelCredentialId") {
		t.Fatalf("wrong credential type: %d %s", resp.StatusCode, body)
	}

	// the credential is in use by the subnet
	resp, body = h.do(t, "DELETE", "/api/v1/credentials/"+strconv.FormatInt(cred.ID, 10), nil, csrf, "1")
	if resp.StatusCode != 409 || !strings.Contains(string(body), "Subnetz 10.99.0.0/24") {
		t.Fatalf("delete in use: %d %s", resp.StatusCode, body)
	}
	// without a tunnel manager (tests, non-Linux) tunnels are reported as unavailable
	resp, body = h.do(t, "GET", "/api/v1/tunnels", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"available":false`) {
		t.Fatalf("tunnels: %d %s", resp.StatusCode, body)
	}
}
