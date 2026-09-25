package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // TOTP
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"netscope/internal/auth"
	"netscope/internal/config"
)

// selfService are the changing routes every signed-in user may call for the own account.
var selfService = map[string]bool{
	"PUT /api/v1/auth/password":               true,
	"POST /api/v1/auth/2fa/totp":              true,
	"POST /api/v1/auth/2fa/totp/confirm":      true,
	"POST /api/v1/auth/2fa/totp/disable":      true,
	"POST /api/v1/auth/2fa/recovery":          true,
	"POST /api/v1/auth/passkeys/options":      true,
	"POST /api/v1/auth/passkeys":              true,
	"PATCH /api/v1/auth/passkeys/{id}":        true,
	"DELETE /api/v1/auth/passkeys/{id}":       true,
	"DELETE /api/v1/tokens/{id}":              true, // own tokens; others need users.manage
	"POST /api/v1/auth/login":                 true,
	"POST /api/v1/auth/logout":                true,
	"POST /api/v1/auth/login/totp":            true,
	"POST /api/v1/auth/login/recovery":        true,
	"POST /api/v1/auth/login/passkey":         true,
	"POST /api/v1/auth/login/passkey/options": true,
}

// TestRoutePermissions makes sure no changing route is open to every signed-in user by
// accident: new routes need a permission or an entry in selfService.
func TestRoutePermissions(t *testing.T) {
	s := New(Deps{Config: &config.Config{}})
	for _, rt := range s.routes {
		key := rt.Method + " " + rt.Path
		if rt.Perm != "" && !auth.ValidPermission(rt.Perm) {
			t.Errorf("%s: unknown permission %q", key, rt.Perm)
		}
		if rt.Scope == scopePublic || rt.Method == http.MethodGet {
			continue
		}
		if rt.Perm == "" && !selfService[key] {
			t.Errorf("%s changes data but needs no permission", key)
		}
		if rt.Perm != "" && selfService[key] {
			t.Errorf("%s is self-service but needs %s", key, rt.Perm)
		}
	}
}

// as returns a harness with its own cookie jar (another browser).
func (h *harness) as() *harness {
	jar, _ := cookiejar.New(nil)
	c := *h
	c.client = &http.Client{Jar: jar}
	return &c
}

func decodeBody[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return v
}

func expect(t *testing.T, what string, resp *http.Response, body []byte, status int, code string) {
	t.Helper()
	if resp.StatusCode != status || (code != "" && !strings.Contains(string(body), `"`+code+`"`)) {
		t.Fatalf("%s: want %d %s, got %d %s", what, status, code, resp.StatusCode, body)
	}
}

func TestRBAC(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	resp, body := h.do(t, "POST", "/api/v1/roles", map[string]any{"name": "Pflege", "permissions": []string{"devices.edit"}}, csrf, "1")
	expect(t, "create role", resp, body, 201, "")
	role := decodeBody[auth.Role](t, body)
	resp, body = h.do(t, "POST", "/api/v1/users", map[string]any{"username": "eva", "roleId": role.ID}, csrf, "1")
	expect(t, "create user", resp, body, 201, "")
	created := decodeBody[userCreated](t, body)

	eva := h.as()
	resp, body = eva.do(t, "POST", "/api/v1/auth/login", map[string]string{"username": "eva", "password": created.Password})
	expect(t, "login", resp, body, 200, "")
	if me := decodeBody[loginResponse](t, body); me.Principal == nil || !me.Principal.PasswordChange {
		t.Fatalf("start password must be changed: %s", body)
	}
	resp, body = eva.do(t, "GET", "/api/v1/devices", nil)
	expect(t, "before password change", resp, body, 403, "password_change_required")
	resp, body = eva.do(t, "PUT", "/api/v1/auth/password", map[string]string{"current": created.Password, "new": "evas-password-1"}, csrf, "1")
	expect(t, "password change", resp, body, 200, "")

	resp, body = eva.do(t, "GET", "/api/v1/devices", nil)
	expect(t, "read devices", resp, body, 200, "")
	resp, body = eva.do(t, "POST", "/api/v1/devices", map[string]string{"name": "drucker", "ip": "192.168.1.50"}, csrf, "1")
	expect(t, "create device", resp, body, 201, "")
	dev := decodeBody[idResponse](t, body)
	resp, body = eva.do(t, "DELETE", "/api/v1/devices/"+strconv.FormatInt(dev.ID, 10), nil, csrf, "1")
	expect(t, "delete device", resp, body, 403, "forbidden")
	resp, body = eva.do(t, "POST", "/api/v1/devices/bulk", map[string]any{"action": "delete", "ids": []int64{dev.ID}}, csrf, "1")
	expect(t, "bulk delete", resp, body, 403, "forbidden")
	resp, body = eva.do(t, "POST", "/api/v1/devices/bulk", map[string]any{"action": "add_tags", "ids": []int64{dev.ID}, "tags": []string{"x"}}, csrf, "1")
	expect(t, "bulk tags", resp, body, 200, "")
	for _, path := range []string{"/api/v1/credentials", "/api/v1/users", "/api/v1/roles", "/api/v1/audit", "/api/v1/system/backups"} {
		resp, body = eva.do(t, "GET", path, nil)
		expect(t, path, resp, body, 403, "forbidden")
	}
	resp, body = eva.do(t, "POST", "/api/v1/tokens", map[string]string{"name": "x", "scope": "read"}, csrf, "1")
	expect(t, "create token", resp, body, 403, "forbidden")

	// the role gains a permission: effective at once
	resp, body = h.do(t, "PUT", "/api/v1/roles/"+strconv.FormatInt(role.ID, 10),
		map[string]any{"name": "Pflege", "permissions": []string{"devices.edit", "devices.delete"}}, csrf, "1")
	expect(t, "update role", resp, body, 200, "")
	resp, body = eva.do(t, "DELETE", "/api/v1/devices/"+strconv.FormatInt(dev.ID, 10), nil, csrf, "1")
	expect(t, "delete device with permission", resp, body, 200, "")

	// disabled: the session ends
	resp, body = h.do(t, "PUT", "/api/v1/users/"+strconv.FormatInt(created.User.ID, 10),
		map[string]any{"username": "eva", "roleId": role.ID, "disabled": true}, csrf, "1")
	expect(t, "disable", resp, body, 200, "")
	resp, body = eva.do(t, "GET", "/api/v1/devices", nil)
	expect(t, "disabled session", resp, body, 401, "")

	// the own account and the last administrator stay
	admin := decodeBody[meResponse](t, func() []byte { _, b := h.do(t, "GET", "/api/v1/auth/me", nil); return b }())
	resp, body = h.do(t, "DELETE", "/api/v1/users/"+strconv.FormatInt(admin.User.ID, 10), nil, csrf, "1")
	expect(t, "delete self", resp, body, 400, "")
}

func totpCode(t *testing.T, secret string, offset int64) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30+offset)) //nolint:gosec // positive
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1000000)
}

func TestTOTPLoginHTTP(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	resp, body := h.do(t, "POST", "/api/v1/auth/2fa/totp", map[string]any{}, csrf, "1")
	expect(t, "totp setup", resp, body, 200, "")
	setup := decodeBody[auth.TOTPSetup](t, body)
	if !strings.HasPrefix(setup.QR, "data:image/svg+xml;base64,") || !strings.HasPrefix(setup.URI, "otpauth://totp/NetScope") {
		t.Fatalf("setup: %+v", setup)
	}
	resp, body = h.do(t, "POST", "/api/v1/auth/2fa/totp/confirm", map[string]string{"code": totpCode(t, setup.Secret, 0)}, csrf, "1")
	expect(t, "totp confirm", resp, body, 200, "")
	if codes := decodeBody[recoveryCodesResponse](t, body); len(codes.RecoveryCodes) != 10 {
		t.Fatalf("recovery codes: %s", body)
	}

	b := h.as()
	resp, body = b.do(t, "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": h.password})
	expect(t, "password step", resp, body, 200, "")
	step := decodeBody[loginResponse](t, body)
	if step.MFA == nil || step.User != nil || len(step.MFA.Methods) != 2 {
		t.Fatalf("second factor expected: %s", body)
	}
	resp, body = b.do(t, "GET", "/api/v1/devices", nil)
	expect(t, "no session before the second factor", resp, body, 401, "")
	resp, body = b.do(t, "POST", "/api/v1/auth/login/totp", map[string]string{"challenge": step.MFA.Challenge, "code": "12345x"})
	expect(t, "wrong code", resp, body, 401, "invalid_code")
	resp, body = b.do(t, "POST", "/api/v1/auth/login/totp", map[string]string{"challenge": step.MFA.Challenge, "code": totpCode(t, setup.Secret, 1)})
	expect(t, "totp step", resp, body, 200, "")
	resp, body = b.do(t, "GET", "/api/v1/devices", nil)
	expect(t, "session after the second factor", resp, body, 200, "")
	resp, body = b.do(t, "POST", "/api/v1/auth/login/totp", map[string]string{"challenge": step.MFA.Challenge, "code": totpCode(t, setup.Secret, 1)})
	expect(t, "used challenge", resp, body, 401, "challenge_expired")

	// passkeys need HTTPS with a host name: not over http://127.0.0.1
	resp, body = h.do(t, "GET", "/api/v1/auth/2fa", nil)
	expect(t, "2fa status", resp, body, 200, "")
	if st := decodeBody[mfaResponse](t, body); st.PasskeysAvailable || !st.TOTP || st.RecoveryCodes != 10 {
		t.Fatalf("status: %s", body)
	}
	resp, body = h.do(t, "POST", "/api/v1/auth/passkeys/options", nil, csrf, "1")
	expect(t, "passkey over http", resp, body, 400, "")
}

func TestRelyingParty(t *testing.T) {
	req := func(scheme, host, origin string) *http.Request {
		r := httptest.NewRequest("POST", "/", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r.WithContext(context.WithValue(r.Context(), keyClient, clientInfo{IP: "10.0.0.1", Scheme: scheme, Host: host}))
	}
	for _, c := range []struct {
		scheme, host, origin string
		want                 auth.RP
	}{
		{"https", "netscope.lan", "https://netscope.lan", auth.RP{ID: "netscope.lan", Origin: "https://netscope.lan"}},
		{"https", "netscope.lan:8443", "", auth.RP{ID: "netscope.lan", Origin: "https://netscope.lan:8443"}},
		{"http", "localhost:5173", "http://localhost:5173", auth.RP{ID: "localhost", Origin: "http://localhost:5173"}},
		{"http", "netscope.lan", "http://netscope.lan", auth.RP{}},   // no HTTPS
		{"https", "192.168.8.123", "", auth.RP{}},                    // IP address
		{"https", "netscope.lan", "https://evil.example", auth.RP{}}, // other origin
	} {
		if got := relyingParty(req(c.scheme, c.host, c.origin)); got != c.want {
			t.Errorf("%s://%s (%s): got %+v want %+v", c.scheme, c.host, c.origin, got, c.want)
		}
	}
}
