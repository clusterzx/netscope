package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// UserAgent is sent with every request.
const UserAgent = "NetScope/1.0"

// Header names set by NetScope. They cannot be overridden by user-defined headers.
const (
	HeaderEvent          = "X-NetScope-Event"
	HeaderNotificationID = "X-NetScope-Notification-Id"
	HeaderTimestamp      = "X-NetScope-Timestamp"
	HeaderSignature      = "X-NetScope-Signature"
)

// DefaultTimeout applies when a request has no timeout.
const DefaultTimeout = 10 * time.Second

// Request is one JSON delivery to an HTTP endpoint.
type Request struct {
	URL    string
	Method string      // POST (default) or PUT
	Header http.Header // additional user-defined headers
	Body   []byte      // JSON body (see EncodePayload)

	Kind           string // notification kind, sent as X-NetScope-Event
	NotificationID int64  // sent as X-NetScope-Notification-Id if > 0

	// Secret enables the HMAC-SHA256 signature (X-NetScope-Timestamp/-Signature).
	Secret    string
	Timestamp time.Time // signature timestamp; defaults to now

	Timeout   time.Duration
	VerifyTLS bool
}

// StatusError is returned when the receiver answers with a non-2xx status.
type StatusError struct {
	StatusCode int
	Status     string // e.g. "404 Not Found"
	Excerpt    string // start of the response body, single line
	Location   string // redirect target (scheme://host only) for 3xx answers
}

func (e *StatusError) Error() string {
	if e.StatusCode >= 300 && e.StatusCode < 400 {
		msg := "Empfänger leitet weiter (HTTP " + e.Status + ")"
		if e.Location != "" {
			msg += " nach " + e.Location
		}
		return msg + " – bitte die endgültige URL eintragen"
	}
	msg := "Empfänger antwortete mit HTTP " + e.Status
	if e.Excerpt != "" {
		msg += ": " + e.Excerpt
	}
	return msg
}

// Sign returns the signature header value "sha256=" + hex(HMAC-SHA256(secret,
// timestamp + "." + body)).
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Send delivers the request. Errors never contain the URL path or query, header
// values or the secret, because they end up in logs and the UI.
func Send(ctx context.Context, r Request) error {
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("ungültige Ziel-URL (http:// oder https:// erwartet)")
	}
	target := u.Scheme + "://" + u.Host
	method := r.Method
	if method == "" {
		method = http.MethodPost
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, r.URL, bytes.NewReader(r.Body))
	if err != nil {
		return errors.New("Anfrage konnte nicht erstellt werden (URL/Methode prüfen)")
	}
	for name, values := range r.Header {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if r.Kind != "" {
		req.Header.Set(HeaderEvent, r.Kind)
	}
	if r.NotificationID > 0 {
		req.Header.Set(HeaderNotificationID, strconv.FormatInt(r.NotificationID, 10))
	}
	if r.Secret != "" {
		ts := r.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		stamp := strconv.FormatInt(ts.Unix(), 10)
		req.Header.Set(HeaderTimestamp, stamp)
		req.Header.Set(HeaderSignature, Sign(r.Secret, stamp, r.Body))
	}

	resp, err := client(r.VerifyTLS).Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
			return fmt.Errorf("%s: Zeitüberschreitung nach %s", target, timeout)
		}
		return fmt.Errorf("%s nicht erreichbar: %w", target, err)
	}
	defer resp.Body.Close()
	head, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	se := &StatusError{StatusCode: resp.StatusCode, Status: resp.Status, Excerpt: Excerpt(head, 200)}
	if se.Status == "" {
		se.Status = strconv.Itoa(resp.StatusCode)
	}
	if loc, err := resp.Location(); err == nil && loc.Host != "" {
		se.Location = loc.Scheme + "://" + loc.Host
	}
	return se
}

// Excerpt returns the start of a response body as a single, printable line of at most
// n runes.
func Excerpt(b []byte, n int) string {
	s := strings.ToValidUTF8(string(b), "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n]) + "…"
	}
	return s
}

var transports = sync.OnceValues(func() (secure, insecure *http.Transport) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	secure = base.Clone()
	secure.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	insecure = base.Clone()
	insecure.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true} //nolint:gosec // explicit user setting verify_tls=false
	return secure, insecure
})

func client(verifyTLS bool) *http.Client {
	secure, insecure := transports()
	tr := secure
	if !verifyTLS {
		tr = insecure
	}
	return &http.Client{
		Transport: tr,
		// Redirects are not followed: net/http would turn a POST into a GET on 301/302
		// and the payload would be lost silently.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
