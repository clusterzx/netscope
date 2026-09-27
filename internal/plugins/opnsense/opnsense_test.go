package opnsense

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/plugins/netsrc"
)

var now = time.Unix(1790500000, 0).UTC()

// firewall serves OPNsense API answers by path; the key "k"/"s" is accepted.
func firewall(t *testing.T, routes map[string]string) (*httptest.Server, map[string]int) {
	t.Helper()
	hits := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		if u, p, ok := r.BasicAuth(); !ok || u != "k" || p != "s" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, ok := routes[r.URL.Path]
		switch {
		case !ok:
			http.NotFound(w, r)
		case body == "403":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func run(t *testing.T, srv *httptest.Server, creds plugintest.Creds, extra map[string]any) (*plugintest.Sink, error) {
	t.Helper()
	p := &Plugin{now: func() time.Time { return now }}
	settings := map[string]any{"hosts": []any{srv.URL}}
	for k, v := range extra {
		settings[k] = v
	}
	rc, sink, _ := plugintest.RunContext(t, p, settings)
	rc.Creds = creds
	return sink, p.Run(context.Background(), rc)
}

func key(id int64, k, s string) plugintest.Creds {
	return plugintest.Creds{id: {ID: id, Name: "api", Type: plugin.CredAPIToken, Public: map[string]string{"token_id": k}, Secret: map[string]string{"token": s}}}
}

// 26.1: snake_case, Dnsmasq with a reservation, Kea not configured, ARP.
func TestOPNsense26(t *testing.T) {
	srv, hits := firewall(t, map[string]string{
		"/api/dnsmasq/leases/search": `{"total":3,"rowCount":3,"current":1,"rows":[
			{"expire":1790509123,"hwaddr":"3c:22:fb:12:34:56","iaid":"","address":"192.168.1.100","hostname":"macbook-pro","client_id":"01:3c:22:fb:12:34:56","if":"igb1","if_descr":"LAN","if_name":"lan","mac_info":"Apple, Inc.","is_reserved":[],"lease_type":"dynamic"},
			{"expire":0,"hwaddr":"00:11:32:aa:bb:cc","address":"192.168.1.20","hostname":"*","client_id":"*","if":"igb1","if_descr":"LAN","mac_info":"Synology Incorporated","is_reserved":["hwaddr"]},
			{"expire":1790400000,"hwaddr":"aa:bb:cc:00:00:09","address":"192.168.1.150","hostname":"weg","if_descr":"LAN","is_reserved":"0"}]}`,
		"/api/dnsmasq/settings/search_host": `{"rows":[{"host":"nas01","domain":"lan","ip":"192.168.1.20","hwaddr":"00:11:32:aa:bb:cc","ignore":"0","descr":"Synology im Keller"}]}`,
		"/api/diagnostics/interface/get_arp": `[
			{"mac":"3c:22:fb:12:34:56","ip":"192.168.1.100","intf":"igb1","expired":false,"expires":1187,"permanent":false,"type":"ethernet","manufacturer":"Apple, Inc.","hostname":"","intf_description":"LAN"},
			{"mac":"00:0d:b9:00:00:01","ip":"192.168.1.1","intf":"igb1","expired":false,"expires":-1,"permanent":true,"manufacturer":"PC Engines","intf_description":"LAN"},
			{"mac":"44:55:66:77:88:99","ip":"192.168.10.5","intf":"igb1_vlan10","expired":false,"expires":845,"permanent":false,"manufacturer":"","hostname":"cam.iot","intf_description":"IOT"}]`,
	})
	sink, err := run(t, srv, key(1, "k", "s"), map[string]any{"create_missing": true})
	if err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 3 {
		t.Fatalf("observations %d: %+v", len(obs), obs)
	}
	byIP := map[string]plugin.Observation{}
	for _, o := range obs {
		byIP[o.IP] = o
	}
	nas := byIP["192.168.1.20"]
	inv := nas.Inventory.(netsrc.Inventory)
	if nas.Hostname != "Synology im Keller" || !inv.Static || inv.Expires != nil || nas.Vendor != "Synology Incorporated" || !nas.Create {
		t.Errorf("nas %+v %+v", nas, inv)
	}
	if mb := byIP["192.168.1.100"]; mb.Hostname != "macbook-pro" || mb.Inventory.(netsrc.Inventory).Interface != "LAN" || mb.Vendor != "Apple, Inc." {
		t.Errorf("macbook %+v", mb)
	}
	if cam := byIP["192.168.10.5"]; cam.Hostname != "cam.iot" || cam.Inventory.(netsrc.Inventory).Kind != netsrc.KindARP || cam.Inventory.(netsrc.Inventory).Interface != "IOT" {
		t.Errorf("arp only %+v", cam)
	}
	// camelCase is not tried once the snake_case URL answered; the missing Kea is fine
	if hits["/api/diagnostics/interface/getArp"] != 0 || hits["/api/kea/leases4/search"] != 1 {
		t.Errorf("hits %v", hits)
	}
}

// 24.7: camelCase only (snake_case → 404), ISC leases with static rows.
func TestOPNsense24(t *testing.T) {
	srv, _ := firewall(t, map[string]string{
		"/api/dhcpv4/leases/searchLease": `{"total":2,"rows":[
			{"address":"192.168.1.100","starts":"2026/09/27 08:12:03","ends":"2026/09/27 10:12:03","binding":"active","type":"dynamic","status":"online","descr":"","mac":"3c:22:fb:12:34:56","hostname":"MacBook-Pro","state":"active","man":"Apple, Inc.","if":"lan","if_descr":"LAN"},
			{"address":"192.168.1.20","starts":"","ends":"","type":"static","status":"offline","descr":"NAS","mac":"00:11:32:aa:bb:cc","hostname":"nas01","state":"active","man":"","if":"lan","if_descr":"LAN"},
			{"address":"192.168.1.101","type":"dynamic","mac":"aa:bb:cc:00:00:01","state":"expired","if_descr":"LAN"}]}`,
		"/api/diagnostics/interface/getArp": `[]`,
	})
	sink, err := run(t, srv, key(1, "k", "s"), nil)
	if err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 2 {
		t.Fatalf("observations %+v", obs)
	}
	for _, o := range obs {
		inv := o.Inventory.(netsrc.Inventory)
		switch o.IP {
		case "192.168.1.20":
			if o.Hostname != "NAS" || !inv.Static || inv.Online == nil || *inv.Online || o.Create {
				t.Errorf("static %+v %+v", o, inv)
			}
		case "192.168.1.100":
			if inv.Online == nil || !*inv.Online || inv.Expires == nil {
				t.Errorf("dynamic %+v", inv)
			}
		default:
			t.Errorf("unexpected %s", o.IP)
		}
	}
}

func TestOPNsenseCredentialsAndErrors(t *testing.T) {
	srv, _ := firewall(t, map[string]string{"/api/diagnostics/interface/get_arp": `[]`})
	// a rejected key is skipped, the next one works
	creds := key(1, "wrong", "x")
	creds[2] = key(2, "k", "s")[2]
	if _, err := run(t, srv, creds, nil); err != nil {
		t.Errorf("second key: %v", err)
	}
	if _, err := run(t, srv, key(1, "wrong", "x"), nil); err == nil || !strings.Contains(err.Error(), "Anmeldung abgelehnt") {
		t.Errorf("wrong key: %v", err)
	}
	// nothing the key may read
	srv2, _ := firewall(t, map[string]string{"/api/diagnostics/interface/get_arp": "403", "/api/diagnostics/interface/getArp": "403"})
	if _, err := run(t, srv2, key(1, "k", "s"), nil); err == nil || !strings.Contains(err.Error(), "Rechte") {
		t.Errorf("no rights: %v", err)
	}
	if err := (&Plugin{}).ValidateSettings(plugin.NewSettings(map[string]any{"hosts": []any{"ftp://x"}})); err == nil {
		t.Error("invalid source accepted")
	}
}
