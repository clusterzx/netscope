package mikrotik

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

var now = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func router(t *testing.T) (*httptest.Server, map[string]string) {
	t.Helper()
	queries := map[string]string{}
	routes := map[string]string{
		"/rest/ip/dhcp-server": `[{".id":"*1","name":"defconf","interface":"bridge"},{".id":"*2","name":"iot","interface":"vlan10"}]`,
		"/rest/ip/dhcp-server/lease": `[
			{".id":"*1A","active-address":"192.168.88.254","active-mac-address":"3C:22:FB:12:34:56","address":"192.168.88.254","dynamic":"true","expires-after":"8m51s","host-name":"MacBook-Pro","last-seen":"1m9s","mac-address":"3C:22:FB:12:34:56","server":"defconf","status":"bound","disabled":"false","blocked":"false"},
			{".id":"*5","address":"192.168.88.20","comment":"NAS im Keller","dynamic":"false","disabled":"false","mac-address":"00:11:32:AA:BB:CC","server":"defconf","status":"waiting","last-seen":"never"},
			{".id":"*6","address":"10.10.0.50","active-address":"10.10.0.50","active-mac-address":"44:55:66:77:88:99","dynamic":"false","comment":"Kamera","mac-address":"44:55:66:77:88:99","server":"iot","status":"bound","expires-after":"1d2h","last-seen":"00:05:00"},
			{".id":"*7","address":"192.168.88.99","dynamic":"true","mac-address":"AA:BB:CC:00:00:07","server":"defconf","status":"offered"},
			{".id":"*8","address":"192.168.88.98","dynamic":"false","disabled":"true","mac-address":"AA:BB:CC:00:00:08","server":"defconf","status":"waiting"}]`,
		"/rest/ip/arp": `[
			{".id":"*3","address":"192.168.88.254","complete":"true","dynamic":"true","interface":"bridge","mac-address":"3C:22:FB:12:34:56","status":"reachable","disabled":"false","invalid":"false","published":"false"},
			{".id":"*4","address":"192.168.88.30","complete":"true","dynamic":"true","interface":"bridge","mac-address":"D8:3A:DD:00:00:01","status":"stale","disabled":"false","invalid":"false","published":"false"},
			{".id":"*9","address":"192.168.88.40","complete":"false","dynamic":"true","interface":"bridge","status":"failed"}]`,
		"/rest/interface/bridge/host": `[
			{".id":"*9","bridge":"bridge","dynamic":"true","external":"false","invalid":"false","local":"false","mac-address":"3C:22:FB:12:34:56","on-interface":"ether2"},
			{".id":"*A","bridge":"bridge","local":"true","mac-address":"48:8F:5A:00:00:01","on-interface":"bridge"},
			{".id":"*B","bridge":"bridge","local":"false","external":"false","mac-address":"44:55:66:77:88:99","on-interface":"ether5","vid":"10"}]`,
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "netscope" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":401,"message":"Unauthorized"}`))
			return
		}
		queries[r.URL.Path] = r.URL.Query().Get(".proplist")
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":404,"message":"Not Found"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, queries
}

func TestMikroTik(t *testing.T) {
	srv, queries := router(t)
	p := &Plugin{now: func() time.Time { return now }}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"hosts": []any{srv.URL}})
	rc.Creds = plugintest.Creds{1: {ID: 1, Type: plugin.CredPassword, Public: map[string]string{"username": "netscope"}, Secret: map[string]string{"password": "pw"}}}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queries["/rest/ip/dhcp-server/lease"], "host-name") || strings.Contains(queries["/rest/ip/dhcp-server/lease"], "class-id") {
		t.Errorf("lease proplist %q", queries["/rest/ip/dhcp-server/lease"])
	}
	byIP := map[string]plugin.Observation{}
	for _, o := range sink.All() {
		byIP[o.IP] = o
	}
	if len(byIP) != 4 {
		t.Fatalf("observations %v", byIP)
	}
	mb := byIP["192.168.88.254"]
	inv := mb.Inventory.(netsrc.Inventory)
	if mb.Hostname != "MacBook-Pro" || inv.Interface != "bridge" || inv.Port != "ether2" || inv.Expires == nil ||
		!inv.Expires.Equal(now.Add(8*time.Minute+51*time.Second)) || !inv.LastSeen.Equal(now) || len(mb.Relations) != 1 || // reachable in ARP = now
		mb.Relations[0].Kind != plugin.RelSwitchPort || mb.Relations[0].RemotePort != "ether2" || mb.Relations[0].Other.IP != "127.0.0.1" {
		t.Errorf("macbook %+v %+v", mb, inv)
	}
	nas := byIP["192.168.88.20"]
	if nas.Hostname != "NAS im Keller" || !nas.Inventory.(netsrc.Inventory).Static || nas.Inventory.(netsrc.Inventory).Kind != netsrc.KindStatic {
		t.Errorf("static waiting %+v", nas)
	}
	cam := byIP["10.10.0.50"].Inventory.(netsrc.Inventory)
	if cam.Name != "Kamera" || cam.Interface != "vlan10" || cam.VLAN != 10 || !cam.Expires.Equal(now.Add(26*time.Hour)) || !cam.LastSeen.Equal(now.Add(-5*time.Minute)) {
		t.Errorf("camera %+v", cam)
	}
	if arp := byIP["192.168.88.30"]; arp.Inventory.(netsrc.Inventory).Kind != netsrc.KindARP {
		t.Errorf("arp only %+v", arp)
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{"8m51s": 8*time.Minute + 51*time.Second, "1w2d3h4m5s": 9*24*time.Hour + 3*time.Hour + 4*time.Minute + 5*time.Second,
		"00:05:00": 5 * time.Minute, "30s": 30 * time.Second, "1d": 24 * time.Hour} {
		if got, ok := parseDuration(in); !ok || got != want {
			t.Errorf("%q = %v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "never", "soon"} {
		if _, ok := parseDuration(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

// The connection test reads like a run, but stores nothing; it reports a wrong password
// and a wrong address per router.
func TestMikroTikConnectionTest(t *testing.T) {
	srv, _ := router(t)
	creds := func(pw string) plugintest.Creds {
		return plugintest.Creds{1: {ID: 1, Type: plugin.CredPassword, Public: map[string]string{"username": "netscope"}, Secret: map[string]string{"password": pw}}}
	}
	p := &Plugin{now: func() time.Time { return now }}
	res, err := plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL}}, creds("pw"), "")
	if err != nil || len(res) != 1 || !res[0].OK || res[0].Target != srv.URL || !strings.Contains(res[0].Message, "4 Clients") {
		t.Fatalf("valid password: %+v, %v", res, err)
	}
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL}}, creds("wrong"), "")
	if err != nil || len(res) != 1 || res[0].OK || !strings.Contains(res[0].Message, "Anmeldung abgelehnt") {
		t.Fatalf("wrong password: %+v, %v", res, err)
	}
	closed := plugintest.ClosedURL(t, "https")
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL, closed}}, creds("pw"), "")
	if err != nil || len(res) != 2 || !res[0].OK || res[1].OK || res[1].Target != closed || res[1].Message == "" {
		t.Fatalf("wrong address: %+v, %v", res, err)
	}
}
