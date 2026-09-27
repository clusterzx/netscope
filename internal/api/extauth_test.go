package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"netscope/internal/auth"
	"netscope/internal/auth/oidctest"
)

// browser is a client that does not follow redirects (the test plays them).
func browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func get(t *testing.T, c *http.Client, u string) *http.Response {
	t.Helper()
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestOIDCSignInOverHTTP(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	idp := oidctest.New(t)
	var admin int64
	roles, _ := h.auth.Roles(context.Background())
	for _, r := range roles {
		if r.Admin {
			admin = r.ID
		}
	}
	secret := idp.Secret
	resp, body := h.do(t, "PUT", "/api/v1/system/auth/oidc", oidcSaveRequest{Config: auth.OIDCConfig{Enabled: true, Name: "Test-IdP",
		Issuer: idp.Issuer(), ClientID: idp.ClientID, Provisioning: auth.Provisioning{Mappings: []auth.RoleMapping{{Group: "ops", RoleID: admin}}}},
		ClientSecret: &secret}, csrf, "1")
	if resp.StatusCode != 200 || strings.Contains(string(body), idp.Secret) {
		t.Fatalf("save: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, "GET", "/api/v1/system/auth", nil)
	var cfg externalAuthResponse
	_ = json.Unmarshal(body, &cfg)
	if resp.StatusCode != 200 || !cfg.OIDC.HasSecret || cfg.RedirectURL != h.srv.URL+"/api/v1/auth/oidc/callback" {
		t.Fatalf("config: %d %s", resp.StatusCode, body)
	}

	b := browser()
	resp, err := b.Get(h.srv.URL + "/api/v1/auth/providers")
	if err != nil {
		t.Fatal(err)
	}
	var prov auth.Providers
	_ = json.NewDecoder(resp.Body).Decode(&prov)
	resp.Body.Close()
	if !prov.OIDC || prov.OIDCName != "Test-IdP" {
		t.Fatalf("providers %+v", prov)
	}

	// start: redirect to the provider, state cookie for this browser
	resp = get(t, b, h.srv.URL+"/api/v1/auth/oidc/start?next=%2Fdevices%3Fq%3Dx")
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, idp.URL+"/authorize?") {
		t.Fatalf("start: %d %s", resp.StatusCode, loc)
	}
	idp.Claims = map[string]any{"preferred_username": "ops-user", "groups": []string{"ops"}}
	code, state := idp.Authorize(t, loc, "sub-ops")

	// another browser cannot finish this sign-in
	other := browser()
	resp = get(t, other, h.srv.URL+"/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "code": {code}}.Encode())
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "/login?error=") {
		t.Fatalf("foreign browser: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp = get(t, b, h.srv.URL+"/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "code": {code}}.Encode())
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/devices?q=x" {
		t.Fatalf("callback: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, err = b.Get(h.srv.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	var me meResponse
	_ = json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()
	if resp.StatusCode != 200 || me.User == nil || me.User.Username != "ops-user" || me.User.AuthSource != auth.SourceOIDC {
		t.Fatalf("me: %d %+v", resp.StatusCode, me.User)
	}

	// the provider refuses: back to the login page with its reason; open redirects are cut
	resp = get(t, b, h.srv.URL+"/api/v1/auth/oidc/start?next=%2F%2Fevil.example")
	code, state = idp.Authorize(t, resp.Header.Get("Location"), "sub-ops")
	_ = code
	resp = get(t, b, h.srv.URL+"/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "error": {"access_denied"}}.Encode())
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/login?error=") || !strings.Contains(loc, "access_denied") {
		t.Fatalf("refused: %s", loc)
	}
	for in, want := range map[string]string{"//evil.example": "/", "/\\evil": "/", "https://x": "/", "/login": "/", "/api/v1/x": "/", "/devices/3": "/devices/3"} {
		if got := localPath(in); got != want {
			t.Errorf("localPath(%q) = %q", in, got)
		}
	}
}

func TestExternalAuthConfigAPI(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	resp, body := h.do(t, "PUT", "/api/v1/system/auth/ldap", ldapSaveRequest{Config: auth.LDAPConfig{Enabled: true, URL: "http://x"}}, csrf, "1")
	if resp.StatusCode != 400 || !strings.Contains(string(body), "url") {
		t.Fatalf("invalid ldap: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, "POST", "/api/v1/system/auth/ldap/test", ldapTestRequest{Config: auth.LDAPConfig{URL: "ldap://127.0.0.1:1", BaseDN: "dc=x"}}, csrf, "1")
	var res auth.LDAPTest
	_ = json.Unmarshal(body, &res)
	if resp.StatusCode != 200 || res.OK || len(res.Steps) != 1 || res.Steps[0].OK {
		t.Fatalf("ldap test: %d %s", resp.StatusCode, body)
	}
	// only users with "users.manage"
	tok, _, err := h.auth.CreateToken(context.Background(), 1, "read", auth.ScopeRead, nil)
	if err != nil {
		t.Fatal(err)
	}
	anon := &harness{srv: h.srv, client: &http.Client{}}
	if resp, _ := anon.do(t, "GET", "/api/v1/system/auth", nil); resp.StatusCode != 401 {
		t.Errorf("anonymous: %d", resp.StatusCode)
	}
	if resp, _ := anon.do(t, "PUT", "/api/v1/system/auth/ldap", ldapSaveRequest{}, "Authorization", "Bearer "+tok); resp.StatusCode != 403 {
		t.Errorf("read token: %d", resp.StatusCode)
	}
}
