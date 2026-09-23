package inventory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"netscope/internal/db"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

// scanIP returns the address a scan section belongs to.
func (g *ingest) scanIP() (string, error) {
	if g.obs.IP != "" {
		return g.obs.IP, nil
	}
	var ip string
	err := g.tx.QueryRowContext(g.ctx, "SELECT primary_ip FROM devices WHERE id = ?", g.devID).Scan(&ip)
	if err == nil && ip == "" {
		err = fmt.Errorf("Gerät %d hat keine IP-Adresse", g.devID)
	}
	return ip, err
}

func (g *ingest) hasHistory(table, where string, args ...any) (bool, error) {
	var n int
	err := g.tx.QueryRowContext(g.ctx, "SELECT EXISTS (SELECT 1 FROM "+table+" WHERE "+where+")", args...).Scan(&n)
	return n == 1, err
}

// ---------------------------------------------------------------- ports

type portRow struct {
	id int64
	p  plugin.Port
}

func portKey(ip, proto string, port int) string { return fmt.Sprintf("%s %s/%d", ip, proto, port) }

func (g *ingest) applyPorts() error {
	scan := g.obs.Ports
	if scan == nil {
		return nil
	}
	proto := strings.ToLower(scan.Protocol)
	if proto != "tcp" && proto != "udp" {
		return fmt.Errorf("ungültiges Protokoll %q", scan.Protocol)
	}
	ip, err := g.scanIP()
	if err != nil {
		return err
	}
	hadPorts, err := g.hasHistory("ports", "device_id = ? AND proto = ?", g.devID, proto)
	if err != nil {
		return err
	}
	initial := !hadPorts
	rows, err := g.tx.QueryContext(g.ctx, `SELECT id, port, state, service, product, version, extra_info, tunnel, cpes
		FROM ports WHERE device_id = ? AND ip = ? AND proto = ? AND gone_at IS NULL`, g.devID, ip, proto)
	if err != nil {
		return err
	}
	current := map[int]portRow{}
	for rows.Next() {
		var (
			r    portRow
			cpes string
		)
		if err := rows.Scan(&r.id, &r.p.Port, &r.p.State, &r.p.Service, &r.p.Product, &r.p.Version, &r.p.ExtraInfo, &r.p.Tunnel, &cpes); err != nil {
			rows.Close()
			return err
		}
		r.p.Proto = proto
		_ = json.Unmarshal([]byte(cpes), &r.p.CPEs)
		current[r.p.Port] = r
	}
	rows.Close()
	now := g.nowMs()
	seen := map[int]bool{}
	for _, p := range scan.Ports {
		if p.Port < 1 || p.Port > 65535 || seen[p.Port] {
			continue
		}
		if p.Proto != "" && strings.ToLower(p.Proto) != proto {
			continue
		}
		seen[p.Port] = true
		p.Proto = proto
		if p.State == "" {
			p.State = "open"
		}
		if p.CPEs == nil {
			p.CPEs = []string{}
		}
		cur, exists := current[p.Port]
		key := portKey(ip, proto, p.Port)
		switch {
		case !exists:
			if err := g.insertPort(ip, p, now); err != nil {
				return err
			}
			np := p
			g.change(plugin.ChangePortOpened, key, nil, &np, initial)
		case p.Product != "" && (p.Product != cur.p.Product || p.Version != cur.p.Version ||
			(p.Service != "" && p.Service != cur.p.Service)):
			// service identity changed: keep history
			if err := g.exec("UPDATE ports SET gone_at = ? WHERE id = ?", now, cur.id); err != nil {
				return err
			}
			if err := g.insertPort(ip, p, now); err != nil {
				return err
			}
			op, np := cur.p, p
			g.change(plugin.ChangePortChanged, key, &op, &np, false)
		default:
			// same service (or no service detection this time): refresh without losing data
			service, product, version, extra, cpes := cur.p.Service, cur.p.Product, cur.p.Version, cur.p.ExtraInfo, cur.p.CPEs
			if p.Service != "" {
				service = p.Service
			}
			if p.Product != "" {
				extra = p.ExtraInfo
				if len(p.CPEs) > 0 {
					cpes = p.CPEs
				}
			}
			tunnel := cur.p.Tunnel
			if p.Tunnel != "" {
				tunnel = p.Tunnel
			}
			if cpes == nil {
				cpes = []string{}
			}
			if err := g.exec(`UPDATE ports SET state = ?, service = ?, product = ?, version = ?, extra_info = ?, tunnel = ?, cpes = ?,
				source = ?, run_id = ?, last_seen = ? WHERE id = ?`, p.State, service, product, version, extra, tunnel,
				db.JSON(cpes), g.plugin, g.runID(), now, cur.id); err != nil {
				return err
			}
		}
	}
	if len(scan.Scanned) == 0 {
		return nil
	}
	for port, cur := range current {
		if seen[port] || !plugin.ContainsPort(scan.Scanned, port) {
			continue
		}
		if err := g.exec("UPDATE ports SET gone_at = ? WHERE id = ?", now, cur.id); err != nil {
			return err
		}
		op := cur.p
		g.change(plugin.ChangePortClosed, portKey(ip, proto, port), &op, nil, false)
	}
	return nil
}

func (g *ingest) insertPort(ip string, p plugin.Port, now int64) error {
	return g.exec(`INSERT INTO ports(device_id, ip, proto, port, state, service, product, version, extra_info, tunnel, cpes,
		source, run_id, first_seen, last_seen) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.devID, ip, p.Proto, p.Port, p.State, p.Service, p.Product, p.Version, p.ExtraInfo, p.Tunnel, db.JSON(p.CPEs),
		g.plugin, g.runID(), now, now)
}

// ---------------------------------------------------------------- http

func (g *ingest) applyHTTP() error {
	scan := g.obs.HTTP
	if scan == nil {
		return nil
	}
	ip, err := g.scanIP()
	if err != nil {
		return err
	}
	now := g.nowMs()
	seen := map[int]bool{}
	for _, h := range scan.Services {
		if h.Port < 1 || seen[h.Port] {
			continue
		}
		seen[h.Port] = true
		var fav any
		if h.FaviconHash != nil {
			fav = int64(*h.FaviconHash)
		}
		apps := h.Apps
		if apps == nil {
			apps = []plugin.DetectedApp{}
		}
		redirects := h.Redirects
		if redirects == nil {
			redirects = []string{}
		}
		headers := h.Headers
		if headers == nil {
			headers = map[string]string{}
		}
		var id int64
		err := g.tx.QueryRowContext(g.ctx, "SELECT id FROM http_services WHERE device_id = ? AND ip = ? AND port = ? AND gone_at IS NULL",
			g.devID, ip, h.Port).Scan(&id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			err = g.exec(`INSERT INTO http_services(device_id, ip, port, scheme, url, status_code, title, server, content_type, redirects,
				final_url, favicon_hash, favicon_md5, apps, headers, source, run_id, first_seen, last_seen)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, g.devID, ip, h.Port, h.Scheme, h.URL, h.StatusCode, h.Title, h.Server,
				h.ContentType, db.JSON(redirects), h.FinalURL, fav, h.FaviconMD5, db.JSON(apps), db.JSON(headers), g.plugin, g.runID(), now, now)
		case err == nil:
			err = g.exec(`UPDATE http_services SET scheme = ?, url = ?, status_code = ?, title = ?, server = ?, content_type = ?, redirects = ?,
				final_url = ?, favicon_hash = ?, favicon_md5 = ?, apps = ?, headers = ?, source = ?, run_id = ?, last_seen = ? WHERE id = ?`,
				h.Scheme, h.URL, h.StatusCode, h.Title, h.Server, h.ContentType, db.JSON(redirects), h.FinalURL, fav, h.FaviconMD5,
				db.JSON(apps), db.JSON(headers), g.plugin, g.runID(), now, id)
		}
		if err != nil {
			return err
		}
	}
	for _, port := range scan.Scanned {
		if seen[port] {
			continue
		}
		if err := g.exec("UPDATE http_services SET gone_at = ? WHERE device_id = ? AND ip = ? AND port = ? AND gone_at IS NULL",
			now, g.devID, ip, port); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- tls

func (g *ingest) loadCert(id int64) (*plugin.TLSCert, error) {
	var (
		c                      plugin.TLSCert
		sans, vers, wp, wc     string
		nb, na                 int64
		selfSigned, chainValid bool
	)
	err := g.tx.QueryRowContext(g.ctx, `SELECT port, server_name, fingerprint, subject_cn, sans, issuer, issuer_cn, serial, not_before, not_after,
		self_signed, chain_valid, chain_error, key_type, key_bits, signature_alg, tls_versions, cipher, weak_protocols, weak_ciphers
		FROM certificates WHERE id = ?`, id).Scan(&c.Port, &c.ServerName, &c.Fingerprint, &c.SubjectCN, &sans, &c.Issuer, &c.IssuerCN,
		&c.Serial, &nb, &na, &selfSigned, &chainValid, &c.ChainError, &c.KeyType, &c.KeyBits, &c.SignatureAlg, &vers, &c.Cipher, &wp, &wc)
	if err != nil {
		return nil, err
	}
	c.NotBefore, c.NotAfter, c.SelfSigned, c.ChainValid = db.Time(nb), db.Time(na), selfSigned, chainValid
	_ = json.Unmarshal([]byte(sans), &c.SANs)
	_ = json.Unmarshal([]byte(vers), &c.Versions)
	_ = json.Unmarshal([]byte(wp), &c.WeakProtocols)
	_ = json.Unmarshal([]byte(wc), &c.WeakCiphers)
	return &c, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (g *ingest) insertCert(ip string, c plugin.TLSCert, now int64) error {
	return g.exec(`INSERT INTO certificates(device_id, ip, port, server_name, fingerprint, subject_cn, sans, issuer, issuer_cn, serial,
		not_before, not_after, self_signed, chain_valid, chain_error, key_type, key_bits, signature_alg, tls_versions, cipher,
		weak_protocols, weak_ciphers, source, run_id, first_seen, last_seen) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.devID, ip, c.Port, c.ServerName, c.Fingerprint, c.SubjectCN, db.JSON(nonNil(c.SANs)), c.Issuer, c.IssuerCN, c.Serial,
		c.NotBefore.UnixMilli(), c.NotAfter.UnixMilli(), db.Bool(c.SelfSigned), db.Bool(c.ChainValid), c.ChainError, c.KeyType,
		c.KeyBits, c.SignatureAlg, db.JSON(nonNil(c.Versions)), c.Cipher, db.JSON(nonNil(c.WeakProtocols)), db.JSON(nonNil(c.WeakCiphers)),
		g.plugin, g.runID(), now, now)
}

func (g *ingest) applyTLS() error {
	scan := g.obs.TLS
	if scan == nil {
		return nil
	}
	ip, err := g.scanIP()
	if err != nil {
		return err
	}
	hadCerts, err := g.hasHistory("certificates", "device_id = ?", g.devID)
	if err != nil {
		return err
	}
	now := g.nowMs()
	seen := map[int]bool{}
	for _, c := range scan.Certs {
		if c.Port < 1 || c.Fingerprint == "" || seen[c.Port] {
			continue
		}
		seen[c.Port] = true
		key := fmt.Sprintf("%s:%d", ip, c.Port)
		var (
			id int64
			fp string
		)
		err := g.tx.QueryRowContext(g.ctx, "SELECT id, fingerprint FROM certificates WHERE device_id = ? AND ip = ? AND port = ? AND gone_at IS NULL",
			g.devID, ip, c.Port).Scan(&id, &fp)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if err := g.insertCert(ip, c, now); err != nil {
				return err
			}
			nc := c
			g.change(plugin.ChangeCertAdded, key, nil, &nc, !hadCerts)
		case err != nil:
			return err
		case fp != c.Fingerprint:
			old, err := g.loadCert(id)
			if err != nil {
				return err
			}
			if err := g.exec("UPDATE certificates SET gone_at = ? WHERE id = ?", now, id); err != nil {
				return err
			}
			if err := g.insertCert(ip, c, now); err != nil {
				return err
			}
			nc := c
			g.change(plugin.ChangeCertChanged, key, old, &nc, false)
		default:
			if err := g.exec(`UPDATE certificates SET server_name = ?, chain_valid = ?, chain_error = ?, tls_versions = ?, cipher = ?,
				weak_protocols = ?, weak_ciphers = ?, source = ?, run_id = ?, last_seen = ? WHERE id = ?`, c.ServerName, db.Bool(c.ChainValid),
				c.ChainError, db.JSON(nonNil(c.Versions)), c.Cipher, db.JSON(nonNil(c.WeakProtocols)), db.JSON(nonNil(c.WeakCiphers)),
				g.plugin, g.runID(), now, id); err != nil {
				return err
			}
		}
	}
	for _, port := range scan.Scanned {
		if seen[port] {
			continue
		}
		var id int64
		err := g.tx.QueryRowContext(g.ctx, "SELECT id FROM certificates WHERE device_id = ? AND ip = ? AND port = ? AND gone_at IS NULL",
			g.devID, ip, port).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		old, err := g.loadCert(id)
		if err != nil {
			return err
		}
		if err := g.exec("UPDATE certificates SET gone_at = ? WHERE id = ?", now, id); err != nil {
			return err
		}
		g.change(plugin.ChangeCertRemoved, fmt.Sprintf("%s:%d", ip, port), old, nil, false)
	}
	return nil
}

// ---------------------------------------------------------------- packages

func (g *ingest) applyPackages() error {
	inv := g.obs.Packages
	if inv == nil || inv.Manager == "" {
		return nil
	}
	had, err := g.hasHistory("packages", "device_id = ? AND manager = ?", g.devID, inv.Manager)
	if err != nil {
		return err
	}
	type cur struct {
		id      int64
		version string
	}
	rows, err := g.tx.QueryContext(g.ctx, "SELECT id, name, arch, version FROM packages WHERE device_id = ? AND manager = ? AND gone_at IS NULL",
		g.devID, inv.Manager)
	if err != nil {
		return err
	}
	current := map[string]cur{}
	for rows.Next() {
		var (
			c          cur
			name, arch string
		)
		if err := rows.Scan(&c.id, &name, &arch, &c.version); err != nil {
			rows.Close()
			return err
		}
		current[name+"\x00"+arch] = c
	}
	rows.Close()
	now := g.nowMs()
	delta := &plugin.PackageDelta{Manager: inv.Manager}
	seen := map[string]bool{}
	for _, p := range inv.Packages {
		if p.Name == "" {
			continue
		}
		k := p.Name + "\x00" + p.Arch
		if seen[k] {
			continue
		}
		seen[k] = true
		c, exists := current[k]
		switch {
		case !exists:
			if err := g.exec(`INSERT INTO packages(device_id, manager, name, version, arch, source, run_id, first_seen, last_seen)
				VALUES (?,?,?,?,?,?,?,?,?)`, g.devID, inv.Manager, p.Name, p.Version, p.Arch, g.plugin, g.runID(), now, now); err != nil {
				return err
			}
			delta.Added = append(delta.Added, p)
		case c.version != p.Version:
			if err := g.exec("UPDATE packages SET gone_at = ? WHERE id = ?", now, c.id); err != nil {
				return err
			}
			if err := g.exec(`INSERT INTO packages(device_id, manager, name, version, arch, source, run_id, first_seen, last_seen)
				VALUES (?,?,?,?,?,?,?,?,?)`, g.devID, inv.Manager, p.Name, p.Version, p.Arch, g.plugin, g.runID(), now, now); err != nil {
				return err
			}
			delta.Updated = append(delta.Updated, plugin.PackageUpdate{Name: p.Name, Arch: p.Arch, From: c.version, To: p.Version})
		default:
			if err := g.exec("UPDATE packages SET last_seen = ?, run_id = ? WHERE id = ?", now, g.runID(), c.id); err != nil {
				return err
			}
		}
	}
	for k, c := range current {
		if seen[k] {
			continue
		}
		name, arch, _ := strings.Cut(k, "\x00")
		if err := g.exec("UPDATE packages SET gone_at = ? WHERE id = ?", now, c.id); err != nil {
			return err
		}
		delta.Removed = append(delta.Removed, plugin.Package{Name: name, Version: c.version, Arch: arch})
	}
	if delta.Count() > 0 && had {
		sort.Slice(delta.Added, func(i, j int) bool { return delta.Added[i].Name < delta.Added[j].Name })
		sort.Slice(delta.Removed, func(i, j int) bool { return delta.Removed[i].Name < delta.Removed[j].Name })
		sort.Slice(delta.Updated, func(i, j int) bool { return delta.Updated[i].Name < delta.Updated[j].Name })
		g.change(plugin.ChangePackages, inv.Manager, nil, delta, false)
	}
	return nil
}

// ---------------------------------------------------------------- containers

func (g *ingest) insertContainer(engine string, c plugin.Container, now int64) error {
	var created any
	if !c.Created.IsZero() {
		created = c.Created.UnixMilli()
	}
	ports := c.Ports
	if ports == nil {
		ports = []plugin.ContainerPort{}
	}
	labels := c.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return g.exec(`INSERT INTO containers(device_id, engine, container_id, name, image, image_id, state, status, ports, networks,
		compose_project, compose_service, labels, created, source, run_id, first_seen, last_seen)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, g.devID, engine, c.ID, c.Name, c.Image, c.ImageID, c.State, c.Status,
		db.JSON(ports), db.JSON(nonNil(c.Networks)), c.ComposeProject, c.ComposeService, db.JSON(labels), created, g.plugin, g.runID(), now, now)
}

func (g *ingest) applyContainers() error {
	inv := g.obs.Containers
	if inv == nil {
		return nil
	}
	engine := inv.Engine
	if engine == "" {
		engine = "docker"
	}
	had, err := g.hasHistory("containers", "device_id = ? AND engine = ?", g.devID, engine)
	if err != nil {
		return err
	}
	type cur struct {
		id                  int64
		image, imageID, cid string
		project             string
	}
	rows, err := g.tx.QueryContext(g.ctx, "SELECT id, name, image, image_id, container_id, compose_project FROM containers WHERE device_id = ? AND engine = ? AND gone_at IS NULL",
		g.devID, engine)
	if err != nil {
		return err
	}
	current := map[string]cur{}
	for rows.Next() {
		var (
			c    cur
			name string
		)
		if err := rows.Scan(&c.id, &name, &c.image, &c.imageID, &c.cid, &c.project); err != nil {
			rows.Close()
			return err
		}
		current[name] = c
	}
	rows.Close()
	now := g.nowMs()
	seen := map[string]bool{}
	for _, c := range inv.Containers {
		name := strings.TrimPrefix(c.Name, "/")
		if name == "" || seen[name] {
			continue
		}
		c.Name = name
		seen[name] = true
		old, exists := current[name]
		switch {
		case !exists:
			if err := g.insertContainer(engine, c, now); err != nil {
				return err
			}
			nc := c
			g.change(plugin.ChangeContainerAdded, name, nil, &nc, !had)
		case old.image != c.Image || (c.ImageID != "" && old.imageID != "" && old.imageID != c.ImageID):
			if err := g.exec("UPDATE containers SET gone_at = ? WHERE id = ?", now, old.id); err != nil {
				return err
			}
			if err := g.insertContainer(engine, c, now); err != nil {
				return err
			}
			oc := &plugin.Container{ID: old.cid, Name: name, Image: old.image, ImageID: old.imageID, ComposeProject: old.project}
			nc := c
			g.change(plugin.ChangeContainerImage, name, oc, &nc, false)
		default:
			ports := c.Ports
			if ports == nil {
				ports = []plugin.ContainerPort{}
			}
			labels := c.Labels
			if labels == nil {
				labels = map[string]string{}
			}
			if err := g.exec(`UPDATE containers SET container_id = ?, image_id = ?, state = ?, status = ?, ports = ?, networks = ?,
				compose_project = ?, compose_service = ?, labels = ?, source = ?, run_id = ?, last_seen = ? WHERE id = ?`,
				c.ID, c.ImageID, c.State, c.Status, db.JSON(ports), db.JSON(nonNil(c.Networks)), c.ComposeProject, c.ComposeService,
				db.JSON(labels), g.plugin, g.runID(), now, old.id); err != nil {
				return err
			}
		}
	}
	for name, c := range current {
		if seen[name] {
			continue
		}
		if err := g.exec("UPDATE containers SET gone_at = ? WHERE id = ?", now, c.id); err != nil {
			return err
		}
		g.change(plugin.ChangeContainerRemoved, name, &plugin.Container{ID: c.cid, Name: name, Image: c.image, ImageID: c.imageID,
			ComposeProject: c.project}, nil, false)
	}
	// images
	if inv.Images != nil {
		irows, err := g.tx.QueryContext(g.ctx, "SELECT id, image_id FROM container_images WHERE device_id = ? AND engine = ? AND gone_at IS NULL", g.devID, engine)
		if err != nil {
			return err
		}
		curImg := map[string]int64{}
		for irows.Next() {
			var (
				id  int64
				iid string
			)
			if err := irows.Scan(&id, &iid); err != nil {
				irows.Close()
				return err
			}
			curImg[iid] = id
		}
		irows.Close()
		seenImg := map[string]bool{}
		for _, im := range inv.Images {
			if im.ID == "" || seenImg[im.ID] {
				continue
			}
			seenImg[im.ID] = true
			var created any
			if !im.Created.IsZero() {
				created = im.Created.UnixMilli()
			}
			if id, ok := curImg[im.ID]; ok {
				if err := g.exec("UPDATE container_images SET tags = ?, size = ?, last_seen = ?, run_id = ? WHERE id = ?",
					db.JSON(nonNil(im.Tags)), im.Size, now, g.runID(), id); err != nil {
					return err
				}
				continue
			}
			if err := g.exec(`INSERT INTO container_images(device_id, engine, image_id, tags, size, created, source, run_id, first_seen, last_seen)
				VALUES (?,?,?,?,?,?,?,?,?,?)`, g.devID, engine, im.ID, db.JSON(nonNil(im.Tags)), im.Size, created, g.plugin, g.runID(), now, now); err != nil {
				return err
			}
		}
		for iid, id := range curImg {
			if !seenImg[iid] {
				if err := g.exec("UPDATE container_images SET gone_at = ? WHERE id = ?", now, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- relations

func (g *ingest) resolveRef(r plugin.DeviceRef) (int64, error) {
	var id int64
	var err error
	switch {
	case r.DeviceID > 0:
		err = g.tx.QueryRowContext(g.ctx, "SELECT id FROM devices WHERE id = ?", r.DeviceID).Scan(&id)
	case r.MAC != "":
		mac, ok := netutil.NormalizeMAC(r.MAC)
		if !ok {
			return 0, nil
		}
		err = g.tx.QueryRowContext(g.ctx, "SELECT device_id FROM device_macs WHERE mac = ?", mac).Scan(&id)
	case r.Ref != nil:
		err = g.tx.QueryRowContext(g.ctx, "SELECT device_id FROM external_refs WHERE source = ? AND ref = ?", r.Ref.Source, r.Ref.ID).Scan(&id)
	case r.IP != "":
		id, _, err = ipOwner(g.ctx, g.tx, r.IP)
	default:
		return 0, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (g *ingest) applyRelations() error {
	if g.obs.Relations == nil {
		return nil
	}
	now := g.nowMs()
	type side struct {
		kind   string
		parent bool
	}
	keep := map[side][]int64{}
	reported := map[side]bool{}
	for _, r := range g.obs.Relations {
		if r.Kind == "" {
			continue
		}
		sd := side{r.Kind, r.Parent}
		reported[sd] = true
		other, err := g.resolveRef(r.Other)
		if err != nil {
			return err
		}
		if other == 0 || other == g.devID {
			continue
		}
		parent, child := other, g.devID
		parentPort, childPort := r.RemotePort, r.LocalPort
		if r.Parent {
			parent, child = g.devID, other
			parentPort, childPort = r.LocalPort, r.RemotePort
		}
		if err := g.exec(`INSERT INTO relations(parent_id, child_id, kind, source, parent_port, child_port, label, first_seen, last_seen)
			VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(parent_id, child_id, kind, source) DO UPDATE SET
			parent_port = excluded.parent_port, child_port = excluded.child_port, label = excluded.label, last_seen = excluded.last_seen`,
			parent, child, r.Kind, g.plugin, parentPort, childPort, r.Label, now, now); err != nil {
			return err
		}
		keep[sd] = append(keep[sd], other)
	}
	for sd := range reported {
		col, otherCol := "child_id", "parent_id"
		if sd.parent {
			col, otherCol = "parent_id", "child_id"
		}
		q := fmt.Sprintf("DELETE FROM relations WHERE %s = ? AND kind = ? AND source = ? AND protected = 0", col)
		args := []any{g.devID, sd.kind, g.plugin}
		if ids := keep[sd]; len(ids) > 0 {
			q += fmt.Sprintf(" AND %s NOT IN (%s)", otherCol, db.Placeholders(len(ids)))
			args = append(args, db.Int64Args(ids)...)
		}
		if err := g.exec(q, args...); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- manual data from imports

func (g *ingest) applyManual() error {
	m := g.obs.Manual
	if m == nil {
		return nil
	}
	var cur struct {
		displayName, location, owner, notes, crit, state, custom string
	}
	if err := g.tx.QueryRowContext(g.ctx, "SELECT display_name, location, owner, notes, criticality, state, custom FROM devices WHERE id = ?", g.devID).
		Scan(&cur.displayName, &cur.location, &cur.owner, &cur.notes, &cur.crit, &cur.state, &cur.custom); err != nil {
		return err
	}
	pick := func(current, incoming string) string {
		incoming = strings.TrimSpace(incoming)
		if incoming == "" {
			return current
		}
		if m.Overwrite || current == "" {
			return incoming
		}
		return current
	}
	crit := cur.crit
	switch m.Criticality {
	case "low", "normal", "high", "critical":
		if m.Overwrite || cur.crit == "normal" {
			crit = m.Criticality
		}
	}
	state := cur.state
	switch m.State {
	case "known", "unknown", "ignored":
		if m.Overwrite || cur.state == "unknown" {
			state = m.State
		}
	}
	custom := map[string]any{}
	_ = json.Unmarshal([]byte(cur.custom), &custom)
	for k, v := range m.Custom {
		if _, exists := custom[k]; !exists || m.Overwrite {
			custom[k] = v
		}
	}
	if err := g.exec(`UPDATE devices SET display_name = ?, location = ?, owner = ?, notes = ?, criticality = ?, state = ?, custom = ? WHERE id = ?`,
		pick(cur.displayName, m.DisplayName), pick(cur.location, m.Location), pick(cur.owner, m.Owner), pick(cur.notes, m.Notes),
		crit, state, db.JSON(custom), g.devID); err != nil {
		return err
	}
	for _, t := range m.Tags {
		if t = normalizeTag(t); t != "" {
			if err := g.exec("INSERT OR IGNORE INTO device_tags(device_id, tag) VALUES (?, ?)", g.devID, t); err != nil {
				return err
			}
		}
	}
	if m.Type != "" {
		var existing int
		_ = g.tx.QueryRowContext(g.ctx, "SELECT COUNT(*) FROM device_facts WHERE device_id = ? AND kind = 'type' AND source = 'manual' AND gone_at IS NULL", g.devID).Scan(&existing)
		if existing == 0 || m.Overwrite {
			if _, err := setFact(g.ctx, g.tx, g.devID, FactType, SourceManual, m.Type, "", nil, g.nowMs()); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeTag(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	t = strings.Join(strings.Fields(t), "-")
	if len(t) > 64 {
		t = t[:64]
	}
	return t
}
