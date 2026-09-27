package meraki

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

func TestMeraki(t *testing.T) {
	var srv *httptest.Server
	throttled := false
	var waited []time.Duration
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errors":["Invalid API key"]}`))
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/api/v1")
		switch {
		case p == "/organizations" && r.URL.Query().Get("shard") == "":
			// the API sends clients to a shard host; the key must survive the redirect
			http.Redirect(w, r, srv.URL+"/api/v1/organizations?shard=1&"+r.URL.RawQuery, http.StatusFound)
		case p == "/organizations":
			_, _ = w.Write([]byte(`[{"id":"549236","name":"Firma GmbH"},{"id":"999","name":"Andere"}]`))
		case p == "/organizations/549236/networks":
			_, _ = w.Write([]byte(`[{"id":"N_1","name":"Zentrale","productTypes":["appliance","switch","wireless"]},{"id":"N_2","name":"Kamera","productTypes":["camera"]}]`))
		case p == "/organizations/549236/devices":
			_, _ = w.Write([]byte(`[{"name":"AP Empfang","serial":"Q234-ABCD-5678","mac":"00:18:0a:00:00:01","lanIp":"10.0.0.2","model":"MR34","networkId":"N_1","productType":"wireless","firmware":"wireless-25-14"},
				{"name":"Core","serial":"Q234-SWIT-0001","mac":"00:18:0a:00:00:02","lanIp":"10.0.0.3","model":"MS120-8","networkId":"N_1","productType":"switch"}]`))
		case p == "/networks/N_1/clients":
			if r.URL.Query().Get("timespan") != "86400" {
				t.Errorf("timespan %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("startingAfter") == "" {
				if !throttled {
					throttled = true
					w.Header().Set("Retry-After", "2")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"errors":["API rate limit exceeded for organization"]}`))
					return
				}
				w.Header().Set("Link", `<`+srv.URL+`/api/v1/networks/N_1/clients?perPage=1000&timespan=86400&startingAfter=k74272e>; rel=next`)
				_, _ = w.Write([]byte(`[{"id":"k74272e","mac":"22:33:44:55:66:77","ip":"10.0.0.50","description":"Miles's phone","firstSeen":1518365681,"lastSeen":1790496000,"manufacturer":"Apple","os":"iOS","user":"milesmeraki","vlan":"100","namedVlan":"Mitarbeiter","ssid":"Firma","switchport":null,"recentDeviceSerial":"Q234-ABCD-5678","recentDeviceName":"AP Empfang","recentDeviceConnection":"Wireless","status":"Online"}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":"k8","mac":"22:33:44:55:66:88","ip":"10.0.0.60","description":null,"firstSeen":"2024-07-09T07:20:42Z","lastSeen":"2026-09-26T10:00:00Z","manufacturer":"Dell","os":"Windows 11","vlan":100,"switchport":"4","recentDeviceSerial":"Q234-SWIT-0001","recentDeviceConnection":"Wired","status":"Offline"}]`))
		case p == "/networks/N_2/clients":
			_, _ = w.Write([]byte(`[]`))
		case p == "/networks/N_1/appliance/vlans":
			_, _ = w.Write([]byte(`[{"id":100,"name":"Mitarbeiter","fixedIpAssignments":{"aa:bb:cc:00:00:10":{"ip":"10.0.0.10","name":"Drucker 1. OG"}}}]`))
		default:
			t.Errorf("unexpected %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := &Plugin{insecureTLS: true, sleep: func(ctx context.Context, d time.Duration) error { waited = append(waited, d); return nil }}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"api_url": srv.URL + "/api/v1", "organizations": []any{"Firma GmbH"}})
	rc.Creds = plugintest.Creds{1: {ID: 1, Type: plugin.CredAPIToken, Secret: map[string]string{"token": "key1"}}}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if len(waited) != 1 || waited[0] != 2*time.Second {
		t.Errorf("rate limit waits %v", waited)
	}
	var devices []plugin.Observation
	byIP := map[string]plugin.Observation{}
	for _, o := range sink.All() {
		if o.DeviceType != "" {
			devices = append(devices, o)
			continue
		}
		byIP[o.IP] = o
	}
	if len(devices) != 2 || devices[0].DeviceType != "access-point" || devices[1].DeviceType != "switch" || devices[0].Vendor != "Cisco Meraki" {
		t.Fatalf("devices %+v", devices)
	}
	if len(byIP) != 3 {
		t.Fatalf("clients %v", byIP)
	}
	phone := byIP["10.0.0.50"]
	inv := phone.Inventory.(netsrc.Inventory)
	if phone.Hostname != "Miles's phone" || phone.OS.Name != "iOS" || inv.VLAN != 100 || inv.Interface != "Mitarbeiter" || inv.SSID != "Firma" ||
		!*inv.Online || phone.Relations[0].Kind != plugin.RelWireless || phone.Relations[0].Other.MAC != "00:18:0a:00:00:01" || inv.Extra["benutzer"] != "milesmeraki" {
		t.Errorf("phone %+v %+v", phone, inv)
	}
	pc := byIP["10.0.0.60"]
	pinv := pc.Inventory.(netsrc.Inventory)
	if *pinv.Online || pinv.Port != "4" || pinv.VLAN != 100 || pc.Relations[0].Kind != plugin.RelSwitchPort || pinv.LastSeen == nil ||
		!pinv.LastSeen.Equal(time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("pc %+v %+v", pc, pinv)
	}
	if pr := byIP["10.0.0.10"]; pr.Hostname != "Drucker 1. OG" || !pr.Inventory.(netsrc.Inventory).Static {
		t.Errorf("fixed ip %+v", pr)
	}
	// unknown organisation
	rc2, _, _ := plugintest.RunContext(t, p, map[string]any{"api_url": srv.URL + "/api/v1", "organizations": []any{"Gibtsnicht"}})
	rc2.Creds = rc.Creds
	if err := p.Run(context.Background(), rc2); err == nil || !strings.Contains(err.Error(), "Firma GmbH, Andere") {
		t.Errorf("unknown org: %v", err)
	}
}

func TestSameSiteRedirect(t *testing.T) {
	check := sameSite("https://api.meraki.com/api/v1")
	orig, _ := http.NewRequest(http.MethodGet, "https://api.meraki.com/api/v1/organizations", nil)
	orig.Header.Set("Authorization", "Bearer key1")
	for target, keep := range map[string]bool{
		"https://n392.meraki.com/api/v1/organizations": true,
		"https://api.meraki.com/api/v1/x":              true,
		"https://evil.example/api":                     false,
		"http://n392.meraki.com/api":                   false,
		"https://meraki.com.evil.example/":             false,
	} {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
		if err := check(req, []*http.Request{orig}); err != nil {
			t.Fatal(err)
		}
		if got := req.Header.Get("Authorization") != ""; got != keep {
			t.Errorf("%s: header kept = %v", target, got)
		}
	}
}
