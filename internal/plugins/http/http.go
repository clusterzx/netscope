// Package http implements HTTP(S) fingerprinting: it probes the web ports of a device,
// records status, title, headers, redirect chain and favicon hash and recognises web
// applications from an extensible signature list.
package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	xhtml "golang.org/x/net/html"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the HTTP(S) fingerprinting scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "http",
		Kind:               plugin.KindScanner,
		Name:               "HTTP-Fingerprinting",
		Description:        "Untersucht HTTP(S)-Ports: Titel, Server-Header, Redirects, Favicon-Hash und erkennt Web-Anwendungen.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "30 3 * * *",
		DefaultTimeout:     30 * time.Minute,
		DefaultConcurrency: 16,
		DefaultRetries:     0,
		Targets:            plugin.TargetDevices,
		Presence:           false,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "use_scanned_ports", Type: plugin.FieldBool, Label: "Bekannte Ports verwenden", Default: true,
			Description: "HTTP(S)-Ports aus den bereits gescannten Diensten des Geräts übernehmen."},
		{Key: "extra_ports", Type: plugin.FieldStringList, Label: "Zusätzliche Ports", Default: []string{"80", "443", "8080", "8443"},
			Description: "Diese Ports werden immer geprüft, auch ohne Portdaten.",
			Validation:  &plugin.Validation{Pattern: `^[0-9]{1,5}$`}},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout je Anfrage", Default: "5s",
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(120)}},
		{Key: "max_redirects", Type: plugin.FieldInt, Label: "Maximale Weiterleitungen", Default: 5,
			Validation: &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(20)}, Advanced: true},
		{Key: "user_agent", Type: plugin.FieldString, Label: "User-Agent", Default: "NetScope/1.0 (+https://github.com/netscope)",
			Advanced: true},
		{Key: "favicon", Type: plugin.FieldBool, Label: "Favicon-Hash berechnen", Default: true,
			Description: "Lädt das Favicon und bildet den Shodan-kompatiblen mmh3-Hash."},
		{Key: "max_body_kb", Type: plugin.FieldInt, Label: "Maximale Body-Größe (KB)", Default: 512,
			Validation: &plugin.Validation{Min: plugin.Int64(4), Max: plugin.Int64(8192)}, Advanced: true},
		{Key: "custom_signatures", Type: plugin.FieldStringList, Label: "Eigene Signaturen", Advanced: true,
			Description: "Je Zeile \"Name|Feld|Regex\". Feld: title, server, body, header:<Name> oder favicon (mmh3-Zahl)."},
	}}
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	var errs []plugin.FieldError
	for _, ep := range s.StringList("extra_ports") {
		if n, err := strconv.Atoi(ep); err != nil || n < 1 || n > 65535 {
			errs = append(errs, plugin.FieldError{Field: "extra_ports", Message: fmt.Sprintf("ungültiger Port %q", ep)})
		}
	}
	for _, line := range s.StringList("custom_signatures") {
		if _, err := parseCustomSignature(line); err != nil {
			errs = append(errs, plugin.FieldError{Field: "custom_signatures", Message: fmt.Sprintf("%q: %v", line, err)})
		}
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

type endpoint struct {
	ip   string
	port int
	tls  bool
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	cfg := loadConfig(rc)
	sigs := append([]signature(nil), builtinSignatures...)
	for _, line := range rc.Settings.StringList("custom_signatures") {
		if s, err := parseCustomSignature(line); err == nil {
			sigs = append(sigs, s)
		}
	}
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: cfg.timeout}).DialContext,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			TLSHandshakeTimeout:   cfg.timeout,
			ResponseHeaderTimeout: cfg.timeout,
			DisableKeepAlives:     true,
			MaxIdleConns:          rc.Parallelism() * 2,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()

	devices := rc.Targets.Devices
	if len(devices) == 0 {
		rc.Log.Info("keine Geräte für HTTP-Scan")
		return nil
	}
	var done, servicesFound int64
	rc.Progress(0, len(devices))
	return plugin.ForEach(ctx, rc.Parallelism(), devices, func(ctx context.Context, d plugin.DeviceInfo) error {
		n := p.scanDevice(ctx, rc, client, d, cfg, sigs)
		atomic.AddInt64(&servicesFound, int64(n))
		cur := atomic.AddInt64(&done, 1)
		rc.Progress(int(cur), len(devices))
		rc.SetStat("services", int(atomic.LoadInt64(&servicesFound)))
		return nil
	})
}

type config struct {
	useScanned   bool
	extraPorts   []int
	timeout      time.Duration
	maxRedirects int
	userAgent    string
	favicon      bool
	maxBody      int
}

func loadConfig(rc *plugin.RunContext) config {
	c := config{
		useScanned:   rc.Settings.Bool("use_scanned_ports"),
		timeout:      rc.Settings.Duration("timeout"),
		maxRedirects: rc.Settings.Int("max_redirects"),
		userAgent:    rc.Settings.String("user_agent"),
		favicon:      rc.Settings.Bool("favicon"),
		maxBody:      rc.Settings.Int("max_body_kb") * 1024,
	}
	if c.timeout <= 0 {
		c.timeout = 5 * time.Second
	}
	if c.maxBody <= 0 {
		c.maxBody = 512 * 1024
	}
	if c.userAgent == "" {
		c.userAgent = "NetScope/1.0 (+https://github.com/netscope)"
	}
	for _, ep := range rc.Settings.StringList("extra_ports") {
		if n, err := strconv.Atoi(ep); err == nil && n >= 1 && n <= 65535 {
			c.extraPorts = append(c.extraPorts, n)
		}
	}
	return c
}

// scanDevice probes all web endpoints of one device and writes one observation per IP.
func (p *Plugin) scanDevice(ctx context.Context, rc *plugin.RunContext, client *http.Client, d plugin.DeviceInfo,
	cfg config, sigs []signature) int {
	byIP := endpointsByIP(d, cfg)
	allowed := allowedHosts(d)
	total := 0
	for _, ip := range sortedKeys(byIP) {
		eps := byIP[ip]
		obs := &plugin.Observation{DeviceID: d.ID, IP: ip, Target: ip}
		scan := &plugin.HTTPScan{}
		var raw strings.Builder
		deviceType := ""
		for _, ep := range eps {
			scan.Scanned = append(scan.Scanned, ep.port)
			svc, dt := p.probeEndpoint(ctx, rc, client, ep, allowed, cfg, sigs)
			if svc == nil {
				continue
			}
			scan.Services = append(scan.Services, *svc)
			if deviceType == "" {
				deviceType = dt
			}
			fmt.Fprintf(&raw, "%s %d %s\n", svc.FinalURL, svc.StatusCode, svc.Title)
		}
		sort.Ints(scan.Scanned)
		obs.HTTP = scan
		obs.Present = len(scan.Services) > 0
		obs.DeviceType = deviceType
		obs.Raw = raw.String()
		total += len(scan.Services)
		if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			rc.Log.Warn("Beobachtung fehlgeschlagen", "ip", ip, "err", err)
		}
	}
	return total
}

// probeEndpoint probes one host:port, trying the alternate scheme when the first is wrong.
func (p *Plugin) probeEndpoint(ctx context.Context, rc *plugin.RunContext, client *http.Client, ep endpoint,
	allowed map[string]bool, cfg config, sigs []signature) (*plugin.HTTPService, string) {
	primary := "http"
	if ep.tls {
		primary = "https"
	}
	other := "https"
	if primary == "https" {
		other = "http"
	}
	res := p.fetch(ctx, rc, client, ep, primary, allowed, cfg)
	if res == nil || res.wrongScheme {
		if alt := p.fetch(ctx, rc, client, ep, other, allowed, cfg); alt != nil && !alt.wrongScheme {
			res = alt
		} else if res == nil {
			res = alt
		}
	}
	// no HTTP response on either scheme (refused, timeout, not HTTP): nothing to report
	if res == nil || res.status == 0 {
		return nil, ""
	}
	svc := &plugin.HTTPService{
		Port:        ep.port,
		Scheme:      res.scheme,
		URL:         res.startURL,
		StatusCode:  res.status,
		Title:       extractTitle(res.body),
		Server:      res.header.Get("Server"),
		ContentType: res.header.Get("Content-Type"),
		FinalURL:    res.finalURL,
		Redirects:   res.chain,
		Headers:     selectHeaders(res.header),
	}
	fp := fingerprint{title: svc.Title, server: svc.Server, body: res.body, headers: res.header}
	if cfg.favicon {
		if icon := p.fetchFavicon(ctx, client, res, allowed, cfg); icon != nil {
			h, md5hex := faviconHash(icon)
			svc.FaviconHash = &h
			svc.FaviconMD5 = md5hex
			fp.favicon = &h
		}
	}
	apps, deviceType := detect(fp, sigs)
	svc.Apps = apps
	return svc, deviceType
}

type fetchResult struct {
	scheme      string
	startURL    string
	finalURL    string
	status      int
	header      http.Header
	body        []byte
	chain       []string
	wrongScheme bool
}

// fetch performs the request and follows same-host redirects up to cfg.maxRedirects.
func (p *Plugin) fetch(ctx context.Context, rc *plugin.RunContext, client *http.Client, ep endpoint, scheme string,
	allowed map[string]bool, cfg config) *fetchResult {
	start := endpointURL(scheme, ep)
	res := &fetchResult{scheme: scheme, startURL: start, finalURL: start}
	cur := start
	for hop := 0; hop <= cfg.maxRedirects; hop++ {
		reqCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, cur, nil)
		if err != nil {
			cancel()
			return nil
		}
		req.Header.Set("User-Agent", cfg.userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			// A failed request on the first hop usually means the wrong scheme
			// (TLS to a plaintext port or vice versa).
			if hop == 0 {
				res.wrongScheme = true
				return res
			}
			return res
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(cfg.maxBody)))
		resp.Body.Close()
		cancel()
		res.status = resp.StatusCode
		res.header = resp.Header
		res.body = body
		res.finalURL = cur
		// A plaintext request to a TLS port is answered with a 400 whose body mentions
		// HTTPS (nginx: "sent to HTTPS port", Go: "HTTP request … to an HTTPS server").
		if hop == 0 && scheme == "http" && resp.StatusCode == 400 {
			low := bytes.ToLower(body)
			if bytes.Contains(low, []byte("https port")) || bytes.Contains(low, []byte("https server")) {
				res.wrongScheme = true
				return res
			}
		}
		loc := resp.Header.Get("Location")
		if resp.StatusCode < 300 || resp.StatusCode >= 400 || loc == "" {
			return res
		}
		next, err := resolveRedirect(cur, loc)
		if err != nil || !allowed[next.Hostname()] {
			// Record the target but do not fetch a foreign host.
			res.chain = append(res.chain, loc)
			return res
		}
		res.chain = append(res.chain, next.String())
		cur = next.String()
	}
	rc.Log.Debug("Redirect-Limit erreicht", "url", start)
	return res
}

// fetchFavicon fetches the icon referenced by the page (same host) or /favicon.ico.
func (p *Plugin) fetchFavicon(ctx context.Context, client *http.Client, res *fetchResult, allowed map[string]bool, cfg config) []byte {
	const maxFavicon = 256 * 1024
	base, err := url.Parse(res.finalURL)
	if err != nil {
		return nil
	}
	var candidates []string
	for _, href := range extractIconHrefs(res.body) {
		if u, err := base.Parse(href); err == nil && allowed[u.Hostname()] {
			candidates = append(candidates, u.String())
		}
	}
	if def, err := base.Parse("/favicon.ico"); err == nil {
		candidates = append(candidates, def.String())
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		reqCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c, nil)
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("User-Agent", cfg.userAgent)
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			continue
		}
		icon, _ := io.ReadAll(io.LimitReader(resp.Body, maxFavicon))
		resp.Body.Close()
		cancel()
		if len(icon) > 0 {
			return icon
		}
	}
	return nil
}

// endpointsByIP collects the web endpoints of a device grouped by IP.
func endpointsByIP(d plugin.DeviceInfo, cfg config) map[string][]endpoint {
	out := map[string]map[int]endpoint{}
	add := func(ip string, port int, tlsHint bool) {
		if ip == "" || port < 1 || port > 65535 {
			return
		}
		if out[ip] == nil {
			out[ip] = map[int]endpoint{}
		}
		e, ok := out[ip][port]
		if !ok {
			out[ip][port] = endpoint{ip: ip, port: port, tls: tlsHint}
			return
		}
		if tlsHint && !e.tls {
			e.tls = true
			out[ip][port] = e
		}
	}
	if cfg.useScanned {
		for _, port := range d.Ports {
			if port.Proto != "" && !strings.EqualFold(port.Proto, "tcp") {
				continue
			}
			if !isWebPort(port) {
				continue
			}
			add(port.IP, port.Port, isTLSPort(port))
		}
	}
	primary := d.PrimaryIP
	if primary == "" && len(d.IPs) > 0 {
		primary = d.IPs[0]
	}
	for _, port := range cfg.extraPorts {
		add(primary, port, tlsPorts[port])
	}
	grouped := map[string][]endpoint{}
	for ip, ports := range out {
		for _, e := range ports {
			grouped[ip] = append(grouped[ip], e)
		}
		sort.Slice(grouped[ip], func(i, j int) bool { return grouped[ip][i].port < grouped[ip][j].port })
	}
	return grouped
}

var commonWebPorts = map[int]bool{
	80: true, 81: true, 443: true, 591: true, 3000: true, 5000: true, 7000: true, 8000: true, 8006: true,
	8008: true, 8043: true, 8080: true, 8081: true, 8088: true, 8096: true, 8123: true, 8443: true, 8843: true,
	8888: true, 9000: true, 9090: true, 9443: true,
}

var tlsPorts = map[int]bool{443: true, 8443: true, 9443: true, 8006: true, 8843: true}

func isWebPort(p plugin.PortRef) bool {
	svc := strings.ToLower(p.Service)
	if strings.Contains(svc, "http") {
		return true
	}
	if p.Tunnel == "ssl" && (svc == "" || strings.Contains(svc, "http") || strings.Contains(svc, "https")) {
		return true
	}
	return commonWebPorts[p.Port]
}

func isTLSPort(p plugin.PortRef) bool {
	return p.Tunnel == "ssl" || tlsPorts[p.Port] || strings.HasPrefix(strings.ToLower(p.Service), "https")
}

func allowedHosts(d plugin.DeviceInfo) map[string]bool {
	out := map[string]bool{}
	for _, ip := range append([]string{d.PrimaryIP}, d.IPs...) {
		if ip != "" {
			out[ip] = true
		}
	}
	if d.Hostname != "" {
		out[strings.ToLower(strings.TrimSuffix(d.Hostname, "."))] = true
	}
	return out
}

func endpointURL(scheme string, ep endpoint) string {
	host := ep.ip
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if (scheme == "http" && ep.port == 80) || (scheme == "https" && ep.port == 443) {
		return scheme + "://" + host + "/"
	}
	return scheme + "://" + host + ":" + strconv.Itoa(ep.port) + "/"
}

func resolveRedirect(base, loc string) (*url.URL, error) {
	b, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	u, err := b.Parse(strings.TrimSpace(loc))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("nicht-http Weiterleitung")
	}
	return u, nil
}

// selectHeaders keeps the headers of interest. Set-Cookie is reduced to cookie names.
func selectHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, name := range []string{"Server", "X-Powered-By", "WWW-Authenticate", "Content-Type", "X-Generator"} {
		if v := h.Get(name); v != "" {
			out[name] = v
		}
	}
	var cookies []string
	for _, sc := range h.Values("Set-Cookie") {
		if name, _, ok := strings.Cut(sc, "="); ok {
			if n := strings.TrimSpace(name); n != "" {
				cookies = append(cookies, n)
			}
		}
	}
	if len(cookies) > 0 {
		out["Set-Cookie"] = strings.Join(cookies, ", ")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// extractTitle returns the trimmed <title> text (max 200 runes).
func extractTitle(b []byte) string {
	z := xhtml.NewTokenizer(bytes.NewReader(b))
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return ""
		case xhtml.StartTagToken:
			if name, _ := z.TagName(); string(name) == "title" {
				if z.Next() == xhtml.TextToken {
					return trimTitle(string(z.Text()))
				}
				return ""
			}
		}
	}
}

func trimTitle(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if r := []rune(s); len(r) > 200 {
		return string(r[:200])
	}
	return s
}

// extractIconHrefs returns the href values of <link rel="…icon…"> tags.
func extractIconHrefs(b []byte) []string {
	var out []string
	z := xhtml.NewTokenizer(bytes.NewReader(b))
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			return out
		}
		if tt != xhtml.StartTagToken && tt != xhtml.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) != "link" || !hasAttr {
			continue
		}
		var rel, href string
		for {
			k, v, more := z.TagAttr()
			switch string(k) {
			case "rel":
				rel = strings.ToLower(string(v))
			case "href":
				href = string(v)
			}
			if !more {
				break
			}
		}
		if href != "" && strings.Contains(rel, "icon") {
			out = append(out, href)
		}
	}
}

func sortedKeys(m map[string][]endpoint) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
