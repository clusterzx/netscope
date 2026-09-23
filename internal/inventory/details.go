package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// PortView is a port row (current or historic).
type PortView struct {
	ID        int64      `json:"id"`
	IP        string     `json:"ip"`
	Proto     string     `json:"proto"`
	Port      int        `json:"port"`
	State     string     `json:"state"`
	Service   string     `json:"service"`
	Product   string     `json:"product"`
	Version   string     `json:"version"`
	ExtraInfo string     `json:"extraInfo"`
	Tunnel    string     `json:"tunnel"`
	CPEs      []string   `json:"cpes"`
	Source    string     `json:"source"`
	FirstSeen time.Time  `json:"firstSeen"`
	LastSeen  time.Time  `json:"lastSeen"`
	GoneAt    *time.Time `json:"goneAt,omitempty"`
}

func activeFilter(history bool) string {
	if history {
		return ""
	}
	return " AND gone_at IS NULL"
}

// Ports returns the ports of a device (with history: also closed/changed rows).
func (s *Store) Ports(ctx context.Context, id int64, history bool) ([]PortView, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, ip, proto, port, state, service, product, version, extra_info, tunnel, cpes, source,
		first_seen, last_seen, gone_at FROM ports WHERE device_id = ?`+activeFilter(history)+` ORDER BY gone_at IS NOT NULL, proto, port, first_seen DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PortView{}
	for rows.Next() {
		var (
			p           PortView
			cpes        string
			first, last int64
			gone        sql.NullInt64
		)
		if err := rows.Scan(&p.ID, &p.IP, &p.Proto, &p.Port, &p.State, &p.Service, &p.Product, &p.Version, &p.ExtraInfo, &p.Tunnel, &cpes,
			&p.Source, &first, &last, &gone); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(cpes), &p.CPEs)
		if p.CPEs == nil {
			p.CPEs = []string{}
		}
		p.FirstSeen, p.LastSeen, p.GoneAt = db.Time(first), db.Time(last), db.NullTime(gone)
		out = append(out, p)
	}
	return out, rows.Err()
}

// CertView is a certificate row.
type CertView struct {
	ID         int64  `json:"id"`
	DeviceID   int64  `json:"deviceId"`
	DeviceName string `json:"deviceName,omitempty"`
	plugin.TLSCert
	IP        string     `json:"ip"`
	DaysLeft  int        `json:"daysLeft"`
	Source    string     `json:"source"`
	FirstSeen time.Time  `json:"firstSeen"`
	LastSeen  time.Time  `json:"lastSeen"`
	GoneAt    *time.Time `json:"goneAt,omitempty"`
}

const certSelect = `SELECT c.id, c.device_id, c.ip, c.port, c.server_name, c.fingerprint, c.subject_cn, c.sans, c.issuer, c.issuer_cn, c.serial,
	c.not_before, c.not_after, c.self_signed, c.chain_valid, c.chain_error, c.key_type, c.key_bits, c.signature_alg, c.tls_versions,
	c.cipher, c.weak_protocols, c.weak_ciphers, c.source, c.first_seen, c.last_seen, c.gone_at,
	COALESCE(NULLIF(d.display_name, ''), NULLIF(d.hostname, ''), d.primary_ip, '') FROM certificates c JOIN devices d ON d.id = c.device_id`

func scanCerts(rows *sql.Rows) ([]CertView, error) {
	defer rows.Close()
	out := []CertView{}
	now := time.Now()
	for rows.Next() {
		var (
			c                   CertView
			sans, vers, wp, wc  string
			nb, na, first, last int64
			gone                sql.NullInt64
		)
		if err := rows.Scan(&c.ID, &c.DeviceID, &c.IP, &c.Port, &c.ServerName, &c.Fingerprint, &c.SubjectCN, &sans, &c.Issuer, &c.IssuerCN,
			&c.Serial, &nb, &na, &c.SelfSigned, &c.ChainValid, &c.ChainError, &c.KeyType, &c.KeyBits, &c.SignatureAlg, &vers, &c.Cipher,
			&wp, &wc, &c.Source, &first, &last, &gone, &c.DeviceName); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(sans), &c.SANs)
		_ = json.Unmarshal([]byte(vers), &c.Versions)
		_ = json.Unmarshal([]byte(wp), &c.WeakProtocols)
		_ = json.Unmarshal([]byte(wc), &c.WeakCiphers)
		c.NotBefore, c.NotAfter = db.Time(nb), db.Time(na)
		c.FirstSeen, c.LastSeen, c.GoneAt = db.Time(first), db.Time(last), db.NullTime(gone)
		c.DaysLeft = int(c.NotAfter.Sub(now).Hours() / 24)
		if c.NotAfter.Before(now) {
			c.DaysLeft = -int(now.Sub(c.NotAfter).Hours()/24) - 1
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Certificates returns the certificates of a device.
func (s *Store) Certificates(ctx context.Context, id int64, history bool) ([]CertView, error) {
	filter := ""
	if !history {
		filter = " AND c.gone_at IS NULL"
	}
	rows, err := s.db.R.QueryContext(ctx, certSelect+" WHERE c.device_id = ?"+filter+" ORDER BY c.gone_at IS NOT NULL, c.port, c.first_seen DESC", id)
	if err != nil {
		return nil, err
	}
	return scanCerts(rows)
}

// AllCertificates returns all active certificates ordered by expiry.
func (s *Store) AllCertificates(ctx context.Context, withinDays int) ([]CertView, error) {
	q := certSelect + " WHERE c.gone_at IS NULL AND d.state <> 'ignored'"
	var args []any
	if withinDays > 0 {
		q += " AND c.not_after < ?"
		args = append(args, time.Now().Add(time.Duration(withinDays)*24*time.Hour).UnixMilli())
	}
	rows, err := s.db.R.QueryContext(ctx, q+" ORDER BY c.not_after", args...)
	if err != nil {
		return nil, err
	}
	return scanCerts(rows)
}

// HTTPView is an HTTP endpoint.
type HTTPView struct {
	ID int64 `json:"id"`
	plugin.HTTPService
	IP        string    `json:"ip"`
	Source    string    `json:"source"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// HTTPServices returns the active HTTP endpoints of a device.
func (s *Store) HTTPServices(ctx context.Context, id int64) ([]HTTPView, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, ip, port, scheme, url, status_code, title, server, content_type, redirects, final_url,
		favicon_hash, favicon_md5, apps, headers, source, first_seen, last_seen FROM http_services WHERE device_id = ? AND gone_at IS NULL ORDER BY port`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HTTPView{}
	for rows.Next() {
		var (
			h                        HTTPView
			redirects, apps, headers string
			fav                      sql.NullInt64
			first, last              int64
		)
		if err := rows.Scan(&h.ID, &h.IP, &h.Port, &h.Scheme, &h.URL, &h.StatusCode, &h.Title, &h.Server, &h.ContentType, &redirects,
			&h.FinalURL, &fav, &h.FaviconMD5, &apps, &headers, &h.Source, &first, &last); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(redirects), &h.Redirects)
		_ = json.Unmarshal([]byte(apps), &h.Apps)
		_ = json.Unmarshal([]byte(headers), &h.Headers)
		if fav.Valid {
			v := int32(fav.Int64)
			h.FaviconHash = &v
		}
		h.FirstSeen, h.LastSeen = db.Time(first), db.Time(last)
		out = append(out, h)
	}
	return out, rows.Err()
}

// PackageView is an installed package.
type PackageView struct {
	Manager   string    `json:"manager"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Arch      string    `json:"arch"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// PackageList is a page of packages.
type PackageList struct {
	Total int           `json:"total"`
	Items []PackageView `json:"items"`
}

// Packages returns installed packages of a device (paged, filtered by name).
func (s *Store) Packages(ctx context.Context, id int64, query string, limit, offset int) (*PackageList, error) {
	where := "device_id = ? AND gone_at IS NULL"
	args := []any{id}
	if q := strings.TrimSpace(query); q != "" {
		where += " AND name LIKE ? ESCAPE '\\'"
		args = append(args, likePattern(q, true))
	}
	var total int
	if err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM packages WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 5000 {
		limit = 200
	}
	rows, err := s.db.R.QueryContext(ctx, "SELECT manager, name, version, arch, first_seen, last_seen FROM packages WHERE "+where+
		" ORDER BY name LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &PackageList{Total: total, Items: []PackageView{}}
	for rows.Next() {
		var (
			p           PackageView
			first, last int64
		)
		if err := rows.Scan(&p.Manager, &p.Name, &p.Version, &p.Arch, &first, &last); err != nil {
			return nil, err
		}
		p.FirstSeen, p.LastSeen = db.Time(first), db.Time(last)
		out.Items = append(out.Items, p)
	}
	return out, rows.Err()
}

// ContainerView is a container of a host.
type ContainerView struct {
	RowID int64 `json:"rowId"`
	plugin.Container
	Engine    string     `json:"engine"`
	Source    string     `json:"source"`
	FirstSeen time.Time  `json:"firstSeen"`
	LastSeen  time.Time  `json:"lastSeen"`
	GoneAt    *time.Time `json:"goneAt,omitempty"`
}

// ImageView is a container image of a host.
type ImageView struct {
	plugin.ContainerImage
	Engine    string    `json:"engine"`
	InUse     int       `json:"inUse"`
	FirstSeen time.Time `json:"firstSeen"`
}

// ContainerData bundles containers and images.
type ContainerData struct {
	Containers []ContainerView `json:"containers"`
	Images     []ImageView     `json:"images"`
}

// Containers returns containers (current, plus removed ones with history) and images.
func (s *Store) Containers(ctx context.Context, id int64, history bool) (*ContainerData, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, engine, container_id, name, image, image_id, state, status, ports, networks,
		compose_project, compose_service, labels, IFNULL(created, 0), source, first_seen, last_seen, gone_at FROM containers
		WHERE device_id = ?`+activeFilter(history)+` ORDER BY gone_at IS NOT NULL, compose_project, name`, id)
	if err != nil {
		return nil, err
	}
	out := &ContainerData{Containers: []ContainerView{}, Images: []ImageView{}}
	inUse := map[string]int{}
	for rows.Next() {
		var (
			c                       ContainerView
			ports, networks, labels string
			created, first, last    int64
			gone                    sql.NullInt64
		)
		if err := rows.Scan(&c.RowID, &c.Engine, &c.Container.ID, &c.Name, &c.Image, &c.ImageID, &c.State, &c.Status, &ports, &networks,
			&c.ComposeProject, &c.ComposeService, &labels, &created, &c.Source, &first, &last, &gone); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal([]byte(ports), &c.Ports)
		_ = json.Unmarshal([]byte(networks), &c.Networks)
		_ = json.Unmarshal([]byte(labels), &c.Labels)
		c.Created = db.Time(created)
		c.FirstSeen, c.LastSeen, c.GoneAt = db.Time(first), db.Time(last), db.NullTime(gone)
		if c.GoneAt == nil {
			inUse[c.ImageID]++
		}
		out.Containers = append(out.Containers, c)
	}
	rows.Close()
	irows, err := s.db.R.QueryContext(ctx, `SELECT engine, image_id, tags, size, IFNULL(created, 0), first_seen FROM container_images
		WHERE device_id = ? AND gone_at IS NULL ORDER BY size DESC`, id)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	for irows.Next() {
		var (
			im             ImageView
			tags           string
			created, first int64
		)
		if err := irows.Scan(&im.Engine, &im.ID, &tags, &im.Size, &created, &first); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tags), &im.Tags)
		im.Created, im.FirstSeen, im.InUse = db.Time(created), db.Time(first), inUse[im.ID]
		out.Images = append(out.Images, im)
	}
	return out, irows.Err()
}

// ObservationView is a stored raw observation.
type ObservationView struct {
	ID       int64     `json:"id"`
	RunID    int64     `json:"runId,omitempty"`
	PluginID string    `json:"pluginId"`
	Target   string    `json:"target"`
	TS       time.Time `json:"ts"`
	Data     any       `json:"data"`
	Raw      string    `json:"raw,omitempty"`
}

// Observations returns the latest observation per plugin (plugin = "") or the recent
// observations of one plugin.
func (s *Store) Observations(ctx context.Context, id int64, pluginID string, limit int) ([]ObservationView, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	if pluginID == "" {
		rows, err = s.db.R.QueryContext(ctx, `SELECT o.id, IFNULL(o.run_id, 0), o.plugin_id, o.target, o.ts, o.data, IFNULL(o.raw, '')
			FROM observations o WHERE o.id IN (SELECT MAX(id) FROM observations WHERE device_id = ? GROUP BY plugin_id) ORDER BY o.plugin_id`, id)
	} else {
		rows, err = s.db.R.QueryContext(ctx, `SELECT id, IFNULL(run_id, 0), plugin_id, target, ts, data, IFNULL(raw, '') FROM observations
			WHERE device_id = ? AND plugin_id = ? ORDER BY id DESC LIMIT ?`, id, pluginID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ObservationView{}
	for rows.Next() {
		var (
			o    ObservationView
			ts   int64
			data string
		)
		if err := rows.Scan(&o.ID, &o.RunID, &o.PluginID, &o.Target, &ts, &data, &o.Raw); err != nil {
			return nil, err
		}
		o.TS = db.Time(ts)
		var v any
		if json.Unmarshal([]byte(data), &v) == nil {
			o.Data = v
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
