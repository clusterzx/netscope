package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
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
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/rules"
	"netscope/internal/settings"
	"netscope/internal/vault"
)

type harness struct {
	srv      *httptest.Server
	client   *http.Client
	password string
	auth     *auth.Service
	inv      *inventory.Store
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "api.db"))
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
	b := bus.New()
	ring := logging.NewRing(100)
	level := new(slog.LevelVar)
	log := logging.New(io.Discard, "text", level, ring)
	a := auth.New(d, v)
	_, pw, err := a.EnsureAdmin(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := inventory.New(ctx, d, b, st, log)
	ev := events.New(d, b, inv)
	host := pluginhost.New(pluginhost.Deps{DB: d, Bus: b, Log: log, Inventory: inv, Vault: v, Events: ev, Settings: st,
		DataDir: dir, Location: time.UTC, Version: "test"})
	if err := host.Init(ctx); err != nil {
		t.Fatal(err)
	}
	engine := rules.New(d, b, log, ev, inv, host, st, time.UTC)
	fed, err := federation.New(ctx, federation.Deps{DB: d, Bus: b, Log: log, Vault: v, Inventory: inv, Events: ev, Settings: st,
		Host: host, Version: "test", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DataDir: dir, Location: time.UTC, Timezone: "UTC",
		Proxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, TrustedProxies: []string{"127.0.0.0/8"}}
	s := New(Deps{Config: cfg, DB: d, Bus: b, Log: log, Logs: ring, LevelVar: level, Auth: a, Vault: v, Settings: st, Inventory: inv,
		Events: ev, Rules: engine, Host: host, Federation: fed, Audit: audit.New(d), Version: "test", StartedAt: time.Now()})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &harness{srv: srv, client: &http.Client{Jar: jar}, password: pw, auth: a, inv: inv}
}

func (h *harness) do(t *testing.T, method, path string, body any, headers ...string) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

const csrf = "X-NetScope-CSRF"

func (h *harness) login(t *testing.T) {
	t.Helper()
	resp, body := h.do(t, "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": h.password})
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d %s", resp.StatusCode, body)
	}
}

func TestAuthAndCSRF(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.do(t, "GET", "/api/v1/devices", nil); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated: %d", resp.StatusCode)
	}
	if resp, _ := h.do(t, "GET", "/api/v1/health", nil); resp.StatusCode != 200 {
		t.Fatalf("health must be public: %d", resp.StatusCode)
	}
	resp, body := h.do(t, "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"})
	if resp.StatusCode != 401 || !strings.Contains(string(body), "invalid_credentials") {
		t.Fatalf("wrong password: %d %s", resp.StatusCode, body)
	}
	h.login(t)
	for _, c := range h.client.Jar.Cookies(mustURL(h.srv.URL)) {
		if c.Name == sessionCookie && c.Value == "" {
			t.Fatal("empty session cookie")
		}
	}
	if resp, _ := h.do(t, "GET", "/api/v1/devices", nil); resp.StatusCode != 200 {
		t.Fatalf("session: %d", resp.StatusCode)
	}
	// cookie-authenticated writes need the CSRF header
	resp, body = h.do(t, "POST", "/api/v1/devices", map[string]string{"name": "x", "ip": "192.168.1.9"})
	if resp.StatusCode != 403 || !strings.Contains(string(body), "csrf") {
		t.Fatalf("csrf: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, "POST", "/api/v1/devices", map[string]string{"name": "x", "ip": "192.168.1.9"}, csrf, "1")
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	// validation errors are 400 with a message
	resp, body = h.do(t, "POST", "/api/v1/devices", map[string]string{"ip": "not-an-ip"}, csrf, "1")
	if resp.StatusCode != 400 {
		t.Fatalf("validation: %d %s", resp.StatusCode, body)
	}
	// unknown fields are rejected on strict endpoints
	resp, _ = h.do(t, "PATCH", "/api/v1/devices/1", map[string]any{"bogus": 1}, csrf, "1")
	if resp.StatusCode != 400 {
		t.Fatalf("unknown field: %d", resp.StatusCode)
	}
	if resp, _ := h.do(t, "GET", "/api/v1/devices/999", nil); resp.StatusCode != 404 {
		t.Fatalf("not found: %d", resp.StatusCode)
	}
	if resp, _ := h.do(t, "GET", "/api/v1/nope", nil); resp.StatusCode != 404 {
		t.Fatalf("unknown route: %d", resp.StatusCode)
	}
	h.do(t, "POST", "/api/v1/auth/logout", nil, csrf, "1")
	if resp, _ := h.do(t, "GET", "/api/v1/devices", nil); resp.StatusCode != 401 {
		t.Fatalf("after logout: %d", resp.StatusCode)
	}
}

func TestTokenScopes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	uid, _ := h.auth.AdminID(ctx)
	read, _, _ := h.auth.CreateToken(ctx, uid, "r", auth.ScopeRead, nil)
	write, _, _ := h.auth.CreateToken(ctx, uid, "w", auth.ScopeWrite, nil)
	jar, _ := cookiejar.New(nil)
	h.client = &http.Client{Jar: jar}
	if resp, _ := h.do(t, "GET", "/api/v1/devices", nil, "Authorization", "Bearer "+read); resp.StatusCode != 200 {
		t.Fatalf("read token GET: %d", resp.StatusCode)
	}
	if resp, _ := h.do(t, "POST", "/api/v1/devices", map[string]string{"name": "x"}, "Authorization", "Bearer "+read); resp.StatusCode != 403 {
		t.Fatalf("read token POST: %d", resp.StatusCode)
	}
	// tokens do not need the CSRF header
	if resp, b := h.do(t, "POST", "/api/v1/devices", map[string]string{"name": "x"}, "Authorization", "Bearer "+write); resp.StatusCode != 201 {
		t.Fatalf("write token POST: %d %s", resp.StatusCode, b)
	}
	if resp, _ := h.do(t, "GET", "/api/v1/devices", nil, "Authorization", "Bearer ns_invalid"); resp.StatusCode != 401 {
		t.Fatalf("invalid token: %d", resp.StatusCode)
	}
	// write-scope routes via GET (backup download) also require write
	if resp, _ := h.do(t, "GET", "/api/v1/system/backups/x.db", nil, "Authorization", "Bearer "+read); resp.StatusCode != 403 {
		t.Fatalf("read token on write route: %d", resp.StatusCode)
	}
}

func TestProxyHeaders(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	// the test client connects from 127.0.0.1, a trusted proxy
	_, body := h.do(t, "GET", "/api/v1/system/info", nil, "X-Forwarded-For", "203.0.113.7, 127.0.0.1",
		"X-Forwarded-Proto", "https", "X-Forwarded-Host", "netscope.example.org")
	var info systemInfo
	_ = json.Unmarshal(body, &info)
	if info.Client.IP != "203.0.113.7" || info.Client.Scheme != "https" || info.Client.Host != "netscope.example.org" {
		t.Fatalf("forwarded headers not applied: %+v", info.Client)
	}
	// login over https (per proxy) sets a Secure cookie
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"`+h.password+`"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	secure := false
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			secure = c.Secure && c.HttpOnly && c.SameSite == http.SameSiteLaxMode
		}
	}
	if !secure {
		t.Fatal("session cookie must be Secure/HttpOnly/SameSite=Lax behind https proxy")
	}
}

func TestUntrustedProxyIgnored(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	s := &Server{Deps: Deps{Config: &config.Config{Proxies: nil}}}
	var got clientInfo
	handler := s.proxyAware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = client(r) }))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.4:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Forwarded-Proto", "https")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if got.IP != "198.51.100.4" || got.Scheme != "http" {
		t.Fatalf("untrusted proxy headers applied: %+v", got)
	}
}

func TestDeviceWorkflowAndAudit(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	ctx := context.Background()
	id, err := h.inv.Observe(ctx, "arpscan", 0, &plugin.Observation{MACs: []string{"aa:bb:cc:dd:ee:01"}, IP: "192.168.1.20", Present: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, body := h.do(t, "PATCH", "/api/v1/devices/"+strconv.FormatInt(id, 10), map[string]any{"displayName": "Drucker", "tags": []string{"Büro"},
		"state": "known"}, csrf, "1")
	if resp.StatusCode != 200 {
		t.Fatalf("patch: %d %s", resp.StatusCode, body)
	}
	var d inventory.DeviceDetail
	_ = json.Unmarshal(body, &d)
	if d.Name != "Drucker" || len(d.Tags) != 1 || d.Tags[0] != "büro" || d.State != "known" {
		t.Fatalf("detail: %+v", d.DeviceRow)
	}
	_, body = h.do(t, "GET", "/api/v1/devices?q=tag:büro", nil)
	var list deviceList
	_ = json.Unmarshal(body, &list)
	if list.Total != 1 {
		t.Fatalf("query: %s", body)
	}
	resp, body = h.do(t, "GET", "/api/v1/devices?q=port:abc", nil)
	if resp.StatusCode != 400 || !strings.Contains(string(body), "ungültiger Port") {
		t.Fatalf("bad query: %d %s", resp.StatusCode, body)
	}
	_, body = h.do(t, "GET", "/api/v1/audit?entity=device", nil)
	if !strings.Contains(string(body), "device.update") || !strings.Contains(string(body), "Drucker") {
		t.Fatalf("audit entry missing: %s", body)
	}
	resp, body = h.do(t, "GET", "/api/v1/reports/inventory?format=csv", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Drucker") {
		t.Fatalf("csv export: %d %s", resp.StatusCode, body)
	}
}

func TestOpenAPIComplete(t *testing.T) {
	h := newHarness(t)
	_, body := h.do(t, "GET", "/api/openapi.json", nil)
	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	for _, p := range []string{"/api/v1/devices", "/api/v1/devices/{id}", "/api/v1/plugins/{id}/config", "/api/v1/rules/{id}/test",
		"/api/v1/credentials", "/api/v1/topology", "/api/v1/stream", "/metrics"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("spec lacks %s", p)
		}
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas["InventoryDeviceRow"]; !ok {
		t.Errorf("schema InventoryDeviceRow missing; have %d schemas", len(schemas))
	}
	// every $ref must resolve
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				name := strings.TrimPrefix(ref, "#/components/schemas/")
				if _, ok := schemas[name]; !ok {
					t.Errorf("dangling ref %s", ref)
				}
			}
			for _, vv := range x {
				walk(vv)
			}
		case []any:
			for _, vv := range x {
				walk(vv)
			}
		}
	}
	walk(spec)
}

func mustURL(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}

func TestCredentialScopeAPI(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	ctx := context.Background()
	id, err := h.inv.Observe(ctx, "arpscan", 0, &plugin.Observation{MACs: []string{"aa:bb:cc:dd:ee:07"}, IP: "192.168.1.27", Present: true})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{"username": "root", "password": "x"}
	resp, body := h.do(t, "POST", "/api/v1/credentials", map[string]any{"name": "Router", "type": "ssh", "values": values,
		"scope": map[string]any{"devices": []int64{id}}}, csrf, "1")
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	var cred credentialView
	_ = json.Unmarshal(body, &cred)
	if len(cred.Scope.Devices) != 1 || cred.Scope.Devices[0] != id || cred.Scope.AllSubnets {
		t.Fatalf("scope: %+v", cred.Scope)
	}
	if resp, body := h.do(t, "POST", "/api/v1/credentials", map[string]any{"name": "Global", "type": "ssh", "values": values}, csrf, "1"); resp.StatusCode != 201 ||
		!strings.Contains(string(body), `"allSubnets":true`) {
		t.Fatalf("default scope: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, "POST", "/api/v1/credentials", map[string]any{"name": "Kaputt", "type": "ssh", "values": values,
		"scope": map[string]any{"subnets": []string{"kein-netz"}}}, csrf, "1")
	if resp.StatusCode != 400 || !strings.Contains(string(body), "scope.subnets") {
		t.Fatalf("invalid scope: %d %s", resp.StatusCode, body)
	}

	resp, body = h.do(t, "GET", "/api/v1/devices/"+strconv.FormatInt(id, 10)+"/credentials", nil)
	var list []deviceCredential
	if err := json.Unmarshal(body, &list); err != nil || resp.StatusCode != 200 {
		t.Fatalf("device credentials: %d %s", resp.StatusCode, body)
	}
	if len(list) != 2 || list[0].ID != cred.ID || list[0].Rank != plugin.RankDevice || list[0].Reason != "Gerät zugewiesen" ||
		list[1].Name != "Global" || list[0].UsedBy == nil {
		t.Fatalf("matches: %s", body)
	}
	if resp, _ := h.do(t, "GET", "/api/v1/devices/999/credentials", nil); resp.StatusCode != 404 {
		t.Errorf("unknown device: %d", resp.StatusCode)
	}
}

func TestFederationIngest(t *testing.T) {
	h := newHarness(t)
	batch := map[string]any{"protocol": 1, "epoch": "e1", "instance": map[string]any{"version": "x"},
		"items": []map[string]any{{"seq": 1, "kind": "observation", "at": time.Now(),
			"data": map[string]any{"plugin": "arpscan", "device": 7, "obs": map[string]any{"ip": "10.1.1.7", "macs": []string{"02:00:00:00:10:07"}, "present": true}}}}}
	if resp, _ := h.do(t, "POST", "/api/v1/federation/ingest", batch); resp.StatusCode != 401 {
		t.Fatalf("without token: %d", resp.StatusCode)
	}
	if resp, body := h.do(t, "POST", "/api/v1/federation/ingest", batch, "Authorization", "Bearer nss_x"); resp.StatusCode != 404 ||
		!strings.Contains(string(body), "not_central") {
		t.Fatalf("not central: %d %s", resp.StatusCode, body)
	}
	h.login(t)
	if resp, body := h.do(t, "PUT", "/api/v1/federation", map[string]any{"role": "central", "localName": "Zuhause"}, csrf, "1"); resp.StatusCode != 200 {
		t.Fatalf("role: %d %s", resp.StatusCode, body)
	}
	resp, body := h.do(t, "POST", "/api/v1/sites", map[string]any{"name": "Colo"}, csrf, "1")
	if resp.StatusCode != 201 {
		t.Fatalf("create site: %d %s", resp.StatusCode, body)
	}
	var created struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(body, &created)

	// the site token works for the ingest endpoint only (a fresh client without session)
	anon := &harness{srv: h.srv, client: &http.Client{}}
	if resp, _ := anon.do(t, "GET", "/api/v1/devices", nil, "Authorization", "Bearer "+created.Token); resp.StatusCode != 401 {
		t.Fatalf("site token accepted by the regular API: %d", resp.StatusCode)
	}
	resp, body = anon.do(t, "POST", "/api/v1/federation/ingest", batch, "Authorization", "Bearer "+created.Token)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"acked":1`) {
		t.Fatalf("ingest: %d %s", resp.StatusCode, body)
	}
	resp, body = anon.do(t, "POST", "/api/v1/federation/ingest", map[string]any{"protocol": 99, "items": []any{}},
		"Authorization", "Bearer "+created.Token)
	if resp.StatusCode != 409 || !strings.Contains(string(body), "Zentrale aktualisieren") {
		t.Fatalf("newer protocol: %d %s", resp.StatusCode, body)
	}

	resp, body = h.do(t, "GET", "/api/v1/devices?site=colo", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"site":"Colo"`) || !strings.Contains(string(body), "10.1.1.7") {
		t.Fatalf("devices of the site: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, "GET", "/api/v1/devices?site=local", nil)
	if resp.StatusCode != 200 || strings.Contains(string(body), "10.1.1.7") {
		t.Fatalf("local devices: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.do(t, "GET", "/api/v1/events?site=nirgends", nil); resp.StatusCode != 400 {
		t.Fatalf("unknown site: %d", resp.StatusCode)
	}
	// scans of site devices are refused
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_, body = h.do(t, "GET", "/api/v1/devices?site=colo", nil)
	_ = json.Unmarshal(body, &list)
	resp, body = h.do(t, "POST", fmt.Sprintf("/api/v1/devices/%d/scan", list.Items[0].ID), map[string]any{"plugins": []string{"icmp"}}, csrf, "1")
	if resp.StatusCode != 400 || !strings.Contains(string(body), "Standort Colo") {
		t.Fatalf("scan of a site device: %d %s", resp.StatusCode, body)
	}
}
