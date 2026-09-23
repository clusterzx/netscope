// Package wol implements the "wol" plugin: a Wake-on-LAN action for devices. It is not
// a scanner in the narrow sense and has no scheduled runs.
package wol

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin sends Wake-on-LAN magic packets.
type Plugin struct {
	// send overrides the UDP transmission (tests).
	send func(ctx context.Context, packet []byte, dst netip.AddrPort) error
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "wol",
		Kind:               plugin.KindScanner,
		Name:               "Wake-on-LAN",
		Description:        "Weckt Geräte per Wake-on-LAN (Magic Packet an die Broadcast-Adressen ihres Netzes).",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultTimeout:     time.Minute,
		DefaultConcurrency: 1,
		Targets:            plugin.TargetDevices,
		Presence:           false,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "port", Type: plugin.FieldInt, Label: "UDP-Port", Default: 9,
			Description: "Zielport des Magic Packets (üblich: 9 oder 7).",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(65535)}},
		{Key: "repeat", Type: plugin.FieldInt, Label: "Wiederholungen", Default: 3,
			Description: "Wie oft das Paket an jede Zieladresse gesendet wird.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(10)}},
		{Key: "extra_broadcasts", Type: plugin.FieldStringList, Label: "Zusätzliche Broadcast-Adressen", Advanced: true,
			Description: "Weitere Zieladressen (eine pro Zeile), z. B. die Broadcast-Adresse eines gerouteten Netzes mit Directed-Broadcast-Weiterleitung.",
			Validation:  &plugin.Validation{Format: "ip"}},
		{Key: "secure_on", Type: plugin.FieldSecret, Label: "SecureOn-Passwort", Advanced: true,
			Description: "Optionales 6-Byte-Passwort (12 Hex-Zeichen, z. B. 01:23:45:67:89:ab), das manche Netzwerkkarten zusätzlich verlangen.",
			Validation:  &plugin.Validation{Pattern: `^([0-9A-Fa-f]{2}[:-]?){5}[0-9A-Fa-f]{2}$`}},
	}}
}

// Actions implements plugin.ActionProvider.
func (p *Plugin) Actions() []plugin.Action {
	return []plugin.Action{{
		Name:        "wake",
		Label:       "Wake-on-LAN senden",
		Description: "Sendet ein Magic Packet an alle MAC-Adressen der ausgewählten Geräte.",
		Scope:       plugin.ActionDevice,
	}}
}

// parseSecureOn parses the optional SecureOn password (nil if empty).
func parseSecureOn(s string) ([]byte, error) {
	s = strings.NewReplacer(":", "", "-", "", " ", "").Replace(strings.TrimSpace(s))
	if s == "" {
		return nil, nil
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 6 {
		return nil, errors.New("SecureOn-Passwort muss aus 6 Byte (12 Hex-Zeichen) bestehen")
	}
	return b, nil
}

// magicPacket builds 6×0xFF followed by 16 repetitions of the MAC and the optional
// SecureOn password.
func magicPacket(mac net.HardwareAddr, secureOn []byte) ([]byte, error) {
	if len(mac) != 6 {
		return nil, fmt.Errorf("ungültige MAC-Adresse %s", mac)
	}
	pkt := make([]byte, 0, 6+16*6+len(secureOn))
	for range 6 {
		pkt = append(pkt, 0xFF)
	}
	for range 16 {
		pkt = append(pkt, mac...)
	}
	return append(pkt, secureOn...), nil
}

// broadcastTargets returns the destination addresses for a device: the directed
// broadcast of every configured subnet that contains one of its addresses, the limited
// broadcast and the configured extra addresses (deduplicated, in this order).
func broadcastTargets(deviceIPs []string, subnets []plugin.SubnetTarget, extra []string) []netip.Addr {
	var out []netip.Addr
	seen := map[netip.Addr]bool{}
	add := func(a netip.Addr) {
		if a.IsValid() && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	var directed []netip.Addr
	for _, raw := range deviceIPs {
		ip, err := netip.ParseAddr(raw)
		if err != nil || !ip.Unmap().Is4() {
			continue
		}
		ip = ip.Unmap()
		for _, s := range subnets {
			if s.CIDR.Contains(ip) && s.CIDR.Bits() < 31 {
				if b, ok := netutil.Broadcast(s.CIDR); ok {
					directed = append(directed, b)
				}
			}
		}
	}
	sort.Slice(directed, func(i, j int) bool { return directed[i].Less(directed[j]) })
	for _, b := range directed {
		add(b)
	}
	add(netip.AddrFrom4([4]byte{255, 255, 255, 255}))
	for _, raw := range extra {
		if a, err := netip.ParseAddr(strings.TrimSpace(raw)); err == nil && a.Unmap().Is4() {
			add(a.Unmap())
		}
	}
	return out
}

// deviceMACs returns the unicast MACs of a device, primary first.
func deviceMACs(d plugin.DeviceInfo) []net.HardwareAddr {
	var out []net.HardwareAddr
	seen := map[string]bool{}
	for _, raw := range append([]string{d.PrimaryMAC}, d.MACs...) {
		mac, ok := netutil.NormalizeMAC(raw)
		if !ok || seen[mac] || netutil.IsMulticastMAC(mac) {
			continue
		}
		seen[mac] = true
		hw, err := net.ParseMAC(mac)
		if err == nil {
			out = append(out, hw)
		}
	}
	return out
}

// sendUDP transmits one datagram (UDP sockets have SO_BROADCAST enabled by Go).
func sendUDP(ctx context.Context, packet []byte, dst netip.AddrPort) error {
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp4", dst.String())
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = c.Write(packet)
	return err
}

// RunAction implements plugin.ActionProvider.
func (p *Plugin) RunAction(ctx context.Context, rc *plugin.RunContext, name string, params map[string]any) (*plugin.ActionResult, error) {
	if name != "wake" {
		return nil, fmt.Errorf("unbekannte Aktion %q", name)
	}
	devices := rc.Targets.Devices
	if len(devices) == 0 {
		return nil, errors.New("kein Gerät ausgewählt")
	}
	secureOn, err := parseSecureOn(rc.Settings.String("secure_on"))
	if err != nil {
		return nil, err
	}
	port := rc.Settings.Int("port")
	if port < 1 || port > 65535 {
		port = 9
	}
	repeat := max(rc.Settings.Int("repeat"), 1)
	subnets := rc.Targets.Subnets
	if rc.Inventory != nil {
		if s, err := rc.Inventory.Subnets(ctx); err == nil && len(s) > 0 {
			subnets = s
		} else if err != nil {
			rc.Log.Warn("Subnetze konnten nicht gelesen werden", "error", err)
		}
	}
	send := p.send
	if send == nil {
		send = sendUDP
	}
	var (
		woken, packets int
		skipped        []string
		failures       []string
		targetsUsed    = map[string]bool{}
	)
	for _, d := range devices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		macs := deviceMACs(d)
		label := deviceLabel(d)
		if len(macs) == 0 {
			skipped = append(skipped, label)
			rc.Log.Warn("Gerät hat keine MAC-Adresse – Wake-on-LAN nicht möglich", "geraet", label)
			continue
		}
		dsts := broadcastTargets(append([]string{d.PrimaryIP}, d.IPs...), subnets, rc.Settings.StringList("extra_broadcasts"))
		sent := 0
		for _, mac := range macs {
			pkt, err := magicPacket(mac, secureOn)
			if err != nil {
				failures = append(failures, err.Error())
				continue
			}
			for _, dst := range dsts {
				ap := netip.AddrPortFrom(dst, uint16(port))
				for range repeat {
					if err := send(ctx, pkt, ap); err != nil {
						failures = append(failures, fmt.Sprintf("%s → %s: %v", mac, ap, err))
						rc.Log.Warn("Magic Packet konnte nicht gesendet werden", "mac", mac.String(), "ziel", ap.String(), "error", err)
						break
					}
					sent++
				}
				targetsUsed[ap.String()] = true
			}
			rc.Log.Info("Magic Packet gesendet", "geraet", label, "mac", mac.String())
		}
		if sent > 0 {
			woken++
			packets += sent
		}
	}
	if woken == 0 {
		msg := "Kein Magic Packet gesendet"
		if len(skipped) > 0 {
			msg += " – ohne MAC-Adresse: " + strings.Join(skipped, ", ")
		}
		if len(failures) > 0 {
			msg += " – " + failures[0]
		}
		return nil, errors.New(msg)
	}
	dstList := make([]string, 0, len(targetsUsed))
	for t := range targetsUsed {
		dstList = append(dstList, t)
	}
	sort.Strings(dstList)
	msg := fmt.Sprintf("Wake-on-LAN an %d %s gesendet (%d Pakete an %s)", woken, plural(woken, "Gerät", "Geräte"),
		packets, strings.Join(dstList, ", "))
	if len(skipped) > 0 {
		msg += "; ohne MAC-Adresse übersprungen: " + strings.Join(skipped, ", ")
	}
	return &plugin.ActionResult{Message: msg, Data: map[string]any{
		"devices": woken, "packets": packets, "targets": dstList, "skipped": skipped, "errors": failures,
	}}, nil
}

func deviceLabel(d plugin.DeviceInfo) string {
	switch {
	case d.Name != "":
		return d.Name
	case d.PrimaryIP != "":
		return d.PrimaryIP
	}
	return "Gerät " + strconv.FormatInt(d.ID, 10)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
