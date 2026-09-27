package sophos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/plugins/netsrc"
)

const okResponse = `<?xml version="1.0" encoding="UTF-8"?>
<Response APIVersion="1900.1" IPS_CAT_VER="0">
 <Login><status>Authentication Successful</status></Login>
 <DHCPServer transactionid="">
  <Name>DHCP1</Name><Status>1</Status><Interface>Port1.10</Interface>
  <IPLease><IP>192.168.224.10-192.168.224.20</IP></IPLease>
  <StaticLease>
   <Lease><HostName>printer</HostName><MACAddress>00:11:22:33:44:55</MACAddress><IPAddress>192.168.224.5</IPAddress></Lease>
   <Lease><HostName>nas</HostName><MACAddress>00:11:22:33:44:66</MACAddress><IPAddress>192.168.224.6</IPAddress></Lease>
  </StaticLease>
  <SubnetMask>255.255.255.0</SubnetMask><DefaultLeaseTime>1440</DefaultLeaseTime>
 </DHCPServer>
 <DHCPServer transactionid=""><Name>DHCP2</Name><Interface>Port2</Interface></DHCPServer>
</Response>`

func firewall(t *testing.T) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webconsole/APIController" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		req := r.FormValue("reqxml")
		switch {
		case strings.Contains(req, "<Username>blocked</Username>"):
			_, _ = w.Write([]byte(`<Response APIVersion="1900.1"><Status code="534">API operations are not allowed from the requester IP address.</Status></Response>`))
		case !strings.Contains(req, "<Password>pw&amp;&lt;x</Password>"):
			_, _ = w.Write([]byte(`<Response APIVersion="1900.1"><Login><status>Authentication Failure</status></Login></Response>`))
		case !strings.Contains(req, "<Get><DHCPServer/></Get>"):
			t.Errorf("request %s", req)
		default:
			_, _ = w.Write([]byte(okResponse))
		}
	}))
}

func runSophos(t *testing.T, srv *httptest.Server, user, pw string) (*plugintest.Sink, error) {
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"hosts": []any{srv.URL}})
	rc.Creds = plugintest.Creds{1: {ID: 1, Type: plugin.CredPassword, Public: map[string]string{"username": user}, Secret: map[string]string{"password": pw}}}
	return sink, p.Run(context.Background(), rc)
}

func TestSophos(t *testing.T) {
	srv := firewall(t)
	defer srv.Close()
	sink, err := runSophos(t, srv, "api", "pw&<x") // the password is XML-escaped
	if err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 2 {
		t.Fatalf("observations %+v", obs)
	}
	inv := obs[0].Inventory.(netsrc.Inventory)
	if obs[0].IP != "192.168.224.5" || obs[0].Hostname != "printer" || !inv.Static || inv.Interface != "Port1.10" || inv.Extra["dhcp_server"] != "DHCP1" {
		t.Errorf("reservation %+v %+v", obs[0], inv)
	}
	if _, err := runSophos(t, srv, "api", "wrong"); err == nil || !strings.Contains(err.Error(), "abgelehnt") {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := runSophos(t, srv, "blocked", "x"); err == nil || !strings.Contains(err.Error(), "nicht zugelassen") {
		t.Errorf("ip not allowed: %v", err)
	}
}
