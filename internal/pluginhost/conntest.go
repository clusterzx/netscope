package pluginhost

import (
	"context"
	"errors"
	"fmt"
	"time"

	"netscope/internal/plugin"
)

// connectionTestTimeout bounds a connection test (all systems of the plugin together).
const connectionTestTimeout = 60 * time.Second

// ErrNoConnectionTest is returned for plugins without a connection test.
var ErrNoConnectionTest = errors.New("Dieses Plugin hat keinen Verbindungstest")

// TestConnection checks the connection of a plugin to the systems it reads, without
// storing anything. settings are unsaved settings from a form (nil = the stored ones;
// secret fields with plugin.SecretMask keep the stored value); target is the address for
// plugins that work on devices (SSH, SNMP). It runs outside the run queue, so it works
// before the setup is finished and while the plugin is disabled.
func (h *Host) TestConnection(ctx context.Context, id string, settings map[string]any, target string) ([]plugin.ConnectionResult, error) {
	p, ok := h.Plugin(id)
	if !ok {
		return nil, fmt.Errorf("Plugin %q existiert nicht", id)
	}
	tester, ok := p.(plugin.ConnectionTester)
	if !ok {
		return nil, ErrNoConnectionTest
	}
	cfg, _ := h.Config(id)
	vals := cfg.Settings
	if settings != nil {
		var err error
		if vals, err = h.ValidateSettings(ctx, id, settings); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, connectionTestTimeout)
	defer cancel()
	rc := &plugin.RunContext{PluginID: id, Trigger: "test", Settings: plugin.NewSettings(vals), Scope: cfg.Scope,
		Params: map[string]any{plugin.TargetParam: target}, Concurrency: cfg.Concurrency, Log: h.Log.With("plugin", id, "test", true),
		Sink: discard{}, Events: discard{}, Creds: h.CredentialProvider(), Inventory: h.Inventory, DataDir: h.dataDir(id), Env: h.Env()}
	var res []plugin.ConnectionResult
	err := safeCall(func() error {
		var err error
		res, err = tester.TestConnection(ctx, rc)
		return err
	})
	if err != nil && len(res) == 0 {
		return nil, err
	}
	if res == nil {
		res = []plugin.ConnectionResult{}
	}
	return res, nil
}

// discard is the sink and event emitter of a connection test: nothing is stored.
type discard struct{}

func (discard) Observe(context.Context, *plugin.Observation) (int64, error) { return 0, nil }
func (discard) Emit(context.Context, plugin.Event) (int64, error)           { return 0, nil }

// ValidateSettings validates plugin settings like a configuration change would, without
// storing them, and returns the normalized values.
func (h *Host) ValidateSettings(ctx context.Context, id string, settings map[string]any) (map[string]any, error) {
	p, ok := h.Plugin(id)
	if !ok {
		return nil, fmt.Errorf("Plugin %q existiert nicht", id)
	}
	cfg, _ := h.Config(id)
	vals, err := p.Schema().Validate(settings, cfg.Settings, func(cid int64, types []string) error {
		return h.Vault.Check(ctx, cid, types)
	})
	if err != nil {
		return nil, err
	}
	if v, ok := p.(plugin.SettingsValidator); ok {
		if err := v.ValidateSettings(plugin.NewSettings(vals)); err != nil {
			return nil, err
		}
	}
	return vals, nil
}
