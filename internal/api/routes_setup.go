package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"netscope/internal/auth"
	"netscope/internal/federation"
	"netscope/internal/i18n"
	"netscope/internal/inventory"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/setup"
)

// Setup wizard (FR-013). Until the setup is completed the endpoints below answer without
// a login, but only with the setup code (header X-NetScope-Setup-Code) – or, once the
// wizard created the administrator, with that administrator's session. Wrong codes count
// against the rate limit of the login. After completion they answer 409.

const setupCodeHeader = "X-NetScope-Setup-Code"

// setupStatus tells the web interface whether to open the wizard.
type setupStatus struct {
	Pending bool `json:"pending"`
	// Account: the wizard already created the administrator (sign in to continue).
	Account bool `json:"account"`
}

type setupCodeRequest struct {
	Code string `json:"code"`
}

// setupScanner is a scanner offered in the scanner step.
type setupScanner struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Load            string   `json:"load"`
	Schedule        string   `json:"schedule"`
	ScheduleText    string   `json:"scheduleText"`
	DefaultEnabled  bool     `json:"defaultEnabled"`
	Presence        bool     `json:"presence"`
	MissingBinaries []string `json:"missingBinaries,omitempty"`
}

// setupCategory is a tab of the sources step.
type setupCategory struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Plugins []string `json:"plugins"`
	// Hints name sources that are set up elsewhere (windows_dhcp: the Windows agent).
	Hints []string `json:"hints,omitempty"`
}

// setupOptions are the suggestions of the wizard: what NetScope detected, nothing of it
// is applied before the user confirms it.
type setupOptions struct {
	// Timezone is the bootstrap time zone (NETSCOPE_TIMEZONE); the browser's wins if set.
	Timezone string `json:"timezone"`
	Language string `json:"language"`
	// PublicURL is the stored public URL (empty on a new installation).
	PublicURL string `json:"publicUrl"`
	// Subnets are the locally attached networks (detected, not stored).
	Subnets []inventory.Subnet `json:"subnets"`
	// ConfiguredSubnets are subnets already stored (a repeated attempt).
	ConfiguredSubnets []inventory.Subnet `json:"configuredSubnets"`
	// DNSServers are the name servers of the system (reverse DNS uses them by default).
	DNSServers []string `json:"dnsServers"`
	// MasterKeyFile is the key file to back up ("" when the key comes from NETSCOPE_MASTER_KEY).
	MasterKeyFile string `json:"masterKeyFile"`
	DataDir       string `json:"dataDir"`
	// Federation is the current federation setting; Managed: set by environment variables.
	Federation federation.SettingsView `json:"federation"`
	Scanners   []setupScanner          `json:"scanners"`
	Categories []setupCategory         `json:"categories"`
	// Account is the administrator the wizard created (nil before).
	Account *auth.User `json:"account,omitempty"`
}

// setupAccountRequest creates the administrator.
type setupAccountRequest struct {
	auth.FirstAdminInput
}

// setupFederation is the role chosen in the instance step.
type setupFederation struct {
	federation.Settings
	Token *string `json:"token,omitempty"`
}

func (f setupFederation) input() federation.SettingsInput {
	return federation.SettingsInput{Settings: f.Settings, Token: f.Token}
}

// setupCompleteRequest are the choices of the wizard (steps 1, 3, 4, 5 and 7; the
// sources of step 6 are stored through the plugin endpoints as they are set up).
type setupCompleteRequest struct {
	Language   string          `json:"language"`
	Timezone   string          `json:"timezone"`
	PublicURL  string          `json:"publicUrl"`
	Federation setupFederation `json:"federation"`
	// Subnets to scan (confirmed detected ones and added ones).
	Subnets []inventory.Subnet `json:"subnets"`
	// ScanExclusions are addresses or ranges no scanner probes.
	ScanExclusions []string `json:"scanExclusions"`
	// DNSServer for reverse DNS ("" = the DNS servers of the system).
	DNSServer string `json:"dnsServer"`
	// Scanners are the ids of the scanners that are active from the start.
	Scanners []string `json:"scanners"`
	// FirstScan starts ARP scan and ping right away (if among the scanners).
	FirstScan bool `json:"firstScan"`
}

type setupCompleteResponse struct {
	OK bool `json:"ok"`
	// Runs are the ids of the runs started by FirstScan.
	Runs []int64 `json:"runs"`
}

func (s *Server) registerSetup() {
	s.add(&route{Method: "GET", Path: "/api/v1/setup", Tag: "Einrichtung", Summary: "Steht die Einrichtung aus? (ohne Login)",
		Scope: scopePublic, Resp: setupStatus{}, handler: s.handleSetupStatus})
	s.add(&route{Method: "POST", Path: "/api/v1/setup/verify", Tag: "Einrichtung",
		Summary: "Einrichtungscode prüfen (aus dem Log bzw. data/setup-code.txt; Fehlversuche zählen zum Rate-Limit des Logins)",
		Scope:   scopePublic, Body: setupCodeRequest{}, Resp: okResponse{}, handler: s.handleSetupVerify})
	s.add(&route{Method: "GET", Path: "/api/v1/setup/options", Tag: "Einrichtung",
		Summary: "Vorschläge des Assistenten: erkannte Netze, DNS, Zeitzone, Scanner, Quellen (Header X-NetScope-Setup-Code oder Sitzung des Administrators)",
		Scope:   scopePublic, Resp: setupOptions{}, handler: s.handleSetupOptions})
	s.add(&route{Method: "POST", Path: "/api/v1/setup/account", Tag: "Einrichtung",
		Summary: "Administrator anlegen und anmelden (nur mit Einrichtungscode und solange es keinen Benutzer gibt)",
		Scope:   scopePublic, Body: setupAccountRequest{}, Resp: loginResponse{}, Status: http.StatusCreated, handler: s.handleSetupAccount})
	s.add(&route{Method: "POST", Path: "/api/v1/setup/federation/test", Tag: "Einrichtung",
		Summary: "Standort: Verbindung zur Zentrale mit den eingegebenen Daten prüfen (nichts wird gespeichert)",
		Scope:   scopePublic, Body: setupFederation{}, Resp: federation.TestResult{}, handler: s.handleSetupFederationTest})
	s.add(&route{Method: "POST", Path: "/api/v1/setup/complete", Tag: "Einrichtung",
		Summary: "Einrichtung abschließen: Einstellungen, Rolle, Netze und Scanner übernehmen; danach laufen die Plugins",
		Scope:   scopePublic, Body: setupCompleteRequest{}, Resp: setupCompleteResponse{}, handler: s.handleSetupComplete})
}

func (s *Server) setupPending() bool { return s.Setup != nil && s.Setup.Pending() }

// setupAccess admits a setup request: with the setup code, or (unless code is required)
// with the session of an administrator. It answers the request itself when it refuses.
func (s *Server) setupAccess(w http.ResponseWriter, r *http.Request, requireCode bool) (*http.Request, bool) {
	if !s.setupPending() {
		writeError(w, r, http.StatusConflict, "setup_completed", setup.ErrCompleted.Error(), nil)
		return r, false
	}
	if code := r.Header.Get(setupCodeHeader); code != "" {
		ok := s.Setup.CheckCode(code)
		if err := s.Auth.Limit(client(r).IP, ok); err != nil {
			s.fail(w, r, err)
			return r, false
		}
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "invalid_setup_code", "Einrichtungscode falsch", nil)
			return r, false
		}
		return r, true
	}
	if !requireCode {
		if p, viaCookie := s.authenticate(w, r); p != nil && p.Admin && p.CanWrite() {
			r = r.WithContext(context.WithValue(r.Context(), keyPrincipal, p))
			if viaCookie && r.Method != http.MethodGet && r.Header.Get(csrfHeader) == "" {
				writeError(w, r, http.StatusForbidden, "csrf", "CSRF-Header fehlt", nil)
				return r, false
			}
			return r, true
		}
	}
	writeError(w, r, http.StatusUnauthorized, "setup_code_required", "Einrichtungscode erforderlich", nil)
	return r, false
}

// hasUsers reports whether an account exists.
func (s *Server) hasUsers(ctx context.Context) (bool, error) {
	var n int
	err := s.DB.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n > 0, err
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	st := setupStatus{Pending: s.setupPending()}
	if st.Pending {
		var err error
		if st.Account, err = s.hasUsers(r.Context()); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleSetupVerify(w http.ResponseWriter, r *http.Request) {
	var req setupCodeRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, r, http.StatusBadRequest, "validation", "Einrichtungscode erforderlich",
			[]plugin.FieldError{{Field: "code", Message: "Einrichtungscode erforderlich"}})
		return
	}
	r.Header.Set(setupCodeHeader, req.Code)
	if _, ok := s.setupAccess(w, r, true); !ok {
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleSetupOptions(w http.ResponseWriter, r *http.Request) {
	r, ok := s.setupAccess(w, r, false)
	if !ok {
		return
	}
	ctx, loc := r.Context(), requestLocale(r)
	sys := s.Settings.System()
	out := setupOptions{Timezone: s.Config.Timezone, Language: sys.Language, PublicURL: sys.PublicURL, DNSServers: netutil.SystemDNS(),
		DataDir: s.Config.DataDir, Scanners: []setupScanner{}, Categories: []setupCategory{}}
	if out.DNSServers == nil {
		out.DNSServers = []string{}
	}
	if s.Config.MasterKey == "" {
		out.MasterKeyFile = s.Config.MasterKeyFile
	}
	var err error
	if out.Subnets, err = inventory.DetectSubnets(); err != nil {
		s.Log.Warn("Subnetze erkennen", "err", err)
		out.Subnets = []inventory.Subnet{}
	}
	if out.ConfiguredSubnets, err = s.Inventory.ListSubnets(ctx); err != nil {
		s.fail(w, r, err)
		return
	}
	if s.Federation != nil {
		out.Federation = s.Federation.Current()
	}
	byCategory := map[string][]string{}
	for _, id := range s.Host.IDs() {
		p, _ := s.Host.Plugin(id)
		info := p.Info().Localize(loc)
		if info.Category != "" {
			byCategory[info.Category] = append(byCategory[info.Category], id)
		}
		if info.Kind != plugin.KindScanner || info.Load == "" {
			continue
		}
		v, err := s.Host.View(ctx, id)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out.Scanners = append(out.Scanners, setupScanner{ID: id, Name: info.Name, Description: info.Description, Load: info.Load,
			Schedule: info.DefaultSchedule, ScheduleText: describeSchedule(info.DefaultSchedule, "", loc), DefaultEnabled: info.DefaultEnabled,
			Presence: info.Presence, MissingBinaries: v.MissingBinaries})
	}
	for _, c := range plugin.Categories {
		sc := setupCategory{ID: c.ID, Label: i18n.T(loc, c.Label), Plugins: byCategory[c.ID]}
		if sc.Plugins == nil {
			sc.Plugins = []string{}
		}
		if c.ID == plugin.CategoryDNS {
			sc.Hints = []string{"windows_dhcp"}
		}
		out.Categories = append(out.Categories, sc)
	}
	if p := principal(r); p != nil {
		out.Account, _ = s.Auth.User(ctx, p.UserID)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetupAccount(w http.ResponseWriter, r *http.Request) {
	r, ok := s.setupAccess(w, r, true)
	if !ok {
		return
	}
	var req setupAccountRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.Auth.CreateFirstAdmin(r.Context(), req.FirstAdminInput)
	if errors.Is(err, auth.ErrUsersExist) {
		writeError(w, r, http.StatusConflict, "account_exists", err.Error(), nil)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Auth.StartSession(r.Context(), u.ID, client(r).IP, r.UserAgent())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.setSessionCookie(w, r, res.Token, res.Expires)
	p, _, _, err := s.Auth.Session(r.Context(), res.Token)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_ = s.Audit.Record(r.Context(), u.Username, "user", client(r).IP, "setup.account", "user", fmt.Sprint(u.ID),
		"Administrator im Einrichtungsassistenten angelegt", nil, nil)
	writeJSON(w, http.StatusCreated, loginResponse{User: u, Principal: p})
}

func (s *Server) handleSetupFederationTest(w http.ResponseWriter, r *http.Request) {
	r, ok := s.setupAccess(w, r, false)
	if !ok {
		return
	}
	if s.Federation == nil {
		s.fail(w, r, errNoFederation)
		return
	}
	var req setupFederation
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Federation.TestSettings(r.Context(), req.input())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	res.Error = i18n.Err(requestLocale(r), res.Error)
	writeJSON(w, http.StatusOK, res)
}

// prefixed moves field errors below a request field (subnets.1.cidr).
func prefixed(err error, prefix string) error {
	var ve *plugin.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	out := &plugin.ValidationError{}
	for _, f := range ve.Errors {
		out.Errors = append(out.Errors, plugin.FieldError{Field: prefix + "." + f.Field, Message: f.Message})
	}
	return out
}

func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	r, ok := s.setupAccess(w, r, false)
	if !ok {
		return
	}
	var req setupCompleteRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	if has, err := s.hasUsers(ctx); err != nil || !has {
		if err == nil {
			err = errors.New("Zuerst das Konto des Administrators anlegen")
		}
		s.fail(w, r, err)
		return
	}
	// check everything before anything is applied
	sys := s.Settings.System()
	sys.Language, sys.Timezone, sys.PublicURL, sys.ScanExclusions = req.Language, req.Timezone, strings.TrimSpace(req.PublicURL), req.ScanExclusions
	if err := sys.Validate(); err != nil {
		s.fail(w, r, err)
		return
	}
	fedIn := req.Federation.input()
	if s.Federation != nil {
		if s.Federation.Managed() {
			fedIn = federation.SettingsInput{Settings: s.Federation.Current().Settings}
		}
		if _, _, err := s.Federation.Check(fedIn); err != nil {
			s.fail(w, r, prefixed(err, "federation"))
			return
		}
	}
	seen := map[string]bool{}
	for i := range req.Subnets {
		sn := &req.Subnets[i]
		sn.ID, sn.Enabled = 0, true
		if sn.Access == inventory.AccessWireGuard {
			s.fail(w, r, plugin.FieldErr(fmt.Sprintf("subnets.%d.access", i), "WireGuard-Tunnel nach der Einrichtung unter System → Subnetze anlegen"))
			return
		}
		if err := sn.Validate(); err != nil {
			s.fail(w, r, prefixed(err, fmt.Sprintf("subnets.%d", i)))
			return
		}
		if seen[sn.CIDR] {
			s.fail(w, r, plugin.FieldErr(fmt.Sprintf("subnets.%d.cidr", i), "Subnetz doppelt angegeben"))
			return
		}
		seen[sn.CIDR] = true
	}
	var dnsSettings map[string]any
	if cfg, ok := s.Host.Config("dns"); ok {
		in := map[string]any{}
		for k, v := range cfg.Settings {
			in[k] = v
		}
		in["resolver"] = strings.TrimSpace(req.DNSServer)
		vals, err := s.Host.ValidateSettings(ctx, "dns", in)
		if err != nil {
			s.fail(w, r, prefixed(err, "dnsServer"))
			return
		}
		dnsSettings = vals
	}
	scanners := map[string]bool{}
	for _, id := range s.Host.IDs() {
		if p, _ := s.Host.Plugin(id); p.Info().Kind == plugin.KindScanner && p.Info().Load != "" {
			scanners[id] = false
		}
	}
	for _, id := range req.Scanners {
		if _, ok := scanners[id]; !ok {
			s.fail(w, r, plugin.FieldErr("scanners", fmt.Sprintf("kein wählbarer Scanner: %q", id)))
			return
		}
		scanners[id] = true
	}

	// apply
	if err := s.Settings.SetSystem(ctx, sys); err != nil {
		s.fail(w, r, err)
		return
	}
	if s.Federation != nil && !s.Federation.Managed() {
		if _, err := s.Federation.Update(ctx, fedIn); err != nil {
			s.fail(w, r, prefixed(err, "federation"))
			return
		}
	}
	if err := s.syncSubnets(ctx, req.Subnets); err != nil {
		s.fail(w, r, err)
		return
	}
	if dnsSettings != nil {
		if _, _, err := s.Host.UpdateConfig(ctx, "dns", pluginhost.ConfigInput{Settings: dnsSettings}); err != nil {
			s.fail(w, r, prefixed(err, "dnsServer"))
			return
		}
	}
	for id, on := range scanners {
		if _, _, err := s.Host.UpdateConfig(ctx, id, pluginhost.ConfigInput{Enabled: &on}); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if err := s.Setup.Complete(ctx, setup.ModeWizard); err != nil {
		s.fail(w, r, err)
		return
	}
	s.Host.Release()
	enabled := slices.Clone(req.Scanners)
	slices.Sort(enabled)
	s.recordSetup(r, "setup.complete", "Einrichtung abgeschlossen", map[string]any{"language": sys.Language, "timezone": sys.Timezone,
		"publicUrl": sys.PublicURL, "role": fedIn.Role, "subnets": len(req.Subnets), "scanExclusions": sys.ScanExclusions,
		"dnsServer": req.DNSServer, "scanners": enabled})
	s.Log.Info("Einrichtung abgeschlossen – Plugins laufen", "scanners", strings.Join(enabled, ", "), "subnets", len(req.Subnets))
	out := setupCompleteResponse{OK: true, Runs: []int64{}}
	if req.FirstScan {
		for _, id := range []string{"arpscan", "icmp"} {
			if !scanners[id] {
				continue
			}
			runID, err := s.Host.Trigger(ctx, id, pluginhost.TriggerOptions{Trigger: pluginhost.TriggerManual, RequestedBy: actorName(r)})
			if err != nil {
				s.Log.Warn("Erster Lauf nach der Einrichtung", "plugin", id, "err", err)
				continue
			}
			out.Runs = append(out.Runs, runID)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// recordSetup writes an audit entry of the wizard (actor: the session, else the code).
func (s *Server) recordSetup(r *http.Request, action, summary string, after any) {
	name, typ := "Einrichtungscode", "system"
	if p := principal(r); p != nil {
		name, typ = p.Actor()
	}
	if err := s.Audit.Record(r.Context(), name, typ, client(r).IP, action, "system", "", summary, nil, after); err != nil {
		s.Log.Error("audit", "err", err)
	}
}

// syncSubnets makes the configured subnets exactly the ones chosen in the wizard (a
// repeated attempt updates what an earlier one stored).
func (s *Server) syncSubnets(ctx context.Context, want []inventory.Subnet) error {
	have, err := s.Inventory.ListSubnets(ctx)
	if err != nil {
		return err
	}
	byCIDR := map[string]inventory.Subnet{}
	for _, sn := range have {
		byCIDR[sn.CIDR] = sn
	}
	keep := map[int64]bool{}
	for i := range want {
		sn := want[i]
		if cur, ok := byCIDR[sn.CIDR]; ok {
			sn.ID = cur.ID
		}
		if sn.Notes == "" {
			sn.Notes = "im Einrichtungsassistenten übernommen"
		}
		if err := s.Inventory.SaveSubnet(ctx, &sn); err != nil {
			return prefixed(err, fmt.Sprintf("subnets.%d", i))
		}
		keep[sn.ID] = true
	}
	for _, sn := range have {
		if !keep[sn.ID] {
			if err := s.Inventory.DeleteSubnet(ctx, sn.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
