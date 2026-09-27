package fritzbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/plugins/netsrc"
)

const hostListXML = `<?xml version="1.0" ?>
<List><!-- devicehosts :3 -->
<Item><Index>1</Index><IPAddress>192.168.178.24</IPAddress><MACAddress>2A:B3:66:30:08:87</MACAddress><Active>0</Active><HostName>PC-192-168-178-24</HostName><InterfaceType/><X_AVM-DE_Port>0</X_AVM-DE_Port><X_AVM-DE_Speed>0</X_AVM-DE_Speed><X_AVM-DE_Guest>0</X_AVM-DE_Guest><X_AVM-DE_DeviceClass>Generic</X_AVM-DE_DeviceClass><X_AVM-DE_FriendlyName>PC-192-168-178-24</X_AVM-DE_FriendlyName></Item>
<Item><Index>2</Index><IPAddress>192.168.178.23</IPAddress><MACAddress>9C:20:7B:E7:FF:5F</MACAddress><Active>1</Active><HostName>Apple-TV</HostName><InterfaceType>Ethernet</InterfaceType><X_AVM-DE_Port>2</X_AVM-DE_Port><X_AVM-DE_Speed>1000</X_AVM-DE_Speed><X_AVM-DE_Guest>0</X_AVM-DE_Guest><X_AVM-DE_Model></X_AVM-DE_Model><X_AVM-DE_DeviceClass>Generic</X_AVM-DE_DeviceClass><X_AVM-DE_DeviceClassUser>MediaPlayer</X_AVM-DE_DeviceClassUser><X_AVM-DE_FriendlyName>Wohnzimmer TV</X_AVM-DE_FriendlyName><X_AVM-DE_NewField>future</X_AVM-DE_NewField></Item>
<Item><Index>3</Index><IPAddress>192.168.179.10</IPAddress><MACAddress>DE:AD:BE:EF:00:01</MACAddress><Active>1</Active><HostName>Gast-Handy</HostName><InterfaceType>802.11</InterfaceType><X_AVM-DE_Guest>1</X_AVM-DE_Guest><X_AVM-DE_FriendlyName>Gast-Handy</X_AVM-DE_FriendlyName></Item>
</List>`

type fakeBox struct {
	listRight bool // user may read the host list (else 606)
	soapCalls []string
}

var actionRe = regexp.MustCompile(`#([A-Za-z_-]+)"`)

func (f *fakeBox) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/devicehostlist.lua" {
			if r.URL.Query().Get("sid") != "s1d" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(hostListXML))
			return
		}
		if r.URL.Path != hostsURL {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), `Digest username="fritz1234"`) {
			w.Header().Set("WWW-Authenticate", `Digest realm="F!Box SOAP-Auth", nonce="n1", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		m := actionRe.FindStringSubmatch(r.Header.Get("SOAPACTION"))
		body, _ := io.ReadAll(r.Body)
		action := m[1]
		f.soapCalls = append(f.soapCalls, action)
		resp := func(inner string) {
			fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body></s:Envelope>`,
				action, hostsURN, inner, action)
		}
		switch action {
		case "X_AVM-DE_GetHostListPath":
			if !f.listRight {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`<s:Envelope><s:Body><s:Fault><detail><UPnPError><errorCode>606</errorCode><errorDescription>Action Not Authorized</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`))
				return
			}
			resp(`<NewX_AVM-DE_HostListPath>/devicehostlist.lua?sid=s1d</NewX_AVM-DE_HostListPath>`)
		case "GetHostNumberOfEntries":
			resp(`<NewHostNumberOfEntries>2</NewHostNumberOfEntries>`)
		case "GetGenericHostEntry":
			switch {
			case strings.Contains(string(body), "<NewIndex>0</NewIndex>"):
				resp(`<NewIPAddress>192.168.178.23</NewIPAddress><NewAddressSource>DHCP</NewAddressSource><NewLeaseTimeRemaining>864000</NewLeaseTimeRemaining><NewMACAddress>9C:20:7B:E7:FF:5F</NewMACAddress><NewInterfaceType>Ethernet</NewInterfaceType><NewActive>1</NewActive><NewHostName>Apple-TV</NewHostName>`)
			default:
				resp(`<NewIPAddress>192.168.178.2</NewIPAddress><NewAddressSource>Static</NewAddressSource><NewLeaseTimeRemaining>0</NewLeaseTimeRemaining><NewMACAddress>00:11:32:AA:BB:CC</NewMACAddress><NewInterfaceType>Ethernet</NewInterfaceType><NewActive>0</NewActive><NewHostName>nas</NewHostName>`)
			}
		default:
			t.Errorf("unexpected action %s", action)
		}
	})
}

func runBox(t *testing.T, f *fakeBox, settings map[string]any) *plugintest.Sink {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	s := map[string]any{"hosts": []any{srv.URL}}
	for k, v := range settings {
		s[k] = v
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, s)
	rc.Creds = plugintest.Creds{1: {ID: 1, Type: plugin.CredPassword, Public: map[string]string{"username": "fritz1234"}, Secret: map[string]string{"password": "x"}}}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	return sink
}

func TestFritzBoxHostList(t *testing.T) {
	f := &fakeBox{listRight: true}
	sink := runBox(t, f, nil)
	got := map[string]plugin.Observation{}
	for _, o := range sink.All() {
		got[o.IP] = o
	}
	if len(got) != 3 {
		t.Fatalf("observations %v", got)
	}
	tv := got["192.168.178.23"]
	inv := tv.Inventory.(netsrc.Inventory)
	if tv.Hostname != "Wohnzimmer TV" || inv.Port != "LAN 2" || inv.Wired == nil || !*inv.Wired || inv.Online == nil || !*inv.Online ||
		inv.Extra["geraeteklasse"] != "MediaPlayer" || inv.Extra["speed_mbit"] != "1000" {
		t.Errorf("tv %+v %+v", tv, inv)
	}
	if pc := got["192.168.178.24"]; pc.Hostname != "" || *pc.Inventory.(netsrc.Inventory).Online {
		t.Errorf("generated name kept: %+v", pc)
	}
	if g := got["192.168.179.10"].Inventory.(netsrc.Inventory); g.Extra["gast"] != "ja" || *g.Wired {
		t.Errorf("guest %+v", g)
	}
	// only connected devices
	sink = runBox(t, &fakeBox{listRight: true}, map[string]any{"include_inactive": false})
	if n := len(sink.All()); n != 2 {
		t.Errorf("active only: %d", n)
	}
}

func TestFritzBoxFallback(t *testing.T) {
	f := &fakeBox{}
	sink := runBox(t, f, nil)
	got := map[string]plugin.Observation{}
	for _, o := range sink.All() {
		got[o.IP] = o
	}
	if len(got) != 2 || got["192.168.178.23"].Inventory.(netsrc.Inventory).Expires == nil || !got["192.168.178.2"].Inventory.(netsrc.Inventory).Static {
		t.Fatalf("fallback %+v", got)
	}
	if strings.Join(f.soapCalls, ",") != "X_AVM-DE_GetHostListPath,GetHostNumberOfEntries,GetGenericHostEntry,GetGenericHostEntry" {
		t.Errorf("calls %v", f.soapCalls)
	}
}

func TestBase(t *testing.T) {
	for in, want := range map[string]string{"fritz.box": "http://fritz.box:49000", "https://192.168.178.1": "https://192.168.178.1:49443",
		"http://fritz.box:8080": "http://fritz.box:8080"} {
		if got, err := base(in); err != nil || got != want {
			t.Errorf("%q = %q %v", in, got, err)
		}
	}
}
