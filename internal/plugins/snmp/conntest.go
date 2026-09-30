package snmp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

// TestConnection implements plugin.ConnectionTester: it asks the test address
// (rc.Params["target"]) for its system group with the applicable credentials.
func (p *Plugin) TestConnection(ctx context.Context, rc *plugin.RunContext) ([]plugin.ConnectionResult, error) {
	return testConnection(ctx, rc)
}

// TestConnection implements plugin.ConnectionTester (same test as the SNMP plugin).
func (t *Traffic) TestConnection(ctx context.Context, rc *plugin.RunContext) ([]plugin.ConnectionResult, error) {
	return testConnection(ctx, rc)
}

func testConnection(ctx context.Context, rc *plugin.RunContext) ([]plugin.ConnectionResult, error) {
	host, port, err := plugin.TestTarget(rc)
	if err != nil {
		return nil, err
	}
	cfg := loadConfig(rc.Settings, rc.DataDir)
	if port > 0 {
		cfg.client.port = port
	}
	start := time.Now()
	res := plugin.ConnectionResult{Target: net.JoinHostPort(host, strconv.Itoa(cfg.client.port))}
	msg, err := testAgent(ctx, rc, cfg, host)
	if err != nil {
		res.Message = err.Error()
	} else {
		res.OK, res.Message = true, msg
	}
	res.DurationMs = time.Since(start).Milliseconds()
	return []plugin.ConnectionResult{res}, nil
}

func testAgent(ctx context.Context, rc *plugin.RunContext, cfg config, host string) (string, error) {
	ip, err := netutil.ResolveHost(ctx, host)
	if err != nil {
		return "", err
	}
	picker := &plugin.CredentialPicker{Creds: rc.Creds, Types: []string{plugin.CredSNMPv2c, plugin.CredSNMPv3}, Allowed: cfg.credIDs, Log: rc.Log,
		Check: func(c *plugin.Credential) error { _, err := newClient(ctx, "127.0.0.1", c, cfg.client); return err }}
	creds, err := picker.For(ctx, plugin.CredentialTarget{IP: ip})
	if err != nil {
		return "", err
	}
	if len(creds) == 0 {
		return "", fmt.Errorf("keine passenden Zugangsdaten für %s (Auswahl oder Geltungsbereich der Credentials prüfen): %w", host, plugin.ErrNoCredential)
	}
	var errs []string
	for _, c := range creds {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		g, err := newClient(ctx, ip, c, cfg.client)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.Name, err))
			continue
		}
		pdus, err := querySystem(g)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.Name, err))
			continue
		}
		g.Close()
		sys := decodeSystem(pdus)
		name := sys.Name
		if name == "" {
			name = firstLine(sys.Descr)
		}
		if name == "" {
			name = ip
		}
		return fmt.Sprintf("Antwort von %s (SNMP %s, %s)", name, versionName(g), c.Name), nil
	}
	return "", fmt.Errorf("keine SNMP-Antwort – Adresse, Community bzw. Benutzer prüfen: %w", errors.New(strings.Join(errs, "; ")))
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}
