package openwrt

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"netscope/internal/netutil"
)

// nullSession is the ubus session id used for session.login.
const nullSession = "00000000000000000000000000000000"

// ubusStatus names the ubus status codes returned in JSON-RPC results.
var ubusStatus = map[int]string{
	1: "ungültiger Befehl", 2: "ungültiges Argument", 3: "Methode nicht gefunden", 4: "nicht gefunden",
	5: "keine Daten", 6: "Zugriff verweigert", 7: "Zeitüberschreitung", 8: "nicht unterstützt",
	9: "unbekannter Fehler", 10: "Verbindung fehlgeschlagen",
}

// ubusError is a non-zero ubus status or a JSON-RPC error.
type ubusError struct {
	Call string
	Code int
	Msg  string
}

func (e *ubusError) Error() string {
	msg := e.Msg
	if msg == "" {
		msg = ubusStatus[e.Code]
	}
	return fmt.Sprintf("ubus %s: %s (%d)", e.Call, msg, e.Code)
}

// errAccessDenied reports a denied login or call.
func errAccessDenied(err error) bool {
	var ue *ubusError
	return errors.As(err, &ue) && (ue.Code == 6 || ue.Code == -32002)
}

// ubusClient calls ubus over the JSON-RPC endpoint of uhttpd (LuCI).
type ubusClient struct {
	endpoint string
	http     *http.Client
	session  string
	id       int
}

// ubusEndpoint derives the /ubus URL from a LuCI URL (http://router, …/cgi-bin/luci, …/ubus).
func ubusEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("ungültige LuCI-URL %q (erwartet z. B. http://192.168.8.1)", raw)
	}
	p := strings.TrimRight(u.Path, "/")
	if i := strings.Index(p, "/cgi-bin/luci"); i >= 0 {
		p = p[:i]
	}
	if !strings.HasSuffix(p, "/ubus") {
		p += "/ubus"
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = p, "", "", ""
	return u.String(), nil
}

func newUbusClient(endpoint string, verifyTLS bool, timeout time.Duration) *ubusClient {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: !verifyTLS} //nolint:gosec // OpenWrt ships self-signed certificates
	return &ubusClient{endpoint: endpoint, http: &http.Client{Transport: tr, Timeout: timeout}, session: nullSession}
}

func (c *ubusClient) close() { c.http.CloseIdleConnections() }

// call invokes object.method with args and decodes the result data into out.
func (c *ubusClient) call(ctx context.Context, object, method string, args any, out any) error {
	c.id++
	name := object + "." + method
	if args == nil {
		args = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": c.id, "method": "call",
		"params": []any{c.session, object, method, args},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ubus %s: %w", name, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("ubus %s: %w", name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ubus %s: HTTP %s", name, resp.Status)
	}
	var r struct {
		Result []json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("ubus %s: ungültige Antwort: %w", name, err)
	}
	if r.Error != nil {
		return &ubusError{Call: name, Code: r.Error.Code, Msg: r.Error.Message}
	}
	if len(r.Result) == 0 {
		return &ubusError{Call: name, Code: 9}
	}
	var code int
	if err := json.Unmarshal(r.Result[0], &code); err != nil {
		return fmt.Errorf("ubus %s: ungültiger Status: %w", name, err)
	}
	if code != 0 {
		return &ubusError{Call: name, Code: code}
	}
	if out != nil && len(r.Result) > 1 {
		if err := json.Unmarshal(r.Result[1], out); err != nil {
			return fmt.Errorf("ubus %s: %w", name, err)
		}
	}
	return nil
}

// login opens a ubus session (session.login).
func (c *ubusClient) login(ctx context.Context, user, password string) error {
	var res struct {
		Session string `json:"ubus_rpc_session"`
	}
	c.session = nullSession
	if err := c.call(ctx, "session", "login", map[string]any{"username": user, "password": password}, &res); err != nil {
		return err
	}
	if res.Session == "" {
		return errors.New("ubus session.login: keine Session erhalten")
	}
	c.session = res.Session
	return nil
}

// luciLease is an item of luci-rpc getDHCPLeases (rpcd-mod-luci). expires holds the
// remaining seconds or false for infinite leases.
type luciLease struct {
	Expires  json.RawMessage `json:"expires"`
	Hostname string          `json:"hostname"`
	MACAddr  string          `json:"macaddr"`
	IPAddr   string          `json:"ipaddr"`
	DUID     string          `json:"duid"`
}

// toLease converts a LuCI lease; now is the time of the request.
func (l luciLease) toLease(now time.Time) (lease, bool) {
	mac, ok := netutil.NormalizeMAC(l.MACAddr)
	if !ok || l.IPAddr == "" {
		return lease{}, false
	}
	out := lease{MAC: mac, IP: l.IPAddr, Hostname: strings.TrimSpace(l.Hostname), DUID: l.DUID}
	var secs int64
	switch v := strings.TrimSpace(string(l.Expires)); v {
	case "false", "":
		out.Infinite = true
	default:
		if err := json.Unmarshal(l.Expires, &secs); err != nil {
			out.Infinite = true
		} else {
			out.Expires = now.Add(time.Duration(secs) * time.Second).UTC().Truncate(time.Second)
		}
	}
	return out, true
}

// uciGetSections converts the "values" object of uci get into sections ordered by .index.
func uciGetSections(values map[string]map[string]json.RawMessage) []uciSection {
	type item struct {
		idx int
		s   uciSection
	}
	var items []item
	for name, opts := range values {
		s := uciSection{Name: name, Options: map[string][]string{}}
		idx := 0
		for k, raw := range opts {
			switch k {
			case ".type":
				_ = json.Unmarshal(raw, &s.Type)
			case ".index":
				_ = json.Unmarshal(raw, &idx)
			case ".name", ".anonymous":
			default:
				var str string
				if err := json.Unmarshal(raw, &str); err == nil {
					s.Options[k] = []string{str}
					continue
				}
				var list []string
				if err := json.Unmarshal(raw, &list); err == nil {
					s.Options[k] = list
				}
			}
		}
		items = append(items, item{idx, s})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].idx != items[j].idx {
			return items[i].idx < items[j].idx
		}
		return items[i].s.Name < items[j].s.Name
	})
	out := make([]uciSection, len(items))
	for i, it := range items {
		out[i] = it.s
	}
	return out
}

// errLoginRejected matches a rejected LuCI login (the next credential may pass).
var errLoginRejected = errors.New("LuCI-Anmeldung abgelehnt")

type loginError struct{ msg string }

func (e *loginError) Error() string        { return e.msg }
func (e *loginError) Is(target error) bool { return target == errLoginRejected }

// fetchLuCI reads leases and static hosts through the ubus JSON-RPC API. A failing
// uci get only loses the static hosts and is logged.
func fetchLuCI(ctx context.Context, log *slog.Logger, c *ubusClient, user, password string) ([]lease, []uciSection, error) {
	if err := c.login(ctx, user, password); err != nil {
		if errAccessDenied(err) {
			return nil, nil, &loginError{fmt.Sprintf("LuCI-Anmeldung als %q fehlgeschlagen – Benutzer und Passwort prüfen", user)}
		}
		return nil, nil, fmt.Errorf("LuCI %s nicht erreichbar: %w", c.endpoint, err)
	}
	var res struct {
		Leases []luciLease `json:"dhcp_leases"`
	}
	now := time.Now()
	if err := c.call(ctx, "luci-rpc", "getDHCPLeases", map[string]any{"family": 4}, &res); err != nil {
		if errAccessDenied(err) {
			return nil, nil, fmt.Errorf("keine Berechtigung für luci-rpc getDHCPLeases (%w)", err)
		}
		return nil, nil, err
	}
	var leases []lease
	for _, l := range res.Leases {
		if le, ok := l.toLease(now); ok {
			leases = append(leases, le)
		}
	}
	var cfg struct {
		Values map[string]map[string]json.RawMessage `json:"values"`
	}
	if err := c.call(ctx, "uci", "get", map[string]any{"config": "dhcp"}, &cfg); err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		log.Warn("DHCP-Konfiguration (uci get dhcp) nicht lesbar, statische Leases fehlen", "error", err)
		return leases, nil, nil
	}
	return leases, uciGetSections(cfg.Values), nil
}
