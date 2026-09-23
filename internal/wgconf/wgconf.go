// Package wgconf parses WireGuard client configurations in the wg-quick format (as
// exported by OPNsense, pfSense, wg-easy, UniFi …) for NetScope's own tunnels. Only what
// a client tunnel needs is kept: keys, addresses, MTU and one peer. Options that would run
// commands or change the host (PostUp, DNS, Table …) are reported and ignored.
package wgconf

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// DefaultKeepalive keeps the tunnel open through NAT when the config has none (seconds).
const DefaultKeepalive = 25

// Config is a parsed client configuration.
type Config struct {
	PrivateKey wgtypes.Key
	Addresses  []netip.Prefix
	ListenPort int
	MTU        int
	Peer       Peer
	// Ignored lists options that NetScope does not apply (with the reason).
	Ignored []string
}

// Peer is the remote side (the WireGuard server).
type Peer struct {
	PublicKey    wgtypes.Key
	PresharedKey *wgtypes.Key
	Endpoint     string // host:port
	AllowedIPs   []netip.Prefix
	Keepalive    int // seconds, 0 = not set in the config
}

// Summary is the public part of a configuration (no keys except public ones) with
// warnings for the given subnets.
type Summary struct {
	PublicKey     string   `json:"publicKey"`     // own public key (register it on the server)
	Addresses     []string `json:"addresses"`     // tunnel addresses
	Endpoint      string   `json:"endpoint"`      // server host:port
	PeerPublicKey string   `json:"peerPublicKey"` // server key
	AllowedIPs    []string `json:"allowedIps"`
	Keepalive     int      `json:"keepalive"`
	MTU           int      `json:"mtu,omitempty"`
	PresharedKey  bool     `json:"presharedKey"`
	Warnings      []string `json:"warnings"`
}

// ParseError is a problem at a line of the configuration.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("Zeile %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

var ignoredKeys = map[string]string{
	"postup":     "NetScope führt keine Befehle aus",
	"postdown":   "NetScope führt keine Befehle aus",
	"preup":      "NetScope führt keine Befehle aus",
	"predown":    "NetScope führt keine Befehle aus",
	"dns":        "die Namensauflösung des Servers bleibt unverändert",
	"table":      "NetScope setzt nur Routen für die zugeordneten Subnetze",
	"saveconfig": "nicht nötig",
	"fwmark":     "nicht nötig",
}

// Parse reads a wg-quick configuration with exactly one [Peer].
func Parse(text string) (*Config, error) {
	c := &Config{}
	var (
		section string
		peers   int
		seen    = map[string]bool{}
	)
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if i := strings.IndexAny(raw, "#;"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			section = strings.ToLower(strings.TrimSpace(raw[1 : len(raw)-1]))
			switch section {
			case "interface":
			case "peer":
				peers++
				if peers > 1 {
					return nil, &ParseError{line, "mehrere [Peer]-Abschnitte – für NetScope genügt genau einer (der WireGuard-Server)"}
				}
			default:
				return nil, &ParseError{line, fmt.Sprintf("unbekannter Abschnitt [%s]", raw[1:len(raw)-1])}
			}
			continue
		}
		k, v, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, &ParseError{line, "Zeile ohne „Schlüssel = Wert“"}
		}
		key, val := strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if section == "" {
			return nil, &ParseError{line, "Wert vor dem ersten Abschnitt ([Interface] oder [Peer])"}
		}
		if why, ok := ignoredKeys[key]; ok {
			c.Ignored = append(c.Ignored, fmt.Sprintf("%s wird ignoriert – %s", strings.TrimSpace(k), why))
			continue
		}
		id := section + "." + key
		if seen[id] && key != "address" && key != "allowedips" {
			return nil, &ParseError{line, fmt.Sprintf("%s mehrfach angegeben", strings.TrimSpace(k))}
		}
		seen[id] = true
		if err := c.set(section, key, val); err != nil {
			return nil, &ParseError{line, err.Error()}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	switch {
	case !seen["interface.privatekey"]:
		return nil, &ParseError{0, "PrivateKey im Abschnitt [Interface] fehlt"}
	case len(c.Addresses) == 0:
		return nil, &ParseError{0, "Address im Abschnitt [Interface] fehlt (Tunnel-Adresse von NetScope)"}
	case peers == 0:
		return nil, &ParseError{0, "Abschnitt [Peer] (WireGuard-Server) fehlt"}
	case !seen["peer.publickey"]:
		return nil, &ParseError{0, "PublicKey im Abschnitt [Peer] fehlt"}
	case c.Peer.Endpoint == "":
		return nil, &ParseError{0, "Endpoint im Abschnitt [Peer] fehlt – NetScope baut den Tunnel zum Server auf und braucht dessen Adresse"}
	case len(c.Peer.AllowedIPs) == 0:
		return nil, &ParseError{0, "AllowedIPs im Abschnitt [Peer] fehlt"}
	}
	if c.Peer.PublicKey == c.PrivateKey.PublicKey() {
		return nil, &ParseError{0, "PublicKey des Peers ist der eigene Schlüssel – dort gehört der Schlüssel des Servers hin"}
	}
	return c, nil
}

func (c *Config) set(section, key, val string) error {
	switch section + "." + key {
	case "interface.privatekey":
		k, err := wgtypes.ParseKey(val)
		if err != nil {
			return errors.New("PrivateKey ist kein gültiger WireGuard-Schlüssel")
		}
		c.PrivateKey = k
	case "interface.address":
		list, err := prefixes(val, true)
		if err != nil {
			return fmt.Errorf("Address: %w", err)
		}
		c.Addresses = append(c.Addresses, list...)
	case "interface.listenport":
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("ListenPort muss zwischen 1 und 65535 liegen")
		}
		c.ListenPort = n
	case "interface.mtu":
		n, err := strconv.Atoi(val)
		if err != nil || n < 1280 || n > 9000 {
			return errors.New("MTU muss zwischen 1280 und 9000 liegen")
		}
		c.MTU = n
	case "peer.publickey":
		k, err := wgtypes.ParseKey(val)
		if err != nil {
			return errors.New("PublicKey ist kein gültiger WireGuard-Schlüssel")
		}
		c.Peer.PublicKey = k
	case "peer.presharedkey":
		k, err := wgtypes.ParseKey(val)
		if err != nil {
			return errors.New("PresharedKey ist kein gültiger WireGuard-Schlüssel")
		}
		c.Peer.PresharedKey = &k
	case "peer.endpoint":
		host, port, err := net.SplitHostPort(val)
		if err != nil || host == "" {
			return errors.New("Endpoint erwartet host:port, z. B. vpn.example.org:51820")
		}
		if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
			return errors.New("Endpoint hat einen ungültigen Port")
		}
		c.Peer.Endpoint = val
	case "peer.allowedips":
		list, err := prefixes(val, false)
		if err != nil {
			return fmt.Errorf("AllowedIPs: %w", err)
		}
		c.Peer.AllowedIPs = append(c.Peer.AllowedIPs, list...)
	case "peer.persistentkeepalive":
		if strings.EqualFold(val, "off") {
			c.Peer.Keepalive = 0
			return nil
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 || n > 65535 {
			return errors.New("PersistentKeepalive muss eine Zahl in Sekunden sein")
		}
		c.Peer.Keepalive = n
	default:
		return fmt.Errorf("unbekannte Option %q", key)
	}
	return nil
}

// prefixes parses a comma-separated address list. host keeps the address bits (Address).
func prefixes(val string, host bool) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(val, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			a, err := netip.ParseAddr(part)
			if err != nil {
				return nil, fmt.Errorf("ungültige Adresse %q", part)
			}
			part = netip.PrefixFrom(a, a.BitLen()).String()
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("ungültiges Netz %q", part)
		}
		if !host {
			p = p.Masked()
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("leer")
	}
	return out, nil
}

// Keepalive returns the keepalive interval to use (the config's or the default).
func (c *Config) Keepalive() int {
	if c.Peer.Keepalive > 0 {
		return c.Peer.Keepalive
	}
	return DefaultKeepalive
}

// Covers reports whether the peer's AllowedIPs contain the whole prefix.
func (c *Config) Covers(p netip.Prefix) bool {
	for _, a := range c.Peer.AllowedIPs {
		if a.Addr().Is4() == p.Addr().Is4() && a.Bits() <= p.Bits() && a.Contains(p.Addr()) {
			return true
		}
	}
	return false
}

// Summary returns the public data and warnings for the subnets routed through the tunnel.
func (c *Config) Summary(subnets []netip.Prefix) Summary {
	s := Summary{
		PublicKey:     c.PrivateKey.PublicKey().String(),
		Endpoint:      c.Peer.Endpoint,
		PeerPublicKey: c.Peer.PublicKey.String(),
		Keepalive:     c.Keepalive(),
		MTU:           c.MTU,
		PresharedKey:  c.Peer.PresharedKey != nil,
		Warnings:      []string{},
	}
	for _, a := range c.Addresses {
		s.Addresses = append(s.Addresses, a.String())
	}
	full := false
	for _, a := range c.Peer.AllowedIPs {
		s.AllowedIPs = append(s.AllowedIPs, a.String())
		if a.Bits() == 0 {
			full = true
		}
	}
	if full {
		s.Warnings = append(s.Warnings, "AllowedIPs leitet alles durch den Tunnel (0.0.0.0/0) – NetScope schickt nur die zugeordneten Subnetze hindurch, der übrige Verkehr des Servers bleibt unverändert.")
	}
	for _, p := range subnets {
		if !c.Covers(p) {
			s.Warnings = append(s.Warnings, fmt.Sprintf("%s ist nicht in AllowedIPs enthalten – die Gegenstelle leitet dieses Netz eventuell nicht weiter.", p))
		}
	}
	if c.Peer.Keepalive == 0 {
		s.Warnings = append(s.Warnings, fmt.Sprintf("Kein PersistentKeepalive angegeben – NetScope verwendet %d s, damit der Tunnel offen bleibt.", DefaultKeepalive))
	}
	s.Warnings = append(s.Warnings, c.Ignored...)
	return s
}
