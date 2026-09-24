package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"netscope/internal/db"
	"netscope/internal/federation/wire"
	"netscope/internal/plugin"
)

// Snapshot of a device for the full synchronisation with a central instance: the current
// state is turned back into one observation per source (and scan address), so the central
// instance applies it like live data and keeps the source priorities. Manual data stays
// here: it belongs to the instance where it was entered.

// DeviceIDs returns the ids of all devices.
func (s *Store) DeviceIDs(ctx context.Context) ([]int64, error) {
	return queryIDs(ctx, s.db.R, "SELECT id FROM devices ORDER BY id")
}

type snapKey struct{ source, ip string }

type snapBuilder struct {
	dev   int64
	order []snapKey
	obs   map[snapKey][]*plugin.Observation
}

// get returns an observation of source/ip whose section is still free.
func (b *snapBuilder) get(source, ip string, free func(*plugin.Observation) bool) *plugin.Observation {
	k := snapKey{source, ip}
	for _, o := range b.obs[k] {
		if free(o) {
			return o
		}
	}
	o := &plugin.Observation{IP: ip}
	if _, ok := b.obs[k]; !ok {
		b.order = append(b.order, k)
	}
	b.obs[k] = append(b.obs[k], o)
	return o
}

func (b *snapBuilder) items() []wire.Observation {
	var out []wire.Observation
	for _, k := range b.order {
		for _, o := range b.obs[k] {
			out = append(out, wire.Observation{Plugin: k.source, Device: b.dev, Obs: o})
		}
	}
	return out
}

// SnapshotDevice returns the state of a device as observations (identity first) and its
// presence state. ok is false if the device no longer exists.
func (s *Store) SnapshotDevice(ctx context.Context, id int64) (obs []wire.Observation, presence wire.Presence, ok bool, err error) {
	q := s.db.R
	var (
		online               bool
		changed, first, last sql.NullInt64
		primary, created     string
	)
	err = q.QueryRowContext(ctx, "SELECT online, online_changed_at, first_seen, last_seen, primary_ip, created_source FROM devices WHERE id = ?", id).
		Scan(&online, &changed, &first, &last, &primary, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, presence, false, nil
	}
	if err != nil {
		return nil, presence, false, err
	}
	presence = wire.Presence{Device: id, Online: online, Since: db.NullTime(changed), LastSeen: db.NullTime(last), FirstSeen: db.NullTime(first)}
	if err := eachRow(ctx, q, "SELECT plugin_id, last_seen, missed FROM device_presence WHERE device_id = ? ORDER BY plugin_id", []any{id}, func(r *sql.Rows) error {
		var (
			sc   wire.Scanner
			seen int64
		)
		if err := r.Scan(&sc.Plugin, &seen, &sc.Missed); err != nil {
			return err
		}
		sc.LastSeen = db.Time(seen)
		presence.Scanners = append(presence.Scanners, sc)
		return nil
	}); err != nil {
		return nil, presence, false, err
	}

	// identity: all MACs and current addresses, created like an import
	if created == "" || created == SourceManual {
		created = "sync"
	}
	ident := &plugin.Observation{Create: true, FirstSeen: db.NullTime(first)}
	if err := eachRow(ctx, q, "SELECT mac FROM device_macs WHERE device_id = ? ORDER BY last_seen DESC", []any{id}, func(r *sql.Rows) error {
		var m string
		if err := r.Scan(&m); err != nil {
			return err
		}
		ident.MACs = append(ident.MACs, m)
		return nil
	}); err != nil {
		return nil, presence, false, err
	}
	if err := eachRow(ctx, q, "SELECT ip FROM device_ips WHERE device_id = ? AND gone_at IS NULL ORDER BY last_seen DESC", []any{id}, func(r *sql.Rows) error {
		var ip string
		if err := r.Scan(&ip); err != nil {
			return err
		}
		if ip == primary && ident.IP == "" {
			ident.IP = ip
		} else {
			ident.IPs = append(ident.IPs, ip)
		}
		return nil
	}); err != nil {
		return nil, presence, false, err
	}
	if ident.IP == "" && len(ident.IPs) > 0 {
		ident.IP, ident.IPs = ident.IPs[0], ident.IPs[1:]
	}
	obs = append(obs, wire.Observation{Plugin: created, Device: id, Obs: ident})

	b := &snapBuilder{dev: id, obs: map[snapKey][]*plugin.Observation{}}
	steps := []func(context.Context, db.Querier, *snapBuilder) error{
		snapFacts, snapPorts, snapHTTP, snapCerts, snapPackages, snapContainers, snapInventory, snapRefs,
	}
	for _, step := range steps {
		if err := step(ctx, q, b); err != nil {
			return nil, presence, false, err
		}
	}
	return append(obs, b.items()...), presence, true, nil
}

// SnapshotRelations returns the edges a device is the upstream side of, one observation
// per source (sent after all devices, so both sides exist at the central instance).
func (s *Store) SnapshotRelations(ctx context.Context, id int64) ([]wire.Observation, error) {
	b := &snapBuilder{dev: id, obs: map[snapKey][]*plugin.Observation{}}
	err := eachRow(ctx, s.db.R, `SELECT source, kind, child_id, parent_port, child_port, label FROM relations
		WHERE parent_id = ? AND source <> 'manual' ORDER BY source, kind, child_id`, []any{id}, func(r *sql.Rows) error {
		var (
			source string
			rel    plugin.Relation
		)
		if err := r.Scan(&source, &rel.Kind, &rel.Other.DeviceID, &rel.LocalPort, &rel.RemotePort, &rel.Label); err != nil {
			return err
		}
		rel.Parent = true
		o := b.get(source, "", func(*plugin.Observation) bool { return true })
		o.Relations = append(o.Relations, rel)
		return nil
	})
	return b.items(), err
}

func snapFacts(ctx context.Context, q db.Querier, b *snapBuilder) error {
	return eachRow(ctx, q, `SELECT kind, source, value, extra FROM device_facts WHERE device_id = ? AND gone_at IS NULL AND source <> 'manual'
		ORDER BY source, kind`, []any{b.dev}, func(r *sql.Rows) error {
		var kind, source, value, extra string
		if err := r.Scan(&kind, &source, &value, &extra); err != nil {
			return err
		}
		o := b.get(source, "", func(*plugin.Observation) bool { return true })
		switch kind {
		case FactHostname:
			o.Hostname = value
		case FactVendor:
			o.Vendor = value
		case FactModel:
			o.Model = value
		case FactType:
			o.DeviceType = value
		case FactOS:
			var os plugin.OSInfo
			if json.Unmarshal([]byte(extra), &os) != nil || os.Name == "" {
				os = plugin.OSInfo{Name: value}
			}
			o.OS = &os
		default:
			if k, ok := strings.CutPrefix(kind, "attr:"); ok {
				if o.Attrs == nil {
					o.Attrs = map[string]string{}
				}
				o.Attrs[k] = value
			}
		}
		return nil
	})
}

func snapPorts(ctx context.Context, q db.Querier, b *snapBuilder) error {
	return eachRow(ctx, q, `SELECT source, ip, proto, port, state, service, product, version, extra_info, tunnel, cpes FROM ports
		WHERE device_id = ? AND gone_at IS NULL ORDER BY source, ip, proto, port`, []any{b.dev}, func(r *sql.Rows) error {
		var (
			source, ip, cpes string
			p                plugin.Port
		)
		if err := r.Scan(&source, &ip, &p.Proto, &p.Port, &p.State, &p.Service, &p.Product, &p.Version, &p.ExtraInfo, &p.Tunnel, &cpes); err != nil {
			return err
		}
		_ = json.Unmarshal([]byte(cpes), &p.CPEs)
		o := b.get(source, ip, func(o *plugin.Observation) bool { return o.Ports == nil || o.Ports.Protocol == p.Proto })
		if o.Ports == nil {
			o.Ports = &plugin.PortScan{Protocol: p.Proto}
		}
		o.Ports.Ports = append(o.Ports.Ports, p)
		return nil
	})
}

func snapHTTP(ctx context.Context, q db.Querier, b *snapBuilder) error {
	return eachRow(ctx, q, `SELECT source, ip, port, scheme, url, status_code, title, server, content_type, redirects, final_url,
		favicon_hash, favicon_md5, apps, headers FROM http_services WHERE device_id = ? AND gone_at IS NULL ORDER BY source, ip, port`,
		[]any{b.dev}, func(r *sql.Rows) error {
			var (
				source, ip, redirects, apps, headers string
				fav                                  sql.NullInt64
				h                                    plugin.HTTPService
			)
			if err := r.Scan(&source, &ip, &h.Port, &h.Scheme, &h.URL, &h.StatusCode, &h.Title, &h.Server, &h.ContentType, &redirects,
				&h.FinalURL, &fav, &h.FaviconMD5, &apps, &headers); err != nil {
				return err
			}
			if fav.Valid {
				v := int32(fav.Int64)
				h.FaviconHash = &v
			}
			_ = json.Unmarshal([]byte(redirects), &h.Redirects)
			_ = json.Unmarshal([]byte(apps), &h.Apps)
			_ = json.Unmarshal([]byte(headers), &h.Headers)
			o := b.get(source, ip, func(*plugin.Observation) bool { return true })
			if o.HTTP == nil {
				o.HTTP = &plugin.HTTPScan{}
			}
			o.HTTP.Services = append(o.HTTP.Services, h)
			return nil
		})
}

func snapCerts(ctx context.Context, q db.Querier, b *snapBuilder) error {
	return eachRow(ctx, q, `SELECT source, ip, port, server_name, fingerprint, subject_cn, sans, issuer, issuer_cn, serial, not_before, not_after,
		self_signed, chain_valid, chain_error, key_type, key_bits, signature_alg, tls_versions, cipher, weak_protocols, weak_ciphers
		FROM certificates WHERE device_id = ? AND gone_at IS NULL ORDER BY source, ip, port`, []any{b.dev}, func(r *sql.Rows) error {
		var (
			source, ip, sans, vers, wp, wc string
			nb, na                         int64
			c                              plugin.TLSCert
		)
		if err := r.Scan(&source, &ip, &c.Port, &c.ServerName, &c.Fingerprint, &c.SubjectCN, &sans, &c.Issuer, &c.IssuerCN, &c.Serial, &nb, &na,
			&c.SelfSigned, &c.ChainValid, &c.ChainError, &c.KeyType, &c.KeyBits, &c.SignatureAlg, &vers, &c.Cipher, &wp, &wc); err != nil {
			return err
		}
		c.NotBefore, c.NotAfter = db.Time(nb), db.Time(na)
		_ = json.Unmarshal([]byte(sans), &c.SANs)
		_ = json.Unmarshal([]byte(vers), &c.Versions)
		_ = json.Unmarshal([]byte(wp), &c.WeakProtocols)
		_ = json.Unmarshal([]byte(wc), &c.WeakCiphers)
		o := b.get(source, ip, func(*plugin.Observation) bool { return true })
		if o.TLS == nil {
			o.TLS = &plugin.TLSScan{}
		}
		o.TLS.Certs = append(o.TLS.Certs, c)
		return nil
	})
}

func snapPackages(ctx context.Context, q db.Querier, b *snapBuilder) error {
	return eachRow(ctx, q, `SELECT source, manager, name, version, arch FROM packages WHERE device_id = ? AND gone_at IS NULL
		ORDER BY source, manager, name`, []any{b.dev}, func(r *sql.Rows) error {
		var (
			source, manager string
			p               plugin.Package
		)
		if err := r.Scan(&source, &manager, &p.Name, &p.Version, &p.Arch); err != nil {
			return err
		}
		o := b.get(source, "", func(o *plugin.Observation) bool { return o.Packages == nil || o.Packages.Manager == manager })
		if o.Packages == nil {
			o.Packages = &plugin.PackageInventory{Manager: manager}
		}
		o.Packages.Packages = append(o.Packages.Packages, p)
		return nil
	})
}

func snapContainers(ctx context.Context, q db.Querier, b *snapBuilder) error {
	section := func(source, engine string) *plugin.ContainerInventory {
		o := b.get(source, "", func(o *plugin.Observation) bool { return o.Containers == nil || o.Containers.Engine == engine })
		if o.Containers == nil {
			o.Containers = &plugin.ContainerInventory{Engine: engine, Containers: []plugin.Container{}}
		}
		return o.Containers
	}
	if err := eachRow(ctx, q, `SELECT source, engine, container_id, name, image, image_id, state, status, ports, networks, compose_project,
		compose_service, labels, created FROM containers WHERE device_id = ? AND gone_at IS NULL ORDER BY source, engine, name`,
		[]any{b.dev}, func(r *sql.Rows) error {
			var (
				source, engine, ports, networks, labels string
				created                                 sql.NullInt64
				c                                       plugin.Container
			)
			if err := r.Scan(&source, &engine, &c.ID, &c.Name, &c.Image, &c.ImageID, &c.State, &c.Status, &ports, &networks,
				&c.ComposeProject, &c.ComposeService, &labels, &created); err != nil {
				return err
			}
			_ = json.Unmarshal([]byte(ports), &c.Ports)
			_ = json.Unmarshal([]byte(networks), &c.Networks)
			_ = json.Unmarshal([]byte(labels), &c.Labels)
			if t := db.NullTime(created); t != nil {
				c.Created = *t
			}
			inv := section(source, engine)
			inv.Containers = append(inv.Containers, c)
			return nil
		}); err != nil {
		return err
	}
	return eachRow(ctx, q, `SELECT source, engine, image_id, tags, size, created FROM container_images WHERE device_id = ? AND gone_at IS NULL
		ORDER BY source, engine, image_id`, []any{b.dev}, func(r *sql.Rows) error {
		var (
			source, engine, tags string
			created              sql.NullInt64
			im                   plugin.ContainerImage
		)
		if err := r.Scan(&source, &engine, &im.ID, &tags, &im.Size, &created); err != nil {
			return err
		}
		_ = json.Unmarshal([]byte(tags), &im.Tags)
		if t := db.NullTime(created); t != nil {
			im.Created = *t
		}
		inv := section(source, engine)
		inv.Images = append(inv.Images, im)
		return nil
	})
}

func snapInventory(ctx context.Context, q db.Querier, b *snapBuilder) error {
	return eachRow(ctx, q, "SELECT source, data FROM device_inventory WHERE device_id = ? ORDER BY source", []any{b.dev}, func(r *sql.Rows) error {
		var source, data string
		if err := r.Scan(&source, &data); err != nil {
			return err
		}
		var v any
		if json.Unmarshal([]byte(data), &v) != nil {
			return nil
		}
		o := b.get(source, "", func(o *plugin.Observation) bool { return o.Inventory == nil })
		o.Inventory = v
		return nil
	})
}

func snapRefs(ctx context.Context, q db.Querier, b *snapBuilder) error {
	return eachRow(ctx, q, "SELECT source, ref, data FROM external_refs WHERE device_id = ? ORDER BY source, ref", []any{b.dev}, func(r *sql.Rows) error {
		var source, ref, data string
		if err := r.Scan(&source, &ref, &data); err != nil {
			return err
		}
		var v any
		_ = json.Unmarshal([]byte(data), &v)
		o := b.get(source, "", func(o *plugin.Observation) bool { return o.Ref == nil })
		o.Ref = &plugin.ExternalRef{Source: source, ID: ref, Data: v}
		return nil
	})
}

func eachRow(ctx context.Context, q db.Querier, query string, args []any, fn func(*sql.Rows) error) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
