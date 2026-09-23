package wgconf

import (
	"net/netip"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func keys(t *testing.T) (priv, peer, psk wgtypes.Key) {
	t.Helper()
	var err error
	if priv, err = wgtypes.GeneratePrivateKey(); err != nil {
		t.Fatal(err)
	}
	p2, _ := wgtypes.GeneratePrivateKey()
	psk, _ = wgtypes.GenerateKey()
	return priv, p2.PublicKey(), psk
}

// opnsense is a config as the OPNsense peer generator exports it.
func opnsense(priv, peer, psk wgtypes.Key) string {
	return `[Interface]
PrivateKey = ` + priv.String() + `
Address = 10.10.10.3/32
DNS = 192.168.1.1

[Peer]
PublicKey = ` + peer.String() + `
PresharedKey = ` + psk.String() + `
Endpoint = vpn.example.org:51820
AllowedIPs = 0.0.0.0/0, ::/0
PostUp = iptables -A FORWARD -i %i -j ACCEPT
`
}

func TestParseOPNsense(t *testing.T) {
	priv, peer, psk := keys(t)
	c, err := Parse(opnsense(priv, peer, psk))
	if err != nil {
		t.Fatal(err)
	}
	if c.PrivateKey != priv || c.Peer.PublicKey != peer || c.Peer.PresharedKey == nil || *c.Peer.PresharedKey != psk {
		t.Error("keys not parsed")
	}
	if len(c.Addresses) != 1 || c.Addresses[0] != netip.MustParsePrefix("10.10.10.3/32") || c.Peer.Endpoint != "vpn.example.org:51820" {
		t.Errorf("interface/peer: %+v", c)
	}
	if len(c.Peer.AllowedIPs) != 2 || c.Keepalive() != DefaultKeepalive || len(c.Ignored) != 2 {
		t.Errorf("allowed=%v keepalive=%d ignored=%v", c.Peer.AllowedIPs, c.Keepalive(), c.Ignored)
	}
	s := c.Summary([]netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")})
	joined := strings.Join(s.Warnings, "\n")
	for _, want := range []string{"0.0.0.0/0", "PersistentKeepalive", "DNS wird ignoriert", "PostUp wird ignoriert"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warning %q missing in %q", want, joined)
		}
	}
	if s.PublicKey != priv.PublicKey().String() || s.PeerPublicKey != peer.String() || !s.PresharedKey || s.Keepalive != 25 {
		t.Errorf("summary: %+v", s)
	}
	if strings.Contains(joined, priv.String()) || strings.Contains(joined, psk.String()) {
		t.Error("summary must not contain secrets")
	}
}

func TestParseSplitTunnel(t *testing.T) {
	priv, peer, _ := keys(t)
	c, err := Parse("[Interface]\nPrivateKey=" + priv.String() + "\nAddress = 10.0.0.2\nMTU = 1380\n\n[Peer]\nPublicKey = " + peer.String() +
		"\nEndpoint = 203.0.113.7:51820\nAllowedIPs = 10.0.0.0/24, 192.168.1.0/24\nPersistentKeepalive = 15 # NAT\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Addresses[0].String() != "10.0.0.2/32" || c.MTU != 1380 || c.Keepalive() != 15 {
		t.Errorf("config: %+v", c)
	}
	if !c.Covers(netip.MustParsePrefix("192.168.1.0/24")) || !c.Covers(netip.MustParsePrefix("192.168.1.128/25")) ||
		c.Covers(netip.MustParsePrefix("192.168.2.0/24")) || c.Covers(netip.MustParsePrefix("192.168.0.0/16")) {
		t.Error("Covers")
	}
	s := c.Summary([]netip.Prefix{netip.MustParsePrefix("192.168.2.0/24")})
	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], "192.168.2.0/24") {
		t.Errorf("warnings: %v", s.Warnings)
	}
}

func TestParseErrors(t *testing.T) {
	priv, peer, _ := keys(t)
	iface := "[Interface]\nPrivateKey = " + priv.String() + "\nAddress = 10.0.0.2/32\n"
	peerSec := "[Peer]\nPublicKey = " + peer.String() + "\nEndpoint = vpn:51820\nAllowedIPs = 10.0.0.0/24\n"
	for name, tc := range map[string]struct{ text, want string }{
		"empty":          {"", "PrivateKey"},
		"bad key":        {"[Interface]\nPrivateKey = abc\n", "Zeile 2"},
		"no address":     {"[Interface]\nPrivateKey = " + priv.String() + "\n" + peerSec, "Address"},
		"no peer":        {iface, "[Peer]"},
		"two peers":      {iface + peerSec + peerSec, "mehrere [Peer]"},
		"no endpoint":    {iface + "[Peer]\nPublicKey = " + peer.String() + "\nAllowedIPs = 10.0.0.0/24\n", "Endpoint"},
		"bad endpoint":   {iface + "[Peer]\nEndpoint = nur-host\n", "host:port"},
		"own key":        {iface + "[Peer]\nPublicKey = " + priv.PublicKey().String() + "\nEndpoint = a:1\nAllowedIPs = 10.0.0.0/8\n", "eigene Schlüssel"},
		"unknown option": {iface + "Foo = bar\n", "unbekannte Option"},
		"no section":     {"PrivateKey = " + priv.String(), "vor dem ersten Abschnitt"},
		"garbage":        {"hello world", "Schlüssel = Wert"},
		"duplicate":      {iface + "PrivateKey = " + priv.String() + "\n", "mehrfach"},
	} {
		if _, err := Parse(tc.text); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}
