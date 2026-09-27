package netsrc

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDigestResponse(t *testing.T) {
	// RFC 2617, section 3.5
	got := digestResponse("MD5", "Mufasa", "testrealm@host.com", "Circle Of Life", "GET", "/dir/index.html",
		"dcd98b7102dd2f0e8b11d0f600bfb0c093", "00000001", "0a4f113b", "auth")
	if got != "6629fae49393a05397450978507c4ef1" {
		t.Fatalf("response %s", got)
	}
}

func TestDigestTransport(t *testing.T) {
	const nonce = "abc123"
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Add("WWW-Authenticate", `Basic realm="x"`)
			w.Header().Add("WWW-Authenticate", `Digest realm="F!Box SOAP-Auth", nonce="`+nonce+`", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := challenge{}
		for _, part := range splitParams(strings.TrimPrefix(auth, "Digest ")) {
			k, v, _ := strings.Cut(part, "=")
			p[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
		}
		want := digestResponse("MD5", "fritz1234", "F!Box SOAP-Auth", "geheim", r.Method, r.URL.RequestURI(), nonce, p["nc"], p["cnonce"], "auth")
		if p["username"] != "fritz1234" || p["response"] != want || p["uri"] != "/upnp/control/hosts" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	hc := &http.Client{Transport: &DigestTransport{User: "fritz1234", Password: "geheim"}}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/upnp/control/hosts", strings.NewReader("<soap/>"))
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "ok" || len(bodies) != 2 || bodies[1] != "<soap/>" {
		t.Fatalf("status %d body %q bodies %q", resp.StatusCode, b, bodies)
	}
	hc = &http.Client{Transport: &DigestTransport{User: "fritz1234", Password: "falsch"}}
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/upnp/control/hosts", strings.NewReader("<soap/>"))
	if resp, err := hc.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password: %v %v", resp.StatusCode, err)
	}
}
