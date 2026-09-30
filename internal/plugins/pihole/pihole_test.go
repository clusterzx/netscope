package pihole

import (
	"context"
	"encoding/json"
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

type fakePihole struct {
	password  string
	dhcp      bool
	sessions  int
	loggedOut int
}

func (f *fakePihole) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			switch r.Method {
			case http.MethodPost:
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				if f.password != "" && body["password"] != f.password {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"session":{"valid":false,"totp":false,"sid":null,"validity":-1,"message":"password incorrect"}}`))
					return
				}
				f.sessions++
				_, _ = w.Write([]byte(`{"session":{"valid":true,"totp":false,"sid":"SID1","csrf":"x","validity":300,"message":"correct password"}}`))
			case http.MethodDelete:
				f.loggedOut++
				w.WriteHeader(http.StatusNoContent)
			}
			return
		}
		if f.password != "" && r.Header.Get("X-FTL-SID") != "SID1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/config/dhcp/active":
			_, _ = w.Write([]byte(`{"config":{"dhcp":{"active":` + map[bool]string{true: "true", false: "false"}[f.dhcp] + `}}}`))
		case "/api/dhcp/leases":
			_, _ = w.Write([]byte(`{"leases":[{"expires":1790509123,"name":"macbook-pro","hwaddr":"3c:22:fb:12:34:56","ip":"192.168.1.100","clientid":"01:3c:22:fb:12:34:56"},
				{"expires":0,"name":"*","hwaddr":"00:11:32:aa:bb:cc","ip":"192.168.1.20","clientid":"*"},
				{"expires":1790400000,"name":"old","hwaddr":"aa:bb:cc:00:00:01","ip":"192.168.1.150","clientid":"*"}]}`))
		case "/api/config/dhcp/hosts":
			_, _ = w.Write([]byte(`{"config":{"dhcp":{"hosts":["00:11:32:aa:bb:cc,192.168.1.20,nas01,24h","aa:bb:cc:00:00:02,set:iot,192.168.1.30,infinite","aa:bb:cc:00:00:03,ignore"]}}}`))
		case "/api/network/devices":
			if r.URL.Query().Get("max_devices") != "10000" {
				t.Errorf("network query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"devices":[
				{"id":1,"hwaddr":"3c:22:fb:12:34:56","interface":"eth0","firstSeen":1664623620,"lastQuery":1790496723,"numQueries":585462,"macVendor":"Apple, Inc.","ips":[{"ip":"192.168.1.100","name":"macbook-pro.lan","lastSeen":1790496723},{"ip":"fe80::1","name":null,"lastSeen":1790499999}]},
				{"id":2,"hwaddr":"ip-192.168.5.20","interface":"eth0","lastQuery":1790496000,"macVendor":null,"ips":[{"ip":"192.168.5.20","name":"routed","lastSeen":1790496000}]},
				{"id":3,"hwaddr":"00:00:00:00:00:00","interface":"lo","lastQuery":0,"macVendor":null,"ips":[]},
				{"id":4,"hwaddr":"d8:3a:dd:00:00:01","interface":"eth0","lastQuery":1790490000,"macVendor":"Raspberry Pi","ips":[{"ip":"192.168.1.40","name":"octopi.lan.","lastSeen":1790490000}]}]}`))
		default:
			http.NotFound(w, r)
		}
	})
}

func runPihole(t *testing.T, f *fakePihole, creds plugintest.Creds) (*plugintest.Sink, error) {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	p := &Plugin{now: func() time.Time { return now }}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"hosts": []any{srv.URL}})
	rc.Creds = creds
	return sink, p.Run(context.Background(), rc)
}

func appPassword(pw string) plugintest.Creds {
	return plugintest.Creds{1: {ID: 1, Type: plugin.CredPassword, Secret: map[string]string{"password": pw}}}
}

func TestPiholeWithDHCP(t *testing.T) {
	f := &fakePihole{password: "app-pw", dhcp: true}
	sink, err := runPihole(t, f, appPassword("app-pw"))
	if err != nil {
		t.Fatal(err)
	}
	if f.sessions != 1 || f.loggedOut != 1 {
		t.Errorf("sessions %d, logged out %d", f.sessions, f.loggedOut)
	}
	byIP := map[string]plugin.Observation{}
	for _, o := range sink.All() {
		byIP[o.IP] = o
	}
	if len(byIP) != 4 {
		t.Fatalf("observations %v", byIP)
	}
	nas := byIP["192.168.1.20"]
	if nas.Hostname != "nas01" || !nas.Inventory.(netsrc.Inventory).Static {
		t.Errorf("reservation %+v", nas)
	}
	mb := byIP["192.168.1.100"]
	if mb.Hostname != "macbook-pro" || mb.Vendor != "Apple, Inc." || mb.Inventory.(netsrc.Inventory).Kind != netsrc.KindDHCP {
		t.Errorf("lease + network table %+v", mb)
	}
	if pi := byIP["192.168.1.40"]; pi.Hostname != "octopi.lan" || pi.Inventory.(netsrc.Inventory).Kind != netsrc.KindARP {
		t.Errorf("network table only %+v", pi)
	}
	if res := byIP["192.168.1.30"]; !res.Inventory.(netsrc.Inventory).Static {
		t.Errorf("tagged reservation %+v", res)
	}
}

func TestPiholeWithoutDHCPOrPassword(t *testing.T) {
	// no password set, DHCP off: only the network table, no credentials needed
	f := &fakePihole{dhcp: false}
	sink, err := runPihole(t, f, plugintest.Creds{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(sink.All()); n != 2 {
		t.Errorf("observations %d", n)
	}
	// wrong password
	f2 := &fakePihole{password: "app-pw", dhcp: true}
	if _, err := runPihole(t, f2, appPassword("nope")); err == nil || !strings.Contains(err.Error(), "abgelehnt") {
		t.Errorf("wrong password: %v", err)
	}
}

func TestParseHost(t *testing.T) {
	for spec, want := range map[string]string{
		"00:20:e0:3b:13:af,192.168.0.123,laptop,24h": "00:20:e0:3b:13:af 192.168.0.123 laptop",
		"192.168.0.5,drucker,00:20:e0:3b:13:b0":      "00:20:e0:3b:13:b0 192.168.0.5 drucker",
		"00:20:e0:3b:13:b1,id:*,set:x,10.0.0.9":      "00:20:e0:3b:13:b1 10.0.0.9 ",
	} {
		c, ok := parseHost(spec)
		if got := c.MAC + " " + c.IP + " " + c.Name; !ok || got != want {
			t.Errorf("%q = %q", spec, got)
		}
	}
	if _, ok := parseHost("laptop,192.168.0.9"); ok {
		t.Error("reservation without MAC accepted")
	}
}

// The connection test signs in and reads like a run (and closes its session again), but
// stores nothing; it reports a wrong password and a wrong address per Pi-hole.
func TestPiholeConnectionTest(t *testing.T) {
	f := &fakePihole{password: "app-pw", dhcp: true}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	p := &Plugin{now: func() time.Time { return now }}
	res, err := plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL}}, appPassword("app-pw"), "")
	if err != nil || len(res) != 1 || !res[0].OK || res[0].Target != srv.URL || !strings.Contains(res[0].Message, "4 Clients") {
		t.Fatalf("valid password: %+v, %v", res, err)
	}
	if f.sessions != 1 || f.loggedOut != 1 {
		t.Errorf("sessions %d, logged out %d", f.sessions, f.loggedOut)
	}
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL}}, appPassword("nope"), "")
	if err != nil || len(res) != 1 || res[0].OK || !strings.Contains(res[0].Message, "Anmeldung abgelehnt") {
		t.Fatalf("wrong password: %+v, %v", res, err)
	}
	closed := plugintest.ClosedURL(t, "http")
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL, closed}}, appPassword("app-pw"), "")
	if err != nil || len(res) != 2 || !res[0].OK || res[1].OK || res[1].Target != closed || res[1].Message == "" {
		t.Fatalf("wrong address: %+v, %v", res, err)
	}
}
