package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/plugins/netsrc"
)

const (
	devicesJSON = `[
		{"_id":"d1","mac":"80:2a:a8:00:01:02","ip":"192.168.0.20","name":"AP Flur","model":"U7PG2","type":"uap","serial":"802AA8000102","version":"6.6.77","state":1,"adopted":true,"last_seen":1790496000},
		{"_id":"d2","mac":"fc:ec:da:11:22:33","ip":"192.168.0.57","name":"Switch 16","model":"US16P150","type":"usw","serial":"FCECDA112233","version":"7.1.26","state":1,"adopted":true,"port_table":[]}]`
	staJSON = `[
		{"_id":"c1","mac":"00:00:00:00:00:01","ip":"192.168.0.101","hostname":"iphone","name":"Erikas iPhone","oui":"Apple","is_wired":false,"is_guest":false,"essid":"Office","ap_mac":"80:2a:a8:00:01:02","bssid":"80:2a:a8:00:01:02","signal":-61,"network":"LAN","network_id":"n1","vlan":0,"use_fixedip":false,"last_seen":1790496000},
		{"_id":"c2","mac":"00:11:32:aa:bb:cc","ip":"192.168.10.20","hostname":"nas01","oui":"Synology","is_wired":true,"sw_mac":"fc:ec:da:11:22:33","sw_port":7,"network":"IoT","network_id":"n2","use_fixedip":true,"fixed_ip":"192.168.10.20","last_seen":1790496100,"wired-tx_bytes":1}]`
	userJSON = `[
		{"_id":"c2","mac":"00:11:32:aa:bb:cc","name":"NAS Keller","oui":"Synology","use_fixedip":true,"fixed_ip":"192.168.10.20","network_id":"n2","last_seen":1790496100},
		{"_id":"c3","mac":"aa:bb:cc:00:00:03","hostname":"drucker","oui":"HP","use_fixedip":true,"fixed_ip":"192.168.10.30","network_id":"n2","noted":true,"name":"Drucker","last_seen":1790000000}]`
	netJSON = `[{"_id":"n1","name":"LAN","purpose":"corporate"},{"_id":"n2","name":"IoT","vlan":"10","vlan_enabled":true}]`
)

// controller emulates a UniFi OS console (unifiOS) or a classic controller.
func controller(t *testing.T, unifiOS bool, loggedOut *int) *httptest.Server {
	prefix := ""
	if unifiOS {
		prefix = "/proxy/network"
	}
	env := func(w http.ResponseWriter, data string) { fmt.Fprintf(w, `{"meta":{"rc":"ok"},"data":%s}`, data) }
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			if !unifiOS {
				http.Redirect(w, r, "/manage", http.StatusFound)
				return
			}
			_, _ = w.Write([]byte("<html>UniFi OS</html>"))
			return
		case r.URL.Path == "/api/auth/login" && unifiOS:
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["username"] != "netscope" || b["password"] != "pw" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("X-Csrf-Token", "csrf1")
			http.SetCookie(w, &http.Cookie{Name: "TOKEN", Value: "tok", Path: "/"})
			_, _ = w.Write([]byte(`{"unique_id":"x"}`))
			return
		case r.URL.Path == "/api/login" && !unifiOS:
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["username"] != "netscope" || b["password"] != "pw" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"meta":{"rc":"error","msg":"api.err.Invalid"},"data":[]}`))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "unifises", Value: "s", Path: "/"})
			env(w, `[]`)
			return
		case r.URL.Path == "/api/auth/logout" || r.URL.Path == "/api/logout":
			if unifiOS && r.Header.Get("X-Csrf-Token") != "csrf1" {
				t.Error("logout without CSRF token")
			}
			*loggedOut++
			return
		}
		name := "unifises"
		if unifiOS {
			name = "TOKEN"
		}
		if c, err := r.Cookie(name); err != nil || c.Value == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch strings.TrimPrefix(r.URL.Path, prefix) {
		case "/api/self/sites":
			env(w, `[{"name":"default","desc":"Default","_id":"s1"},{"name":"x7k2","desc":"Filiale","_id":"s2"}]`)
		case "/api/s/default/stat/device":
			env(w, devicesJSON)
		case "/api/s/default/rest/networkconf":
			env(w, netJSON)
		case "/api/s/default/stat/sta":
			env(w, staJSON)
		case "/api/s/default/rest/user":
			env(w, userJSON)
		default:
			http.NotFound(w, r)
		}
	}))
}

func run(t *testing.T, srv *httptest.Server, cred *plugin.Credential, settings map[string]any) (*plugintest.Sink, error) {
	s := map[string]any{"hosts": []any{srv.URL}}
	for k, v := range settings {
		s[k] = v
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, s)
	rc.Creds = plugintest.Creds{1: cred}
	return sink, p.Run(context.Background(), rc)
}

func password(u, pw string) *plugin.Credential {
	return &plugin.Credential{ID: 1, Type: plugin.CredPassword, Public: map[string]string{"username": u}, Secret: map[string]string{"password": pw}}
}

func check(t *testing.T, sink *plugintest.Sink) {
	t.Helper()
	obs := sink.All()
	var devices, clients []plugin.Observation
	for _, o := range obs {
		if o.DeviceType != "" {
			devices = append(devices, o)
		} else {
			clients = append(clients, o)
		}
	}
	if len(devices) != 2 || devices[0].DeviceType != "access-point" || devices[1].DeviceType != "switch" || devices[0].Vendor != "Ubiquiti" {
		t.Fatalf("devices %+v", devices)
	}
	// devices come first so that the relations of the clients resolve
	if obs[0].DeviceType == "" || obs[1].DeviceType == "" {
		t.Error("devices not observed first")
	}
	byMAC := map[string]plugin.Observation{}
	for _, c := range clients {
		byMAC[c.MACs[0]] = c
	}
	if len(byMAC) != 3 {
		t.Fatalf("clients %v", byMAC)
	}
	phone := byMAC["00:00:00:00:00:01"]
	inv := phone.Inventory.(netsrc.Inventory)
	if phone.Hostname != "Erikas iPhone" || inv.SSID != "Office" || inv.Uplink != "AP Flur" || *inv.Wired || !*inv.Online || inv.Extra["signal_dbm"] != "-61" ||
		len(phone.Relations) != 1 || phone.Relations[0].Kind != plugin.RelWireless || phone.Relations[0].Other.MAC != "80:2a:a8:00:01:02" {
		t.Errorf("phone %+v %+v", phone, inv)
	}
	nas := byMAC["00:11:32:aa:bb:cc"]
	ninv := nas.Inventory.(netsrc.Inventory)
	if nas.Hostname != "NAS Keller" || !ninv.Static || ninv.VLAN != 10 || ninv.Port != "7" || ninv.Uplink != "Switch 16" ||
		nas.Relations[0].Kind != plugin.RelSwitchPort || nas.Relations[0].RemotePort != "7" {
		t.Errorf("nas %+v %+v", nas, ninv)
	}
	printer := byMAC["aa:bb:cc:00:00:03"]
	pinv := printer.Inventory.(netsrc.Inventory)
	if printer.IP != "192.168.10.30" || printer.Hostname != "Drucker" || *pinv.Online || pinv.Interface != "IoT" || len(printer.Relations) != 0 {
		t.Errorf("offline printer %+v %+v", printer, pinv)
	}
}

func TestUniFiOS(t *testing.T) {
	out := 0
	srv := controller(t, true, &out)
	defer srv.Close()
	sink, err := run(t, srv, password("netscope", "pw"), nil)
	if err != nil {
		t.Fatal(err)
	}
	check(t, sink)
	if out != 1 {
		t.Errorf("logouts %d", out)
	}
	// only online clients
	sink, err = run(t, srv, password("netscope", "pw"), map[string]any{"include_offline": false, "include_devices": false})
	if err != nil || len(sink.All()) != 2 {
		t.Errorf("online only: %d %v", len(sink.All()), err)
	}
	if _, err := run(t, srv, password("netscope", "nope"), nil); err == nil || !strings.Contains(err.Error(), "abgelehnt") {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := run(t, srv, password("netscope", "pw"), map[string]any{"sites": []any{"gibtsnicht"}}); err == nil || !strings.Contains(err.Error(), "default, x7k2") {
		t.Errorf("unknown site: %v", err)
	}
}

func TestClassicController(t *testing.T) {
	out := 0
	srv := controller(t, false, &out)
	defer srv.Close()
	sink, err := run(t, srv, password("netscope", "pw"), nil)
	if err != nil {
		t.Fatal(err)
	}
	check(t, sink)
	if _, err := run(t, srv, password("netscope", "nope"), nil); err == nil {
		t.Error("wrong password accepted")
	}
}

func TestIntegrationAPI(t *testing.T) {
	var pages []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "key1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		pages = append(pages, r.URL.Path+"@"+strconv.Itoa(off))
		switch r.URL.Path {
		case "/proxy/network/integration/v1/sites":
			_, _ = w.Write([]byte(`{"offset":0,"limit":200,"count":1,"totalCount":1,"data":[{"id":"88f7af54-98f8-306a-a1c7-c9349722b1f6","internalReference":"default","name":"Default"}]}`))
		case "/proxy/network/integration/v1/sites/88f7af54-98f8-306a-a1c7-c9349722b1f6/devices":
			_, _ = w.Write([]byte(`{"offset":0,"limit":200,"count":1,"totalCount":1,"data":[{"id":"71cb254a","macAddress":"94:2a:6f:26:c6:ca","ipAddress":"192.168.1.55","name":"IW HD","model":"UHDIW","state":"ONLINE","features":["switching","accessPoint"]}]}`))
		case "/proxy/network/integration/v1/sites/88f7af54-98f8-306a-a1c7-c9349722b1f6/clients":
			if off == 0 {
				_, _ = w.Write([]byte(`{"offset":0,"limit":200,"count":2,"totalCount":3,"data":[
					{"type":"WIRED","id":"f95a","name":"truenas 05:8d","connectedAt":"2025-10-19T10:09:31Z","ipAddress":"172.16.10.19","macAddress":"80:af:ca:ad:05:8d","uplinkDeviceId":"71cb254a","access":{"type":"DEFAULT"}},
					{"type":"VPN","id":"0b6c","name":"alice-wg","connectedAt":"2025-10-19T11:00:00Z","ipAddress":"192.168.3.2","access":{"type":"DEFAULT"}}]}`))
			} else {
				_, _ = w.Write([]byte(`{"offset":2,"limit":200,"count":1,"totalCount":3,"data":[
					{"type":"WIRELESS","id":"aa11","name":"Gast","ipAddress":"192.168.50.9","macAddress":"de:ad:be:ef:00:01","uplinkDeviceId":"71cb254a","access":{"type":"GUEST","authorized":true}}]}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	sink, err := run(t, srv, &plugin.Credential{ID: 1, Type: plugin.CredAPIToken, Secret: map[string]string{"token": "key1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 3 || obs[0].DeviceType != "access-point" {
		t.Fatalf("observations %+v", obs)
	}
	nas, guest := obs[1], obs[2]
	if nas.Hostname != "truenas 05:8d" || nas.Relations[0].Other.MAC != "94:2a:6f:26:c6:ca" || nas.Relations[0].Kind != plugin.RelSwitchPort {
		t.Errorf("wired %+v", nas)
	}
	if guest.Inventory.(netsrc.Inventory).Extra["gast"] != "ja" || guest.Relations[0].Kind != plugin.RelWireless {
		t.Errorf("guest %+v", guest)
	}
	if !strings.Contains(strings.Join(pages, " "), "clients@2") {
		t.Errorf("paging %v", pages)
	}
}

// The connection test signs in and reads like a run (and signs out again), but stores
// nothing; it reports a wrong password and a wrong address per controller.
func TestUniFiConnectionTest(t *testing.T) {
	out := 0
	srv := controller(t, true, &out)
	t.Cleanup(srv.Close)
	creds := func(pw string) plugintest.Creds { return plugintest.Creds{1: password("netscope", pw)} }
	p := &Plugin{}
	res, err := plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL}}, creds("pw"), "")
	if err != nil || len(res) != 1 || !res[0].OK || res[0].Target != srv.URL || !strings.Contains(res[0].Message, "3 Clients und 2 Netzwerkgeräte") {
		t.Fatalf("valid password: %+v, %v", res, err)
	}
	if out != 1 {
		t.Errorf("logouts %d", out)
	}
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL}}, creds("nope"), "")
	if err != nil || len(res) != 1 || res[0].OK || !strings.Contains(res[0].Message, "Anmeldung abgelehnt") {
		t.Fatalf("wrong password: %+v, %v", res, err)
	}
	closed := plugintest.ClosedURL(t, "https")
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL, closed}}, creds("pw"), "")
	if err != nil || len(res) != 2 || !res[0].OK || res[1].OK || res[1].Target != closed || !strings.Contains(res[1].Message, "nicht erreichbar") {
		t.Fatalf("wrong address: %+v, %v", res, err)
	}
	// classic controller
	classic := controller(t, false, &out)
	t.Cleanup(classic.Close)
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{classic.URL}}, creds("pw"), "")
	if err != nil || len(res) != 1 || !res[0].OK || !strings.Contains(res[0].Message, "3 Clients und 2 Netzwerkgeräte") {
		t.Fatalf("classic, valid password: %+v, %v", res, err)
	}
	// the classic controller rejects the login with HTTP 400 and api.err.Invalid
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{classic.URL}}, creds("nope"), "")
	if err != nil || len(res) != 1 || res[0].OK || !strings.Contains(res[0].Message, "Anmeldung abgelehnt: api.err.Invalid") {
		t.Fatalf("classic, wrong password: %+v, %v", res, err)
	}
}
