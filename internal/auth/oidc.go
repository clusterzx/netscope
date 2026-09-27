package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sign-in with OpenID Connect: authorization code flow with PKCE (S256), state and nonce.
// The ID token is verified with the provider's published keys (asymmetric algorithms only).

const (
	oidcCallbackPath = "/api/v1/auth/oidc/callback"
	oidcTimeout      = 10 * time.Second
	oidcFlowTTL      = 10 * time.Minute
	oidcMaxPending   = 1000
	oidcCacheTTL     = time.Hour
)

var oidcAlgs = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512", "EdDSA"}

type oidcDiscovery struct {
	Issuer                   string   `json:"issuer"`
	AuthorizationEndpoint    string   `json:"authorization_endpoint"`
	TokenEndpoint            string   `json:"token_endpoint"`
	UserinfoEndpoint         string   `json:"userinfo_endpoint"`
	JWKSURI                  string   `json:"jwks_uri"`
	TokenEndpointAuthMethods []string `json:"token_endpoint_auth_methods_supported"`
	IDTokenSigningAlgs       []string `json:"id_token_signing_alg_values_supported"`
}

type oidcPending struct {
	nonce, verifier    string
	redirect, returnTo string
	created            time.Time
}

// oidcState caches discovery and keys and holds the running sign-ins.
type oidcState struct {
	mu        sync.Mutex
	issuer    string // configuration the cache belongs to
	disc      *oidcDiscovery
	discAt    time.Time
	keys      map[string]crypto.PublicKey
	keysAt    time.Time
	refreshed time.Time // last forced key refresh (unknown kid)
	pending   map[string]*oidcPending
}

func (o *oidcState) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.issuer, o.disc, o.keys = "", nil, nil
}

func (c OIDCConfig) client() (*http.Client, error) {
	// no fixed server name: token and key endpoints may live on other hosts
	tlsCfg, err := c.TLSOptions.config("")
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: oidcTimeout, Transport: &http.Transport{TLSClientConfig: tlsCfg, Proxy: http.ProxyFromEnvironment}}, nil
}

func getJSON(ctx context.Context, hc *http.Client, u string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dst)
}

// discovery returns the provider metadata (cached for an hour).
func (s *Service) discovery(ctx context.Context, c OIDCConfig, hc *http.Client) (*oidcDiscovery, error) {
	o := &s.oidc
	o.mu.Lock()
	if o.issuer == c.Issuer && o.disc != nil && time.Since(o.discAt) < oidcCacheTTL {
		d := o.disc
		o.mu.Unlock()
		return d, nil
	}
	o.mu.Unlock()
	var d oidcDiscovery
	if err := getJSON(ctx, hc, c.Issuer+"/.well-known/openid-configuration", &d); err != nil {
		return nil, fmt.Errorf("OIDC-Discovery: %w", err)
	}
	// the metadata must belong to the configured issuer (a trailing slash may differ)
	if strings.TrimRight(d.Issuer, "/") != strings.TrimRight(c.Issuer, "/") {
		return nil, fmt.Errorf("OIDC-Discovery: Issuer %q passt nicht zur Konfiguration %q", d.Issuer, c.Issuer)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		return nil, errors.New("OIDC-Discovery: Endpunkte für Autorisierung, Token oder Schlüssel fehlen")
	}
	o.mu.Lock()
	if o.issuer != c.Issuer {
		o.keys = nil
	}
	o.issuer, o.disc, o.discAt = c.Issuer, &d, time.Now()
	o.mu.Unlock()
	return &d, nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Crv string `json:"crv"`
	N   string `json:"n"`
	E   string `json:"e"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func b64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		n, err1 := b64(k.N)
		e, err2 := b64(k.E)
		if err1 != nil || err2 != nil || len(n) == 0 || len(e) == 0 || len(e) > 4 {
			return nil, errors.New("ungültiger RSA-Schlüssel")
		}
		ev := 0
		for _, b := range e {
			ev = ev<<8 | int(b)
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: ev}, nil
	case "EC":
		var curve elliptic.Curve
		switch k.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("Kurve %q nicht unterstützt", k.Crv)
		}
		x, err1 := b64(k.X)
		y, err2 := b64(k.Y)
		size := (curve.Params().BitSize + 7) / 8
		if err1 != nil || err2 != nil || len(x) > size || len(y) > size {
			return nil, errors.New("ungültiger EC-Schlüssel")
		}
		// uncompressed point 04 || X || Y; parsing checks that it lies on the curve
		point := make([]byte, 1+2*size)
		point[0] = 4
		copy(point[1+size-len(x):1+size], x)
		copy(point[1+2*size-len(y):], y)
		pk, err := ecdsa.ParseUncompressedPublicKey(curve, point)
		if err != nil {
			return nil, fmt.Errorf("ungültiger EC-Schlüssel: %w", err)
		}
		return pk, nil
	case "OKP":
		if k.Crv != "Ed25519" {
			return nil, fmt.Errorf("Kurve %q nicht unterstützt", k.Crv)
		}
		x, err := b64(k.X)
		if err != nil || len(x) != ed25519.PublicKeySize {
			return nil, errors.New("ungültiger Ed25519-Schlüssel")
		}
		return ed25519.PublicKey(x), nil
	}
	return nil, fmt.Errorf("Schlüsseltyp %q nicht unterstützt", k.Kty)
}

// fetchKeys loads the signing keys of the provider.
func fetchKeys(ctx context.Context, hc *http.Client, u string) (map[string]crypto.PublicKey, error) {
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := getJSON(ctx, hc, u, &set); err != nil {
		return nil, fmt.Errorf("OIDC-Schlüssel: %w", err)
	}
	keys := map[string]crypto.PublicKey{}
	for _, k := range set.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		pk, err := k.publicKey()
		if err != nil {
			continue
		}
		keys[k.Kid] = pk
	}
	if len(keys) == 0 {
		return nil, errors.New("OIDC-Schlüssel: keine verwendbaren Signaturschlüssel")
	}
	return keys, nil
}

// key returns the signing key kid, reloading the key set once when the kid is unknown
// (key rotation at the provider).
func (s *Service) key(ctx context.Context, hc *http.Client, d *oidcDiscovery, kid string) (crypto.PublicKey, error) {
	o := &s.oidc
	lookup := func(keys map[string]crypto.PublicKey) crypto.PublicKey {
		if k, ok := keys[kid]; ok {
			return k
		}
		if kid == "" && len(keys) == 1 {
			for _, k := range keys {
				return k
			}
		}
		return nil
	}
	o.mu.Lock()
	keys, fresh := o.keys, time.Since(o.keysAt) < oidcCacheTTL
	canRefresh := time.Since(o.refreshed) > time.Minute
	o.mu.Unlock()
	if k := lookup(keys); k != nil && fresh {
		return k, nil
	}
	if keys != nil && fresh && !canRefresh {
		return nil, fmt.Errorf("unbekannter Signaturschlüssel %q", kid)
	}
	keys, err := fetchKeys(ctx, hc, d.JWKSURI)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	o.keys, o.keysAt, o.refreshed = keys, time.Now(), time.Now()
	o.mu.Unlock()
	if k := lookup(keys); k != nil {
		return k, nil
	}
	return nil, fmt.Errorf("unbekannter Signaturschlüssel %q", kid)
}

// OIDCStart begins a sign-in: it returns the provider URL to send the browser to and the
// state to bind to the browser (cookie). redirectURI is the callback address of this
// request, returnTo the page to open afterwards.
func (s *Service) OIDCStart(ctx context.Context, redirectURI, returnTo string) (authURL, state string, err error) {
	c, _, err := s.oidcConfig(ctx)
	if err != nil {
		return "", "", err
	}
	if !c.Enabled {
		return "", "", errors.New("Die Anmeldung per OIDC ist nicht eingerichtet")
	}
	if c.RedirectURL != "" {
		redirectURI = c.RedirectURL
	}
	hc, err := c.client()
	if err != nil {
		return "", "", err
	}
	d, err := s.discovery(ctx, c, hc)
	if err != nil {
		return "", "", err
	}
	state, err = randomString(32)
	if err != nil {
		return "", "", err
	}
	nonce, err := randomString(32)
	if err != nil {
		return "", "", err
	}
	verifier, err := randomString(48)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	o := &s.oidc
	o.mu.Lock()
	if o.pending == nil {
		o.pending = map[string]*oidcPending{}
	}
	for k, p := range o.pending {
		if time.Since(p.created) > oidcFlowTTL {
			delete(o.pending, k)
		}
	}
	if len(o.pending) >= oidcMaxPending {
		o.mu.Unlock()
		return "", "", errors.New("zu viele offene Anmeldungen, bitte später erneut versuchen")
	}
	o.pending[state] = &oidcPending{nonce: nonce, verifier: verifier, redirect: redirectURI, returnTo: returnTo, created: time.Now()}
	o.mu.Unlock()
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {strings.Join(append([]string{"openid"}, c.Scopes...), " ")},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	sep := "?"
	if strings.Contains(d.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return d.AuthorizationEndpoint + sep + q.Encode(), state, nil
}

type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// exchange redeems the authorization code at the token endpoint.
func exchange(ctx context.Context, hc *http.Client, d *oidcDiscovery, c OIDCConfig, secret, code string, p *oidcPending) (*tokenResponse, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {p.redirect}, "code_verifier": {p.verifier},
		"client_id": {c.ClientID}}
	basic := secret != ""
	if basic && len(d.TokenEndpointAuthMethods) > 0 && !contains(d.TokenEndpointAuthMethods, "client_secret_basic") &&
		contains(d.TokenEndpointAuthMethods, "client_secret_post") {
		basic = false
		form.Set("client_secret", secret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if basic {
		req.SetBasicAuth(url.QueryEscape(c.ClientID), url.QueryEscape(secret))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Token-Abruf: %w", err)
	}
	defer resp.Body.Close()
	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tr); err != nil {
		return nil, fmt.Errorf("Token-Abruf: HTTP %d, Antwort nicht lesbar", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || tr.Error != "" {
		msg := tr.Error
		if tr.ErrorDesc != "" {
			msg += ": " + tr.ErrorDesc
		}
		return nil, fmt.Errorf("Token-Abruf abgelehnt (HTTP %d): %s", resp.StatusCode, msg)
	}
	if tr.IDToken == "" {
		return nil, errors.New("Token-Abruf: kein ID-Token (Scope openid fehlt?)")
	}
	return &tr, nil
}

// claimPath reads a claim by name or dot path (realm_access.roles).
func claimPath(claims map[string]any, path string) (any, bool) {
	if v, ok := claims[path]; ok {
		return v, true
	}
	cur := any(claims)
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func claimString(claims map[string]any, name string) string {
	v, _ := claimPath(claims, name)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func claimStrings(v any) []string {
	switch x := v.(type) {
	case string:
		if x = strings.TrimSpace(x); x != "" {
			return []string{x}
		}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

// verifyIDToken checks signature, issuer, audience, lifetime and nonce of an ID token.
func (s *Service) verifyIDToken(ctx context.Context, hc *http.Client, d *oidcDiscovery, c OIDCConfig, raw, nonce string) (jwt.MapClaims, error) {
	parser := jwt.NewParser(jwt.WithValidMethods(oidcAlgs), jwt.WithIssuer(d.Issuer), jwt.WithAudience(c.ClientID),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(2*time.Minute))
	claims := jwt.MapClaims{}
	_, err := parser.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return s.key(ctx, hc, d, kid)
	})
	if err != nil {
		return nil, fmt.Errorf("ID-Token ungültig: %w", err)
	}
	if n, _ := claims["nonce"].(string); n == "" || n != nonce {
		return nil, errors.New("ID-Token ungültig: Nonce passt nicht")
	}
	if aud, _ := claims.GetAudience(); len(aud) > 1 {
		if azp, _ := claims["azp"].(string); azp != c.ClientID {
			return nil, errors.New("ID-Token ungültig: für einen anderen Client ausgestellt (azp)")
		}
	}
	if sub, _ := claims.GetSubject(); sub == "" {
		return nil, errors.New("ID-Token ungültig: sub fehlt")
	}
	return claims, nil
}

// userinfo reads the userinfo endpoint (for claims the ID token does not carry).
func userinfo(ctx context.Context, hc *http.Client, d *oidcDiscovery, accessToken string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.UserinfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Userinfo: HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "jwt") {
		return nil, errors.New("Userinfo als JWT wird nicht unterstützt")
	}
	out := map[string]any{}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
	return out, err
}

// oidcIdentity maps the claims to an identity.
func (c OIDCConfig) identity(issuer string, claims map[string]any) Identity {
	sub, _ := claims["sub"].(string)
	id := Identity{Source: SourceOIDC, ExternalID: issuer + "|" + sub}
	for _, name := range []string{c.UsernameClaim, "preferred_username", "email", "sub"} {
		if name == "" {
			continue
		}
		if v := claimString(claims, name); v != "" {
			id.Username = v
			break
		}
	}
	id.DisplayName = claimString(claims, "name")
	id.Email = claimString(claims, "email")
	if v, ok := claimPath(claims, c.groupsClaim()); ok {
		id.Groups = claimStrings(v)
	}
	return id
}

func (c OIDCConfig) groupsClaim() string {
	if c.GroupsClaim == "" {
		return "groups"
	}
	return c.GroupsClaim
}

func oidcGroupMatch(want, have string) bool {
	return strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(have))
}

// OIDCCallback completes a sign-in: it checks the state, redeems the code, verifies the
// ID token, provisions the user and starts a session. It returns the page to open.
func (s *Service) OIDCCallback(ctx context.Context, state, code, ip, userAgent string) (*LoginResult, string, error) {
	o := &s.oidc
	o.mu.Lock()
	p := o.pending[state]
	delete(o.pending, state)
	o.mu.Unlock()
	if p == nil || time.Since(p.created) > oidcFlowTTL {
		return nil, "", errors.New("Die Anmeldung ist abgelaufen oder ungültig – bitte erneut versuchen")
	}
	if code == "" {
		return nil, p.returnTo, errors.New("Der Identity Provider hat keinen Code geliefert")
	}
	c, secret, err := s.oidcConfig(ctx)
	if err != nil {
		return nil, p.returnTo, err
	}
	if !c.Enabled {
		return nil, p.returnTo, errors.New("Die Anmeldung per OIDC ist nicht eingerichtet")
	}
	hc, err := c.client()
	if err != nil {
		return nil, p.returnTo, err
	}
	d, err := s.discovery(ctx, c, hc)
	if err != nil {
		return nil, p.returnTo, err
	}
	tr, err := exchange(ctx, hc, d, c, secret, code, p)
	if err != nil {
		return nil, p.returnTo, err
	}
	claims, err := s.verifyIDToken(ctx, hc, d, c, tr.IDToken, p.nonce)
	if err != nil {
		return nil, p.returnTo, err
	}
	// groups (and missing names) from userinfo when the ID token does not carry them
	if _, ok := claimPath(claims, c.groupsClaim()); !ok && d.UserinfoEndpoint != "" && tr.AccessToken != "" {
		info, err := userinfo(ctx, hc, d, tr.AccessToken)
		if err != nil {
			return nil, p.returnTo, err
		}
		if sub, _ := info["sub"].(string); sub != claims["sub"] {
			return nil, p.returnTo, errors.New("Userinfo gehört zu einem anderen Konto")
		}
		for k, v := range info {
			if _, ok := claims[k]; !ok {
				claims[k] = v
			}
		}
	}
	id := c.identity(d.Issuer, claims)
	uid, err := s.provision(ctx, id, c.Provisioning, oidcGroupMatch)
	if err != nil {
		return nil, p.returnTo, err
	}
	res, err := s.startSession(ctx, uid, ip, userAgent)
	return res, p.returnTo, err
}

// OIDCTest is the outcome of an OIDC configuration test.
type OIDCTest struct {
	OK                     bool       `json:"ok"`
	Steps                  []TestStep `json:"steps"`
	AuthorizationEndpoint  string     `json:"authorizationEndpoint,omitempty"`
	TokenEndpoint          string     `json:"tokenEndpoint,omitempty"`
	UserinfoEndpoint       string     `json:"userinfoEndpoint,omitempty"`
	SigningAlgorithms      []string   `json:"signingAlgorithms,omitempty"`
	Keys                   int        `json:"keys"`
	TokenEndpointAuthModes []string   `json:"tokenEndpointAuthMethods,omitempty"`
}

// TestOIDC checks a configuration without saving it: discovery and signing keys.
func (s *Service) TestOIDC(ctx context.Context, c OIDCConfig) (*OIDCTest, error) {
	c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	t := &OIDCTest{Steps: []TestStep{}}
	step := func(name string, err error) bool {
		st := TestStep{Name: name, OK: err == nil}
		if err != nil {
			st.Error = err.Error()
		}
		t.Steps = append(t.Steps, st)
		return err == nil
	}
	hc, err := c.client()
	if !step("TLS-Einstellungen", err) {
		return t, nil
	}
	var d oidcDiscovery
	err = getJSON(ctx, hc, c.Issuer+"/.well-known/openid-configuration", &d)
	if err == nil && strings.TrimRight(d.Issuer, "/") != c.Issuer {
		err = fmt.Errorf("Issuer %q passt nicht zur Konfiguration", d.Issuer)
	}
	if !step("Discovery", err) {
		return t, nil
	}
	t.AuthorizationEndpoint, t.TokenEndpoint, t.UserinfoEndpoint = d.AuthorizationEndpoint, d.TokenEndpoint, d.UserinfoEndpoint
	t.SigningAlgorithms, t.TokenEndpointAuthModes = d.IDTokenSigningAlgs, d.TokenEndpointAuthMethods
	keys, err := fetchKeys(ctx, hc, d.JWKSURI)
	if !step("Signaturschlüssel", err) {
		return t, nil
	}
	t.Keys = len(keys)
	t.OK = true
	return t, nil
}
