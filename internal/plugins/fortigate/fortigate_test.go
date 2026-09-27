package fortigate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/plugins/netsrc"
)

var now = time.Unix(1790500000, 0).UTC()

func TestFortiGate(t *testing.T) {
	vdoms := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		vdoms[r.URL.Query().Get("vdom")]++
		switch r.URL.Path {
		case "/api/v2/monitor/system/dhcp":
			_, _ = w.Write([]byte(`{"http_method":"GET","results":[
				{"ip":"192.168.1.102","reserved":true,"mac":"e8:1c:ba:86:eb:66","vci":"FortiSwitch-108E-POE","hostname":"S108EP5918010897","expire_time":1790509000,"status":"leased","interface":"port5","type":"ipv4","server_mkey":3},
				{"ip":"192.168.1.110","reserved":false,"mac":"3c:22:fb:12:34:56","hostname":"MacBook-Pro","expire_time":1790505000,"status":"leased","interface":"internal","type":"ipv4","ssid":"Office","access_point":"FP231FTF20000001"},
				{"ip":"192.168.1.111","mac":"aa:bb:cc:00:00:01","status":"conflicted","type":"ipv4"}],
				"vdom":"root","path":"system","name":"dhcp","status":"success","serial":"FG100FTK00000000","version":"v7.4.4","build":2662}`))
		case "/api/v2/monitor/network/arp":
			_, _ = w.Write([]byte(`{"results":[{"ip":"10.0.10.23","age":30,"mac":"00:11:22:33:44:55","interface":"internal1"}],"status":"success"}`))
		case "/api/v2/monitor/user/device/query":
			if r.URL.Query().Get("start") != "0" {
				_, _ = w.Write([]byte(`{"results":[],"status":"success","total":1}`))
				return
			}
			_, _ = w.Write([]byte(`{"http_method":"GET","results":[
				{"ipv4_address":"10.0.10.23","mac":"00:11:22:33:44:55","hardware_vendor":"Dell","hardware_type":"Server","os_name":"Windows","os_version":"Server 2022","hostname":"DC01","last_seen":"1790499000","is_online":"true","detected_interface":"internal1","fortiswitch_name":"FSW-Core","fortiswitch_port_name":"port12","fortiswitch_vlan_id":20}],
				"vdom":"root","query_type":"memory","count":1,"total":1,"start":0,"number":1000,"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := &Plugin{now: func() time.Time { return now }}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"hosts": []any{srv.URL}, "vdoms": []any{"root"}})
	rc.Creds = plugintest.Creds{1: {ID: 1, Type: plugin.CredAPIToken, Secret: map[string]string{"token": "tok"}}}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if vdoms["root"] != 3 || vdoms[""] != 0 {
		t.Errorf("vdom parameter %v", vdoms)
	}
	byIP := map[string]plugin.Observation{}
	for _, o := range sink.All() {
		byIP[o.IP] = o
	}
	if len(byIP) != 3 {
		t.Fatalf("observations %v", byIP)
	}
	sw := byIP["192.168.1.102"].Inventory.(netsrc.Inventory)
	if !sw.Static || sw.Extra["dhcp_vci"] != "FortiSwitch-108E-POE" || sw.Interface != "port5" {
		t.Errorf("reserved %+v", sw)
	}
	mb := byIP["192.168.1.110"].Inventory.(netsrc.Inventory)
	if mb.SSID != "Office" || mb.Extra["fortiap"] != "FP231FTF20000001" || mb.Expires == nil {
		t.Errorf("wifi lease %+v", mb)
	}
	dc := byIP["10.0.10.23"]
	dinv := dc.Inventory.(netsrc.Inventory)
	if dc.Hostname != "DC01" || dc.OS == nil || dc.OS.Name != "Windows Server 2022" || dinv.Uplink != "FSW-Core" || dinv.Port != "port12" ||
		dinv.VLAN != 20 || !*dinv.Online || dc.Vendor != "Dell" || !dinv.LastSeen.Equal(time.Unix(1790499970, 0).UTC()) {
		t.Errorf("detected device %+v %+v", dc, dinv)
	}
}
