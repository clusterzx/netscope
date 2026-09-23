// Package netbios implements the "netbios" scanner: NetBIOS node status (NBSTAT)
// queries that reveal the computer name, workgroup/domain and adapter MAC of Windows
// and Samba hosts.
package netbios

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"time"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the NetBIOS name scanner.
type Plugin struct {
	// port overrides the NetBIOS name service port 137 (tests).
	port uint16
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "netbios",
		Kind:               plugin.KindScanner,
		Name:               "NetBIOS",
		Description:        "Fragt die NetBIOS-Namenstabelle (NBSTAT) ab: Rechnername, Arbeitsgruppe/Domäne und MAC-Adresse von Windows- und Samba-Geräten.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "20 * * * *",
		DefaultTimeout:     10 * time.Minute,
		DefaultConcurrency: 32,
		DefaultRetries:     0,
		Targets:            plugin.TargetDevices,
		Presence:           false,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout pro Gerät", Default: "1500ms",
			Description: "Wartezeit auf die NBSTAT-Antwort; nach der Hälfte wird die Anfrage einmal wiederholt.",
			Validation:  &plugin.Validation{Max: plugin.Int64(30)}},
	}}
}

// query sends a node status request to dst and waits for the matching response.
// A nil status without error means the host did not answer.
func query(ctx context.Context, dst netip.AddrPort, timeout time.Duration) (*Status, []byte, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp4", dst.String())
	if err != nil {
		return nil, nil, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.SetDeadline(time.Now()) })
	defer stop()
	id := uint16(rand.IntN(0xFFFF) + 1)
	req := request(id)
	buf := make([]byte, 2048)
	end := time.Now().Add(timeout)
	for attempt := range 2 {
		if _, err := c.Write(req); err != nil {
			return nil, nil, err
		}
		deadline := end
		if attempt == 0 {
			deadline = time.Now().Add(timeout / 2)
		}
		_ = c.SetReadDeadline(deadline)
		for {
			n, err := c.Read(buf)
			if err != nil {
				if ctx.Err() != nil {
					return nil, nil, ctx.Err()
				}
				if errors.Is(err, os.ErrDeadlineExceeded) {
					break
				}
				return nil, nil, err // e.g. ICMP port unreachable: no NetBIOS
			}
			st, perr := parseStatus(buf[:n])
			if perr != nil || st.ID != id {
				continue
			}
			return st, append([]byte(nil), buf[:n]...), nil
		}
	}
	return nil, nil, nil
}

// observation converts a node status into an observation of the device.
func observation(deviceID int64, ip string, st *Status) *plugin.Observation {
	obs := &plugin.Observation{
		DeviceID: deviceID,
		IP:       ip,
		Present:  true,
		Hostname: st.Hostname(),
		Raw:      st.Table(),
	}
	attrs := map[string]string{}
	if d := st.Domain(); d != "" {
		attrs["netbios.domain"] = d
	}
	if st.MAC != "" {
		attrs["netbios.mac"] = st.MAC
	}
	if len(attrs) > 0 {
		obs.Attrs = attrs
	}
	obs.Inventory = map[string]any{
		"hostname": st.Hostname(),
		"domain":   st.Domain(),
		"mac":      st.MAC,
		"names":    st.Names,
	}
	return obs
}

// deviceIP returns the IPv4 address to query (primary preferred).
func deviceIP(d plugin.DeviceInfo) (netip.Addr, bool) {
	for _, raw := range append([]string{d.PrimaryIP}, d.IPs...) {
		if a, err := netip.ParseAddr(raw); err == nil && a.Unmap().Is4() {
			return a.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	timeout := rc.Settings.Duration("timeout")
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	port := p.port
	if port == 0 {
		port = 137
	}
	devices := rc.Targets.Devices
	var done atomic.Int64
	rc.Progress(0, len(devices))
	return plugin.ForEach(ctx, rc.Parallelism(), devices, func(ctx context.Context, d plugin.DeviceInfo) error {
		defer func() { rc.Progress(int(done.Add(1)), len(devices)) }()
		ip, ok := deviceIP(d)
		if !ok {
			return nil
		}
		st, _, err := query(ctx, netip.AddrPortFrom(ip, port), timeout)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			rc.Log.Debug("Keine NetBIOS-Antwort", "ip", ip.String(), "error", err)
			return nil
		}
		if st == nil {
			rc.Log.Debug("Keine NetBIOS-Antwort", "ip", ip.String())
			return nil
		}
		obs := observation(d.ID, ip.String(), st)
		if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			if ctx.Err() == nil {
				rc.Log.Warn("Ergebnis konnte nicht gespeichert werden", "ip", ip.String(), "error", err)
			}
			return nil
		}
		rc.AddStat("hosts", 1)
		return nil
	})
}
