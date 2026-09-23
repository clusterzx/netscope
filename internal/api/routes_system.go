package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"netscope/internal/audit"
	"netscope/internal/config"
	"netscope/internal/cron"
	"netscope/internal/db"
	"netscope/internal/inventory"
	"netscope/internal/logging"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/settings"
	"netscope/internal/vault"
)

type healthResponse struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	Database      string `json:"database"`
}

type systemInfo struct {
	Version         string              `json:"version"`
	GoVersion       string              `json:"goVersion"`
	StartedAt       time.Time           `json:"startedAt"`
	UptimeSeconds   int64               `json:"uptimeSeconds"`
	DataDir         string              `json:"dataDir"`
	ConfigPath      string              `json:"configPath"`
	Listen          string              `json:"listen"`
	LogLevel        string              `json:"logLevel"`
	LogFormat       string              `json:"logFormat"`
	TimeZone        string              `json:"timeZone"`
	TrustedProxies  []string            `json:"trustedProxies"`
	DBPath          string              `json:"dbPath"`
	DBSizeBytes     int64               `json:"dbSizeBytes"`
	SchemaVersion   int                 `json:"schemaVersion"`
	VaultKeyID      string              `json:"vaultKeyId"`
	VaultKeySource  string              `json:"vaultKeySource"`
	Plugins         int                 `json:"plugins"`
	MissingBinaries map[string][]string `json:"missingBinaries"`
	Goroutines      int                 `json:"goroutines"`
	MemoryBytes     uint64              `json:"memoryBytes"`
	Client          clientInfo          `json:"client"`
}

type logLevelRequest struct {
	Level string `json:"level"`
}

type backupInfo struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

type restoreRequest struct {
	Name string `json:"name"`
}

type cronResponse struct {
	Valid bool        `json:"valid"`
	Error string      `json:"error,omitempty"`
	Text  string      `json:"text,omitempty"`
	Next  []time.Time `json:"next"`
}

type severityInfo struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type deviceAction struct {
	Plugin      string         `json:"plugin"`
	PluginName  string         `json:"pluginName"`
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Description string         `json:"description,omitempty"`
	Confirm     string         `json:"confirm,omitempty"`
	Params      []plugin.Field `json:"params,omitempty"`
}

type pluginShort struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Kind    plugin.Kind `json:"kind"`
	Enabled bool        `json:"enabled"`
}

type metaResponse struct {
	Version         string                     `json:"version"`
	EventTypes      []plugin.EventSpec         `json:"eventTypes"`
	CredentialTypes []plugin.CredentialType    `json:"credentialTypes"`
	DeviceTypes     []string                   `json:"deviceTypes"`
	QueryFields     []inventory.QueryField     `json:"queryFields"`
	SortFields      []string                   `json:"sortFields"`
	Severities      []severityInfo             `json:"severities"`
	Priorities      []string                   `json:"priorities"`
	PublicURL       string                     `json:"publicUrl"`
	TimeZone        string                     `json:"timeZone"`
	Publishers      []pluginhost.PublisherInfo `json:"publishers"`
	DeviceActions   []deviceAction             `json:"deviceActions"`
	Scanners        []pluginShort              `json:"scanners"`
}

type uploadResponse struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type auditList struct {
	Total int           `json:"total"`
	Items []audit.Entry `json:"items"`
}

func (s *Server) registerSystem() {
	s.add(&route{Method: "GET", Path: "/api/v1/health", Tag: "System", Summary: "Health-Endpoint (ohne Anmeldung)", Scope: scopePublic,
		Resp: healthResponse{}, handler: s.handleHealth})
	s.add(&route{Method: "GET", Path: "/api/v1/system/info", Tag: "System", Summary: "Systeminformationen", Scope: scopeRead,
		Resp: systemInfo{}, handler: s.handleSystemInfo})
	s.add(&route{Method: "GET", Path: "/api/v1/system/settings", Tag: "System", Summary: "Systemeinstellungen", Scope: scopeRead,
		Resp: settings.System{}, handler: s.handleGetSettings})
	s.add(&route{Method: "PUT", Path: "/api/v1/system/settings", Tag: "System", Summary: "Systemeinstellungen speichern", Scope: scopeWrite,
		Body: settings.System{}, Resp: settings.System{}, handler: s.handlePutSettings})
	s.add(&route{Method: "PUT", Path: "/api/v1/system/loglevel", Tag: "System", Summary: "Log-Level zur Laufzeit ändern", Scope: scopeWrite,
		Body: logLevelRequest{}, Resp: logLevelRequest{}, handler: s.handleLogLevel})
	s.add(&route{Method: "GET", Path: "/api/v1/system/logs", Tag: "System", Summary: "Anwendungsprotokoll (Ringpuffer)", Scope: scopeRead,
		Params: []param{{Name: "level", Desc: "Mindest-Level: debug, info, warn, error"}, {Name: "plugin"}, {Name: "q", Desc: "Textsuche"},
			{Name: "limit", Type: "integer"}}, Resp: []logging.Entry{}, handler: s.handleLogs})
	s.add(&route{Method: "GET", Path: "/api/v1/system/backups", Tag: "System", Summary: "Backups auflisten", Scope: scopeRead,
		Resp: []backupInfo{}, handler: s.handleListBackups})
	s.add(&route{Method: "POST", Path: "/api/v1/system/backups", Tag: "System", Summary: "Backup der Datenbank erstellen", Scope: scopeWrite,
		Resp: backupInfo{}, Status: http.StatusCreated, handler: s.handleCreateBackup})
	s.add(&route{Method: "GET", Path: "/api/v1/system/backups/{name}", Tag: "System", Summary: "Backup herunterladen", Scope: scopeWrite,
		Content: "application/octet-stream", handler: s.handleDownloadBackup})
	s.add(&route{Method: "DELETE", Path: "/api/v1/system/backups/{name}", Tag: "System", Summary: "Backup löschen", Scope: scopeWrite,
		Resp: okResponse{}, handler: s.handleDeleteBackup})
	s.add(&route{Method: "POST", Path: "/api/v1/system/restore", Tag: "System",
		Summary: "Datenbank wiederherstellen (multipart „file“ oder JSON {name}); startet die Dienste neu", Scope: scopeWrite,
		Resp: okResponse{}, Status: http.StatusAccepted, handler: s.handleRestore})
	s.add(&route{Method: "POST", Path: "/api/v1/system/vault/rotate", Tag: "System", Summary: "Master-Key des Vaults rotieren", Scope: scopeWrite,
		Resp: map[string]string{}, handler: s.handleRotate})
	s.add(&route{Method: "GET", Path: "/api/v1/audit", Tag: "System", Summary: "Audit-Log", Scope: scopeRead,
		Params: []param{{Name: "entity"}, {Name: "id"}, {Name: "action"}, {Name: "q"}, {Name: "limit", Type: "integer"}, {Name: "offset", Type: "integer"}},
		Resp:   auditList{}, handler: s.handleAudit})
	s.add(&route{Method: "GET", Path: "/api/v1/cron/describe", Tag: "System", Summary: "Cron-Ausdruck prüfen und in Klartext beschreiben",
		Scope: scopeRead, Params: []param{{Name: "expr", Required: true}}, Resp: cronResponse{}, handler: s.handleCron})
	s.add(&route{Method: "GET", Path: "/api/v1/meta", Tag: "System", Summary: "Kataloge und Metadaten für die Oberfläche", Scope: scopeRead,
		Resp: metaResponse{}, handler: s.handleMeta})
	s.add(&route{Method: "POST", Path: "/api/v1/uploads", Tag: "System", Summary: "Datei hochladen (multipart „file“, z. B. für Importer)",
		Scope: scopeWrite, Resp: uploadResponse{}, Status: http.StatusCreated, handler: s.handleUpload})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status, dbs := "ok", "ok"
	if err := s.DB.R.PingContext(r.Context()); err != nil {
		status, dbs = "degraded", err.Error()
	}
	code := http.StatusOK
	if status != "ok" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, healthResponse{Status: status, Version: s.Version, UptimeSeconds: int64(time.Since(s.StartedAt).Seconds()), Database: dbs})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	ver, _ := s.DB.SchemaVersion(r.Context())
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	src := s.Vault.Source()
	keySrc := "Datei " + src.File
	if src.FromEnv {
		keySrc = "Umgebungsvariable NETSCOPE_MASTER_KEY"
	}
	missing := map[string][]string{}
	views, err := s.Host.Views(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, v := range views {
		if len(v.MissingBinaries) > 0 {
			missing[v.Info.ID] = v.MissingBinaries
		}
	}
	writeJSON(w, http.StatusOK, systemInfo{Version: s.Version, GoVersion: runtime.Version(), StartedAt: s.StartedAt,
		UptimeSeconds: int64(time.Since(s.StartedAt).Seconds()), DataDir: s.Config.DataDir, ConfigPath: s.Config.Path,
		Listen: s.Config.Listen, LogLevel: s.LevelVar.Level().String(), LogFormat: s.Config.LogFormat, TimeZone: s.Config.Timezone,
		TrustedProxies: s.Config.TrustedProxies, DBPath: s.DB.Path, DBSizeBytes: s.DB.Size(), SchemaVersion: ver,
		VaultKeyID: s.Vault.KeyID(), VaultKeySource: keySrc, Plugins: len(views), MissingBinaries: missing,
		Goroutines: runtime.NumGoroutine(), MemoryBytes: ms.Alloc, Client: client(r)})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Settings.System())
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in settings.System
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	before := s.Settings.System()
	if err := s.Settings.SetSystem(r.Context(), in); err != nil {
		s.fail(w, r, err)
		return
	}
	after := s.Settings.System()
	if strings.Join(before.HostnamePriority, ",") != strings.Join(after.HostnamePriority, ",") {
		if err := s.Inventory.RecomputeAll(r.Context()); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.record(r, "system.settings", "settings", "system", "Systemeinstellungen geändert", before, after)
	writeJSON(w, http.StatusOK, after)
}

func (s *Server) handleLogLevel(w http.ResponseWriter, r *http.Request) {
	var req logLevelRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	lvl, err := config.ParseLevel(req.Level)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	old := s.LevelVar.Level().String()
	s.LevelVar.Set(lvl)
	s.record(r, "system.loglevel", "settings", "loglevel", "Log-Level "+old+" → "+lvl.String(), old, lvl.String())
	writeJSON(w, http.StatusOK, logLevelRequest{Level: lvl.String()})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	lvl := slog.LevelDebug
	if v := r.URL.Query().Get("level"); v != "" {
		l, err := config.ParseLevel(v)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		lvl = l
	}
	entries := s.Logs.Query(lvl, r.URL.Query().Get("plugin"), r.URL.Query().Get("q"), qInt(r, "limit", 500))
	if entries == nil {
		entries = []logging.Entry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

var backupNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+\.db$`)

func (s *Server) backupDir() string { return filepath.Join(s.Config.DataDir, "backups") }

func (s *Server) backupPath(r *http.Request) (string, error) {
	name := r.PathValue("name")
	if !backupNameRe.MatchString(name) || strings.Contains(name, "..") {
		return "", errors.New("ungültiger Backup-Name")
	}
	p := filepath.Join(s.backupDir(), name)
	if _, err := os.Stat(p); err != nil {
		return "", db.ErrNotFound
	}
	return p, nil
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.backupDir())
	out := []backupInfo{}
	if err == nil {
		for _, e := range entries {
			if !e.Type().IsRegular() || !backupNameRe.MatchString(e.Name()) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, backupInfo{Name: e.Name(), Size: info.Size(), CreatedAt: info.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	name := "netscope-" + time.Now().In(s.Config.Location).Format("20060102-150405") + ".db"
	p := filepath.Join(s.backupDir(), name)
	if err := s.DB.Backup(r.Context(), p); err != nil {
		s.fail(w, r, err)
		return
	}
	st, err := os.Stat(p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "system.backup", "backup", name, "Backup erstellt", nil, nil)
	writeJSON(w, http.StatusCreated, backupInfo{Name: name, Size: st.Size(), CreatedAt: st.ModTime()})
}

func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	p, err := s.backupPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(p)))
	http.ServeContent(w, r, filepath.Base(p), st.ModTime(), f)
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	p, err := s.backupPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := os.Remove(p); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "system.backup_delete", "backup", filepath.Base(p), "Backup gelöscht", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if s.Restore == nil {
		writeError(w, http.StatusNotImplemented, "unsupported", "Wiederherstellung nicht verfügbar", nil)
		return
	}
	stage := filepath.Join(s.Config.DataDir, "restore")
	if err := os.MkdirAll(stage, 0o750); err != nil {
		s.fail(w, r, err)
		return
	}
	target := filepath.Join(stage, "restore-"+time.Now().Format("20060102-150405")+".db")
	source := ""
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<30)
		f, _, err := r.FormFile("file")
		if err != nil {
			s.fail(w, r, fmt.Errorf("Datei fehlt: %w", err))
			return
		}
		defer f.Close()
		out, err := os.Create(target)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if _, err := io.Copy(out, f); err != nil {
			out.Close()
			os.Remove(target)
			s.fail(w, r, err)
			return
		}
		out.Close()
		source = "Upload"
	} else {
		var req restoreRequest
		if err := decode(r, &req); err != nil {
			s.fail(w, r, err)
			return
		}
		r.SetPathValue("name", req.Name)
		p, err := s.backupPath(r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		in, err := os.Open(p)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out, err := os.Create(target)
		if err == nil {
			_, err = io.Copy(out, in)
			out.Close()
		}
		in.Close()
		if err != nil {
			os.Remove(target)
			s.fail(w, r, err)
			return
		}
		source = req.Name
	}
	ver, err := db.Validate(r.Context(), target)
	if err != nil {
		os.Remove(target)
		s.fail(w, r, fmt.Errorf("Backup ungültig: %w", err))
		return
	}
	s.record(r, "system.restore", "backup", source, fmt.Sprintf("Wiederherstellung aus %s (Schema %d) gestartet", source, ver), nil, nil)
	writeJSON(w, http.StatusAccepted, okResponse{OK: true})
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.Restore(target)
	}()
}

func (s *Server) handleRotate(w http.ResponseWriter, r *http.Request) {
	if s.Vault.Source().FromEnv {
		writeError(w, http.StatusConflict, "unsupported",
			"Der Master-Key kommt aus NETSCOPE_MASTER_KEY und kann nur dort geändert werden. Für eine Rotation per UI den Schlüssel in eine Datei auslagern.", nil)
		return
	}
	old := s.Vault.KeyID()
	k, err := vault.NewKey()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Vault.Rotate(r.Context(), k); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "vault.rotate", "vault", "master", "Master-Key rotiert ("+old+" → "+k.ID()+")", nil, nil)
	writeJSON(w, http.StatusOK, map[string]string{"keyId": k.ID(), "previousKeyId": old})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, total, err := s.Audit.List(r.Context(), audit.Filter{EntityType: q.Get("entity"), EntityID: q.Get("id"), Action: q.Get("action"),
		Text: q.Get("q"), Limit: qInt(r, "limit", 100), Offset: qInt(r, "offset", 0)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, auditList{Total: total, Items: items})
}

func (s *Server) handleCron(w http.ResponseWriter, r *http.Request) {
	expr := r.URL.Query().Get("expr")
	sched, err := cron.Parse(expr)
	if err != nil {
		writeJSON(w, http.StatusOK, cronResponse{Valid: false, Error: err.Error(), Next: []time.Time{}})
		return
	}
	text, _ := cron.Describe(expr)
	writeJSON(w, http.StatusOK, cronResponse{Valid: true, Text: text, Next: cron.NextN(sched, time.Now().In(s.Config.Location), 5)})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	m := metaResponse{Version: s.Version, EventTypes: plugin.Catalog(), CredentialTypes: plugin.CredentialTypes(),
		DeviceTypes: s.Settings.System().DeviceTypes, QueryFields: inventory.QueryFields(), SortFields: inventory.SortFields(),
		PublicURL: s.Settings.System().PublicURL, TimeZone: s.Config.Timezone, Publishers: s.Host.Publishers(),
		DeviceActions: []deviceAction{}, Scanners: []pluginShort{}}
	if m.Publishers == nil {
		m.Publishers = []pluginhost.PublisherInfo{}
	}
	for _, sv := range plugin.Severities {
		m.Severities = append(m.Severities, severityInfo{Value: string(sv), Label: sv.Label()})
	}
	for _, p := range plugin.Priorities {
		m.Priorities = append(m.Priorities, string(p))
	}
	for _, id := range s.Host.IDs() {
		p, _ := s.Host.Plugin(id)
		cfg, _ := s.Host.Config(id)
		info := p.Info()
		if ap, ok := p.(plugin.ActionProvider); ok {
			for _, a := range ap.Actions() {
				if a.Scope == plugin.ActionDevice {
					m.DeviceActions = append(m.DeviceActions, deviceAction{Plugin: id, PluginName: info.Name, Name: a.Name, Label: a.Label,
						Description: a.Description, Confirm: a.Confirm, Params: a.Params})
				}
			}
		}
		if _, ok := p.(plugin.Runner); ok && info.Kind == plugin.KindScanner && info.Targets != plugin.TargetNone {
			m.Scanners = append(m.Scanners, pluginShort{ID: id, Name: info.Name, Kind: info.Kind, Enabled: cfg.Enabled})
		}
	}
	writeJSON(w, http.StatusOK, m)
}

var uploadNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		s.fail(w, r, fmt.Errorf("Datei fehlt: %w", err))
		return
	}
	defer f.Close()
	dir := filepath.Join(s.Config.DataDir, "uploads")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		s.fail(w, r, err)
		return
	}
	name := uploadNameRe.ReplaceAllString(filepath.Base(hdr.Filename), "_")
	if name == "" || name == "." {
		name = "upload"
	}
	p := filepath.Join(dir, time.Now().Format("20060102-150405")+"-"+name)
	out, err := os.Create(p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	n, err := io.Copy(out, f)
	out.Close()
	if err != nil {
		os.Remove(p)
		s.fail(w, r, err)
		return
	}
	s.record(r, "upload", "file", filepath.Base(p), fmt.Sprintf("Datei hochgeladen (%d Bytes)", n), nil, nil)
	writeJSON(w, http.StatusCreated, uploadResponse{Path: p, Name: hdr.Filename, Size: n})
}
