package netsrc

import (
	"testing"
	"time"

	"netscope/internal/plugin"
)

func TestMergeAndObservation(t *testing.T) {
	exp := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	seen := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	leases := []Client{
		{MAC: "AA-BB-CC-00-00-01", IP: "10.0.0.5", Hostname: "laptop", Kind: KindDHCP, Expires: exp},
		{MAC: "not-a-mac", IP: "10.0.0.9"},
	}
	static := []Client{{MAC: "aa:bb:cc:00:00:01", Name: "Arbeitsplatz Erika", Kind: KindStatic, Static: true}}
	arp := []Client{
		{MAC: "aa:bb:cc:00:00:01", IP: "10.0.0.99", Kind: KindARP, Interface: "LAN", LastSeen: seen},
		{MAC: "aa:bb:cc:00:00:02", IP: "10.0.0.6", Kind: KindARP, Hostname: "*"},
	}
	got := Merge(leases, static, arp)
	if len(got) != 2 {
		t.Fatalf("merged %d", len(got))
	}
	a := got[0]
	if a.MAC != "aa:bb:cc:00:00:01" || a.IP != "10.0.0.5" || !a.Static || a.Kind != KindDHCP || a.Interface != "LAN" ||
		!a.Expires.Equal(exp) || !a.LastSeen.Equal(seen) || a.name() != "Arbeitsplatz Erika" {
		t.Fatalf("merged client %+v", a)
	}
	// an ARP entry alone takes the kind of a later lease
	b := Merge([]Client{{MAC: "aa:bb:cc:00:00:03", Kind: KindARP}}, []Client{{MAC: "aa:bb:cc:00:00:03", Kind: KindDHCP, Hostname: "tv"}})[0]
	if b.Kind != KindDHCP || b.Hostname != "tv" {
		t.Errorf("kind %+v", b)
	}

	obs := a.Observation("opnsense", "fw.example", true)
	inv := obs.Inventory.(Inventory)
	if obs.IP != "10.0.0.5" || obs.Hostname != "Arbeitsplatz Erika" || !obs.Create || obs.Present || obs.Attrs["opnsense.static"] != "true" ||
		inv.Source != "fw.example" || inv.Name != "Arbeitsplatz Erika" || inv.Hostname != "laptop" || inv.Expires == nil || len(obs.Relations) != 0 {
		t.Fatalf("observation %+v %+v", obs, inv)
	}
	if o := got[1].Observation("x", "s", false); o.Hostname != "" || o.Target != "10.0.0.6" {
		t.Errorf("placeholder name: %+v", o)
	}

	// wireless client of an access point
	w := Client{MAC: "aa:bb:cc:00:00:04", IP: "10.0.0.7", Wired: Bool(false), UplinkMAC: "F0:9F:C2:00:00:01", UplinkName: "AP Flur", SSID: "Office", OS: "iOS"}
	o := w.Observation("unifi", "ctrl", false)
	if len(o.Relations) != 1 || o.Relations[0].Kind != plugin.RelWireless || o.Relations[0].Other.MAC != "f0:9f:c2:00:00:01" ||
		o.Relations[0].Label != "Office" || o.OS == nil || o.OS.Name != "iOS" || o.Inventory.(Inventory).Uplink != "AP Flur" {
		t.Fatalf("wireless %+v", o)
	}
	wired := Client{MAC: "aa:bb:cc:00:00:05", Wired: Bool(true), UplinkMAC: "f0:9f:c2:00:00:02", Port: "7"}
	if r := wired.Observation("unifi", "ctrl", false).Relations; len(r) != 1 || r[0].Kind != plugin.RelSwitchPort || r[0].RemotePort != "7" {
		t.Errorf("wired %+v", r)
	}
}

func TestBaseURLAndRedact(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.1":              "https://192.168.1.1:443",
		"fw.lan:8443":              "https://fw.lan:8443",
		"http://pi.hole/admin/":    "http://pi.hole/admin",
		"https://[fe80::1]/x?y=1":  "https://[fe80::1]/x",
		"fe80::1":                  "https://[fe80::1]:443",
		"https://ctrl.example:444": "https://ctrl.example:444",
	} {
		u, err := BaseURL(in, "https", 443)
		if err != nil || u.String() != want {
			t.Errorf("BaseURL(%q) = %v %v, want %s", in, u, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x", "https://"} {
		if _, err := BaseURL(bad, "https", 443); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got := redact("https://u:p@fw/api?access_token=secret&vdom=root"); got != "https://fw/api?access_token=%2A%2A%2A&vdom=root" {
		t.Errorf("redact: %s", got)
	}
	if Host("https://fw.lan:8443/x") != "fw.lan" || Host("10.0.0.1") != "10.0.0.1" {
		t.Error("host")
	}
}
