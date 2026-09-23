package diff

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func defaultConfig(t *testing.T) config {
	t.Helper()
	vals, err := (&Plugin{}).Schema().Validate(map[string]any{"ignore_ports": []any{"5353/udp"}, "ignore_tags": []any{"quiet"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return load(plugin.NewSettings(vals))
}

func TestMapEvents(t *testing.T) {
	now := time.Now()
	since := now.Add(-3 * time.Hour)
	devs := map[int64]*plugin.DeviceInfo{
		1: {ID: 1, Name: "nas", State: "known"},
		2: {ID: 2, Name: "cam", State: "ignored"},
		3: {ID: 3, Name: "tv", State: "unknown", Tags: []string{"quiet"}},
	}
	lookup := func(id int64) *plugin.DeviceInfo { return devs[id] }
	kinds := func(id string) plugin.Kind {
		if id == "proxmox" {
			return plugin.KindImporter
		}
		return plugin.KindScanner
	}
	ssh := &plugin.Port{Port: 22, Proto: "tcp", Service: "ssh", Product: "OpenSSH", Version: "9.6"}
	ssh2 := &plugin.Port{Port: 22, Proto: "tcp", Service: "ssh", Product: "OpenSSH", Version: "9.7"}
	mdns := &plugin.Port{Port: 5353, Proto: "udp", Service: "mdns"}
	weak := &plugin.TLSCert{Port: 443, Fingerprint: "b", SubjectCN: "nas", NotAfter: now.Add(time.Hour), WeakProtocols: []string{"TLS 1.0"}}
	cases := []struct {
		name string
		ch   plugin.Change
		want []string
	}{
		{"new device from scanner", plugin.Change{Type: plugin.ChangeDeviceCreated, DeviceID: 1, PluginID: "arpscan", New: &plugin.DeviceSnapshot{IP: "192.168.8.5"}}, []string{plugin.EvDeviceNew}},
		{"new device from importer is silent", plugin.Change{Type: plugin.ChangeDeviceCreated, DeviceID: 1, PluginID: "proxmox", New: &plugin.DeviceSnapshot{}}, nil},
		{"offline", plugin.Change{Type: plugin.ChangeDeviceOffline, DeviceID: 1, Old: since}, []string{plugin.EvDeviceOffline}},
		{"back online", plugin.Change{Type: plugin.ChangeDeviceOnline, DeviceID: 1, At: now, Old: &since}, []string{plugin.EvDeviceOnline}},
		{"ip change", plugin.Change{Type: plugin.ChangeIPChanged, DeviceID: 1, Old: "192.168.8.5", New: "192.168.8.6"}, []string{plugin.EvDeviceIPChanged}},
		{"mac change", plugin.Change{Type: plugin.ChangeMACChanged, DeviceID: 1, New: &plugin.MACChange{IP: "192.168.8.5", OldMAC: "a", NewMAC: "b"}}, []string{plugin.EvDeviceMACChanged}},
		{"hostname", plugin.Change{Type: plugin.ChangeHostname, DeviceID: 1, Old: "a", New: "b"}, []string{plugin.EvDeviceHostnameChanged}},
		{"os", plugin.Change{Type: plugin.ChangeOS, DeviceID: 1, Old: "Linux 5", New: "Linux 6"}, []string{plugin.EvDeviceOSChanged}},
		{"port opened", plugin.Change{Type: plugin.ChangePortOpened, DeviceID: 1, Key: "192.168.8.5 tcp/22", New: ssh}, []string{plugin.EvPortOpened}},
		{"initial port is silent", plugin.Change{Type: plugin.ChangePortOpened, DeviceID: 1, Initial: true, Key: "192.168.8.5 tcp/22", New: ssh}, nil},
		{"ignored port", plugin.Change{Type: plugin.ChangePortOpened, DeviceID: 1, Key: "192.168.8.5 udp/5353", New: mdns}, nil},
		{"port closed", plugin.Change{Type: plugin.ChangePortClosed, DeviceID: 1, Key: "192.168.8.5 tcp/22", Old: ssh}, []string{plugin.EvPortClosed}},
		{"version change", plugin.Change{Type: plugin.ChangePortChanged, DeviceID: 1, Key: "192.168.8.5 tcp/22", Old: ssh, New: ssh2}, []string{plugin.EvServiceChanged}},
		{"cert changed to weak", plugin.Change{Type: plugin.ChangeCertChanged, DeviceID: 1, Key: "192.168.8.5:443", Old: &plugin.TLSCert{Fingerprint: "a"}, New: weak},
			[]string{plugin.EvCertChanged, plugin.EvTLSWeak}},
		{"container image", plugin.Change{Type: plugin.ChangeContainerImage, DeviceID: 1, Old: &plugin.Container{Name: "g", Image: "g:1"}, New: &plugin.Container{Name: "g", Image: "g:2"}},
			[]string{plugin.EvContainerImageChanged}},
		{"packages", plugin.Change{Type: plugin.ChangePackages, DeviceID: 1, New: &plugin.PackageDelta{Manager: "dpkg",
			Updated: []plugin.PackageUpdate{{Name: "openssl", From: "1", To: "2"}}}}, []string{plugin.EvPackagesChanged}},
		{"ignored device", plugin.Change{Type: plugin.ChangePortOpened, DeviceID: 2, Key: "x tcp/22", New: ssh}, nil},
		{"ignored tag", plugin.Change{Type: plugin.ChangePortOpened, DeviceID: 3, Key: "x tcp/22", New: ssh}, nil},
		{"mac added is not an event", plugin.Change{Type: plugin.ChangeMACAdded, DeviceID: 1, New: "aa"}, nil},
	}
	c := defaultConfig(t)
	for _, tc := range cases {
		got := mapEvents([]plugin.Change{tc.ch}, c, lookup, kinds)
		var types []string
		for _, ev := range got {
			types = append(types, ev.Type)
			if _, ok := plugin.LookupEvent(ev.Type); !ok {
				t.Errorf("%s: unknown event type %s", tc.name, ev.Type)
			}
			if ev.Title == "" {
				t.Errorf("%s: empty title", tc.name)
			}
		}
		if len(types) != len(tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, types, tc.want)
			continue
		}
		for i := range types {
			if types[i] != tc.want[i] {
				t.Errorf("%s: got %v want %v", tc.name, types, tc.want)
			}
		}
	}
	// a randomized (locally administered) MAC downgrades the severity
	mc := func(newMAC string) plugin.Severity {
		evs := mapEvents([]plugin.Change{{Type: plugin.ChangeMACChanged, DeviceID: 1,
			New: &plugin.MACChange{IP: "192.168.8.5", OldMAC: "00:11:22:33:44:55", NewMAC: newMAC}}}, c, lookup, kinds)
		if len(evs) != 1 {
			t.Fatalf("mac change: %d events", len(evs))
		}
		return evs[0].Severity
	}
	if got := mc("da:a1:19:00:00:01"); got != plugin.SevLow {
		t.Errorf("randomized mac severity = %s, want low", got)
	}
	if got := mc("00:11:22:33:44:66"); got == plugin.SevLow {
		t.Errorf("global mac severity must not be downgraded")
	}
	// disabled event types are not produced
	c.types = map[string]bool{plugin.EvPortClosed: true}
	if got := mapEvents([]plugin.Change{{Type: plugin.ChangePortOpened, DeviceID: 1, Key: "x tcp/22", New: ssh}}, c, lookup, kinds); len(got) != 0 {
		t.Errorf("disabled type produced %v", got)
	}
}

func TestExpiryEvent(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	mk := func(d time.Duration) certRow {
		return certRow{deviceID: 1, ip: "192.168.8.5", port: 443, cn: "nas", fingerprint: "ff", notAfter: now.Add(d)}
	}
	cases := []struct {
		left    time.Duration
		typ     string
		sev     plugin.Severity
		days    int
		present bool
	}{
		{40 * 24 * time.Hour, "", "", 0, false},
		{20 * 24 * time.Hour, plugin.EvCertExpiring, plugin.SevLow, 20, true},
		{10*24*time.Hour + time.Hour, plugin.EvCertExpiring, plugin.SevMedium, 10, true},
		{3 * 24 * time.Hour, plugin.EvCertExpiring, plugin.SevHigh, 3, true},
		{-2 * 24 * time.Hour, plugin.EvCertExpired, plugin.SevHigh, -2, true},
	}
	for _, c := range cases {
		ev := expiryEvent(mk(c.left), now, 30, 24*time.Hour, "nas")
		if (ev != nil) != c.present {
			t.Fatalf("left %v: present=%v", c.left, ev != nil)
		}
		if ev == nil {
			continue
		}
		if ev.Type != c.typ || ev.Severity != c.sev || ev.Payload["days_left"] != c.days || ev.DedupKey == "" {
			t.Errorf("left %v: %+v", c.left, ev)
		}
	}
}

func TestPresenceQuietTypes(t *testing.T) {
	c := defaultConfig(t)
	devs := map[int64]*plugin.DeviceInfo{
		1: {ID: 1, Name: "iPhone", Type: "phone", State: "known"},
		2: {ID: 2, Name: "nas", Type: "nas", State: "known"},
	}
	lookup := func(id int64) *plugin.DeviceInfo { return devs[id] }
	kinds := func(string) plugin.Kind { return plugin.KindScanner }
	since := time.Now().Add(-time.Hour)
	changes := []plugin.Change{
		{Type: plugin.ChangeDeviceOffline, DeviceID: 1, Old: since},
		{Type: plugin.ChangeDeviceOnline, DeviceID: 1, At: time.Now(), Old: &since},
		{Type: plugin.ChangeDeviceOffline, DeviceID: 2, Old: since},
		{Type: plugin.ChangeIPChanged, DeviceID: 1, Old: "192.168.8.5", New: "192.168.8.6"},
	}
	got := mapEvents(changes, c, lookup, kinds)
	var types []string
	for _, ev := range got {
		types = append(types, fmt.Sprintf("%d:%s", ev.DeviceID, ev.Type))
	}
	want := []string{"2:" + plugin.EvDeviceOffline, "1:" + plugin.EvDeviceIPChanged}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", types, want)
	}
}

func TestDampPresence(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "netscope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	now := time.Now().UnixMilli()
	for _, id := range []int64{1, 2} {
		if _, err := d.W.Exec(`INSERT INTO devices(id, display_name, created_source, first_seen, state, created_at, updated_at)
			VALUES (?, 'dev', 'arpscan', ?, 'known', ?, ?)`, id, now, now, now); err != nil {
			t.Fatal(err)
		}
	}
	// device 1 already flapped three times today, one old event of device 2 is outside the window
	for _, e := range []struct {
		dev int64
		typ string
		ts  int64
	}{
		{1, plugin.EvDeviceOffline, now - 3*3600e3}, {1, plugin.EvDeviceOnline, now - 2*3600e3}, {1, plugin.EvDeviceOffline, now - 3600e3},
		{2, plugin.EvDeviceOffline, now - 30*3600e3},
	} {
		if _, err := d.W.Exec(`INSERT INTO events(ts, type, category, severity, device_id, title) VALUES (?, ?, 'device', 'low', ?, 'x')`,
			e.ts, e.typ, e.dev); err != nil {
			t.Fatal(err)
		}
	}
	rc, _, _ := plugintest.RunContext(t, &Plugin{}, nil)
	rc.DB = d
	c := defaultConfig(t)
	evs := []plugin.Event{
		{Type: plugin.EvDeviceOnline, DeviceID: 1},  // 4th within 24 h: allowed
		{Type: plugin.EvDeviceOffline, DeviceID: 1}, // 5th: damped
		{Type: plugin.EvPortOpened, DeviceID: 1},    // other types are never damped
		{Type: plugin.EvDeviceOnline, DeviceID: 2},
	}
	got := dampPresence(ctx, rc, c, evs)
	var types []string
	for _, ev := range got {
		types = append(types, fmt.Sprintf("%d:%s", ev.DeviceID, ev.Type))
	}
	want := []string{"1:" + plugin.EvDeviceOnline, "1:" + plugin.EvPortOpened, "2:" + plugin.EvDeviceOnline}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", types, want)
	}
	c.flapLimit = 0
	if got := dampPresence(ctx, rc, c, []plugin.Event{{Type: plugin.EvDeviceOffline, DeviceID: 1}}); len(got) != 1 {
		t.Errorf("limit 0 must not damp: %v", got)
	}
}
