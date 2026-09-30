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

// The connection test signs in and reads the reservations like a run, but stores nothing;
// it reports a wrong password and a wrong address per firewall.
func TestSophosConnectionTest(t *testing.T) {
	srv := firewall(t)
	t.Cleanup(srv.Close)
	creds := func(pw string) plugintest.Creds {
		return plugintest.Creds{1: {ID: 1, Type: plugin.CredPassword, Public: map[string]string{"username": "api"}, Secret: map[string]string{"password": pw}}}
	}
	p := &Plugin{}
	res, err := plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL}}, creds("pw&<x"), "")
	if err != nil || len(res) != 1 || !res[0].OK || res[0].Target != srv.URL || !strings.Contains(res[0].Message, "2 Clients") {
		t.Fatalf("valid password: %+v, %v", res, err)
	}
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL}}, creds("wrong"), "")
	if err != nil || len(res) != 1 || res[0].OK || !strings.Contains(res[0].Message, "Anmeldung abgelehnt") {
		t.Fatalf("wrong password: %+v, %v", res, err)
	}
	closed := plugintest.ClosedURL(t, "https")
	res, err = plugintest.ConnectionTest(t, p, map[string]any{"hosts": []any{srv.URL, closed}}, creds("pw&<x"), "")
	if err != nil || len(res) != 2 || !res[0].OK || res[1].OK || res[1].Target != closed || res[1].Message == "" {
		t.Fatalf("wrong address: %+v, %v", res, err)
	}
}
