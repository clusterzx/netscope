package netsrc

import (
	"crypto/md5" //nolint:gosec // HTTP digest authentication (RFC 2617) is defined with MD5
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// DigestTransport answers HTTP digest challenges (RFC 7616, MD5 and SHA-256, qop=auth):
// a request answered with 401 is sent again with credentials. Request bodies must be
// replayable (http.NewRequest with a bytes/strings reader sets GetBody).
type DigestTransport struct {
	User, Password string
	Base           http.RoundTripper

	mu sync.Mutex
	nc int
}

// RoundTrip implements http.RoundTripper.
func (t *DigestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	first := req.Clone(req.Context())
	if req.GetBody != nil {
		b, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		first.Body = b
	}
	resp, err := base.RoundTrip(first)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	chal := parseChallenge(resp.Header.Values("WWW-Authenticate"))
	if chal == nil {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	second := req.Clone(req.Context())
	if req.GetBody != nil {
		b, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		second.Body = b
	}
	auth, err := t.authorization(chal, req.Method, req.URL.RequestURI())
	if err != nil {
		return nil, err
	}
	second.Header.Set("Authorization", auth)
	return base.RoundTrip(second)
}

type challenge map[string]string

// parseChallenge picks the Digest challenge (preferring SHA-256 over MD5).
func parseChallenge(headers []string) challenge {
	var best challenge
	for _, h := range headers {
		scheme, rest, ok := strings.Cut(strings.TrimSpace(h), " ")
		if !ok || !strings.EqualFold(scheme, "Digest") {
			continue
		}
		c := challenge{}
		for _, part := range splitParams(rest) {
			k, v, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			c[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
		}
		alg := strings.ToUpper(c["algorithm"])
		if alg != "" && alg != "MD5" && alg != "SHA-256" {
			continue
		}
		if best == nil || alg == "SHA-256" {
			best = c
		}
	}
	return best
}

// splitParams splits "a=1, b=\"x, y\"" at commas outside quotes.
func splitParams(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			cur.WriteRune(r)
		case r == ',' && !quoted:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func (t *DigestTransport) authorization(c challenge, method, uri string) (string, error) {
	alg := "MD5"
	if strings.EqualFold(c["algorithm"], "SHA-256") {
		alg = "SHA-256"
	}
	qop := ""
	for _, q := range strings.Split(c["qop"], ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `Digest username="%s", realm="%s", nonce="%s", uri="%s", algorithm=%s`, t.User, c["realm"], c["nonce"], uri, alg)
	if qop == "" {
		fmt.Fprintf(&b, `, response="%s"`, digestResponse(alg, t.User, c["realm"], t.Password, method, uri, c["nonce"], "", "", ""))
	} else {
		cb := make([]byte, 8)
		if _, err := rand.Read(cb); err != nil {
			return "", err
		}
		cnonce := hex.EncodeToString(cb)
		t.mu.Lock()
		t.nc++
		nc := fmt.Sprintf("%08x", t.nc)
		t.mu.Unlock()
		resp := digestResponse(alg, t.User, c["realm"], t.Password, method, uri, c["nonce"], nc, cnonce, qop)
		fmt.Fprintf(&b, `, response="%s", qop=%s, nc=%s, cnonce="%s"`, resp, qop, nc, cnonce)
	}
	if o := c["opaque"]; o != "" {
		fmt.Fprintf(&b, `, opaque="%s"`, o)
	}
	return b.String(), nil
}

// digestResponse computes the response value (qop "" = RFC 2069 compatibility).
func digestResponse(alg, user, realm, pass, method, uri, nonce, nc, cnonce, qop string) string {
	h := md5.New
	if alg == "SHA-256" {
		h = sha256.New
	}
	sum := func(s string) string {
		x := h()
		x.Write([]byte(s))
		return hex.EncodeToString(x.Sum(nil))
	}
	ha1 := sum(user + ":" + realm + ":" + pass)
	ha2 := sum(method + ":" + uri)
	if qop == "" {
		return sum(ha1 + ":" + nonce + ":" + ha2)
	}
	return sum(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
}
