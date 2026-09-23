package cve

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// NVD CVE JSON 2.0 data feeds: <base>/nvdcve-2.0-<name>.json.gz with a .meta file.
const (
	feedFilePrefix = "nvdcve-2.0-"
	feedModified   = "modified"
	// firstFeedYear is the oldest yearly feed; it also contains CVE-1999..CVE-2001.
	firstFeedYear = 2002
	maxRefs       = 20
)

// feedMeta is the content of a .meta file.
type feedMeta struct {
	LastModified string
	Size         int64 // uncompressed JSON
	GzSize       int64
	SHA256       string // lowercase hex of the uncompressed JSON
}

// parseMeta parses "key:value" lines (the value of lastModifiedDate contains colons).
func parseMeta(r io.Reader) (*feedMeta, error) {
	m := &feedMeta{}
	sc := bufio.NewScanner(io.LimitReader(r, 64<<10))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "lastModifiedDate":
			m.LastModified = v
		case "size":
			m.Size, _ = strconv.ParseInt(v, 10, 64)
		case "gzSize":
			m.GzSize, _ = strconv.ParseInt(v, 10, 64)
		case "sha256":
			m.SHA256 = strings.ToLower(v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(m.SHA256) != 64 {
		return nil, errors.New("Meta-Datei ohne gültige SHA-256-Prüfsumme")
	}
	return m, nil
}

// httpStatusError is returned for non-200 responses.
type httpStatusError struct {
	URL  string
	Code int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d für %s", e.Code, e.URL)
}

func isNotFound(err error) bool {
	var he *httpStatusError
	return errors.As(err, &he) && he.Code == http.StatusNotFound
}

// retryable reports whether a failed request is worth repeating. The NVD answers 404
// for a feed for a short time while it is being regenerated.
func retryable(err error) bool {
	var he *httpStatusError
	if errors.As(err, &he) {
		return he.Code == http.StatusNotFound || he.Code == http.StatusTooManyRequests || he.Code >= 500
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// fetcher downloads feeds.
type fetcher struct {
	client     *http.Client
	base       string
	dir        string
	keep       bool
	userAgent  string
	attempts   int
	retryDelay time.Duration
}

func (f *fetcher) url(name, ext string) string {
	return strings.TrimRight(f.base, "/") + "/" + feedFilePrefix + name + ext
}

func (f *fetcher) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, &httpStatusError{URL: url, Code: resp.StatusCode}
	}
	return resp, nil
}

// retry runs fn up to f.attempts times with a growing delay between attempts.
func (f *fetcher) retry(ctx context.Context, fn func() error) error {
	attempts := max(f.attempts, 1)
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			t := time.NewTimer(f.retryDelay * time.Duration(i))
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		if err = fn(); err == nil || !retryable(err) || ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// meta fetches and parses the .meta file of a feed.
func (f *fetcher) meta(ctx context.Context, name string) (*feedMeta, error) {
	var m *feedMeta
	err := f.retry(ctx, func() error {
		resp, err := f.get(ctx, f.url(name, ".meta"))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		m, err = parseMeta(resp.Body)
		return err
	})
	return m, err
}

// checksumError reports a downloaded feed that does not match its meta data. The NVD
// regenerates feeds regularly, so the meta file is fetched again before the next attempt.
type checksumError struct{ msg string }

func (e *checksumError) Error() string { return e.msg }

// download stores the gzip feed in f.dir and verifies it against the meta data (which
// is refreshed in place if the feed was regenerated meanwhile). The caller removes the
// file (see release).
func (f *fetcher) download(ctx context.Context, name string, m *feedMeta) (string, int64, error) {
	if err := os.MkdirAll(f.dir, 0o750); err != nil {
		return "", 0, err
	}
	var (
		path    string
		n       int64
		lastErr error
	)
	err := f.retry(ctx, func() error {
		var ce *checksumError
		if errors.As(lastErr, &ce) {
			fresh, err := f.meta(ctx, name)
			if err != nil {
				return err
			}
			*m = *fresh
		}
		err := f.fetchOnce(ctx, name, m, &path, &n)
		lastErr = err
		return err
	})
	if err != nil {
		return "", n, err
	}
	if f.keep {
		final := filepath.Join(f.dir, feedFilePrefix+name+".json.gz")
		_ = os.Remove(final)
		if err := os.Rename(path, final); err == nil {
			path = final
		}
	}
	return path, n, nil
}

func (f *fetcher) fetchOnce(ctx context.Context, name string, m *feedMeta, path *string, n *int64) error {
	tmp, err := os.CreateTemp(f.dir, feedFilePrefix+name+"-*.part")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	resp, err := f.get(ctx, f.url(name, ".json.gz"))
	if err != nil {
		return err
	}
	*n, err = io.Copy(tmp, resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("Download %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if m.GzSize > 0 && *n != m.GzSize {
		return &checksumError{fmt.Sprintf("Download %s unvollständig oder veraltet: %d statt %d Bytes", name, *n, m.GzSize)}
	}
	if err := verifyFeed(ctx, tmp.Name(), m.SHA256); err != nil {
		return err
	}
	ok = true
	*path = tmp.Name()
	return nil
}

// removeStale deletes partial downloads left behind by an interrupted sync.
func (f *fetcher) removeStale() {
	parts, _ := filepath.Glob(filepath.Join(f.dir, feedFilePrefix+"*.part"))
	for _, p := range parts {
		_ = os.Remove(p)
	}
}

// release removes a downloaded file unless downloads are kept.
func (f *fetcher) release(path string) {
	if path != "" && !f.keep {
		_ = os.Remove(path)
	}
}

// verifyFeed checks the SHA-256 of the uncompressed feed (streamed, constant memory).
func verifyFeed(ctx context.Context, path, want string) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	gz, err := gzip.NewReader(fh)
	if err != nil {
		return fmt.Errorf("Feed ist kein gzip: %w", err)
	}
	defer gz.Close()
	h := sha256.New()
	if _, err := io.Copy(h, ctxReader{ctx, gz}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &checksumError{fmt.Sprintf("Feed beschädigt: %v", err)}
	}
	if got := hex.EncodeToString(h.Sum(nil)); want != "" && got != want {
		return &checksumError{fmt.Sprintf("Prüfsumme stimmt nicht (erwartet %s, erhalten %s)", want, got)}
	}
	return nil
}

// ctxReader aborts reads once ctx is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// ---------------------------------------------------------------- parsing

// cveRecord is the stored form of one CVE.
type cveRecord struct {
	ID           string
	Published    int64
	LastModified int64
	Status       string
	Rejected     bool
	Score        *float64
	Vector       string
	CVSSVersion  string
	Severity     string
	Description  string
	Refs         []Reference
	CWEs         []string
	Matches      []cpeCriteria
}

// cpeCriteria is one vulnerable cpeMatch entry of a CVE configuration.
type cpeCriteria struct {
	CPE
	StartIncl, StartExcl, EndIncl, EndExcl string
}

// Reference is a link of a CVE.
type Reference struct {
	URL  string   `json:"url"`
	Tags []string `json:"tags,omitempty"`
}

type nvdLang struct {
	Lang  string `json:"lang"`
	Value string `json:"value"`
}

type nvdMetric struct {
	Type         string `json:"type"`
	BaseSeverity string `json:"baseSeverity"` // CVSS v2 keeps it outside cvssData
	CVSSData     struct {
		Version      string  `json:"version"`
		VectorString string  `json:"vectorString"`
		BaseScore    float64 `json:"baseScore"`
		BaseSeverity string  `json:"baseSeverity"`
	} `json:"cvssData"`
}

type nvdCPEMatch struct {
	Vulnerable            bool   `json:"vulnerable"`
	Criteria              string `json:"criteria"`
	VersionStartIncluding string `json:"versionStartIncluding"`
	VersionStartExcluding string `json:"versionStartExcluding"`
	VersionEndIncluding   string `json:"versionEndIncluding"`
	VersionEndExcluding   string `json:"versionEndExcluding"`
}

type nvdItem struct {
	CVE struct {
		ID           string    `json:"id"`
		Published    string    `json:"published"`
		LastModified string    `json:"lastModified"`
		VulnStatus   string    `json:"vulnStatus"`
		Descriptions []nvdLang `json:"descriptions"`
		Metrics      struct {
			V31 []nvdMetric `json:"cvssMetricV31"`
			V30 []nvdMetric `json:"cvssMetricV30"`
			V40 []nvdMetric `json:"cvssMetricV40"`
			V2  []nvdMetric `json:"cvssMetricV2"`
		} `json:"metrics"`
		Weaknesses []struct {
			Description []nvdLang `json:"description"`
		} `json:"weaknesses"`
		Configurations []struct {
			Nodes []struct {
				CPEMatch []nvdCPEMatch `json:"cpeMatch"`
			} `json:"nodes"`
		} `json:"configurations"`
		References []struct {
			URL  string   `json:"url"`
			Tags []string `json:"tags"`
		} `json:"references"`
	} `json:"cve"`
}

// parseNVDTime parses NVD timestamps ("2024-07-01T13:15:06.467", UTC without zone).
func parseNVDTime(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339Nano} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// bestMetric picks the preferred CVSS metric: v3.1, then v3.0, then v4.0, then v2; within
// one version the NVD's own (Primary) assessment wins over the CNA's (Secondary).
func bestMetric(it *nvdItem) (*nvdMetric, string) {
	lists := []struct {
		ms  []nvdMetric
		ver string
	}{{it.CVE.Metrics.V31, "3.1"}, {it.CVE.Metrics.V30, "3.0"}, {it.CVE.Metrics.V40, "4.0"}, {it.CVE.Metrics.V2, "2.0"}}
	for _, l := range lists {
		var pick *nvdMetric
		for i := range l.ms {
			m := &l.ms[i]
			if pick == nil || (m.Type == "Primary" && pick.Type != "Primary") {
				pick = m
			}
		}
		if pick != nil {
			ver := pick.CVSSData.Version
			if ver == "" {
				ver = l.ver
			}
			return pick, ver
		}
	}
	return nil, ""
}

// toRecord converts a feed item.
func toRecord(it *nvdItem) (*cveRecord, error) {
	c := &it.CVE
	if !strings.HasPrefix(c.ID, "CVE-") {
		return nil, fmt.Errorf("ungültige CVE-ID %q", c.ID)
	}
	r := &cveRecord{ID: c.ID, Published: parseNVDTime(c.Published), LastModified: parseNVDTime(c.LastModified), Status: c.VulnStatus}
	if strings.EqualFold(c.VulnStatus, "Rejected") {
		r.Rejected = true
		return r, nil
	}
	for _, d := range c.Descriptions {
		if d.Lang == "en" {
			r.Description = strings.TrimSpace(d.Value)
			break
		}
	}
	if m, ver := bestMetric(it); m != nil {
		score := m.CVSSData.BaseScore
		r.Score = &score
		r.Vector = m.CVSSData.VectorString
		r.CVSSVersion = ver
		sev := m.CVSSData.BaseSeverity
		if sev == "" {
			sev = m.BaseSeverity
		}
		r.Severity = strings.ToLower(sev)
	}
	seenRef := map[string]int{}
	for _, ref := range c.References {
		if ref.URL == "" {
			continue
		}
		if i, ok := seenRef[ref.URL]; ok {
			r.Refs[i].Tags = mergeTags(r.Refs[i].Tags, ref.Tags)
			continue
		}
		if len(r.Refs) >= maxRefs {
			continue
		}
		seenRef[ref.URL] = len(r.Refs)
		r.Refs = append(r.Refs, Reference{URL: ref.URL, Tags: mergeTags(nil, ref.Tags)})
	}
	seenCWE := map[string]bool{}
	for _, w := range c.Weaknesses {
		for _, d := range w.Description {
			v := strings.TrimSpace(d.Value)
			if v == "" || strings.HasPrefix(v, "NVD-CWE-") || seenCWE[v] {
				continue
			}
			seenCWE[v] = true
			r.CWEs = append(r.CWEs, v)
		}
	}
	seenCrit := map[cpeCriteria]bool{}
	for _, cfg := range c.Configurations {
		for _, n := range cfg.Nodes {
			for _, m := range n.CPEMatch {
				if !m.Vulnerable {
					continue
				}
				cpe, err := ParseCPE23(m.Criteria)
				if err != nil {
					continue
				}
				cr := cpeCriteria{CPE: cpe,
					StartIncl: unescapeFS(m.VersionStartIncluding), StartExcl: unescapeFS(m.VersionStartExcluding),
					EndIncl: unescapeFS(m.VersionEndIncluding), EndExcl: unescapeFS(m.VersionEndExcluding)}
				if seenCrit[cr] {
					continue
				}
				seenCrit[cr] = true
				r.Matches = append(r.Matches, cr)
			}
		}
	}
	return r, nil
}

// unescapeFS removes formatted-string quoting from version bounds ("11.6\(1\)").
func unescapeFS(v string) string {
	if !strings.Contains(v, `\`) {
		return strings.ToLower(strings.TrimSpace(v))
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) {
			i++
		}
		b.WriteByte(v[i])
	}
	return strings.ToLower(strings.TrimSpace(b.String()))
}

func mergeTags(have, add []string) []string {
	for _, t := range add {
		dup := false
		for _, h := range have {
			if h == t {
				dup = true
				break
			}
		}
		if !dup {
			have = append(have, t)
		}
	}
	return have
}

// parseFeed streams an (uncompressed) NVD 2.0 feed document and calls fn for every CVE.
// Only one CVE is decoded at a time, so memory stays bounded regardless of the feed
// size. total is called with "totalResults" if the document states it.
func parseFeed(ctx context.Context, r io.Reader, total func(int), fn func(*cveRecord) error) (int, error) {
	dec := json.NewDecoder(r)
	if err := expectDelim(dec, '{'); err != nil {
		return 0, err
	}
	n := 0
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return n, err
		}
		key, _ := t.(string)
		switch key {
		case "vulnerabilities":
			if err := expectDelim(dec, '['); err != nil {
				return n, err
			}
			for dec.More() {
				if n%200 == 0 {
					if err := ctx.Err(); err != nil {
						return n, err
					}
				}
				var it nvdItem
				if err := dec.Decode(&it); err != nil {
					return n, fmt.Errorf("CVE-Eintrag %d: %w", n+1, err)
				}
				rec, err := toRecord(&it)
				if err != nil {
					continue
				}
				n++
				if err := fn(rec); err != nil {
					return n, err
				}
			}
			if err := expectDelim(dec, ']'); err != nil {
				return n, err
			}
		case "totalResults":
			var v int
			if err := dec.Decode(&v); err != nil {
				return n, err
			}
			if total != nil {
				total(v)
			}
		default:
			if err := skipValue(dec); err != nil {
				return n, err
			}
		}
	}
	return n, expectDelim(dec, '}')
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); !ok || d != want {
		return fmt.Errorf("unerwartetes JSON-Token %v, erwartet %v", t, want)
	}
	return nil
}

// skipValue consumes one JSON value token by token (never materialising it).
func skipValue(dec *json.Decoder) error {
	depth := 0
	for {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
		if depth == 0 {
			return nil
		}
	}
}

// openFeed opens a gzip feed file for parsing.
func openFeed(path string) (io.ReadCloser, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bufio.NewReaderSize(fh, 256<<10))
	if err != nil {
		fh.Close()
		return nil, err
	}
	return &gzFile{gz, fh}, nil
}

type gzFile struct {
	*gzip.Reader
	f *os.File
}

func (g *gzFile) Close() error {
	g.Reader.Close()
	return g.f.Close()
}
