package federation

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/federation/wire"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/settings"
	"netscope/internal/vault"
)

// instance is a NetScope instance without plugins.
type instance struct {
	db  *db.DB
	inv *inventory.Store
	ev  *events.Store
	fed *Service
}

func newInstance(t *testing.T, name string) *instance {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), name+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	key, _ := vault.NewKey()
	v, err := vault.Open(ctx, d, key, vault.KeySource{})
	if err != nil {
		t.Fatal(err)
	}
	st, err := settings.Load(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := bus.New()
	inv, err := inventory.New(ctx, d, b, st, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := inv.SaveSubnet(ctx, &inventory.Subnet{CIDR: "192.168.1.0/24", Name: "lan", Interface: "eth0", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	ev := events.New(d, b, inv)
	fed, err := New(ctx, Deps{DB: d, Bus: b, Log: log, Vault: v, Inventory: inv, Events: ev, Settings: st, Version: "test", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fed.ctx = ctx
	return &instance{db: d, inv: inv, ev: ev, fed: fed}
}

// ingestHandler is the central endpoint (as in the API, without the rest of it).
func ingestHandler(c *Service, down *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down != nil && down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		id, name, err := c.AuthenticateSite(r.Context(), tok)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var b wire.Batch
		if err := json.NewDecoder(zr).Decode(&b); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := c.Ingest(r.Context(), id, name, &b, "127.0.0.1")
		var pe *ProtocolError
		if errors.As(err, &pe) {
			http.Error(w, pe.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// pair makes central a central instance with a site "Colo" and connects site to it.
func pair(t *testing.T, central, site *instance, down *atomic.Bool) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := central.fed.Update(ctx, SettingsInput{Settings: Settings{Role: RoleCentral, LocalName: "Zuhause"}}); err != nil {
		t.Fatal(err)
	}
	st, token, err := central.fed.CreateSite(ctx, SiteInput{Name: "Colo"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Slug != "colo" || !strings.HasPrefix(token, wire.TokenPrefix) {
		t.Fatalf("site %+v token %q", st, token)
	}
	srv := httptest.NewServer(ingestHandler(central.fed, down))
	t.Cleanup(srv.Close)
	if _, err := site.fed.Update(ctx, SettingsInput{Settings: Settings{Role: RoleSite, CentralURL: srv.URL}, Token: &token}); err != nil {
		t.Fatal(err)
	}
	return st.ID
}

// drain delivers everything the site has queued.
func drain(t *testing.T, site *instance) {
	t.Helper()
	site.fed.deliver(context.Background())
	if n, _ := site.fed.buffered(context.Background()); n != 0 {
		t.Fatalf("outbox not drained: %d items (%s)", n, site.fed.SiteStatus(context.Background()).LastError)
	}
}

func observe(t *testing.T, in *instance, pluginID string, o *plugin.Observation) int64 {
	t.Helper()
	id, err := in.inv.Observe(context.Background(), pluginID, 0, o)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type devRow struct {
	id     int64
	site   int64
	online bool
	ips    string
	macs   string
	host   string
}

func devices(t *testing.T, in *instance) []devRow {
	t.Helper()
	rows, err := in.db.R.Query(`SELECT d.id, IFNULL(d.site_id, 0), d.online, d.hostname,
		IFNULL((SELECT group_concat(ip) FROM (SELECT ip FROM device_ips WHERE device_id = d.id AND gone_at IS NULL ORDER BY ip)), ''),
		IFNULL((SELECT group_concat(mac) FROM (SELECT mac FROM device_macs WHERE device_id = d.id ORDER BY mac)), '')
		FROM devices d ORDER BY d.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []devRow
	for rows.Next() {
		var r devRow
		if err := rows.Scan(&r.id, &r.site, &r.online, &r.host, &r.ips, &r.macs); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func find(list []devRow, pred func(devRow) bool) *devRow {
	for i := range list {
		if pred(list[i]) {
			return &list[i]
		}
	}
	return nil
}

func TestSiteDeliversToCentral(t *testing.T) {
	ctx := context.Background()
	central, site := newInstance(t, "central"), newInstance(t, "site")

	// the central instance knows its own 192.168.1.10 and a device with MAC …:0a
	observe(t, central, "arpscan", &plugin.Observation{IP: "192.168.1.10", MACs: []string{"02:00:00:00:00:01"}, Present: true})
	observe(t, central, "arpscan", &plugin.Observation{IP: "192.168.1.20", MACs: []string{"02:00:00:00:00:0a"}, Present: true})
	// the site knows devices before it is paired: the first delivery synchronises them
	old := observe(t, site, "arpscan", &plugin.Observation{IP: "192.168.1.30", MACs: []string{"02:00:00:00:00:30"}, Present: true,
		Hostname: "nas"})
	observe(t, site, "nmap", &plugin.Observation{DeviceID: old, IP: "192.168.1.30", Ports: &plugin.PortScan{Protocol: "tcp",
		Ports: []plugin.Port{{Port: 22, Service: "ssh"}, {Port: 443, Service: "https"}}}})

	siteID := pair(t, central, site, nil)
	// the same address at the site is another device; the same MAC is the same device
	observe(t, site, "arpscan", &plugin.Observation{IP: "192.168.1.10", MACs: []string{"02:00:00:00:00:02"}, Present: true, Hostname: "colo-fw"})
	observe(t, site, "arpscan", &plugin.Observation{IP: "10.9.0.5", MACs: []string{"02:00:00:00:00:0a"}, Present: true})
	drain(t, site)

	list := devices(t, central)
	if len(list) != 4 {
		t.Fatalf("central devices: %+v", list)
	}
	home := find(list, func(d devRow) bool { return d.macs == "02:00:00:00:00:01" })
	colo := find(list, func(d devRow) bool { return d.macs == "02:00:00:00:00:02" })
	if home == nil || colo == nil || home.site != 0 || colo.site != siteID || colo.ips != "192.168.1.10" || colo.host != "colo-fw" {
		t.Fatalf("same IP at two sites must be two devices: %+v", list)
	}
	moved := find(list, func(d devRow) bool { return d.macs == "02:00:00:00:00:0a" })
	if moved == nil || moved.site != 0 || !strings.Contains(moved.ips, "10.9.0.5") {
		t.Fatalf("same MAC must be one device: %+v", list)
	}
	nas := find(list, func(d devRow) bool { return d.macs == "02:00:00:00:00:30" })
	if nas == nil || nas.site != siteID || nas.host != "nas" || !nas.online {
		t.Fatalf("device known before pairing not synchronised: %+v", list)
	}
	ports, err := central.inv.Ports(ctx, nas.id, false)
	if err != nil || len(ports) != 2 {
		t.Fatalf("ports of synchronised device: %v %v", ports, err)
	}
	// the site's address space does not leak into the central subnets or scan targets
	targets, err := central.inv.ResolveTargets(ctx, plugin.Scope{AllSubnets: true}, plugin.TargetSubnets)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range targets.Devices {
		if d.ID == colo.id || d.ID == nas.id {
			t.Fatalf("site device %d is a scan target of the central instance", d.ID)
		}
	}
	// filter language
	ids, err := central.inv.MatchingIDs(ctx, "site:colo")
	if err != nil || len(ids) != 2 {
		t.Fatalf("site:colo = %v %v", ids, err)
	}
	ids, err = central.inv.MatchingIDs(ctx, "site:local")
	if err != nil || len(ids) != 2 {
		t.Fatalf("site:local = %v %v", ids, err)
	}

	// events: the site's events come over, the central instance raises none of its own for site devices
	if _, err := site.ev.Emit(ctx, "diff", plugin.Event{Type: plugin.EvPortOpened, DeviceID: old, Title: "Port 8080 neu",
		Payload: map[string]any{"port": 8080}}); err != nil {
		t.Fatal(err)
	}
	if n, err := central.ev.Emit(ctx, "diff", plugin.Event{Type: plugin.EvPortOpened, DeviceID: nas.id, Title: "doppelt"}); err != nil || n != 0 {
		t.Fatalf("central event for a site device: id %d err %v", n, err)
	}
	drain(t, site)
	drain(t, site) // a second delivery must not repeat anything
	evs, total, err := central.ev.List(ctx, events.Filter{Types: []string{plugin.EvPortOpened}})
	if err != nil || total != 1 {
		t.Fatalf("central events: %d %v", total, err)
	}
	if e := evs[0]; e.SiteID != siteID || e.Site != "Colo" || e.DeviceID != nas.id || e.Payload["site"] != "Colo" {
		t.Fatalf("imported event %+v", e)
	}
	colSite := siteID
	if list, _, _ := central.ev.List(ctx, events.Filter{Site: &colSite}); len(list) != 1 {
		t.Fatalf("event filter by site: %d", len(list))
	}

	// presence: offline at the site is offline at the central instance
	if err := site.inv.RunFinished(ctx, plugin.RunSummary{RunID: 99, PluginID: "arpscan", Status: "success", Presence: true,
		Targets: plugin.Targets{Subnets: siteSubnets(t, site)}}); err != nil {
		t.Fatal(err)
	}
	if err := site.inv.RunFinished(ctx, plugin.RunSummary{RunID: 100, PluginID: "arpscan", Status: "success", Presence: true,
		Targets: plugin.Targets{Subnets: siteSubnets(t, site)}}); err != nil {
		t.Fatal(err)
	}
	drain(t, site)
	list = devices(t, central)
	if d := find(list, func(d devRow) bool { return d.id == nas.id }); d == nil || d.online {
		t.Fatalf("offline at the site, online at the central instance: %+v", d)
	}

	// deleting at the site removes the device at the central instance
	if err := site.inv.Delete(ctx, old); err != nil {
		t.Fatal(err)
	}
	drain(t, site)
	if d := find(devices(t, central), func(d devRow) bool { return d.id == nas.id }); d != nil {
		t.Fatalf("deleted device still at the central instance: %+v", d)
	}
	st, err := central.fed.Site(ctx, siteID)
	if err != nil || !st.Connected || st.Status == nil || st.Instance == nil || st.Instance.Version != "test" {
		t.Fatalf("site overview %+v %v", st, err)
	}
}

func siteSubnets(t *testing.T, in *instance) []plugin.SubnetTarget {
	t.Helper()
	list, err := in.inv.Subnets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestBufferingAndRedelivery(t *testing.T) {
	ctx := context.Background()
	central, site := newInstance(t, "central"), newInstance(t, "site")
	var down atomic.Bool
	pair(t, central, site, &down)
	drain(t, site)

	down.Store(true)
	dev := observe(t, site, "arpscan", &plugin.Observation{IP: "192.168.1.40", MACs: []string{"02:00:00:00:00:40"}, Present: true})
	if _, err := site.ev.Emit(ctx, "diff", plugin.Event{Type: plugin.EvDeviceNew, DeviceID: dev}); err != nil {
		t.Fatal(err)
	}
	site.fed.deliver(ctx)
	st := site.fed.SiteStatus(ctx)
	if st.Connected || st.Buffered < 2 || st.LastError == "" {
		t.Fatalf("while the central instance is down: %+v", st)
	}
	if len(devices(t, central)) != 0 {
		t.Fatal("delivered while down")
	}

	down.Store(false)
	site.fed.site.reset() // skip the backoff
	drain(t, site)
	if len(devices(t, central)) != 1 {
		t.Fatalf("not delivered after the outage: %+v", devices(t, central))
	}
	if _, total, _ := central.ev.List(ctx, events.Filter{Types: []string{plugin.EvDeviceNew}}); total != 1 {
		t.Fatalf("buffered event: %d", total)
	}
}

func TestGapStartsFullSync(t *testing.T) {
	central, site := newInstance(t, "central"), newInstance(t, "site")
	siteID := pair(t, central, site, nil)
	observe(t, site, "arpscan", &plugin.Observation{IP: "192.168.1.50", MACs: []string{"02:00:00:00:00:50"}, Present: true})
	drain(t, site)

	// the central instance restores an older state: it has seen fewer items than the site
	if _, err := central.db.W.Exec("UPDATE sites SET last_seq = 1 WHERE id = ?", siteID); err != nil {
		t.Fatal(err)
	}
	if _, err := central.db.W.Exec("DELETE FROM devices"); err != nil {
		t.Fatal(err)
	}
	observe(t, site, "arpscan", &plugin.Observation{IP: "192.168.1.51", MACs: []string{"02:00:00:00:00:51"}, Present: true})
	drain(t, site)
	if n := len(devices(t, central)); n != 2 {
		t.Fatalf("after the gap the full synchronisation must restore both devices, got %d", n)
	}
}

func TestSiteSettingsValidation(t *testing.T) {
	ctx := context.Background()
	in := newInstance(t, "x")
	bad := "abc"
	if _, err := in.fed.Update(ctx, SettingsInput{Settings: Settings{Role: RoleSite, CentralURL: "https://central.example"}, Token: &bad}); err == nil {
		t.Fatal("token without prefix accepted")
	}
	if _, err := in.fed.Update(ctx, SettingsInput{Settings: Settings{Role: RoleSite, CentralURL: "ftp://x"}}); err == nil {
		t.Fatal("bad URL accepted")
	}
	tok := wire.TokenPrefix + "abc"
	v, err := in.fed.Update(ctx, SettingsInput{Settings: Settings{Role: RoleSite, CentralURL: "https://central.example/",
		Fingerprint: "AA:" + strings.Repeat("b", 62)}, Token: &tok})
	if err != nil || !v.HasToken || v.CentralURL != "https://central.example" || len(v.Fingerprint) != 64 {
		t.Fatalf("site settings %+v %v", v, err)
	}
	// the token is stored encrypted and survives a restart of the service
	again, err := New(ctx, in.fed.Deps)
	if err != nil || again.token != tok {
		t.Fatalf("reload: %q %v", again.token, err)
	}
	var raw string
	_ = in.db.R.QueryRow("SELECT value FROM settings WHERE key = ?", keyToken).Scan(&raw)
	if strings.Contains(raw, "abc") {
		t.Fatal("token stored in plain text")
	}
	if _, err := in.fed.Update(ctx, SettingsInput{Settings: Settings{Role: RoleStandalone}}); err != nil {
		t.Fatal(err)
	}
	if in.fed.Active() {
		t.Fatal("standalone instance still delivers")
	}
}

func TestProtocolAndTokens(t *testing.T) {
	ctx := context.Background()
	central := newInstance(t, "central")
	if _, _, err := central.fed.AuthenticateSite(ctx, wire.TokenPrefix+"x"); !errors.Is(err, ErrNotCentral) {
		t.Fatalf("not central: %v", err)
	}
	if _, err := central.fed.Update(ctx, SettingsInput{Settings: Settings{Role: RoleCentral}}); err != nil {
		t.Fatal(err)
	}
	st, token, err := central.fed.CreateSite(ctx, SiteInput{Name: "Büro Süd"})
	if err != nil || st.Slug != "buero-sued" {
		t.Fatalf("slug %+v %v", st, err)
	}
	if _, _, err := central.fed.CreateSite(ctx, SiteInput{Name: "büro süd"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if id, _, err := central.fed.AuthenticateSite(ctx, token); err != nil || id != st.ID {
		t.Fatalf("auth: %d %v", id, err)
	}
	fresh, err := central.fed.RotateSiteToken(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := central.fed.AuthenticateSite(ctx, token); err == nil {
		t.Fatal("old token still valid")
	}
	if _, err := central.fed.Ingest(ctx, st.ID, st.Name, &wire.Batch{Protocol: wire.Protocol + 1}, ""); err == nil {
		t.Fatal("newer protocol accepted")
	}
	// unknown item kinds of a newer site are skipped, not fatal
	resp, err := central.fed.Ingest(ctx, st.ID, st.Name, &wire.Batch{Protocol: wire.Protocol, Epoch: "e1",
		Items: []wire.Item{{Seq: 7, Kind: "future", Data: json.RawMessage(`{}`)}}}, "")
	if err != nil || resp.Acked != 7 {
		t.Fatalf("unknown kind: %+v %v", resp, err)
	}
	_ = fresh
}

func TestMergeAndSplitAreMirrored(t *testing.T) {
	ctx := context.Background()
	central, site := newInstance(t, "central"), newInstance(t, "site")
	pair(t, central, site, nil)
	a := observe(t, site, "arpscan", &plugin.Observation{IP: "192.168.1.60", MACs: []string{"02:00:00:00:00:60"}, Present: true})
	b := observe(t, site, "arpscan", &plugin.Observation{IP: "192.168.1.61", MACs: []string{"02:00:00:00:00:61"}, Present: true})
	// a proxmox guest and its node: the relation arrives with site ids and is translated
	node := observe(t, site, "proxmox", &plugin.Observation{Ref: &plugin.ExternalRef{Source: "proxmox", ID: "pve/node"}, Create: true, Hostname: "pve"})
	observe(t, site, "proxmox", &plugin.Observation{DeviceID: a, Relations: []plugin.Relation{{Kind: plugin.RelRunsOn, Other: plugin.DeviceRef{DeviceID: node}}}})
	drain(t, site)
	if n := len(devices(t, central)); n != 3 {
		t.Fatalf("devices before merge: %d", n)
	}
	var rels int
	_ = central.db.R.QueryRow("SELECT COUNT(*) FROM relations WHERE kind = 'runs_on'").Scan(&rels)
	if rels != 1 {
		t.Fatalf("relation not translated: %d", rels)
	}

	if err := site.inv.Merge(ctx, a, []int64{b}); err != nil {
		t.Fatal(err)
	}
	drain(t, site)
	list := devices(t, central)
	if len(list) != 2 || find(list, func(d devRow) bool { return d.macs == "02:00:00:00:00:60,02:00:00:00:00:61" }) == nil {
		t.Fatalf("merge not mirrored: %+v", list)
	}

	newID, err := site.inv.Split(ctx, a, []string{"02:00:00:00:00:61"})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, site)
	list = devices(t, central)
	if len(list) != 3 || find(list, func(d devRow) bool { return d.macs == "02:00:00:00:00:61" && d.site > 0 }) == nil {
		t.Fatalf("split not mirrored: %+v", list)
	}
	// the new device of the split is linked: its next observation updates it
	observe(t, site, "dns", &plugin.Observation{DeviceID: newID, Hostname: "printer"})
	drain(t, site)
	if d := find(devices(t, central), func(d devRow) bool { return d.macs == "02:00:00:00:00:61" }); d == nil || d.host != "printer" {
		t.Fatalf("split device not linked: %+v", d)
	}
}

func TestRestoredSiteResetsMapping(t *testing.T) {
	ctx := context.Background()
	central, site := newInstance(t, "central"), newInstance(t, "site")
	siteID := pair(t, central, site, nil)
	observe(t, site, "arpscan", &plugin.Observation{IP: "192.168.1.70", MACs: []string{"02:00:00:00:00:70"}, Present: true})
	drain(t, site)

	// the site restores a backup: the next stream starts with a reset and a full sync
	if err := settings.SetJSON(ctx, site.db.W, KeyRestored, true); err != nil {
		t.Fatal(err)
	}
	drain(t, site)
	var mapped int
	_ = central.db.R.QueryRow("SELECT COUNT(*) FROM site_devices WHERE site_id = ?", siteID).Scan(&mapped)
	list := devices(t, central)
	if len(list) != 1 || mapped != 1 {
		t.Fatalf("after reset: %d devices, %d mapped", len(list), mapped)
	}
}

func TestSiteDownAndUp(t *testing.T) {
	ctx := context.Background()
	central := newInstance(t, "central")
	if _, err := central.fed.Update(ctx, SettingsInput{Settings: Settings{Role: RoleCentral}}); err != nil {
		t.Fatal(err)
	}
	st, _, err := central.fed.CreateSite(ctx, SiteInput{Name: "Colo"})
	if err != nil {
		t.Fatal(err)
	}
	// a site that never reported is not "down"
	if err := central.fed.checkSites(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := central.db.W.Exec("UPDATE sites SET last_contact_at = ? WHERE id = ?", time.Now().Add(-10*time.Minute).UnixMilli(), st.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 { // raised once
		if err := central.fed.checkSites(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, n, _ := central.ev.List(ctx, events.Filter{Types: []string{plugin.EvSiteDown}}); n != 1 {
		t.Fatalf("site.down events: %d", n)
	}
	if _, err := central.fed.Ingest(ctx, st.ID, st.Name, &wire.Batch{Protocol: wire.Protocol, Items: []wire.Item{}}, ""); err != nil {
		t.Fatal(err)
	}
	evs, n, _ := central.ev.List(ctx, events.Filter{Types: []string{plugin.EvSiteUp}})
	if n != 1 || evs[0].Payload["down_seconds"] == nil {
		t.Fatalf("site.up: %d %+v", n, evs)
	}
	if s, _ := central.fed.Site(ctx, st.ID); s.Down || !s.Connected {
		t.Fatalf("site after contact: %+v", s)
	}
}
