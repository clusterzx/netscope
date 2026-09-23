package upnp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func TestParseSSDP(t *testing.T) {
	cases := []struct {
		file                  string
		location, server, usn string
	}{
		{"ssdp-192.168.8.98-1.txt", "http://192.168.8.98:1900/igd.xml", "TPOS/V1.0.0 UPnP/1.0 Archer C80/2.20",
			"uuid:upnp-InternetGatewayDevice-05BC0B05BBC1::upnp:rootdevice"},
		{"ssdp-192.168.8.44-10.txt", "http://192.168.8.44:49000/igddesc.xml", "FRITZ!Box 7590 UPnP/1.0 AVM FRITZ!Box 7590 154.08.25", ""},
		{"ssdp-192.168.8.31-1.txt", "http://192.168.8.31:50000/", "Linux/2.6 UPnp/1.0 AMI-Stack/1.0",
			"uuid:a09d156b-debf-0010-e603-0011223344e4::upnp:rootdevice"},
		// vendor specific header (hue-bridgeid) and HOST header in a response
		{"ssdp-192.168.8.75-1.txt", "http://192.168.8.75:80/description.xml", "Hue/1.0 UPnP/1.0 IpBridge/1.78.0",
			"uuid:2f402f80-da50-11e1-9b23-ecb5fabcc753::upnp:rootdevice"},
		// lower-case header names ("Usn:", "St:")
		{"ssdp-192.168.8.178-3.txt", "http://192.168.8.178:36762/dev/bb439eec-5188-f343-0000-000038b27980/desc.xml",
			"Linux/4.4.120 UPnP/1.0 Teleal-Cling/1.0", "uuid:bb439eec-5188-f343-0000-000038b27980::urn:schemas-upnp-org:device:MediaRenderer:1"},
	}
	for _, c := range cases {
		r, ok := parseSSDP(plugintest.Fixture(t, c.file))
		if !ok || r.Location != c.location || r.Server != c.server || (c.usn != "" && r.USN != c.usn) {
			t.Errorf("%s: %+v ok=%v", c.file, r, ok)
		}
	}
	if r, _ := parseSSDP(plugintest.Fixture(t, "ssdp-192.168.8.178-3.txt")); r.ST != "urn:schemas-upnp-org:device:MediaRenderer:1" {
		t.Errorf("st: %q", r.ST)
	}
	for _, bad := range []string{"", "NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nNT: upnp:rootdevice\r\n\r\n",
		"M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\n\r\n", "HTTP/1.1 404 Not Found\r\nLocation: http://x/\r\n\r\n"} {
		if _, ok := parseSSDP([]byte(bad)); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func parseFixture(t *testing.T, name string) *Device {
	t.Helper()
	d, err := parseDescription(plugintest.Fixture(t, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return d
}

func TestParseDescriptions(t *testing.T) {
	cases := []struct {
		file, friendly, vendor, model, devType, udn string
	}{
		{"desc-192.168.8.98-igd.xml", "Archer C80 AC1900 MU-MIMO Wi-Fi Router", "TP-Link", "Archer C80 2.20", "router",
			"uuid:upnp-InternetGatewayDevice-05BC0B05BBC1"},
		{"desc-192.168.8.44-igd.xml", "FRITZ!Box 7590", "FRITZ! GmbH", "FRITZ!Box 7590", "router", "uuid:75802409-bccb-40e7-8e6c-DC15C822C6B3"},
		{"desc-192.168.8.44-mediaserver.xml", "AVM FRITZ!Mediaserver", "FRITZ! GmbH", "FRITZ!Box 7590", "media-player",
			"uuid:fa095ecc-e13e-40e7-8e6c-DC15C822C6B3"},
		{"desc-192.168.8.44-fritzbox.xml", "FRITZ!Box 7590", "FRITZ! GmbH", "FRITZ!Box 7590", "", "uuid:123402409-bccb-40e7-8e6c-DC15C822C6B3"},
		{"desc-192.168.8.31-bmc.xml", "BMC0011223344E4", "American Megatrends Inc", "AMI SPX 1.0.0", "", "uuid:a09d156b-debf-0010-e603-0011223344e4"},
		{"desc-192.168.8.178-renderer.xml", "AFTMM@ES(192.168.8.88)", "ES", "ES File Explorer V1", "media-player",
			"uuid:bb439eec-5188-f343-0000-000038b27980"},
		// "Basic" device type: no device type guess
		{"desc-192.168.8.75-hue.xml", "Hue Bridge neu (192.168.8.75)", "Signify", "Philips hue bridge 2015 BSB002", "",
			"uuid:2f402f80-da50-11e1-9b23-ecb5fabcc753"},
	}
	for _, c := range cases {
		d := parseFixture(t, c.file)
		if d.FriendlyName != c.friendly || d.Manufacturer != c.vendor || modelOf(d) != c.model || category(d) != c.devType || d.UDN != c.udn {
			t.Errorf("%s: name=%q vendor=%q model=%q type=%q udn=%q", c.file, d.FriendlyName, d.Manufacturer, modelOf(d), category(d), d.UDN)
		}
	}
	igd := parseFixture(t, "desc-192.168.8.98-igd.xml")
	if igd.PresentationURL != "http://192.168.8.98:80" || len(igd.Devices) != 1 || len(igd.Devices[0].Devices) != 1 ||
		igd.Devices[0].Devices[0].Services[0].ServiceType != "urn:schemas-upnp-org:service:WANIPConnection:1" {
		t.Errorf("TP-Link structure: %+v", igd)
	}
	if ms := parseFixture(t, "desc-192.168.8.44-mediaserver.xml"); len(ms.Services) != 4 || ms.ModelDescription != "FRITZ!Box 7590" {
		t.Errorf("mediaserver services: %+v", ms.Services)
	}
	for _, bad := range []string{"", "<html><body>Not a description</body></html>", "<root><device>"} {
		if _, err := parseDescription([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestLatin1Description(t *testing.T) {
	doc := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><root xmlns=\"urn:schemas-upnp-org:device-1-0\"><device>" +
		"<deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType><friendlyName>K\xfcche</friendlyName></device></root>")
	d, err := parseDescription(doc)
	if err != nil || d.FriendlyName != "Küche" {
		t.Fatalf("%v %+v", err, d)
	}
}

func TestPrimaryFritzBox(t *testing.T) {
	var descs []description
	for _, f := range []string{"desc-192.168.8.44-mediaserver.xml", "desc-192.168.8.44-igd2.xml", "desc-192.168.8.44-fritzbox.xml", "desc-192.168.8.44-igd.xml"} {
		descs = append(descs, description{Location: "http://192.168.8.44:49000/" + f, Device: parseFixture(t, f)})
	}
	r := &responder{ip: netip.MustParseAddr("192.168.8.44")}
	for _, f := range []string{"ssdp-192.168.8.44-1.txt", "ssdp-192.168.8.44-10.txt"} {
		resp, _ := parseSSDP(plugintest.Fixture(t, f))
		r.responses = append(r.responses, resp)
	}
	obs := observation(r, descs, nil)
	if obs.IP != "192.168.8.44" || !obs.Present || obs.Hostname != "FRITZ!Box 7590" || obs.Vendor != "FRITZ! GmbH" ||
		obs.Model != "FRITZ!Box 7590" || obs.DeviceType != "router" {
		t.Fatalf("observation: %+v", obs)
	}
	if obs.Attrs["upnp.deviceType"] != "urn:schemas-upnp-org:device:InternetGatewayDevice:1" ||
		obs.Attrs["upnp.udn"] != "uuid:75802409-bccb-40e7-8e6c-DC15C822C6B3" || obs.Attrs["upnp.friendlyName"] != "FRITZ!Box 7590" ||
		obs.Attrs["upnp.server"] != "FRITZ!Box 7590 UPnP/1.0 AVM FRITZ!Box 7590 154.08.25" {
		t.Fatalf("attrs: %v", obs.Attrs)
	}
	inv := obs.Inventory.(map[string]any)
	if d := inv["descriptions"].([]description); len(d) != 4 {
		t.Fatalf("inventory: %+v", inv)
	}
	// Without descriptions only presence and the SERVER header remain.
	obs = observation(r, nil, nil)
	if obs.Hostname != "" || obs.DeviceType != "" || len(obs.Attrs) != 1 || !obs.Present {
		t.Fatalf("bare observation: %+v", obs)
	}
}

func TestCheckLocation(t *testing.T) {
	ip := netip.MustParseAddr("192.168.8.98")
	for loc, ok := range map[string]bool{
		"http://192.168.8.98:1900/igd.xml":     true,
		"https://192.168.8.98/desc.xml":        true,
		"http://192.168.8.99:1900/igd.xml":     false, // other host
		"http://127.0.0.1:8080/api/v1/devices": false, // SSRF into the NetScope host
		"http://router.lan/igd.xml":            false, // names are not resolved
		"ftp://192.168.8.98/igd.xml":           false,
		"http://user:pw@192.168.8.98/igd.xml":  false,
		"file:///etc/passwd":                   false,
		"http://[::ffff:192.168.8.98]:1900/x":  true,
	} {
		_, err := checkLocation(loc, ip)
		if (err == nil) != ok {
			t.Errorf("%s: err=%v, want ok=%v", loc, err, ok)
		}
	}
}

func TestFetchDescription(t *testing.T) {
	doc := plugintest.Fixture(t, "desc-192.168.8.98-igd.xml")
	mux := http.NewServeMux()
	mux.HandleFunc("/igd.xml", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(doc) })
	mux.HandleFunc("/big.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<root><device><friendlyName>"))
		_, _ = w.Write([]byte(strings.Repeat("x", maxDescSize)))
		_, _ = w.Write([]byte("</friendlyName></device></root>"))
	})
	mux.HandleFunc("/redirect.xml", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://192.0.2.1/igd.xml", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	local := netip.MustParseAddr(u.Hostname())
	client := newHTTPClient()
	ctx := context.Background()

	dev, raw, err := fetchDescription(ctx, client, srv.URL+"/igd.xml", local)
	if err != nil || dev.ModelName != "Archer C80" || len(raw) != len(doc) {
		t.Fatalf("fetch: %v %+v", err, dev)
	}
	if _, _, err := fetchDescription(ctx, client, srv.URL+"/big.xml", local); err == nil {
		t.Fatal("oversized description accepted")
	}
	if _, _, err := fetchDescription(ctx, client, srv.URL+"/redirect.xml", local); err == nil {
		t.Fatal("redirect followed")
	}
	if _, _, err := fetchDescription(ctx, client, srv.URL+"/igd.xml", netip.MustParseAddr("192.0.2.1")); !errors.Is(err, errForeignLocation) {
		t.Fatalf("foreign location: %v", err)
	}

	// describeHost: one valid and one foreign location.
	p := &Plugin{client: client}
	rc, _, _ := plugintest.RunContext(t, p, nil)
	r := &responder{ip: local, responses: []ssdpResponse{
		{Location: srv.URL + "/igd.xml", Server: "TPOS/V1.0.0 UPnP/1.0 Archer C80/2.20"},
		{Location: "http://192.0.2.1/evil.xml"},
	}}
	obs := describeHost(ctx, rc, client, r)
	if obs.Hostname != "Archer C80 AC1900 MU-MIMO Wi-Fi Router" || obs.DeviceType != "router" || obs.Model != "Archer C80 2.20" ||
		!strings.Contains(obs.Raw, "<modelName>Archer C80</modelName>") {
		t.Fatalf("observation: %+v", obs)
	}
}

func TestBuildSessions(t *testing.T) {
	lan := netip.MustParsePrefix("192.168.8.0/24")
	ifaceFor := func(a netip.Addr) string {
		if lan.Contains(a) {
			return "eth0"
		}
		return ""
	}
	sessions, errs := buildSessions(plugin.Targets{Subnets: []plugin.SubnetTarget{
		{CIDR: lan}, {CIDR: netip.MustParsePrefix("10.1.0.0/24"), Interface: "eth0"}, {CIDR: netip.MustParsePrefix("172.16.0.0/24")},
	}}, ifaceFor)
	if len(sessions) != 1 || len(errs) != 1 || !sessions[0].scope(netip.MustParseAddr("10.1.0.9")) ||
		sessions[0].scope(netip.MustParseAddr("172.16.0.1")) {
		t.Fatalf("sessions=%+v errs=%v", sessions, errs)
	}
	sessions, _ = buildSessions(plugin.Targets{DeviceMode: true, Devices: []plugin.DeviceInfo{{ID: 1, PrimaryIP: "192.168.8.98"}}}, ifaceFor)
	if len(sessions) != 1 || !sessions[0].scope(netip.MustParseAddr("192.168.8.98")) || sessions[0].scope(netip.MustParseAddr("192.168.8.44")) {
		t.Fatalf("device sessions: %+v", sessions)
	}
}

func TestMSearch(t *testing.T) {
	m := string(mSearch("ssdp:all"))
	if !strings.HasPrefix(m, "M-SEARCH * HTTP/1.1\r\n") || !strings.Contains(m, "MAN: \"ssdp:discover\"\r\n") ||
		!strings.Contains(m, "ST: ssdp:all\r\n") || !strings.HasSuffix(m, "\r\n\r\n") {
		t.Fatalf("%q", m)
	}
}
