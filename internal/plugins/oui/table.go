package oui

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// table maps upper-case hex MAC prefixes of any length (2–12 digits) to vendor names.
// A lookup returns the longest matching prefix.
type table struct {
	entries map[string]string
	lengths []int // prefix lengths present, longest first
}

func newTable() *table { return &table{entries: map[string]string{}} }

func (t *table) add(prefix, vendor string) {
	if _, ok := t.entries[prefix]; !ok {
		found := false
		for _, l := range t.lengths {
			if l == len(prefix) {
				found = true
				break
			}
		}
		if !found {
			t.lengths = append(t.lengths, len(prefix))
			sort.Sort(sort.Reverse(sort.IntSlice(t.lengths)))
		}
	}
	t.entries[prefix] = vendor
}

// lookup returns the vendor of the longest prefix of hex (12 upper-case hex digits).
func (t *table) lookup(hex string) (string, bool) {
	for _, l := range t.lengths {
		if l > len(hex) {
			continue
		}
		if v, ok := t.entries[hex[:l]]; ok {
			return v, true
		}
	}
	return "", false
}

func (t *table) len() int { return len(t.entries) }

// normalizePrefix strips separators and upper-cases a hex prefix. It returns false for
// anything that is not 2–12 hex digits.
func normalizePrefix(s string) (string, bool) {
	s = strings.ToUpper(strings.NewReplacer(":", "", "-", "", ".", "").Replace(strings.TrimSpace(s)))
	if len(s) < 2 || len(s) > 12 {
		return "", false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
			return "", false
		}
	}
	return s, true
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func skipBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && bytes.Equal(b, utf8BOM) {
		_, _ = br.Discard(3)
	}
	return br
}

// parseIEEECSV reads an IEEE registry CSV (oui.csv, mam.csv, oui36.csv):
//
//	Registry,Assignment,Organization Name,Organization Address
//	MA-L,9483C4,GL Technologies (Hong Kong) Limited,"103B Enterprise Place, …"
func parseIEEECSV(r io.Reader, t *table) (int, error) {
	cr := csv.NewReader(skipBOM(r))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil {
		return 0, fmt.Errorf("CSV-Kopfzeile fehlt: %w", err)
	}
	if len(header) < 3 || !strings.EqualFold(strings.TrimSpace(header[0]), "Registry") ||
		!strings.EqualFold(strings.TrimSpace(header[1]), "Assignment") {
		return 0, errors.New("keine IEEE-Registerdatei (Kopfzeile „Registry,Assignment,…“ erwartet)")
	}
	n := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, fmt.Errorf("CSV-Fehler: %w", err)
		}
		if len(rec) < 3 {
			continue
		}
		prefix, ok := normalizePrefix(rec[1])
		vendor := strings.TrimSpace(rec[2])
		if !ok || vendor == "" {
			continue
		}
		switch len(prefix) {
		case 6, 7, 9: // MA-L 24 bit, MA-M 28 bit, MA-S/IAB 36 bit
		default:
			continue
		}
		t.add(prefix, vendor)
		n++
	}
	return n, nil
}

// parseTabFile reads arp-scan's ieee-oui.txt and mac-vendor.txt ("<prefix>\t<vendor>",
// "#" comments, prefixes with optional ":", "-" or "." separators).
func parseTabFile(r io.Reader, t *table) (int, error) {
	return parseLines(r, t, "\t")
}

// parseNmapFile reads nmap's nmap-mac-prefixes ("<prefix> <vendor>").
func parseNmapFile(r io.Reader, t *table) (int, error) {
	return parseLines(r, t, " ")
}

func parseLines(r io.Reader, t *table, sep string) (int, error) {
	sc := bufio.NewScanner(skipBOM(r))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		key, rest, ok := strings.Cut(line, sep)
		if !ok {
			continue
		}
		prefix, ok := normalizePrefix(key)
		if !ok {
			continue
		}
		vendor := strings.TrimLeft(rest, " \t")
		if i := strings.IndexByte(vendor, '\t'); i >= 0 {
			vendor = vendor[:i]
		}
		vendor = strings.TrimSpace(vendor)
		if vendor == "" {
			continue
		}
		t.add(prefix, vendor)
		n++
	}
	return n, sc.Err()
}

// source is one vendor file of a layer.
type source struct {
	path  string
	parse func(io.Reader, *table) (int, error)
}

// layer is one data source family; layers are consulted in order.
type layer struct {
	name    string
	sources []source
}

// dataLayers returns the lookup layers in priority order: the IEEE files downloaded into
// dataDir, then arp-scan's and nmap's vendor files.
func dataLayers(dataDir string, fallbacks []layer) []layer {
	ieee := layer{name: "IEEE"}
	if dataDir != "" {
		for _, f := range ieeeFiles {
			ieee.sources = append(ieee.sources, source{path: joinPath(dataDir, f), parse: parseIEEECSV})
		}
	}
	return append([]layer{ieee}, fallbacks...)
}

// defaultFallbacks are the vendor files shipped with arp-scan and nmap.
var defaultFallbacks = []layer{
	{name: "arp-scan", sources: []source{
		{path: "/usr/share/arp-scan/ieee-oui.txt", parse: parseTabFile},
		{path: "/usr/share/arp-scan/mac-vendor.txt", parse: parseTabFile},
		{path: "/etc/arp-scan/mac-vendor.txt", parse: parseTabFile},
	}},
	{name: "nmap", sources: []source{
		{path: "/usr/share/nmap/nmap-mac-prefixes", parse: parseNmapFile},
	}},
}

// database is the loaded, layered vendor table.
type database struct {
	layers []loadedLayer
	files  map[string]int // loaded file → entry count
}

type loadedLayer struct {
	name string
	t    *table
}

// placeholder vendors that do not identify a manufacturer.
func placeholder(v string) bool {
	switch strings.ToLower(v) {
	case "private", "ieee registration authority":
		return true
	}
	return false
}

// lookup returns the vendor and the layer it came from for a normalized MAC
// (aa:bb:cc:dd:ee:ff). Placeholder entries ("Private", "IEEE Registration Authority")
// fall through to the next layer.
func (db *database) lookup(mac string) (vendor, layerName string) {
	hex := strings.ToUpper(strings.ReplaceAll(mac, ":", ""))
	if len(hex) != 12 {
		return "", ""
	}
	for _, l := range db.layers {
		if v, ok := l.t.lookup(hex); ok && !placeholder(v) {
			return v, l.name
		}
	}
	return "", ""
}

func (db *database) empty() bool { return len(db.layers) == 0 }

// entries returns the total number of entries of all layers.
func (db *database) entries() int {
	n := 0
	for _, l := range db.layers {
		n += l.t.len()
	}
	return n
}

// loadDatabase parses all existing files of the layers. Missing files are skipped,
// unreadable or broken files are reported.
func loadDatabase(layers []layer) (*database, error) {
	db := &database{files: map[string]int{}}
	var errs []error
	for _, l := range layers {
		t := newTable()
		for _, s := range l.sources {
			f, err := os.Open(s.path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
			n, err := s.parse(f, t)
			f.Close()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", s.path, err))
				continue
			}
			db.files[s.path] = n
		}
		if t.len() > 0 {
			db.layers = append(db.layers, loadedLayer{name: l.name, t: t})
		}
	}
	return db, errors.Join(errs...)
}

// signature identifies the current state of all source files (path, size, mtime).
func signature(layers []layer) string {
	var b strings.Builder
	for _, l := range layers {
		for _, s := range l.sources {
			fi, err := os.Stat(s.path)
			if err != nil {
				continue
			}
			fmt.Fprintf(&b, "%s|%d|%d\n", s.path, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	return b.String()
}
