package oui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ieeeFiles are the registry files kept in the plugin's data directory.
var ieeeFiles = []string{"oui.csv", "mam.csv", "oui36.csv"}

// defaultURLs are the IEEE registry downloads (MA-L, MA-M, MA-S).
var defaultURLs = []string{
	"https://standards-oui.ieee.org/oui/oui.csv",
	"https://standards-oui.ieee.org/oui28/mam.csv",
	"https://standards-oui.ieee.org/oui36/oui36.csv",
}

const (
	// userAgent is a regular browser user agent: the IEEE server rejects unknown clients.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"
	// maxDownload bounds a single registry file (oui.csv is about 4 MB).
	maxDownload = 64 << 20
	// minEntries is the minimum number of entries a downloaded registry file must contain.
	minEntries = 1000
)

func joinPath(dir, name string) string { return filepath.Join(dir, name) }

// fileNameFor derives the target file name from a download URL.
func fileNameFor(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("ungültige URL %q", raw)
	}
	name := path.Base(u.Path)
	if !strings.HasSuffix(strings.ToLower(name), ".csv") || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("URL %q verweist auf keine CSV-Datei", raw)
	}
	return name, nil
}

// download fetches all registry files, validates them and replaces the files in dir
// only if every download succeeded. It returns the entry count per file.
func download(ctx context.Context, client *http.Client, urls []string, dir string, min int) (map[string]int, error) {
	if len(urls) == 0 {
		return nil, errors.New("keine Download-URLs konfiguriert")
	}
	if dir == "" {
		return nil, errors.New("kein Datenverzeichnis für das Plugin konfiguriert")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	type result struct {
		name, tmp string
		entries   int
	}
	var results []result
	cleanup := func() {
		for _, r := range results {
			_ = os.Remove(r.tmp)
		}
	}
	seen := map[string]bool{}
	for _, raw := range urls {
		name, err := fileNameFor(raw)
		if err != nil {
			cleanup()
			return nil, err
		}
		if seen[name] {
			cleanup()
			return nil, fmt.Errorf("Dateiname %s mehrfach konfiguriert", name)
		}
		seen[name] = true
		tmp, n, err := fetch(ctx, client, raw, dir, name, min)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("%s: %w", raw, err)
		}
		results = append(results, result{name: name, tmp: tmp, entries: n})
	}
	counts := map[string]int{}
	for i, r := range results {
		if err := os.Rename(r.tmp, filepath.Join(dir, r.name)); err != nil {
			results = results[i:]
			cleanup()
			return nil, err
		}
		counts[r.name] = r.entries
	}
	return counts, nil
}

// fetch downloads one file into a temporary file in dir and validates it.
func fetch(ctx context.Context, client *http.Client, raw, dir, name string, min int) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/csv,text/plain;q=0.9,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("HTTP-Status %d", resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, "."+name+"-*.tmp")
	if err != nil {
		return "", 0, err
	}
	tmp := f.Name()
	fail := func(err error) (string, int, error) {
		f.Close()
		_ = os.Remove(tmp)
		return "", 0, err
	}
	size, err := io.Copy(f, io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return fail(fmt.Errorf("Download abgebrochen: %w", err))
	}
	if size > maxDownload {
		return fail(fmt.Errorf("Datei größer als %d MB", maxDownload>>20))
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	n, err := parseIEEECSV(f, newTable())
	if err != nil {
		return fail(fmt.Errorf("ungültige Datei: %w", err))
	}
	if n < min {
		return fail(fmt.Errorf("ungültige Datei: nur %d Einträge (mindestens %d erwartet)", n, min))
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}
	return tmp, n, nil
}
