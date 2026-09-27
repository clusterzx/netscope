package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"netscope/internal/auth/oidctest"
)

const testRedirect = "https://netscope.example/api/v1/auth/oidc/callback"

// oidcSetup configures sign-in with idp: group "netscope-admins" → Administrator,
// "staff" → a role that requires 2FA, others no access.
func oidcSetup(t *testing.T, s *Service, idp *oidctest.Provider) (admin, staff int64) {
	t.Helper()
	ctx := context.Background()
	_ = s.db.R.QueryRow("SELECT id FROM roles WHERE builtin = 'admin'").Scan(&admin)
	r, err := s.CreateRole(ctx, RoleInput{Name: "Team", Permissions: []string{}, Require2FA: true})
	if err != nil {
		t.Fatal(err)
	}
	staff = r.ID
	secret := idp.Secret
	out, err := s.SaveOIDC(ctx, OIDCConfig{Enabled: true, Name: "Authentik", Issuer: idp.Issuer() + "/", ClientID: idp.ClientID,
		Scopes: []string{"profile email", "groups", "openid"}, GroupsClaim: "groups",
		Provisioning: Provisioning{Mappings: []RoleMapping{{Group: "netscope-admins", RoleID: admin}, {Group: "Staff", RoleID: staff}}, SyncRole: true}},
		&secret)
	if err != nil {
		t.Fatal(err)
	}
	if !out.HasSecret || out.Issuer != idp.Issuer() || strings.Join(out.Scopes, " ") != "profile email groups" {
		t.Fatalf("saved %+v", out)
	}
	return admin, staff
}

// oidcLogin runs a complete sign-in for sub and returns the result of the callback.
func oidcLogin(t *testing.T, s *Service, idp *oidctest.Provider, sub string) (*LoginResult, string, error) {
	t.Helper()
	ctx := context.Background()
	authURL, state, err := s.OIDCStart(ctx, testRedirect, "/devices")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(authURL, idp.URL+"/authorize?") || !strings.Contains(authURL, "scope=openid+profile+email+groups") {
		t.Fatalf("auth url %s", authURL)
	}
	code, st := idp.Authorize(t, authURL, sub)
	if st != state {
		t.Fatal("state not passed through")
	}
	return s.OIDCCallback(ctx, state, code, "1.2.3.4", "test")
}

func TestOIDCLogin(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	if _, _, err := s.EnsureAdmin(ctx, "local-admin-pw"); err != nil {
		t.Fatal(err)
	}
	idp := oidctest.New(t)
	admin, staff := oidcSetup(t, s, idp)
	if p := s.Providers(ctx); !p.OIDC || p.OIDCName != "Authentik" || p.LDAP {
		t.Fatalf("providers %+v", p)
	}

	idp.Claims = map[string]any{"preferred_username": "Erika Muster", "email": "erika@example.org", "name": "Erika Mustermann",
		"groups": []string{"users", "netscope-admins"}}
	res, next, err := oidcLogin(t, s, idp, "sub-erika")
	if err != nil || res.Token == "" || next != "/devices" {
		t.Fatalf("login: %+v %q %v", res, next, err)
	}
	p := sessionOf(t, s, res.Token)
	if p.Username != "Erika.Muster" || !p.Admin || p.RoleID != admin || p.Restricted() != "" {
		t.Fatalf("erika: %+v", p)
	}
	u, _ := s.User(ctx, p.UserID)
	if u.AuthSource != SourceOIDC || u.Email != "erika@example.org" || u.DisplayName != "Erika Mustermann" {
		t.Fatalf("user %+v", u)
	}
	// same subject, same account; no password sign-in
	if res, _, err := oidcLogin(t, s, idp, "sub-erika"); err != nil || sessionOf(t, s, res.Token).UserID != p.UserID {
		t.Fatalf("second login: %v", err)
	}
	if _, err := s.Login(ctx, "Erika.Muster", "", "", "9.9.9.9", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("password login of an OIDC account: %v", err)
	}
	if err := s.ChangePassword(ctx, p.UserID, "", "x", "a-new-password"); err == nil {
		t.Error("password set for an OIDC account")
	}

	// groups from userinfo when the ID token has none; a role requiring 2FA does not
	// restrict OIDC sessions (the provider decides about the second factor)
	idp.Claims = map[string]any{"preferred_username": "max"}
	idp.Userinfo = map[string]any{"groups": "staff"}
	res, _, err = oidcLogin(t, s, idp, "sub-max")
	if err != nil {
		t.Fatal(err)
	}
	if p := sessionOf(t, s, res.Token); p.RoleID != staff || p.MFASetup || p.Restricted() != "" {
		t.Fatalf("max: %+v", p)
	}

	// no matching group, no default role
	idp.Claims = map[string]any{"preferred_username": "guest", "groups": []string{"visitors"}}
	idp.Userinfo = nil
	if _, _, err := oidcLogin(t, s, idp, "sub-guest"); !errors.Is(err, ErrNoRole) {
		t.Errorf("no role: %v", err)
	}
	// a local account of the same name is never taken over
	if _, _, err := s.CreateUser(ctx, UserInput{Username: "mallory", RoleID: staff, Password: "local-mallory-pw"}); err != nil {
		t.Fatal(err)
	}
	idp.Claims = map[string]any{"preferred_username": "mallory", "groups": []string{"netscope-admins"}}
	if _, _, err := oidcLogin(t, s, idp, "sub-mallory"); err == nil || !strings.Contains(err.Error(), "bereits vergeben") {
		t.Errorf("name conflict: %v", err)
	}
	// a nested claim path (Keycloak: realm_access.roles)
	c, _, _ := s.oidcConfig(ctx)
	c.GroupsClaim = "realm_access.roles"
	if _, err := s.SaveOIDC(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	idp.Claims = map[string]any{"preferred_username": "kc", "realm_access": map[string]any{"roles": []string{"staff"}}}
	if res, _, err := oidcLogin(t, s, idp, "sub-kc"); err != nil || sessionOf(t, s, res.Token).RoleID != staff {
		t.Errorf("nested claim: %v", err)
	}
}

func TestOIDCRejects(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	idp := oidctest.New(t)
	oidcSetup(t, s, idp)
	idp.Claims = map[string]any{"preferred_username": "erika", "groups": []string{"netscope-admins"}}

	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	for name, c := range map[string]struct {
		tamper func(jwt.MapClaims)
		method jwt.SigningMethod
		key    any
		want   string
	}{
		"other client":    {tamper: func(c jwt.MapClaims) { c["aud"] = "someone-else" }, want: "audience"},
		"other nonce":     {tamper: func(c jwt.MapClaims) { c["nonce"] = "x" }, want: "Nonce"},
		"expired":         {tamper: func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, want: "expired"},
		"other issuer":    {tamper: func(c jwt.MapClaims) { c["iss"] = "https://evil.example" }, want: "issuer"},
		"no subject":      {tamper: func(c jwt.MapClaims) { delete(c, "sub") }, want: "sub"},
		"azp":             {tamper: func(c jwt.MapClaims) { c["aud"] = []string{"netscope", "other"}; c["azp"] = "other" }, want: "azp"},
		"symmetric alg":   {method: jwt.SigningMethodHS256, key: []byte(idp.Secret), want: "signing method"},
		"unknown key":     {method: jwt.SigningMethodES256, key: ecKey, want: "ID-Token ungültig"},
		"unsigned (none)": {method: jwt.SigningMethodNone, key: jwt.UnsafeAllowNoneSignatureType, want: "ID-Token ungültig"},
	} {
		t.Run(name, func(t *testing.T) {
			idp.Tamper, idp.SignMethod, idp.SignKey = c.tamper, c.method, c.key
			defer func() { idp.Tamper, idp.SignMethod, idp.SignKey = nil, nil, nil }()
			_, _, err := oidcLogin(t, s, idp, "sub-erika")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}

	// state: unknown, and never usable twice
	if _, _, err := s.OIDCCallback(ctx, "unknown", "code", "", ""); err == nil {
		t.Error("unknown state accepted")
	}
	authURL, state, _ := s.OIDCStart(ctx, testRedirect, "/")
	code, _ := idp.Authorize(t, authURL, "sub-erika")
	if _, _, err := s.OIDCCallback(ctx, state, code, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OIDCCallback(ctx, state, code, "", ""); err == nil {
		t.Error("state replayed")
	}
	// a wrong client secret is refused by the provider
	c, _, _ := s.oidcConfig(ctx)
	wrong := "nope"
	if _, err := s.SaveOIDC(ctx, c, &wrong); err != nil {
		t.Fatal(err)
	}
	if _, _, err := oidcLogin(t, s, idp, "sub-erika"); err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("wrong secret: %v", err)
	}
	// discovery of another issuer
	idp.IssuerOverride = "https://evil.example"
	s.oidc.reset()
	if _, _, err := s.OIDCStart(ctx, testRedirect, "/"); err == nil || !strings.Contains(err.Error(), "passt nicht") {
		t.Errorf("issuer mismatch: %v", err)
	}
}

func TestOIDCConfigAndTest(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	idp := oidctest.New(t)
	for _, bad := range []OIDCConfig{
		{Enabled: true, Issuer: "not a url", ClientID: "x"},
		{Enabled: true, Issuer: "https://idp.example"},
		{Issuer: "https://idp.example", RedirectURL: "https://x/elsewhere"},
		{Provisioning: Provisioning{DefaultRoleID: 999}},
	} {
		if _, err := s.SaveOIDC(ctx, bad, nil); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	res, err := s.TestOIDC(ctx, OIDCConfig{Issuer: idp.Issuer()})
	if err != nil || !res.OK || res.Keys != 1 || res.TokenEndpoint != idp.URL+"/token" {
		t.Fatalf("test: %+v %v", res, err)
	}
	if res, _ := s.TestOIDC(ctx, OIDCConfig{Issuer: idp.URL + "/nope"}); res.OK {
		t.Error("missing discovery accepted")
	}
	if _, _, err := s.OIDCStart(ctx, testRedirect, "/"); err == nil {
		t.Error("start without configuration")
	}
}

func TestJWKKeys(t *testing.T) {
	ec, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	raw, _ := ec.PublicKey.Bytes() // 04 || X || Y
	size := (len(raw) - 1) / 2
	b := func(p []byte) string { return base64.RawURLEncoding.EncodeToString(p) }
	k, err := jwk{Kty: "EC", Crv: "P-384", X: b(raw[1 : 1+size]), Y: b(raw[1+size:])}.publicKey()
	if err != nil || !k.(*ecdsa.PublicKey).Equal(&ec.PublicKey) {
		t.Fatalf("EC: %v", err)
	}
	// a point that is not on the curve
	bad := append([]byte{}, raw...)
	bad[len(bad)-1] ^= 1
	if _, err := (jwk{Kty: "EC", Crv: "P-384", X: b(bad[1 : 1+size]), Y: b(bad[1+size:])}).publicKey(); err == nil {
		t.Error("invalid EC point accepted")
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, err = jwk{Kty: "OKP", Crv: "Ed25519", X: b(pub)}.publicKey()
	if err != nil || !k.(ed25519.PublicKey).Equal(pub) {
		t.Fatalf("Ed25519: %v", err)
	}
	for _, bad := range []jwk{{Kty: "EC", Crv: "P-192"}, {Kty: "oct"}, {Kty: "RSA", N: "AQAB", E: ""}, {Kty: "OKP", Crv: "X25519", X: b(pub)}} {
		if _, err := bad.publicKey(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
