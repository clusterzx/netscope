// Package oidctest is an OpenID Connect provider for tests: discovery, signing keys,
// token endpoint (authorization code with PKCE and client_secret_basic) and userinfo.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Provider is a running test provider.
type Provider struct {
	*httptest.Server
	ClientID, Secret string
	Key              *rsa.PrivateKey
	KID              string

	mu    sync.Mutex
	codes map[string]*grant
	// Claims are added to every ID token (sub is set per sign-in).
	Claims map[string]any
	// Userinfo claims (sub is added); nil: endpoint answers 404.
	Userinfo map[string]any
	// Tamper changes the ID token claims before signing (tests of the verification).
	Tamper func(claims jwt.MapClaims)
	// SignWith replaces the signing (method, key) when set.
	SignMethod jwt.SigningMethod
	SignKey    any
	// IssuerOverride is announced in the discovery document instead of the real URL.
	IssuerOverride string
}

type grant struct {
	sub, nonce, challenge, redirect string
}

// New starts a provider with an RSA key and client "netscope" / "s3cret".
func New(t testing.TB) *Provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{ClientID: "netscope", Secret: "s3cret", Key: key, KID: "k1", codes: map[string]*grant{}, Claims: map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("POST /token", p.token)
	mux.HandleFunc("GET /userinfo", p.userinfo)
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Close)
	return p
}

// Issuer is the issuer URL.
func (p *Provider) Issuer() string { return p.URL }

func (p *Provider) discovery(w http.ResponseWriter, r *http.Request) {
	iss := p.URL
	if p.IssuerOverride != "" {
		iss = p.IssuerOverride
	}
	writeJSON(w, map[string]any{"issuer": iss, "authorization_endpoint": p.URL + "/authorize", "token_endpoint": p.URL + "/token",
		"userinfo_endpoint": p.URL + "/userinfo", "jwks_uri": p.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"}})
}

func (p *Provider) jwks(w http.ResponseWriter, r *http.Request) {
	pub := p.Key.PublicKey
	writeJSON(w, map[string]any{"keys": []map[string]any{{"kty": "RSA", "kid": p.KID, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())}}})
}

// Authorize plays the user's sign-in at the provider: it reads the authorization URL
// NetScope redirected to and returns the code and state the provider sends back.
func (p *Provider) Authorize(t testing.TB, authURL, sub string) (code, state string) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization request %v", q)
	}
	code = sub + "-code-" + q.Get("state")[:8]
	p.mu.Lock()
	p.codes[code] = &grant{sub: sub, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	p.mu.Unlock()
	return code, q.Get("state")
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	id, _ = url.QueryUnescape(id)
	secret, _ = url.QueryUnescape(secret)
	if id != p.ClientID || secret != p.Secret {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]any{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	g := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if g == nil || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge || r.PostForm.Get("redirect_uri") != g.redirect {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "invalid_grant", "error_description": "code, verifier or redirect_uri wrong"})
		return
	}
	now := time.Now()
	claims := jwt.MapClaims{"iss": p.URL, "aud": p.ClientID, "sub": g.sub, "nonce": g.nonce, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()}
	for k, v := range p.Claims {
		claims[k] = v
	}
	if p.Tamper != nil {
		p.Tamper(claims)
	}
	method, key := jwt.SigningMethod(jwt.SigningMethodRS256), any(p.Key)
	if p.SignMethod != nil {
		method, key = p.SignMethod, p.SignKey
	}
	tok := jwt.NewWithClaims(method, claims)
	tok.Header["kid"] = p.KID
	signed, err := tok.SignedString(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"id_token": signed, "access_token": "at-" + g.sub, "token_type": "Bearer", "expires_in": 300})
}

func (p *Provider) userinfo(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	info := p.Userinfo
	p.mu.Unlock()
	if info == nil {
		http.NotFound(w, r)
		return
	}
	tok := r.Header.Get("Authorization")
	if len(tok) < 10 || tok[:10] != "Bearer at-" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	out := map[string]any{"sub": tok[10:]}
	for k, v := range info {
		out[k] = v
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
