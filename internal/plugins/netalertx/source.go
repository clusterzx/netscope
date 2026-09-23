package netalertx

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	_ "netscope/internal/db" // registers the "sqlite" driver
)

// columnAliases maps the column names of the current NetAlertX schema (camelCase) and of
// the older Pi.Alert/NetAlertX schema (dev_ prefix) to canonical field names. Keys are
// lower-case.
var columnAliases = map[string]string{
	"devmac":                    "mac",
	"dev_mac":                   "mac",
	"devname":                   "name",
	"dev_name":                  "name",
	"devowner":                  "owner",
	"dev_owner":                 "owner",
	"devtype":                   "type",
	"dev_devicetype":            "type",
	"devvendor":                 "vendor",
	"dev_vendor":                "vendor",
	"devfavorite":               "favorite",
	"dev_favorite":              "favorite",
	"devgroup":                  "group",
	"dev_group":                 "group",
	"devcomments":               "comments",
	"dev_comments":              "comments",
	"devfirstconnection":        "first_connection",
	"dev_firstconnection":       "first_connection",
	"devlastconnection":         "last_connection",
	"dev_lastconnection":        "last_connection",
	"devlastip":                 "last_ip",
	"dev_lastip":                "last_ip",
	"devprimaryipv4":            "ipv4",
	"devprimaryipv6":            "ipv6",
	"devstaticip":               "static_ip",
	"dev_staticip":              "static_ip",
	"devisnew":                  "new",
	"dev_newdevice":             "new",
	"devlocation":               "location",
	"dev_location":              "location",
	"devisarchived":             "archived",
	"dev_archived":              "archived",
	"devparentmac":              "parent_mac",
	"dev_network_node_mac_addr": "parent_mac",
	"devparentport":             "parent_port",
	"dev_network_node_port":     "parent_port",
	"devparentreltype":          "parent_rel_type",
	"devpresentlastscan":        "present_last_scan",
	"dev_presentlastscan":       "present_last_scan",
	"devalertdown":              "alert_down",
	"dev_alertdevicedown":       "alert_down",
	"devssid":                   "ssid",
	"dev_ssid":                  "ssid",
	"devsite":                   "site",
	"dev_networksite":           "site",
	"devvlan":                   "vlan",
	"devfqdn":                   "fqdn",
	"devguid":                   "guid",
	"dev_guid":                  "guid",
	"devsourceplugin":           "source_plugin",
	"devsynchubnode":            "sync_hub_node",
	"dev_synchubnodename":       "sync_hub_node",
}

// device is one NetAlertX device row with canonical field names.
type device struct {
	Line   int // CSV line or row number, for log messages
	Fields map[string]string
}

// get returns a field with NetAlertX NULL markers ("None" from CSV exports) removed.
func (d device) get(field string) string {
	v := strings.TrimSpace(d.Fields[field])
	if v == "None" || v == "NULL" || v == "null" {
		return ""
	}
	return v
}

// flag interprets NetAlertX booleans (0/1, also true/false).
func (d device) flag(field string) bool {
	switch strings.ToLower(d.get(field)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// dataset is the content of an export.
type dataset struct {
	Format  string // sqlite | csv
	Schema  string // netalertx | pialert
	UTC     bool   // timestamps are stored in UTC
	Devices []device
}

// sqliteMagic starts every SQLite 3 database file.
var sqliteMagic = []byte("SQLite format 3\x00")

// detectFormat inspects the first bytes of the file.
func detectFormat(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, len(sqliteMagic))
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	if n == len(sqliteMagic) && bytes.Equal(head, sqliteMagic) {
		return "sqlite", nil
	}
	return "csv", nil
}

// sqliteDSN opens path read-only and immutable (no locks, no journal, no WAL writes).
func sqliteDSN(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // windows drive paths: file:/C:/x.db
	}
	p = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(p)
	return "file:" + p + "?mode=ro&immutable=1", nil
}

// readSQLite reads the Devices table of a NetAlertX or Pi.Alert database.
func readSQLite(ctx context.Context, path string) (*dataset, error) {
	dsn, err := sqliteDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('Devices')`)
	if err != nil {
		return nil, fmt.Errorf("Datenbank nicht lesbar: %w", err)
	}
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return nil, err
		}
		cols = append(cols, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Datenbank nicht lesbar: %w", err)
	}
	if len(cols) == 0 {
		return nil, errors.New("keine NetAlertX-Datenbank: Tabelle Devices fehlt")
	}
	ds := &dataset{Format: "sqlite", Schema: schemaOf(cols)}
	if ds.Schema == "" {
		return nil, errors.New("unbekanntes Schema: Tabelle Devices hat keine Spalte devMac/dev_MAC")
	}
	var (
		selected []string
		fields   []string
	)
	for _, c := range cols {
		if f, ok := columnAliases[strings.ToLower(c)]; ok {
			// CAST keeps the stored text (the driver would convert DATETIME columns)
			selected = append(selected, `CAST("`+strings.ReplaceAll(c, `"`, `""`)+`" AS TEXT)`)
			fields = append(fields, f)
		}
	}
	rows, err = db.QueryContext(ctx, "SELECT "+strings.Join(selected, ", ")+" FROM Devices ORDER BY rowid")
	if err != nil {
		return nil, fmt.Errorf("Geräte nicht lesbar: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
		vals := make([]sql.NullString, len(fields))
		ptrs := make([]any, len(fields))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		d := device{Line: n, Fields: map[string]string{}}
		for i, f := range fields {
			if vals[i].Valid {
				d.Fields[f] = vals[i].String
			}
		}
		ds.Devices = append(ds.Devices, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Geräte nicht lesbar: %w", err)
	}
	rows.Close() // release the single connection for the settings query
	ds.UTC = timestampsUTC(ctx, db)
	return ds, nil
}

func schemaOf(cols []string) string {
	for _, c := range cols {
		switch strings.ToLower(c) {
		case "devmac":
			return "netalertx"
		case "dev_mac":
			return "pialert"
		}
	}
	return ""
}

// timestampsUTC reports whether the database stores UTC timestamps: NetAlertX sets
// DB_TIMESTAMPS_UTC_MIGRATED after converting, and versions after 26.2.6 write UTC.
func timestampsUTC(ctx context.Context, db *sql.DB) bool {
	rows, err := db.QueryContext(ctx, `SELECT setKey, setValue FROM Settings WHERE setKey IN ('DB_TIMESTAMPS_UTC_MIGRATED', 'VERSION')`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var key, value sql.NullString
		if rows.Scan(&key, &value) != nil {
			continue
		}
		switch key.String {
		case "DB_TIMESTAMPS_UTC_MIGRATED":
			if strings.TrimSpace(value.String) == "1" {
				return true
			}
		case "VERSION":
			if versionAfter(value.String, [3]int{26, 2, 6}) {
				return true
			}
		}
	}
	return false
}

// versionAfter compares "v26.3.15" style versions.
func versionAfter(v string, ref [3]int) bool {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) < 2 {
		return false
	}
	var got [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return false
		}
		got[i] = n
	}
	for i := range got {
		if got[i] != ref[i] {
			return got[i] > ref[i]
		}
	}
	return false
}

// readCSV reads a NetAlertX CSV export (CSV Backup plugin: header with column names,
// NULL written as "None").
func readCSV(path string) (*dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return nil, errors.New("CSV-Datei ist nicht UTF-8-kodiert")
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = sniffDelimiter(data)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("CSV-Kopfzeile nicht lesbar: %w", err)
	}
	fields := make([]string, len(header))
	for i, h := range header {
		header[i] = strings.Trim(strings.TrimSpace(h), `"`)
		fields[i] = columnAliases[strings.ToLower(header[i])]
	}
	ds := &dataset{Format: "csv", Schema: schemaOf(header)}
	if ds.Schema == "" {
		return nil, errors.New("keine NetAlertX-CSV: Spalte devMac bzw. dev_MAC fehlt")
	}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line, _ := r.FieldPos(0)
		if err != nil {
			return nil, fmt.Errorf("CSV Zeile %d: %w", line, err)
		}
		d := device{Line: line, Fields: map[string]string{}}
		empty := true
		for i, v := range rec {
			if i < len(fields) && fields[i] != "" {
				d.Fields[fields[i]] = v
				if strings.TrimSpace(v) != "" {
					empty = false
				}
			}
		}
		if !empty {
			ds.Devices = append(ds.Devices, d)
		}
	}
	return ds, nil
}

// sniffDelimiter picks comma, semicolon or tab by counting them in the header line.
func sniffDelimiter(data []byte) rune {
	line := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		line = data[:i]
	}
	best, count := ',', bytes.Count(line, []byte{','})
	for _, d := range []rune{';', '\t'} {
		if n := bytes.Count(line, []byte(string(d))); n > count {
			best, count = d, n
		}
	}
	return best
}
