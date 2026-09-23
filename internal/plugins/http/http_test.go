package http

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func TestInfoAndSchema(t *testing.T) {
	p := &Plugin{}
	info := p.Info()
	if info.ID != "http" || info.Targets != plugin.TargetDevices || info.DefaultConcurrency != 16 {
		t.Fatalf("info = %+v", info)
	}
	if err := p.Schema().Check(); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := p.Schema().Validate(nil, nil, nil); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestExtractTitle(t *testing.T) {
	cases := map[string]string{
		`<html><head><title>  Hello   World </title></head>`: "Hello World",
		`<title>A &amp; B</title>`:                           "A & B",
		`<html><body>no title</body></html>`:                 "",
	}
	for in, want := range cases {
		if got := extractTitle([]byte(in)); got != want {
			t.Errorf("extractTitle(%q) = %q, want %q", in, got, want)
		}
	}
	long := "<title>" + strings.Repeat("x", 300) + "</title>"
	if got := extractTitle([]byte(long)); len([]rune(got)) != 200 {
		t.Errorf("title length = %d, want 200", len([]rune(got)))
	}
}

func TestExtractIconHrefs(t *testing.T) {
	body := readFixture(t, "adguard-login.html")
	hrefs := extractIconHrefs(body)
	found := false
	for _, h := range hrefs {
		if strings.Contains(h, "favicon.png") {
			found = true
		}
	}
	if !found {
		t.Errorf("icon hrefs = %v, want one containing favicon.png", hrefs)
	}
}

func TestDetectSignatures(t *testing.T) {
	sigs := builtinSignatures
	cases := []struct {
		name string
		fp   fingerprint
		want string
	}{
		{"luci", fingerprint{title: "GL-MT6000 - Overview - LuCI", body: readFixture(t, "luci.html"),
			headers: http.Header{"X-Luci-Login-Required": {"yes"}}}, "OpenWrt LuCI"},
		{"glinet", fingerprint{title: "Admin Panel", body: readFixture(t, "glinet.html")}, "GL.iNet"},
		{"grafana", fingerprint{title: "Grafana", body: []byte(`<div class="grafana-app"></div>`)}, "Grafana"},
		{"proxmox", fingerprint{title: "pxc - Proxmox Virtual Environment", body: []byte(`pve-manager/8.1.4`)}, "Proxmox VE"},
		{"portainer", fingerprint{title: "Portainer", body: []byte(`portainer`)}, "Portainer"},
		{"jellyfin", fingerprint{title: "Jellyfin", body: []byte(`jellyfin`)}, "Jellyfin"},
	}
	for _, c := range cases {
		apps, _ := detect(c.fp, sigs)
		if !hasApp(apps, c.want) {
			t.Errorf("%s: apps = %v, want %s", c.name, appNames(apps), c.want)
		}
	}
}

func TestDetectDeviceTypeAndCPE(t *testing.T) {
	apps, dt := detect(fingerprint{title: "pxc - Proxmox Virtual Environment", body: []byte(`pve-manager/8.1.4 something`)}, builtinSignatures)
	if dt != "hypervisor" {
		t.Errorf("deviceType = %q, want hypervisor", dt)
	}
	var pve *plugin.DetectedApp
	for i := range apps {
		if apps[i].Name == "Proxmox VE" {
			pve = &apps[i]
		}
	}
	if pve == nil {
		t.Fatal("Proxmox VE not detected")
	}
	if pve.Version != "8.1.4" {
		t.Errorf("version = %q, want 8.1.4", pve.Version)
	}
	if pve.CPE != "cpe:2.3:a:proxmox:proxmox_ve:8.1.4:*:*:*:*:*:*:*" {
		t.Errorf("cpe = %q", pve.CPE)
	}
}

func TestGrafanaVersion(t *testing.T) {
	apps, _ := detect(fingerprint{title: "Grafana", body: []byte(`{"version":"10.4.2","commit":"abc"}`)}, builtinSignatures)
	for _, a := range apps {
		if a.Name == "Grafana" {
			if a.Version != "10.4.2" || a.CPE != "cpe:2.3:a:grafana:grafana:10.4.2:*:*:*:*:*:*:*" {
				t.Errorf("grafana = %+v", a)
			}
			return
		}
	}
	t.Error("Grafana not detected")
}

func TestParseCustomSignature(t *testing.T) {
	if _, err := parseCustomSignature("MyApp|title|My App"); err != nil {
		t.Errorf("valid title sig: %v", err)
	}
	if _, err := parseCustomSignature("MyApp|favicon|12345"); err != nil {
		t.Errorf("valid favicon sig: %v", err)
	}
	if _, err := parseCustomSignature("MyApp|header:X-Thing|.*"); err != nil {
		t.Errorf("valid header sig: %v", err)
	}
	bad := []string{"nofields", "Name|bogus|x", "Name|favicon|notanumber", "|title|x", "Name|body|(unclosed"}
	for _, b := range bad {
		if _, err := parseCustomSignature(b); err == nil {
			t.Errorf("parseCustomSignature(%q) should fail", b)
		}
	}
	// A custom favicon signature detects a matching favicon.
	s, _ := parseCustomSignature("Widget|favicon|-918516514")
	apps, _ := detect(fingerprint{favicon: i32(-918516514)}, []signature{s})
	if !hasApp(apps, "Widget") {
		t.Errorf("custom favicon sig did not match")
	}
}

func TestValidateSettings(t *testing.T) {
	p := &Plugin{}
	good, _ := p.Schema().Validate(map[string]any{"extra_ports": []string{"80", "8443"}, "custom_signatures": []string{"X|title|y"}}, nil, nil)
	if err := p.ValidateSettings(plugin.NewSettings(good)); err != nil {
		t.Errorf("valid rejected: %v", err)
	}
	bad, _ := p.Schema().Validate(map[string]any{"custom_signatures": []string{"broken"}}, nil, nil)
	if err := p.ValidateSettings(plugin.NewSettings(bad)); err == nil {
		t.Error("expected custom_signatures error")
	}
}

// serveFixture starts an httptest server replaying a page body + favicon.
func newAppServer(tls bool, body string, headers map[string]string, favicon []byte) *httptest.Server {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve the icon for any image path the page might reference.
		if p := r.URL.Path; favicon != nil && (strings.HasSuffix(p, ".png") || strings.HasSuffix(p, ".ico") || strings.HasSuffix(p, ".svg")) {
			w.Header().Set("Content-Type", "image/png")
			w.Write(favicon)
			return
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		w.Write([]byte(body))
	})
	if tls {
		return httptest.NewTLSServer(h)
	}
	return httptest.NewServer(h)
}

func hostPort(t *testing.T, srv *httptest.Server) (string, int) {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	host, portStr, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func testConfig() config {
	return config{timeout: 3 * time.Second, maxRedirects: 5, userAgent: "test", favicon: true, maxBody: 512 * 1024}
}

func TestProbeEndpointEndToEnd(t *testing.T) {
	favicon := readFixture(t, "favicon-luci.png")
	srv := newAppServer(false, string(readFixture(t, "luci.html")),
		map[string]string{"X-LuCI-Login-Required": "yes", "Server": "uhttpd"}, favicon)
	defer srv.Close()
	host, port := hostPort(t, srv)
	p := &Plugin{}
	svc, dt := p.probeEndpoint(context.Background(), testRC(t), testClient(), endpoint{ip: host, port: port, tls: false},
		map[string]bool{host: true}, testConfig(), builtinSignatures)
	if svc == nil {
		t.Fatal("no service")
	}
	if svc.StatusCode != 200 {
		t.Errorf("status = %d", svc.StatusCode)
	}
	if !strings.Contains(svc.Title, "LuCI") {
		t.Errorf("title = %q", svc.Title)
	}
	if svc.FaviconHash == nil || *svc.FaviconHash != -918516514 {
		t.Errorf("favicon hash = %v", svc.FaviconHash)
	}
	if !hasApp(svc.Apps, "OpenWrt LuCI") {
		t.Errorf("apps = %v", appNames(svc.Apps))
	}
	if dt != "router" {
		t.Errorf("deviceType = %q", dt)
	}
}

func TestProbeSchemeFallback(t *testing.T) {
	// A TLS server probed with the http scheme first must retry https and succeed.
	srv := newAppServer(true, "<title>Secure</title>", nil, nil)
	defer srv.Close()
	host, port := hostPort(t, srv)
	p := &Plugin{}
	svc, _ := p.probeEndpoint(context.Background(), testRC(t), testClient(), endpoint{ip: host, port: port, tls: false},
		map[string]bool{host: true}, testConfig(), nil)
	if svc == nil || svc.Scheme != "https" || svc.Title != "Secure" {
		t.Fatalf("scheme fallback failed: %+v", svc)
	}
}

func TestProbeClosedPortReportsNothing(t *testing.T) {
	// A port without any listener fails on both schemes and must not become a service.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	p := &Plugin{}
	svc, _ := p.probeEndpoint(context.Background(), testRC(t), testClient(), endpoint{ip: "127.0.0.1", port: port, tls: false},
		map[string]bool{"127.0.0.1": true}, testConfig(), nil)
	if svc != nil {
		t.Fatalf("closed port reported as service: %+v", svc)
	}
}

func TestScanDeviceObservation(t *testing.T) {
	srv := newAppServer(false, "<title>Grafana</title><div class=\"grafana-app\"></div>", nil, nil)
	defer srv.Close()
	host, port := hostPort(t, srv)
	rc, sink, _ := plugintest.RunContext(t, &Plugin{}, map[string]any{"extra_ports": []string{}, "favicon": false})
	dev := plugin.DeviceInfo{ID: 5, PrimaryIP: host, IPs: []string{host},
		Ports: []plugin.PortRef{{IP: host, Proto: "tcp", Port: port, Service: "http"}}}
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{dev}}
	// scanDevice uses the plugin's own client via Run; call Run.
	if err := (&Plugin{}).Run(context.Background(), rc); err != nil {
		t.Fatalf("run: %v", err)
	}
	obs := sink.All()
	if len(obs) != 1 {
		t.Fatalf("observations = %d", len(obs))
	}
	o := obs[0]
	if o.DeviceID != 5 || !o.Present || o.HTTP == nil || len(o.HTTP.Services) != 1 {
		t.Fatalf("obs = %+v", o)
	}
	if !hasApp(o.HTTP.Services[0].Apps, "Grafana") {
		t.Errorf("apps = %v", appNames(o.HTTP.Services[0].Apps))
	}
	if len(o.HTTP.Scanned) != 1 || o.HTTP.Scanned[0] != port {
		t.Errorf("scanned = %v", o.HTTP.Scanned)
	}
}

func testRC(t *testing.T) *plugin.RunContext {
	rc, _, _ := plugintest.RunContext(t, &Plugin{}, nil)
	return rc
}

func i32(v int32) *int32 { return &v }

func hasApp(apps []plugin.DetectedApp, name string) bool {
	for _, a := range apps {
		if a.Name == name {
			return true
		}
	}
	return false
}

func appNames(apps []plugin.DetectedApp) []string {
	var out []string
	for _, a := range apps {
		out = append(out, a.Name)
	}
	return out
}
