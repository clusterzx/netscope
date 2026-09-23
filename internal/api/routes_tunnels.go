package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"netscope/internal/inventory"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/tunnel"
	"netscope/internal/wgconf"
)

// subnetView is a subnet with the state of its tunnel.
type subnetView struct {
	inventory.Subnet
	Tunnel *tunnel.Status `json:"tunnel,omitempty"`
}

// tunnelOverview is the tunnel state of the host.
type tunnelOverview struct {
	tunnel.Availability
	Tunnels []tunnel.Status `json:"tunnels"`
}

// tunnelInput is a configuration to inspect or test: pasted text, or a stored WireGuard
// credential (config empty or masked).
type tunnelInput struct {
	Config       string   `json:"config,omitempty"`
	CredentialID int64    `json:"credentialId,omitempty"`
	Subnets      []string `json:"subnets,omitempty"` // CIDRs routed through the tunnel (for warnings)
}

func (s *Server) registerTunnels() {
	s.add(&route{Method: "GET", Path: "/api/v1/tunnels", Tag: "Subnetze", Summary: "WireGuard-Tunnel und ihr Zustand", Scope: scopeRead,
		Resp: tunnelOverview{}, handler: s.handleTunnels})
	s.add(&route{Method: "POST", Path: "/api/v1/tunnels/inspect", Tag: "Subnetze",
		Summary: "WireGuard-Konfiguration prüfen (liefert nur öffentliche Angaben und Hinweise)", Scope: scopeWrite,
		Body: tunnelInput{}, Resp: wgconf.Summary{}, handler: s.handleInspectTunnel})
	s.add(&route{Method: "POST", Path: "/api/v1/tunnels/test", Tag: "Subnetze",
		Summary: "Verbindungstest: Handshake mit dem WireGuard-Server (bis 10 s)", Scope: scopeWrite,
		Body: tunnelInput{}, Resp: tunnel.TestResult{}, handler: s.handleTestTunnel})
}

func (s *Server) handleTunnels(w http.ResponseWriter, r *http.Request) {
	out := tunnelOverview{Availability: tunnel.Availability{Reason: "Tunnel-Verwaltung nicht gestartet"}, Tunnels: []tunnel.Status{}}
	if s.Tunnels != nil {
		out.Availability, out.Tunnels = s.Tunnels.Availability(), s.Tunnels.Statuses()
	}
	writeJSON(w, http.StatusOK, out)
}

// tunnelConfig resolves the configuration of a request.
func (s *Server) tunnelConfig(ctx context.Context, in tunnelInput) (*wgconf.Config, error) {
	text := in.Config
	if (text == "" || text == plugin.SecretMask) && in.CredentialID > 0 {
		c, err := s.Vault.Get(ctx, in.CredentialID)
		if err != nil {
			return nil, err
		}
		if c.Type != plugin.CredWireGuard {
			return nil, plugin.FieldErr("credentialId", fmt.Sprintf("Credential „%s“ ist keine WireGuard-Konfiguration", c.Name))
		}
		text = c.Get("config")
	}
	if strings.TrimSpace(text) == "" {
		return nil, plugin.FieldErr("config", "Konfiguration fehlt")
	}
	cfg, err := wgconf.Parse(text)
	if err != nil {
		return nil, plugin.FieldErr("config", err.Error())
	}
	return cfg, nil
}

func (s *Server) handleInspectTunnel(w http.ResponseWriter, r *http.Request) {
	var in tunnelInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	cfg, err := s.tunnelConfig(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var subnets []netip.Prefix
	for _, c := range in.Subnets {
		if p, err := netip.ParsePrefix(strings.TrimSpace(c)); err == nil {
			subnets = append(subnets, p.Masked())
		}
	}
	writeJSON(w, http.StatusOK, cfg.Summary(subnets))
}

func (s *Server) handleTestTunnel(w http.ResponseWriter, r *http.Request) {
	var in tunnelInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	cfg, err := s.tunnelConfig(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.Tunnels == nil {
		s.fail(w, r, errors.New("Tunnel-Verwaltung nicht gestartet"))
		return
	}
	writeJSON(w, http.StatusOK, s.Tunnels.Test(r.Context(), cfg))
}

// subnetViews adds the tunnel states to subnets.
func (s *Server) subnetViews(list []inventory.Subnet) []subnetView {
	out := make([]subnetView, 0, len(list))
	for _, sn := range list {
		v := subnetView{Subnet: sn}
		if s.Tunnels != nil && sn.Access == inventory.AccessWireGuard && sn.TunnelCredentialID != nil && sn.Enabled {
			if st, ok := s.Tunnels.Status(*sn.TunnelCredentialID); ok {
				v.Tunnel = &st
			}
		}
		out = append(out, v)
	}
	return out
}

// checkTunnelSubnet validates the tunnel of a subnet: a WireGuard credential and no
// overlap with a directly attached network.
func (s *Server) checkTunnelSubnet(ctx context.Context, sn *inventory.Subnet) error {
	if sn.Access != inventory.AccessWireGuard {
		return nil
	}
	if sn.TunnelCredentialID != nil {
		if err := s.Vault.Check(ctx, *sn.TunnelCredentialID, []string{plugin.CredWireGuard}); err != nil {
			return plugin.FieldErr("tunnelCredentialId", err.Error())
		}
	}
	p, err := netip.ParsePrefix(strings.TrimSpace(sn.CIDR))
	if err != nil {
		return nil // reported by the subnet validation
	}
	if local, err := netutil.LocalSubnets(); err == nil {
		for _, l := range local {
			if l.Prefix.Overlaps(p.Masked()) {
				return plugin.FieldErr("cidr", fmt.Sprintf("überschneidet sich mit dem lokal angeschlossenen Netz %s (%s) – ein Tunnel ist nur für entfernte Netze möglich", l.Prefix, l.Interface))
			}
		}
	}
	return nil
}

// reconcileTunnels applies subnet or credential changes to the tunnels.
func (s *Server) reconcileTunnels() {
	if s.Tunnels != nil {
		s.Tunnels.Reconcile()
	}
}
