package pluginhost

import (
	"context"
	"testing"

	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/vault"
)

func TestCredentialScopes(t *testing.T) {
	ctx := context.Background()
	h, _, inv := newHost(t)
	pve, err := inv.Observe(ctx, "arpscan", 0, &plugin.Observation{MACs: []string{"aa:bb:cc:00:00:10"}, IP: "192.168.99.10", Present: true,
		Manual: &plugin.ManualData{Tags: []string{"Proxmox"}}})
	if err != nil {
		t.Fatal(err)
	}
	nas, err := inv.Observe(ctx, "arpscan", 0, &plugin.Observation{MACs: []string{"aa:bb:cc:00:00:20"}, IP: "192.168.99.20", Present: true})
	if err != nil {
		t.Fatal(err)
	}
	g := &inventory.Group{Name: "Admins", Kind: "manual"}
	if err := inv.SaveGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB.W.ExecContext(ctx, "INSERT INTO group_members(group_id, device_id) VALUES (?, ?)", g.ID, nas); err != nil {
		t.Fatal(err)
	}

	sshValues := map[string]any{"username": "root", "password": "x"}
	create := func(name, typ string, scope *plugin.Scope) int64 {
		t.Helper()
		if scope != nil {
			if err := h.ValidateScope(ctx, scope); err != nil {
				t.Fatal(err)
			}
		}
		values := sshValues
		if typ == plugin.CredAPIToken {
			values = map[string]any{"token_id": "a@pve!b", "token": "t"}
		}
		id, err := h.Vault.CreateCredential(ctx, vault.CredentialInput{Name: name, Type: typ, Values: values, Scope: scope})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	global := create("global", plugin.CredSSH, nil)
	subnet := create("subnet", plugin.CredSSH, &plugin.Scope{Subnets: []string{"192.168.99.0/24"}})
	tagged := create("tagged", plugin.CredSSH, &plugin.Scope{Tags: []string{"proxmox"}})
	device := create("device", plugin.CredSSH, &plugin.Scope{Devices: []int64{pve}})
	other := create("other", plugin.CredSSH, &plugin.Scope{Subnets: []string{"10.0.0.0/8"}})
	group := create("group", plugin.CredSSH, &plugin.Scope{Groups: []int64{g.ID}})
	narrowed := create("narrowed", plugin.CredSSH, &plugin.Scope{Tags: []string{"proxmox"}, Subnets: []string{"10.0.0.0/8"}})
	token := create("token", plugin.CredAPIToken, nil)

	p := h.CredentialProvider()
	ids := func(t plugin.CredentialTarget, types []string, allowed []int64) ([]int64, []plugin.CredentialMatch) {
		list, err := p.Applicable(ctx, t, types, allowed)
		if err != nil {
			panic(err)
		}
		out := make([]int64, len(list))
		for i, m := range list {
			out[i] = m.ID
		}
		return out, list
	}
	ssh := []string{plugin.CredSSH}
	check := func(name string, got, want []int64) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", name, got, want)
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: got %v, want %v", name, got, want)
				return
			}
		}
	}

	got, list := ids(plugin.CredentialTarget{DeviceID: pve}, ssh, nil)
	check("pve by device", got, []int64{device, tagged, subnet, global})
	if list[0].Rank != plugin.RankDevice || list[1].Reason != "Tag „proxmox“" || list[2].Reason != "Subnetz 192.168.99.0/24" || list[3].Rank != plugin.RankEverywhere {
		t.Errorf("matches: %+v", list)
	}
	got, list = ids(plugin.CredentialTarget{IP: "192.168.99.20"}, ssh, nil)
	check("nas by ip", got, []int64{group, subnet, global})
	if list[0].Reason != "Gruppe „Admins“" {
		t.Errorf("group reason: %+v", list[0])
	}
	got, _ = ids(plugin.CredentialTarget{IP: "10.1.2.3"}, ssh, nil)
	check("unknown host", got, []int64{other, global})
	got, _ = ids(plugin.CredentialTarget{DeviceID: pve}, ssh, []int64{global, subnet, other})
	check("allowed list", got, []int64{subnet, global})
	got, _ = ids(plugin.CredentialTarget{IP: "192.168.99.10"}, nil, nil)
	check("all types", got, []int64{device, tagged, subnet, global, token})
	_ = narrowed // tag matches, but the subnet restriction excludes the device

	// the scope is stored and returned with the credential
	m, err := h.Vault.Meta(ctx, tagged)
	if err != nil || len(m.Scope.Tags) != 1 || m.Scope.AllSubnets {
		t.Errorf("meta scope: %+v %v", m.Scope, err)
	}
	if m, _ := h.Vault.Meta(ctx, global); !m.Scope.AllSubnets {
		t.Errorf("default scope: %+v", m.Scope)
	}
	// an update without scope keeps it
	if err := h.Vault.UpdateCredential(ctx, tagged, vault.CredentialInput{Name: "tagged", Values: map[string]any{"username": "admin", "password": plugin.SecretMask}}); err != nil {
		t.Fatal(err)
	}
	if m, _ := h.Vault.Meta(ctx, tagged); len(m.Scope.Tags) != 1 || m.Public["username"] != "admin" {
		t.Errorf("after update: %+v", m)
	}
	// invalid scopes are rejected with the field
	if err := h.ValidateScope(ctx, &plugin.Scope{Subnets: []string{"kein-netz"}}); err == nil {
		t.Error("invalid subnet accepted")
	}
}

func TestCredentialPicker(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newHost(t)
	good, _ := h.Vault.CreateCredential(ctx, vault.CredentialInput{Name: "good", Type: plugin.CredSSH, Values: map[string]any{"username": "root", "password": "x"}})
	bad, _ := h.Vault.CreateCredential(ctx, vault.CredentialInput{Name: "bad", Type: plugin.CredSSH, Values: map[string]any{"username": "nobody", "password": "x"}})
	checks := 0
	pk := &plugin.CredentialPicker{Creds: h.CredentialProvider(), Types: []string{plugin.CredSSH},
		Check: func(c *plugin.Credential) error {
			checks++
			if c.Get("username") == "nobody" {
				return errNoUser
			}
			return nil
		}}
	for range 3 {
		list, err := pk.For(ctx, plugin.CredentialTarget{IP: "192.168.99.5"})
		if err != nil || len(list) != 1 || list[0].ID != good {
			t.Fatalf("list = %v, err = %v (bad = %d)", list, err, bad)
		}
	}
	if checks != 2 {
		t.Errorf("each credential must be checked once per run, got %d checks", checks)
	}
}

var errNoUser = &ConfigError{"username", "fehlt"}

// migrating renamed its single "url" setting to "urls".
type migrating struct{}

func (migrating) Info() plugin.Info {
	return plugin.Info{ID: "zz_migrate", Kind: plugin.KindImporter, Name: "Migrate", Description: "Test", Version: "2"}
}

func (migrating) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "urls", Type: plugin.FieldStringList, Label: "URLs"},
		{Key: "verify_tls", Type: plugin.FieldBool, Label: "TLS"},
	}}
}

func (migrating) Run(context.Context, *plugin.RunContext) error { return nil }

func (migrating) MigrateSettings(stored map[string]any) map[string]any {
	if u, ok := stored["url"].(string); ok {
		if _, has := stored["urls"]; !has {
			stored["urls"] = []any{u}
		}
		delete(stored, "url")
	}
	return stored
}

func init() { plugin.Register(migrating{}) }

func TestSettingsMigration(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newHost(t)
	if _, err := h.DB.W.ExecContext(ctx, `UPDATE plugin_configs SET settings = ? WHERE plugin_id = 'zz_migrate'`,
		`{"url":"https://pve.lan:8006","verify_tls":true}`); err != nil {
		t.Fatal(err)
	}
	p, _ := h.Plugin("zz_migrate")
	cfg, err := h.loadConfig(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	s := plugin.NewSettings(cfg.Settings)
	if urls := s.StringList("urls"); len(urls) != 1 || urls[0] != "https://pve.lan:8006" || !s.Bool("verify_tls") {
		t.Errorf("settings = %v", cfg.Settings)
	}
	if _, ok := cfg.Settings["url"]; ok {
		t.Error("old key must be dropped")
	}
}
