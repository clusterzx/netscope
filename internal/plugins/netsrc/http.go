package netsrc

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// ErrAuth reports rejected credentials (HTTP 401/403); the next credential may be tried.
var ErrAuth = errors.New("Anmeldung abgelehnt")

// HTTPStatusError is a non-success answer.
type HTTPStatusError struct {
	URL  string
	Code int
	Body string
}

func (e *HTTPStatusError) Error() string {
	msg := fmt.Sprintf("%s: HTTP %d", e.URL, e.Code)
	if b := strings.TrimSpace(e.Body); b != "" {
		if len(b) > 200 {
			b = b[:200] + "…"
		}
		msg += " (" + b + ")"
	}
	return msg
}

// NewHTTPClient returns a client for a device API. Devices often use self-signed
// certificates; verify turns the check on. A cookie jar keeps session cookies.
func NewHTTPClient(verify bool, timeout time.Duration) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Timeout: timeout, Jar: jar, Transport: &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verify, MinVersion: tls.VersionTLS12}, //nolint:gosec // opt-in per plugin setting
	}}
}

// Reply is what Do reports about an answer (the body is already read and closed).
type Reply struct {
	StatusCode int
	Header     http.Header
}

// Do sends a request and decodes a JSON answer into dst (nil: discard). 401 and 403 give
// ErrAuth; other non-2xx answers an HTTPStatusError. The reply is nil only when no answer
// arrived.
func Do(ctx context.Context, hc *http.Client, method, u string, body io.Reader, header http.Header, dst any) (*Reply, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	resp.Body.Close()
	reply := &Reply{StatusCode: resp.StatusCode, Header: resp.Header}
	if err != nil {
		return reply, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return reply, fmt.Errorf("%w (HTTP %d)", ErrAuth, resp.StatusCode)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return reply, &HTTPStatusError{URL: redact(u), Code: resp.StatusCode, Body: string(data)}
	}
	if dst == nil {
		return reply, nil
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return reply, fmt.Errorf("%s: Antwort ist kein erwartetes JSON: %w", redact(u), err)
	}
	return reply, nil
}

// redact hides credentials in query strings (access_token, api_key, password, sid).
func redact(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := p.Query()
	for k := range q {
		switch strings.ToLower(k) {
		case "access_token", "api_key", "apikey", "password", "sid", "token":
			q.Set(k, "***")
		}
	}
	p.RawQuery = q.Encode()
	p.User = nil
	return p.String()
}

// BaseURL turns a source entry (host, host:port or URL) into a base URL with scheme.
func BaseURL(entry, defScheme string, defPort int) (*url.URL, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil, errors.New("leerer Eintrag")
	}
	// the default port belongs to the default scheme; an explicit scheme keeps its own
	bare := !strings.Contains(entry, "://")
	if bare {
		if ip := net.ParseIP(entry); ip != nil && ip.To4() == nil {
			entry = "[" + entry + "]" // bare IPv6 address
		}
		entry = defScheme + "://" + entry
	}
	u, err := url.Parse(entry)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%q: Hostname, IP oder http(s)-URL erwartet", entry)
	}
	if bare && u.Port() == "" && defPort > 0 {
		u.Host = joinHostPort(u.Hostname(), defPort)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// Host is the host name of a source entry (for credential scopes and endpoints).
func Host(entry string) string {
	if u, err := BaseURL(entry, "https", 0); err == nil {
		return u.Hostname()
	}
	return strings.TrimSpace(entry)
}

func isAuth(err error) bool { return errors.Is(err, ErrAuth) }

// BasicAuth returns a header with HTTP basic authentication.
func BasicAuth(user, pass string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	return h
}
