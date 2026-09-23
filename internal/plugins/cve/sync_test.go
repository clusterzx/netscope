package cve

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// feedServer serves NVD 2.0 feeds and .meta files like nvd.nist.gov.
type feedServer struct {
	mu      sync.Mutex
	feeds   map[string][]byte // name -> uncompressed JSON
	fail    map[string]int    // name -> HTTP status for the .json.gz
	badMeta map[string]bool   // name -> announce a wrong checksum
	hits    map[string]int    // request path -> count
	srv     *httptest.Server
}

const emptyFeed = `{"resultsPerPage":0,"startIndex":0,"totalResults":0,"format":"NVD_CVE","version":"2.0","timestamp":"2026-09-01T03:00:00.000","vulnerabilities":[]}`

func newFeedServer(t *testing.T) *feedServer {
	s := &feedServer{feeds: map[string][]byte{}, fail: map[string]int{}, badMeta: map[string]bool{}, hits: map[string]int{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *feedServer) set(name string, doc []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feeds[name] = doc
}

func (s *feedServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

func (s *feedServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits[r.URL.Path]++
	file := strings.TrimPrefix(r.URL.Path, "/"+feedFilePrefix)
	name, ext, _ := strings.Cut(file, ".")
	doc, ok := s.feeds[name]
	if !ok {
		doc = []byte(emptyFeed)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(doc)
	zw.Close()
	switch ext {
	case "meta":
		sum := sha256.Sum256(doc)
		hash := strings.ToUpper(hex.EncodeToString(sum[:]))
		if s.badMeta[name] {
			hash = strings.Repeat("A", 64)
		}
		fmt.Fprintf(w, "lastModifiedDate:2026-09-01T03:00:00-04:00\r\nsize:%d\r\nzipSize:%d\r\ngzSize:%d\r\nsha256:%s\r\n", len(doc), gz.Len()+100, gz.Len(), hash)
	case "json.gz":
		if code := s.fail[name]; code != 0 {
			w.WriteHeader(code)
			return
		}
		w.Write(gz.Bytes())
	default:
		http.NotFound(w, r)
	}
}

// fixtureDoc rebuilds a fixture feed keeping selected CVEs and applying edits.
func fixtureDoc(t *testing.T, name string, keep func(id string) bool, edit func(id string, cve map[string]any)) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var items []map[string]map[string]any
	if err := json.Unmarshal(doc["vulnerabilities"], &items); err != nil {
		t.Fatal(err)
	}
	var out []map[string]map[string]any
	for _, it := range items {
		id, _ := it["cve"]["id"].(string)
		if keep != nil && !keep(id) {
			continue
		}
		if edit != nil {
			edit(id, it["cve"])
		}
		out = append(out, it)
	}
	b, _ := json.Marshal(out)
	doc["vulnerabilities"] = b
	doc["totalResults"] = json.RawMessage(fmt.Sprint(len(out)))
	doc["resultsPerPage"] = json.RawMessage(fmt.Sprint(len(out)))
	res, _ := json.Marshal(doc)
	return res
}

func only(ids ...string) func(string) bool {
	return func(id string) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
}

func feedRow(t *testing.T, rc *plugin.RunContext, name string) feedState {
	t.Helper()
	states, err := loadFeedStates(context.Background(), rc.DB.R)
	if err != nil {
		t.Fatal(err)
	}
	return states[name]
}

func TestSyncEndToEnd(t *testing.T) {
	ctx := context.Background()
	d := newTestDB(t)
	fs := newFeedServer(t)
	fs.set("2024", fixtureDoc(t, "nvdcve-2.0-2024.json.gz", func(id string) bool { return id != "CVE-2024-9264" }, nil))
	fs.set("2021", fixtureDoc(t, "nvdcve-2.0-2021.json.gz", nil, nil))
	c := newClock()
	t0 := c.Ms()
	ssh := addDevice(t, d, "nas", t0)
	addPort(t, d, ssh, 22, "OpenSSH", "9.6p1", []string{"cpe:/a:openbsd:openssh:9.6p1"}, t0)
	graf := addDevice(t, d, "grafana", t0)
	addHTTPApp(t, d, graf, 3000, []plugin.DetectedApp{{Name: "Grafana", Version: "11.0.0", CPE: "cpe:2.3:a:grafana:grafana", Confidence: "high"}}, t0)
	nginx := addDevice(t, d, "proxy", t0)
	addPort(t, d, nginx, 80, "nginx", "1.18.0", []string{"cpe:/a:igor_sysoev:nginx:1.18.0"}, t0)

	p := newTestPlugin(c)
	settings := map[string]any{"feed_base_url": fs.srv.URL, "start_year": 2021}
	rc, evs := testRC(t, p, d, settings)

	// 1. initial load: every yearly feed plus modified, then a full match without events
	c.Advance(time.Hour)
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	for y := 2021; y <= 2026; y++ {
		if n := fs.count(fmt.Sprintf("/%s%d.json.gz", feedFilePrefix, y)); n != 1 {
			t.Errorf("feed %d downloaded %d times", y, n)
		}
		if st := feedRow(t, rc, fmt.Sprint(y)); st.Status != "ok" || st.SHA256 == "" {
			t.Errorf("feed %d state %+v", y, st)
		}
	}
	if st := feedRow(t, rc, "2024"); st.CVECount != 14 { // 16 fixture entries without CVE-2024-9264 and the rejected one
		t.Errorf("2024 count %d", st.CVECount)
	}
	if got := activeCVEs(t, d, ssh); got["CVE-2024-6387"] != MatchRange {
		t.Fatalf("ssh: %v", got)
	}
	if got := activeCVEs(t, d, graf); got["CVE-2024-9264"] != "" {
		t.Fatalf("9264 not published yet: %v", got)
	}
	if len(evs.Events) != 0 {
		t.Fatalf("initial load raised events: %+v", evs.Events)
	}
	stats := rc.Stats()
	if stats["sync_mode"] != "initial" || stats["feeds_downloaded"] != 7 {
		t.Errorf("stats %+v", stats)
	}
	var files []string
	entries, _ := os.ReadDir(rc.DataDir)
	for _, e := range entries {
		files = append(files, e.Name())
	}
	if len(files) != 0 {
		t.Errorf("downloads left behind: %v", files)
	}
	status, err := SyncStatus(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if status.Empty || len(status.Feeds) != 7 || status.Feeds[0].Name != "modified" || status.Feeds[1].Name != "2026" ||
		status.SyncMode != "initial" || status.CVECount == 0 || status.CPEMatchCount == 0 || status.LastMatch == nil ||
		status.LastMatch.Status != "ok" || status.LastMatch.Devices != 3 || status.Disclaimer == "" {
		t.Fatalf("status %+v", status)
	}

	// 2. next day: only the modified feed; a newly published CVE raises cve.new, a CVE the
	// NVD merely modified that was already reported does not
	fs.set("modified", fixtureDoc(t, "nvdcve-2.0-2024.json.gz", only("CVE-2024-9264", "CVE-2024-6387"), func(id string, cve map[string]any) {
		if id == "CVE-2024-6387" {
			cve["lastModified"] = "2026-09-01T10:00:00.000"
		}
	}))
	c.Advance(24 * time.Hour)
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	if n := fs.count("/" + feedFilePrefix + "2024.json.gz"); n != 1 {
		t.Errorf("incremental sync downloaded a yearly feed again (%d)", n)
	}
	if n := fs.count("/" + feedFilePrefix + "2024.meta"); n != 1 {
		t.Errorf("incremental sync checked yearly metas (%d)", n)
	}
	if got := activeCVEs(t, d, graf); got["CVE-2024-9264"] != MatchExact {
		t.Fatalf("grafana after modified feed: %v", got)
	}
	news := eventsOf(evs, plugin.EvCVENew)
	if len(news) != 1 || news[0].Payload["cve"] != "CVE-2024-9264" || news[0].DeviceID != graf || news[0].Severity != plugin.SevHigh {
		t.Fatalf("events after incremental sync: %+v", evs.Events)
	}
	if feedRow(t, rc, "modified").Status != "ok" || rc.Stats()["sync_mode"] != "incremental" {
		t.Errorf("modified state %+v, stats %+v", feedRow(t, rc, "modified"), rc.Stats())
	}

	// 3. a failing feed: nvd.sync_failed, error, the match still runs
	fs.mu.Lock()
	fs.fail["modified"] = http.StatusInternalServerError
	fs.feeds["modified"] = []byte(emptyFeed) // new checksum forces a download
	fs.mu.Unlock()
	c.Advance(24 * time.Hour)
	err = p.Run(ctx, rc)
	if err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("expected sync error, got %v", err)
	}
	failed := eventsOf(evs, plugin.EvNVDSyncFailed)
	if len(failed) != 1 || failed[0].Payload["feed"] != "modified" || !strings.Contains(failed[0].Payload["error"].(string), "500") {
		t.Fatalf("sync_failed events: %+v", failed)
	}
	if st := feedRow(t, rc, "modified"); st.Status != "error" || st.Error == "" {
		t.Errorf("modified after failure: %+v", st)
	}
	if last, _ := lastFullMatch(ctx, d); last != c.Ms() {
		t.Errorf("match did not run after the failed sync (%d vs %d)", last, c.Ms())
	}
	fs.mu.Lock()
	delete(fs.fail, "modified")
	fs.mu.Unlock()

	// 4. more than 7 days later every yearly meta is checked again (unchanged: no download)
	c.Advance(8 * 24 * time.Hour)
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	if rc.Stats()["sync_mode"] != "full" || fs.count("/"+feedFilePrefix+"2024.meta") != 2 || fs.count("/"+feedFilePrefix+"2024.json.gz") != 1 {
		t.Errorf("full re-check: stats %+v, meta %d, gz %d", rc.Stats(), fs.count("/"+feedFilePrefix+"2024.meta"), fs.count("/"+feedFilePrefix+"2024.json.gz"))
	}

	// 5. a checksum that never matches fails after the retries
	fs.mu.Lock()
	fs.badMeta["2025"] = true
	fs.feeds["2025"] = []byte(strings.Replace(emptyFeed, "03:00:00", "04:00:00", 1))
	fs.mu.Unlock()
	c.Advance(8 * 24 * time.Hour)
	if err := p.Run(ctx, rc); err == nil || !strings.Contains(err.Error(), "Prüfsumme") {
		t.Fatalf("checksum error expected, got %v", err)
	}
	fs.mu.Lock()
	delete(fs.badMeta, "2025")
	fs.mu.Unlock()

	// 6. raising the start year removes older CVEs, their feeds and matches (silently)
	rc2, evs2 := testRC(t, p, d, map[string]any{"feed_base_url": fs.srv.URL, "start_year": 2024})
	c.Advance(time.Hour)
	if err := p.Run(ctx, rc2); err != nil {
		t.Fatal(err)
	}
	var old int
	_ = d.R.QueryRow("SELECT COUNT(*) FROM nvd_cves WHERE id < 'CVE-2024-'").Scan(&old)
	if old != 0 || feedRow(t, rc2, "2021").Name != "" {
		t.Errorf("old CVEs %d, 2021 row %+v", old, feedRow(t, rc2, "2021"))
	}
	if got := activeCVEs(t, d, nginx); got["CVE-2021-23017"] != "" || got["CVE-2024-7347"] != MatchRange {
		t.Errorf("nginx after pruning: %v", got)
	}
	if n := len(eventsOf(evs2, plugin.EvCVEResolved)); n != 0 {
		t.Errorf("pruning raised %d resolved events", n)
	}
}

func TestSyncKeepDownloadsAndActions(t *testing.T) {
	ctx := context.Background()
	d := newTestDB(t)
	fs := newFeedServer(t)
	fs.set("2024", fixtureDoc(t, "nvdcve-2.0-2024.json.gz", only("CVE-2024-6387"), nil))
	c := newClock()
	p := newTestPlugin(c)
	rc, _ := testRC(t, p, d, map[string]any{"feed_base_url": fs.srv.URL + "/", "start_year": 2024, "keep_downloads": true})
	actions := p.Actions()
	if len(actions) != 2 || actions[0].Name != "sync" || actions[1].Name != "match" || actions[0].Scope != plugin.ActionPlugin {
		t.Fatalf("actions %+v", actions)
	}
	// match on an empty mirror is refused with a German hint
	if _, err := p.RunAction(ctx, rc, "match", nil); err == nil || !strings.Contains(err.Error(), "leer") {
		t.Fatalf("match on empty mirror: %v", err)
	}
	res, err := p.RunAction(ctx, rc, "sync", map[string]any{"full": false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Message, "NVD: 4 Feed(s) geladen") {
		t.Errorf("message %q", res.Message)
	}
	for _, name := range []string{"nvdcve-2.0-2024.json.gz", "nvdcve-2.0-2026.json.gz", "nvdcve-2.0-modified.json.gz"} {
		if _, err := os.Stat(filepath.Join(rc.DataDir, name)); err != nil {
			t.Errorf("kept download %s: %v", name, err)
		}
	}
	// forced full sync downloads everything again
	c.Advance(time.Hour)
	res, err = p.RunAction(ctx, rc, "sync", map[string]any{"full": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Data.(map[string]any)["syncMode"] != "full" {
		t.Errorf("forced sync data %+v", res.Data)
	}
	if fs.count("/"+feedFilePrefix+"2024.json.gz") != 2 {
		t.Errorf("forced sync: %d downloads", fs.count("/"+feedFilePrefix+"2024.json.gz"))
	}
	res, err = p.RunAction(ctx, rc, "match", nil)
	if err != nil || !strings.HasPrefix(res.Message, "Abgleich:") {
		t.Fatalf("match action: %v %+v", err, res)
	}
	if _, err := p.RunAction(ctx, rc, "nope", nil); err == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestSyncCancel(t *testing.T) {
	d := newTestDB(t)
	fs := newFeedServer(t)
	c := newClock()
	p := newTestPlugin(c)
	rc, _ := testRC(t, p, d, map[string]any{"feed_base_url": fs.srv.URL, "start_year": 2002})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Run(ctx, rc); err == nil {
		t.Fatal("cancelled run succeeded")
	}
}

func TestSettings(t *testing.T) {
	p := New()
	rc, _, _ := plugintest.RunContext(t, p, nil)
	cfg := loadConfig(rc.Settings)
	if cfg.baseURL != DefaultFeedURL || cfg.startYear != 2002 || cfg.minScore != 7 || !cfg.includePackages || !cfg.includeOS ||
		!cfg.includeHTTP || cfg.keepDownloads || cfg.httpTimeout != 5*time.Minute || !cfg.heuristicEvents {
		t.Fatalf("defaults %+v", cfg)
	}
	info := p.Info()
	if info.ID != "cve" || info.Kind != plugin.KindProcessor || info.DefaultSchedule != "30 4 * * *" || info.DefaultTimeout != 2*time.Hour ||
		info.DefaultConcurrency != 4 || !info.DefaultEnabled || info.Targets != plugin.TargetNone {
		t.Fatalf("info %+v", info)
	}
	if _, err := p.Schema().Validate(map[string]any{"min_event_score": "6.5"}, nil, nil); err == nil {
		t.Error("invalid score accepted")
	}
	if _, err := p.Schema().Validate(map[string]any{"start_year": 1999}, nil, nil); err == nil {
		t.Error("start year 1999 accepted")
	}
	vals, err := p.Schema().Validate(map[string]any{"start_year": time.Now().Year() + 1}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateSettings(plugin.NewSettings(vals)); err == nil {
		t.Error("future start year accepted")
	}
	vals, _ = p.Schema().Validate(map[string]any{"feed_base_url": "https://mirror.example/nvdcve-2.0-2024.json.gz"}, nil, nil)
	if err := p.ValidateSettings(plugin.NewSettings(vals)); err == nil {
		t.Error("file URL accepted as base URL")
	}
	var _ plugin.Runner = p
	var _ plugin.ChangeHandler = p
	var _ plugin.RunFinishedHandler = p
	var _ plugin.ActionProvider = p
	var _ plugin.SettingsValidator = p
}
