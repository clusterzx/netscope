package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"netscope/internal/audit"
	"netscope/internal/auth"
	"netscope/internal/bus"
	"netscope/internal/config"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/federation"
	"netscope/internal/inventory"
	"netscope/internal/logging"
	"netscope/internal/pluginhost"
	_ "netscope/internal/plugins/all" // the plugins of the wizard
	"netscope/internal/rules"
	"netscope/internal/settings"
	"netscope/internal/setup"
	"netscope/internal/vault"
)

// instance is a NetScope instance for the setup tests: a new installation (setup pending,
// plugins held, no user) unless admin is set.
type instance struct {
	*harness
	dir   string
	code  string
	setup *setup.Service
	host  *pluginhost.Host
	fed   *federation.Service
	st    *settings.Store
}

func newInstance(t *testing.T, admin bool) *instance {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "netscope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	key, _ := vault.NewKey()
	v, err := vault.Open(ctx, d, key, vault.KeySource{})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := settings.Load(ctx, d)
	_ = st.InitTimezone(ctx, "UTC")
	b := bus.New()
	ring := logging.NewRing(100)
	level := new(slog.LevelVar)
	log := logging.New(io.Discard, "text", level, ring)
	a := auth.New(d, v)
	su, err := setup.Load(ctx, d, dir)
	if err != nil {
		t.Fatal(err)
	}
	in := &instance{dir: dir, setup: su, st: st}
	pw := ""
	if admin {
		if _, pw, err = a.EnsureAdmin(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if err := su.Complete(ctx, setup.ModeAutomatic); err != nil {
			t.Fatal(err)
		}
	} else if in.code, err = su.PrepareCode(); err != nil {
		t.Fatal(err)
	}
	inv, _ := inventory.New(ctx, d, b, st, log)
	ev := events.New(d, b, inv)
	host := pluginhost.New(pluginhost.Deps{DB: d, Bus: b, Log: log, Inventory: inv, Vault: v, Events: ev, Settings: st, DataDir: dir, Version: "test"})
	if err := host.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if su.Pending() {
		host.Hold()
	}
	hctx, cancel := context.WithCancel(ctx)
	host.Start(hctx)
	engine := rules.New(d, b, log, ev, inv, host, st)
	cfg := &config.Config{DataDir: dir, Timezone: "Europe/Berlin", MasterKeyFile: filepath.Join(dir, "master.key"),
		Proxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, TrustedProxies: []string{"127.0.0.0/8"}}
	fed, err := federation.New(ctx, federation.Deps{DB: d, Bus: b, Log: log, Vault: v, Inventory: inv, Events: ev, Settings: st,
		Host: host, Config: cfg, Version: "test", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fed.Start(hctx)
	t.Cleanup(func() {
		fed.Stop()
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		host.Stop(sctx)
		c()
		cancel()
	})
	s := New(Deps{Config: cfg, DB: d, Bus: b, Log: log, Logs: ring, LevelVar: level, Auth: a, Vault: v, Settings: st, Inventory: inv,
		Events: ev, Rules: engine, Host: host, Federation: fed, Audit: audit.New(d), Setup: su, Version: "test", StartedAt: time.Now()})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	in.harness = &harness{srv: srv, client: &http.Client{Jar: jar}, password: pw, auth: a, inv: inv}
	in.host, in.fed = host, fed
	return in
}

func (in *instance) account(t *testing.T) {
	t.Helper()
	resp, body := in.do(t, "POST", "/api/v1/setup/account", map[string]any{"username": "chef", "displayName": "Chefin",
		"password": "geheim-geheim", "locale": "en"}, setupCodeHeader, in.code)
	expect(t, "account", resp, body, http.StatusCreated, "")
}

// The setup endpoints answer only with the valid code (or the administrator's session)
// and only before the setup is finished; wrong codes fall under the login rate limit.
func TestSetupNeedsCodeAndEnds(t *testing.T) {
	in := newInstance(t, false)
	resp, body := in.do(t, "GET", "/api/v1/setup", nil)
	expect(t, "status", resp, body, 200, "")
	if st := decodeBody[setupStatus](t, body); !st.Pending || st.Account {
		t.Fatalf("status: %s", body)
	}
	if b, _ := os.ReadFile(filepath.Join(in.dir, setup.CodeFile)); strings.TrimSpace(string(b)) != in.code {
		t.Fatalf("code file %q", b)
	}
	// no code, wrong code
	resp, body = in.do(t, "GET", "/api/v1/setup/options", nil)
	expect(t, "options without code", resp, body, 401, "setup_code_required")
	resp, body = in.do(t, "GET", "/api/v1/setup/options", nil, setupCodeHeader, "AAAA-BBBB-CCCC")
	expect(t, "options with wrong code", resp, body, 401, "invalid_setup_code")
	resp, body = in.do(t, "POST", "/api/v1/setup/account", map[string]any{"username": "x", "password": "geheim-geheim"})
	expect(t, "account without code", resp, body, 401, "setup_code_required")
	resp, body = in.do(t, "POST", "/api/v1/setup/verify", map[string]any{"code": "nope"})
	expect(t, "verify wrong", resp, body, 401, "invalid_setup_code")
	// a login elsewhere does not open the wizard: no user yet
	resp, body = in.do(t, "POST", "/api/v1/setup/verify", map[string]any{"code": strings.ToLower(in.code)})
	expect(t, "verify", resp, body, 200, "")
	resp, body = in.do(t, "GET", "/api/v1/setup/options", nil, setupCodeHeader, in.code)
	expect(t, "options", resp, body, 200, "")
	opt := decodeBody[setupOptions](t, body)
	if opt.Timezone != "Europe/Berlin" || opt.MasterKeyFile == "" || len(opt.Scanners) != 10 || len(opt.Categories) != 5 {
		t.Fatalf("options: %s", body)
	}
	for _, sc := range opt.Scanners {
		if sc.Load == "" || sc.Schedule == "" {
			t.Fatalf("scanner without load or schedule: %+v", sc)
		}
	}
	// the account needs the code, a session does not replace it; afterwards the session
	// continues the wizard
	in.account(t)
	resp, body = in.do(t, "POST", "/api/v1/setup/account", map[string]any{"username": "zwei", "password": "geheim-geheim", "locale": "de"},
		setupCodeHeader, in.code)
	expect(t, "second account", resp, body, 409, "account_exists")
	resp, body = in.do(t, "GET", "/api/v1/setup/options", nil)
	expect(t, "options with session", resp, body, 200, "")
	if opt := decodeBody[setupOptions](t, body); opt.Account == nil || opt.Account.Username != "chef" || opt.Account.Locale != "en" {
		t.Fatalf("account: %s", body)
	}
	resp, body = in.do(t, "POST", "/api/v1/setup/complete", map[string]any{"language": "de"})
	expect(t, "complete without csrf", resp, body, 403, "csrf")
	// another browser without code or session gets nothing
	other := in.as()
	resp, body = other.do(t, "GET", "/api/v1/setup/options", nil)
	expect(t, "other browser", resp, body, 401, "setup_code_required")
	// a code file that cannot be removed does not keep the plugins held
	if err := os.Remove(filepath.Join(in.dir, setup.CodeFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(in.dir, setup.CodeFile, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	resp, body = in.do(t, "POST", "/api/v1/setup/complete", map[string]any{"language": "en", "timezone": "Europe/Vienna",
		"federation": map[string]any{"role": "standalone"}, "subnets": []any{}, "scanners": []string{"arpscan", "icmp"}}, csrf, "1")
	expect(t, "complete", resp, body, 200, "")
	if in.setup.Pending() || in.host.Held() {
		t.Fatal("setup still pending")
	}
	// after completion every setup endpoint is closed, also with the old code
	for _, c := range []struct{ method, path string }{{"GET", "/api/v1/setup/options"}, {"POST", "/api/v1/setup/account"},
		{"POST", "/api/v1/setup/complete"}, {"POST", "/api/v1/setup/federation/test"}} {
		resp, body = in.do(t, c.method, c.path, map[string]any{}, setupCodeHeader, in.code, csrf, "1")
		expect(t, c.path+" after completion", resp, body, 409, "setup_completed")
	}
	resp, body = in.do(t, "GET", "/api/v1/setup", nil)
	expect(t, "status after completion", resp, body, 200, "")
	if st := decodeBody[setupStatus](t, body); st.Pending {
		t.Fatalf("status after completion: %s", body)
	}
	// five wrong codes block the address like failed logins
	fresh := newInstance(t, false)
	for i := 0; i < 5; i++ {
		fresh.do(t, "GET", "/api/v1/setup/options", nil, setupCodeHeader, "WRONG-CODE-0000")
	}
	resp, body = fresh.do(t, "GET", "/api/v1/setup/options", nil, setupCodeHeader, fresh.code)
	expect(t, "rate limit", resp, body, 429, "rate_limited")
}

// Completing the wizard applies exactly the choices: settings, subnets, DNS server,
// the chosen scanners (all others off), and releases the plugins.
func TestSetupCompleteAppliesChoices(t *testing.T) {
	ctx := context.Background()
	in := newInstance(t, false)
	if _, err := in.host.Trigger(ctx, "arpscan", pluginhost.TriggerOptions{}); err == nil {
		t.Fatal("plugin ran before the setup was finished")
	}
	in.account(t)
	complete := func(body map[string]any) (*http.Response, []byte) {
		return in.do(t, "POST", "/api/v1/setup/complete", body, csrf, "1")
	}
	base := func() map[string]any {
		return map[string]any{"language": "en", "timezone": "America/New_York", "publicUrl": "https://netscope.example.org/",
			"federation": map[string]any{"role": "central", "localName": "Zuhause"},
			"subnets": []any{
				map[string]any{"cidr": "192.168.50.0/24", "name": "LAN", "access": "direct"},
				map[string]any{"cidr": "10.20.0.0/24", "name": "Büro", "access": "routed", "gateway": "10.20.0.1"},
			},
			"scanExclusions": []string{"192.168.50.9", " "}, "dnsServer": "192.168.50.1",
			"scanners": []string{"arpscan", "icmp", "dns"}, "firstScan": false}
	}
	// validation happens before anything is applied
	bad := base()
	bad["subnets"] = []any{map[string]any{"cidr": "nonsense"}}
	resp, body := complete(bad)
	expect(t, "bad subnet", resp, body, 400, "validation")
	if !strings.Contains(string(body), "subnets.0.cidr") {
		t.Fatalf("field of the error: %s", body)
	}
	bad = base()
	bad["scanners"] = []string{"cve"}
	resp, body = complete(bad)
	expect(t, "not a scanner", resp, body, 400, "validation")
	bad = base()
	bad["federation"] = map[string]any{"role": "site", "centralUrl": "ftp://x"}
	resp, body = complete(bad)
	expect(t, "bad federation", resp, body, 400, "federation.centralUrl")
	bad = base()
	bad["timezone"] = "Mars/Olympus"
	resp, body = complete(bad)
	expect(t, "bad time zone", resp, body, 400, "timezone")
	if !in.setup.Pending() || in.fed.Role() != federation.RoleStandalone || in.st.System().Timezone != "UTC" {
		t.Fatal("a rejected completion changed something")
	}
	resp, body = complete(base())
	expect(t, "complete", resp, body, 200, "")
	sys := in.st.System()
	if sys.Language != "en" || sys.Timezone != "America/New_York" || sys.PublicURL != "https://netscope.example.org" ||
		len(sys.ScanExclusions) != 1 || sys.ScanExclusions[0] != "192.168.50.9" {
		t.Fatalf("settings: %+v", sys)
	}
	if in.fed.Role() != federation.RoleCentral || in.fed.LocalName() != "Zuhause" {
		t.Fatalf("federation: %s %s", in.fed.Role(), in.fed.LocalName())
	}
	subs, _ := in.inv.ListSubnets(ctx)
	if len(subs) != 2 || subs[0].CIDR != "10.20.0.0/24" || subs[0].Access != "routed" || subs[1].Name != "LAN" {
		t.Fatalf("subnets: %+v", subs)
	}
	if c, _ := in.host.Config("dns"); c.Settings["resolver"] != "192.168.50.1" {
		t.Fatalf("dns resolver: %v", c.Settings["resolver"])
	}
	for _, id := range []string{"arpscan", "icmp", "dns", "nmap", "nmap_udp", "http", "tls", "mdns", "upnp", "netbios"} {
		c, _ := in.host.Config(id)
		want := id == "arpscan" || id == "icmp" || id == "dns"
		if c.Enabled != want {
			t.Errorf("scanner %s enabled=%v, want %v", id, c.Enabled, want)
		}
	}
	// evaluating plugins keep their defaults
	for _, id := range []string{"cve", "oui", "diff", "topology", "cleanup"} {
		if c, _ := in.host.Config(id); !c.Enabled {
			t.Errorf("%s was switched off", id)
		}
	}
	if in.host.Held() {
		t.Fatal("plugins still held")
	}
	if _, err := in.host.Trigger(ctx, "zz_none", pluginhost.TriggerOptions{}); err == nil {
		t.Fatal("unknown plugin")
	}
}

// The connection test of the plugin page and of the wizard uses unsaved settings, needs
// the plugins.manage right and stores nothing.
func TestConnectionTestEndpoint(t *testing.T) {
	in := newInstance(t, false)
	in.account(t)
	fw := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(fw.Close)
	resp, body := in.do(t, "POST", "/api/v1/credentials", map[string]any{"name": "fw", "type": "api_token",
		"values": map[string]any{"token_id": "k", "token": "s"}}, csrf, "1")
	expect(t, "credential", resp, body, 201, "")
	resp, body = in.do(t, "POST", "/api/v1/plugins/opnsense/connection-test", map[string]any{"settings": map[string]any{"hosts": []string{fw.URL}}},
		csrf, "1")
	expect(t, "test", resp, body, 200, "")
	res := decodeBody[connectionTestResponse](t, body)
	if res.OK || len(res.Results) != 1 || !strings.Contains(res.Results[0].Message, "login rejected") {
		t.Fatalf("result (English for the user): %s", body)
	}
	if c, _ := in.host.Config("opnsense"); len(c.Settings["hosts"].([]string)) != 0 || c.Enabled {
		t.Fatalf("test stored settings: %v", c.Settings)
	}
	resp, body = in.do(t, "POST", "/api/v1/plugins/opnsense/connection-test", map[string]any{"settings": map[string]any{"hosts": []string{"ftp://x"}}},
		csrf, "1")
	expect(t, "invalid settings", resp, body, 400, "validation")
	resp, body = in.do(t, "POST", "/api/v1/plugins/arpscan/connection-test", nil, csrf, "1")
	expect(t, "no connection test", resp, body, 400, "")
	resp, body = in.do(t, "POST", "/api/v1/plugins/ssh/connection-test", map[string]any{}, csrf, "1")
	expect(t, "ssh without target", resp, body, 400, "")
}

// The role "site" chosen in the wizard connects to the central instance: the connection
// test uses the entered data, completion stores it and deliveries reach the central.
func TestSetupSiteConnectsToCentral(t *testing.T) {
	ctx := context.Background()
	central := newInstance(t, true)
	if _, err := central.fed.Update(ctx, federation.SettingsInput{Settings: federation.Settings{Role: federation.RoleCentral}}); err != nil {
		t.Fatal(err)
	}
	st, token, err := central.fed.CreateSite(ctx, federation.SiteInput{Name: "Filiale"})
	if err != nil {
		t.Fatal(err)
	}
	site := newInstance(t, false)
	fedBody := map[string]any{"role": "site", "centralUrl": central.srv.URL, "token": "nss_wrong"}
	resp, body := site.do(t, "POST", "/api/v1/setup/federation/test", fedBody, setupCodeHeader, site.code)
	expect(t, "wrong token", resp, body, 200, "")
	if res := decodeBody[federation.TestResult](t, body); res.OK || res.Error == "" {
		t.Fatalf("wrong token accepted: %s", body)
	}
	fedBody["token"] = token
	resp, body = site.do(t, "POST", "/api/v1/setup/federation/test", fedBody, setupCodeHeader, site.code)
	expect(t, "test", resp, body, 200, "")
	if res := decodeBody[federation.TestResult](t, body); !res.OK || res.Site != "Filiale" {
		t.Fatalf("test: %s", body)
	}
	if site.fed.Role() != federation.RoleStandalone {
		t.Fatal("the test stored the role")
	}
	site.account(t)
	resp, body = site.do(t, "POST", "/api/v1/setup/complete", map[string]any{"language": "de", "timezone": "Europe/Berlin",
		"federation": fedBody, "subnets": []any{}, "scanners": []string{"arpscan"}}, csrf, "1")
	expect(t, "complete", resp, body, 200, "")
	if site.fed.Role() != federation.RoleSite || !site.fed.Active() {
		t.Fatalf("site role not active: %s", site.fed.Role())
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		s, err := central.fed.Site(ctx, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		if s.LastContact != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the site never reported to the central instance")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// A time zone changed in the system settings applies at once to schedules and reports.
func TestTimezoneSettingWithoutRestart(t *testing.T) {
	in := newInstance(t, true)
	in.login(t)
	next := func() time.Time {
		t.Helper()
		resp, body := in.do(t, "GET", "/api/v1/cron/describe?expr="+url.QueryEscape("0 3 * * *"), nil)
		expect(t, "describe", resp, body, 200, "")
		c := decodeBody[cronResponse](t, body)
		if len(c.Next) == 0 {
			t.Fatalf("no next runs: %s", body)
		}
		return c.Next[0]
	}
	if h := next().UTC().Hour(); h != 3 {
		t.Fatalf("UTC: next run at %d h", h)
	}
	resp, body := in.do(t, "PUT", "/api/v1/system/settings", map[string]any{"timezone": "Asia/Tokyo"}, csrf, "1")
	expect(t, "settings", resp, body, 200, "")
	if h := next().UTC().Hour(); h != 18 {
		t.Fatalf("Tokyo: next run at %d h", h)
	}
	if loc := in.host.Env().Location.String(); loc != "Asia/Tokyo" {
		t.Fatalf("scheduled reports use %s", loc)
	}
	// a client that does not know the setting keeps it
	resp, body = in.do(t, "PUT", "/api/v1/system/settings", map[string]any{"offlineAfterMissed": 3}, csrf, "1")
	expect(t, "settings without time zone", resp, body, 200, "")
	if tz := in.st.System().Timezone; tz != "Asia/Tokyo" {
		t.Fatalf("time zone reset to %s", tz)
	}
	resp, body = in.do(t, "PUT", "/api/v1/system/settings", map[string]any{"timezone": "Nowhere/Land"}, csrf, "1")
	expect(t, "unknown time zone", resp, body, 400, "timezone")
}
