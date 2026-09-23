// Package diff turns state changes derived by the core into events (device new/gone/back,
// IP/MAC/hostname/OS changes, ports, service versions, certificates, containers,
// packages) and checks certificate expiry periodically.
package diff

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the diff processor.
type Plugin struct{}

var eventOptions = []plugin.Option{
	{Value: plugin.EvDeviceNew, Label: "Gerät neu"},
	{Value: plugin.EvDeviceOffline, Label: "Gerät weg"},
	{Value: plugin.EvDeviceOnline, Label: "Gerät wieder da"},
	{Value: plugin.EvDeviceIPChanged, Label: "IP-Wechsel"},
	{Value: plugin.EvDeviceMACChanged, Label: "MAC-Wechsel auf bekannter IP"},
	{Value: plugin.EvDeviceHostnameChanged, Label: "Hostname geändert"},
	{Value: plugin.EvDeviceOSChanged, Label: "OS-Erkennung geändert"},
	{Value: plugin.EvPortOpened, Label: "Port neu"},
	{Value: plugin.EvPortClosed, Label: "Port geschlossen"},
	{Value: plugin.EvServiceChanged, Label: "Dienstversion geändert"},
	{Value: plugin.EvCertExpiring, Label: "Zertifikat läuft ab"},
	{Value: plugin.EvCertExpired, Label: "Zertifikat abgelaufen"},
	{Value: plugin.EvCertChanged, Label: "Zertifikat gewechselt"},
	{Value: plugin.EvTLSWeak, Label: "Schwache TLS-Konfiguration"},
	{Value: plugin.EvContainerNew, Label: "Container neu"},
	{Value: plugin.EvContainerRemoved, Label: "Container weg"},
	{Value: plugin.EvContainerImageChanged, Label: "Container-Image geändert"},
	{Value: plugin.EvPackagesChanged, Label: "Paket-Änderungen"},
}

func allEventTypes() []any {
	out := make([]any, len(eventOptions))
	for i, o := range eventOptions {
		out[i] = o.Value
	}
	return out
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "diff",
		Kind: plugin.KindProcessor,
		Name: "Änderungserkennung",
		Description: "Vergleicht den Zustand nach jedem Lauf mit dem vorherigen und erzeugt Events: Geräte neu/weg/wieder da, " +
			"IP-, MAC-, Hostname- und OS-Wechsel, Ports, Dienstversionen, Zertifikate (inkl. Ablauf), Container und Pakete.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "5 * * * *",
		DefaultTimeout:     10 * time.Minute,
		DefaultConcurrency: 1,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "event_types", Type: plugin.FieldEnum, Multi: true, Label: "Erzeugte Events", Options: eventOptions,
			Default: allEventTypes(), Description: "Welche Änderungen zu Events werden."},
		{Key: "report_initial", Type: plugin.FieldBool, Label: "Erstdaten melden", Default: false,
			Description: "Auch beim ersten Scan eines Geräts Events erzeugen (z. B. jeden gefundenen Port). Erzeugt beim Start viele Events."},
		{Key: "new_from_importers", Type: plugin.FieldBool, Label: "„Gerät neu“ auch für importierte Geräte", Default: false,
			Description: "Geräte, die Importer (Proxmox, NetAlertX, CSV …) anlegen, lösen normalerweise kein „Gerät neu“ aus."},
		{Key: "skip_ignored", Type: plugin.FieldBool, Label: "Ignorierte Geräte überspringen", Default: true},
		{Key: "ignore_tags", Type: plugin.FieldStringList, Label: "Geräte mit diesen Tags überspringen"},
		{Key: "ignore_ports", Type: plugin.FieldStringList, Label: "Ports ohne Events", Placeholder: "5353/udp",
			Description: "Ein Eintrag pro Zeile im Format port/proto, z. B. 1900/udp.",
			Validation:  &plugin.Validation{Pattern: `^\d{1,5}/(tcp|udp)$`}},
		{Key: "presence_quiet_types", Type: plugin.FieldStringList, Label: "Keine Online/Offline-Events für Gerätetypen",
			Default: []any{"phone", "tablet"}, Group: "Anwesenheit",
			Description: "Mobilgeräte kommen und gehen ständig (Standby, WLAN-Schlaf). Ihr Online-Status wird weiter erfasst, erzeugt aber keine Events. Ein Gerätetyp pro Zeile."},
		{Key: "presence_flap_limit", Type: plugin.FieldInt, Label: "Flap-Dämpfung: höchstens … Online/Offline-Events pro Gerät in 24 h",
			Default: 4, Group: "Anwesenheit", Validation: &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(1000)},
			Description: "Weitere Wechsel eines unruhigen Geräts werden unterdrückt, bis es wieder 24 h lang weniger oft wechselt. 0 = keine Begrenzung."},
		{Key: "cert_warn_days", Type: plugin.FieldInt, Label: "Zertifikatswarnung ab (Tage vor Ablauf)", Default: 30,
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(365)}, Group: "Zertifikate"},
		{Key: "cert_repeat", Type: plugin.FieldDuration, Label: "Ablaufwarnung wiederholen alle", Default: "24h",
			Validation: &plugin.Validation{Min: plugin.Int64(3600)}, Group: "Zertifikate"},
		{Key: "package_list_max", Type: plugin.FieldInt, Label: "Max. Paketnamen im Event", Default: 50,
			Validation: &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(1000)}, Advanced: true},
	}}
}

type config struct {
	types          map[string]bool
	reportInitial  bool
	fromImporters  bool
	skipIgnored    bool
	ignoreTags     map[string]bool
	ignorePorts    map[string]bool
	warnDays       int
	certRepeat     time.Duration
	packageListMax int
	quietTypes     map[string]bool // device types without online/offline events
	flapLimit      int             // max online/offline events per device and 24 h (0 = off)
}

func load(s plugin.Settings) config {
	c := config{types: map[string]bool{}, ignoreTags: map[string]bool{}, ignorePorts: map[string]bool{}, quietTypes: map[string]bool{},
		reportInitial: s.Bool("report_initial"), fromImporters: s.Bool("new_from_importers"), skipIgnored: s.Bool("skip_ignored"),
		warnDays: s.Int("cert_warn_days"), certRepeat: s.Duration("cert_repeat"), packageListMax: s.Int("package_list_max"),
		flapLimit: s.Int("presence_flap_limit")}
	for _, t := range s.StringList("presence_quiet_types") {
		c.quietTypes[strings.ToLower(strings.TrimSpace(t))] = true
	}
	for _, t := range s.StringList("event_types") {
		c.types[t] = true
	}
	for _, t := range s.StringList("ignore_tags") {
		c.ignoreTags[strings.ToLower(t)] = true
	}
	for _, p := range s.StringList("ignore_ports") {
		c.ignorePorts[strings.ToLower(p)] = true
	}
	if c.certRepeat <= 0 {
		c.certRepeat = 24 * time.Hour
	}
	return c
}

// deviceCache avoids repeated lookups within one batch.
type deviceCache struct {
	rc   *plugin.RunContext
	devs map[int64]*plugin.DeviceInfo
}

func (d *deviceCache) get(ctx context.Context, id int64) *plugin.DeviceInfo {
	if dev, ok := d.devs[id]; ok {
		return dev
	}
	dev, err := d.rc.Inventory.Device(ctx, id)
	if err != nil {
		dev = nil
	}
	d.devs[id] = dev
	return dev
}

func (c config) skipDevice(dev *plugin.DeviceInfo) bool {
	if dev == nil {
		return false
	}
	if c.skipIgnored && dev.State == "ignored" {
		return true
	}
	for _, t := range dev.Tags {
		if c.ignoreTags[strings.ToLower(t)] {
			return true
		}
	}
	return false
}

func name(dev *plugin.DeviceInfo, id int64) string {
	if dev == nil {
		return fmt.Sprintf("Gerät %d", id)
	}
	return dev.Name
}

func portDesc(p *plugin.Port) string {
	s := strings.TrimSpace(strings.Join([]string{p.Product, p.Version}, " "))
	if s == "" {
		s = p.Service
	}
	return s
}

// mapEvents maps changes to events (pure function, table-tested).
func mapEvents(changes []plugin.Change, c config, lookup func(id int64) *plugin.DeviceInfo, kindOf func(pluginID string) plugin.Kind) []plugin.Event {
	var out []plugin.Event
	emit := func(ev plugin.Event) {
		if c.types[ev.Type] {
			out = append(out, ev)
		}
	}
	for _, ch := range changes {
		dev := lookup(ch.DeviceID)
		if c.skipDevice(dev) {
			continue
		}
		if ch.Initial && !c.reportInitial {
			continue
		}
		n := name(dev, ch.DeviceID)
		base := plugin.Event{DeviceID: ch.DeviceID, RunID: ch.RunID, At: ch.At}
		switch ch.Type {
		case plugin.ChangeDeviceCreated:
			snap, _ := ch.New.(*plugin.DeviceSnapshot)
			if !c.fromImporters && kindOf(ch.PluginID) != plugin.KindScanner {
				continue
			}
			ev := base
			ev.Type = plugin.EvDeviceNew
			ip, vendor := "", ""
			if snap != nil {
				ip, vendor = snap.IP, snap.Vendor
			}
			ev.Title = "Neues Gerät: " + n
			if ip != "" && !strings.Contains(n, ip) {
				ev.Title += " (" + ip + ")"
			}
			if vendor != "" {
				ev.Message = "Hersteller: " + vendor
			}
			ev.Payload = map[string]any{"vendor": vendor, "source": ch.PluginID}
			emit(ev)
		case plugin.ChangeDeviceOffline:
			if dev != nil && c.quietTypes[strings.ToLower(dev.Type)] {
				continue
			}
			ev := base
			ev.Type = plugin.EvDeviceOffline
			ev.Title = "Gerät weg: " + n
			ev.Payload = map[string]any{}
			if t, ok := ch.Old.(time.Time); ok {
				ev.Payload["last_seen"] = t.Format(time.RFC3339)
				ev.Message = "Zuletzt gesehen " + t.Local().Format("02.01.2006 15:04")
			}
			emit(ev)
		case plugin.ChangeDeviceOnline:
			if dev != nil && c.quietTypes[strings.ToLower(dev.Type)] {
				continue
			}
			ev := base
			ev.Type = plugin.EvDeviceOnline
			ev.Title = "Gerät wieder da: " + n
			ev.Payload = map[string]any{}
			if t, ok := ch.Old.(*time.Time); ok && t != nil {
				d := ch.At.Sub(*t)
				ev.Payload["offline_since"] = t.Format(time.RFC3339)
				ev.Payload["offline_seconds"] = math.Round(d.Seconds())
				ev.Message = "Offline seit " + t.Local().Format("02.01.2006 15:04") + " (" + humanDuration(d) + ")"
			}
			emit(ev)
		case plugin.ChangeIPChanged:
			ev := base
			ev.Type = plugin.EvDeviceIPChanged
			oldIP, _ := ch.Old.(string)
			newIP, _ := ch.New.(string)
			ev.Title = fmt.Sprintf("IP-Wechsel: %s %s → %s", n, oldIP, newIP)
			ev.Payload = map[string]any{"old_ip": oldIP, "new_ip": newIP}
			emit(ev)
		case plugin.ChangeMACChanged:
			mc, _ := ch.New.(*plugin.MACChange)
			if mc == nil {
				continue
			}
			ev := base
			ev.Type = plugin.EvDeviceMACChanged
			ev.Title = fmt.Sprintf("MAC-Wechsel auf %s: %s → %s", mc.IP, mc.OldMAC, mc.NewMAC)
			ev.Message = "Die IP-Adresse wurde bisher von einem anderen Gerät verwendet. Gerätetausch oder ARP-Spoofing prüfen."
			if netutil.IsRandomizedMAC(mc.NewMAC) || netutil.IsRandomizedMAC(mc.OldMAC) {
				// phones and laptops rotate private MAC addresses: usually the same device
				ev.Severity = plugin.SevLow
				ev.Message = "Private (zufällige) MAC-Adresse – vermutlich dasselbe Gerät mit neuer MAC. Bei Bedarf die Geräte zusammenführen."
			}
			ev.Payload = map[string]any{"ip": mc.IP, "old_mac": mc.OldMAC, "new_mac": mc.NewMAC, "old_device_id": mc.OldDeviceID}
			emit(ev)
		case plugin.ChangeHostname:
			ev := base
			ev.Type = plugin.EvDeviceHostnameChanged
			o, _ := ch.Old.(string)
			nw, _ := ch.New.(string)
			ev.Title = fmt.Sprintf("Hostname geändert: %s → %s", o, nw)
			ev.Payload = map[string]any{"old": o, "new": nw}
			emit(ev)
		case plugin.ChangeOS:
			ev := base
			ev.Type = plugin.EvDeviceOSChanged
			o, _ := ch.Old.(string)
			nw, _ := ch.New.(string)
			ev.Title = fmt.Sprintf("OS geändert: %s", n)
			ev.Message = fmt.Sprintf("%s → %s", o, nw)
			ev.Payload = map[string]any{"old": o, "new": nw}
			emit(ev)
		case plugin.ChangePortOpened, plugin.ChangePortClosed, plugin.ChangePortChanged:
			p, _ := ch.New.(*plugin.Port)
			if ch.Type == plugin.ChangePortClosed {
				p, _ = ch.Old.(*plugin.Port)
			}
			if p == nil {
				continue
			}
			if c.ignorePorts[fmt.Sprintf("%d/%s", p.Port, p.Proto)] {
				continue
			}
			ip := strings.SplitN(ch.Key, " ", 2)[0]
			ev := base
			ev.Payload = map[string]any{"ip": ip, "port": p.Port, "proto": p.Proto, "service": p.Service, "product": p.Product, "version": p.Version}
			switch ch.Type {
			case plugin.ChangePortOpened:
				ev.Type = plugin.EvPortOpened
				ev.Title = fmt.Sprintf("Port %d/%s neu auf %s", p.Port, p.Proto, n)
				if d := portDesc(p); d != "" {
					ev.Message = d
				}
			case plugin.ChangePortClosed:
				ev.Type = plugin.EvPortClosed
				ev.Title = fmt.Sprintf("Port %d/%s geschlossen auf %s", p.Port, p.Proto, n)
			default:
				old, _ := ch.Old.(*plugin.Port)
				ev.Type = plugin.EvServiceChanged
				ev.Title = fmt.Sprintf("Dienst auf %s %d/%s geändert", n, p.Port, p.Proto)
				oldDesc := ""
				if old != nil {
					oldDesc = portDesc(old)
				}
				ev.Message = fmt.Sprintf("%s → %s", oldDesc, portDesc(p))
				ev.Payload["old"], ev.Payload["new"] = oldDesc, portDesc(p)
			}
			emit(ev)
		case plugin.ChangeCertAdded, plugin.ChangeCertChanged:
			cert, _ := ch.New.(*plugin.TLSCert)
			if cert == nil {
				continue
			}
			ip := strings.SplitN(ch.Key, ":", 2)[0]
			if ch.Type == plugin.ChangeCertChanged {
				old, _ := ch.Old.(*plugin.TLSCert)
				ev := base
				ev.Type = plugin.EvCertChanged
				ev.Title = fmt.Sprintf("Zertifikat gewechselt: %s:%d (%s)", ip, cert.Port, n)
				ev.Message = fmt.Sprintf("CN=%s, gültig bis %s", cert.SubjectCN, cert.NotAfter.Local().Format("02.01.2006"))
				oldFP := ""
				if old != nil {
					oldFP = old.Fingerprint
				}
				ev.Payload = map[string]any{"ip": ip, "port": cert.Port, "old_fingerprint": oldFP, "new_fingerprint": cert.Fingerprint,
					"subject": cert.SubjectCN, "not_after": cert.NotAfter.Format(time.RFC3339)}
				emit(ev)
			}
			if len(cert.WeakProtocols) > 0 || len(cert.WeakCiphers) > 0 {
				old, _ := ch.Old.(*plugin.TLSCert)
				if old == nil || (len(old.WeakProtocols) == 0 && len(old.WeakCiphers) == 0) {
					ev := base
					ev.Type = plugin.EvTLSWeak
					ev.Title = fmt.Sprintf("Schwache TLS-Konfiguration auf %s:%d (%s)", ip, cert.Port, n)
					ev.Message = strings.TrimSpace(strings.Join(append(append([]string{}, cert.WeakProtocols...), cert.WeakCiphers...), ", "))
					ev.Payload = map[string]any{"ip": ip, "port": cert.Port, "weak_protocols": cert.WeakProtocols, "weak_ciphers": cert.WeakCiphers}
					emit(ev)
				}
			}
		case plugin.ChangeContainerAdded:
			ct, _ := ch.New.(*plugin.Container)
			if ct == nil {
				continue
			}
			ev := base
			ev.Type = plugin.EvContainerNew
			ev.Title = fmt.Sprintf("Container neu auf %s: %s", n, ct.Name)
			ev.Message = ct.Image
			ev.Payload = map[string]any{"name": ct.Name, "image": ct.Image, "compose_project": ct.ComposeProject}
			emit(ev)
		case plugin.ChangeContainerRemoved:
			ct, _ := ch.Old.(*plugin.Container)
			if ct == nil {
				continue
			}
			ev := base
			ev.Type = plugin.EvContainerRemoved
			ev.Title = fmt.Sprintf("Container weg auf %s: %s", n, ct.Name)
			ev.Message = ct.Image
			ev.Payload = map[string]any{"name": ct.Name, "image": ct.Image}
			emit(ev)
		case plugin.ChangeContainerImage:
			oc, _ := ch.Old.(*plugin.Container)
			nc, _ := ch.New.(*plugin.Container)
			if oc == nil || nc == nil {
				continue
			}
			ev := base
			ev.Type = plugin.EvContainerImageChanged
			ev.Title = fmt.Sprintf("Container-Image geändert auf %s: %s", n, nc.Name)
			ev.Message = fmt.Sprintf("%s → %s", oc.Image, nc.Image)
			if oc.Image == nc.Image {
				ev.Message = fmt.Sprintf("%s (neue Image-Version)", nc.Image)
			}
			ev.Payload = map[string]any{"name": nc.Name, "old_image": oc.Image, "new_image": nc.Image}
			emit(ev)
		case plugin.ChangePackages:
			d, _ := ch.New.(*plugin.PackageDelta)
			if d == nil || d.Count() == 0 {
				continue
			}
			ev := base
			ev.Type = plugin.EvPackagesChanged
			ev.Title = fmt.Sprintf("%d Paket-Änderung(en) auf %s", d.Count(), n)
			var parts []string
			if len(d.Updated) > 0 {
				parts = append(parts, fmt.Sprintf("%d aktualisiert", len(d.Updated)))
			}
			if len(d.Added) > 0 {
				parts = append(parts, fmt.Sprintf("%d neu", len(d.Added)))
			}
			if len(d.Removed) > 0 {
				parts = append(parts, fmt.Sprintf("%d entfernt", len(d.Removed)))
			}
			ev.Message = strings.Join(parts, ", ")
			ev.Payload = map[string]any{"manager": d.Manager, "count": d.Count(),
				"added": pkgNames(d.Added, c.packageListMax), "removed": pkgNames(d.Removed, c.packageListMax),
				"updated": updNames(d.Updated, c.packageListMax)}
			emit(ev)
		}
	}
	return out
}

func pkgNames(ps []plugin.Package, max int) []string {
	out := []string{}
	for i, p := range ps {
		if i >= max {
			out = append(out, fmt.Sprintf("… und %d weitere", len(ps)-max))
			break
		}
		out = append(out, p.Name+" "+p.Version)
	}
	return out
}

func updNames(ps []plugin.PackageUpdate, max int) []string {
	out := []string{}
	for i, p := range ps {
		if i >= max {
			out = append(out, fmt.Sprintf("… und %d weitere", len(ps)-max))
			break
		}
		out = append(out, fmt.Sprintf("%s %s → %s", p.Name, p.From, p.To))
	}
	return out
}

func humanDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d Tage", int(d.Hours()/24))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d Stunden", int(d.Hours()))
	case d >= 2*time.Minute:
		return fmt.Sprintf("%d Minuten", int(d.Minutes()))
	}
	return fmt.Sprintf("%d Sekunden", int(d.Seconds()))
}

func kindOf(id string) plugin.Kind {
	if p, ok := plugin.Get(id); ok {
		return p.Info().Kind
	}
	return ""
}

// HandleChanges implements plugin.ChangeHandler.
func (p *Plugin) HandleChanges(ctx context.Context, rc *plugin.RunContext, changes []plugin.Change) error {
	c := load(rc.Settings)
	cache := &deviceCache{rc: rc, devs: map[int64]*plugin.DeviceInfo{}}
	evs := mapEvents(changes, c, func(id int64) *plugin.DeviceInfo { return cache.get(ctx, id) }, kindOf)
	evs = dampPresence(ctx, rc, c, evs)
	for _, ev := range evs {
		if _, err := rc.Events.Emit(ctx, ev); err != nil {
			rc.Log.Warn("Event konnte nicht gespeichert werden", "type", ev.Type, "err", err)
		}
	}
	return nil
}

// dampPresence drops online/offline events of devices that already produced flapLimit
// of them within the last 24 hours.
func dampPresence(ctx context.Context, rc *plugin.RunContext, c config, evs []plugin.Event) []plugin.Event {
	if c.flapLimit <= 0 || rc.DB == nil {
		return evs
	}
	since := time.Now().Add(-24 * time.Hour).UnixMilli()
	counts := map[int64]int{}
	out := evs[:0]
	for _, ev := range evs {
		if (ev.Type != plugin.EvDeviceOnline && ev.Type != plugin.EvDeviceOffline) || ev.DeviceID == 0 {
			out = append(out, ev)
			continue
		}
		n, ok := counts[ev.DeviceID]
		if !ok {
			err := rc.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE device_id = ? AND type IN (?, ?) AND ts >= ?`,
				ev.DeviceID, plugin.EvDeviceOnline, plugin.EvDeviceOffline, since).Scan(&n)
			if err != nil {
				rc.Log.Warn("Flap-Dämpfung: Events nicht lesbar", "device", ev.DeviceID, "err", err)
				out = append(out, ev)
				continue
			}
		}
		if n >= c.flapLimit {
			rc.Log.Debug("Online/Offline-Event gedämpft", "device", ev.DeviceID, "type", ev.Type, "events_24h", n)
			counts[ev.DeviceID] = n
			continue
		}
		counts[ev.DeviceID] = n + 1
		out = append(out, ev)
	}
	return out
}

// certRow is an active certificate for the expiry check.
type certRow struct {
	deviceID    int64
	ip          string
	port        int
	cn          string
	fingerprint string
	notAfter    time.Time
}

// expiryEvent builds the expiry event for a certificate (nil if not due).
func expiryEvent(c certRow, now time.Time, warnDays int, repeat time.Duration, devName string) *plugin.Event {
	left := c.notAfter.Sub(now)
	days := int(math.Floor(left.Hours() / 24))
	if left > time.Duration(warnDays)*24*time.Hour {
		return nil
	}
	ev := &plugin.Event{DeviceID: c.deviceID, DedupWindow: repeat,
		Payload: map[string]any{"ip": c.ip, "port": c.port, "subject": c.cn, "not_after": c.notAfter.Format(time.RFC3339),
			"days_left": days, "fingerprint": c.fingerprint}}
	if left < 0 {
		ev.Type = plugin.EvCertExpired
		ev.Title = fmt.Sprintf("Zertifikat abgelaufen: %s (%s:%d, %s)", c.cn, c.ip, c.port, devName)
		ev.Message = "Abgelaufen am " + c.notAfter.Local().Format("02.01.2006")
		ev.Severity = plugin.SevHigh
	} else {
		ev.Type = plugin.EvCertExpiring
		ev.Title = fmt.Sprintf("Zertifikat läuft in %d Tagen ab: %s (%s:%d, %s)", days, c.cn, c.ip, c.port, devName)
		ev.Message = "Gültig bis " + c.notAfter.Local().Format("02.01.2006 15:04")
		switch {
		case days < 7:
			ev.Severity = plugin.SevHigh
		case days < 14:
			ev.Severity = plugin.SevMedium
		default:
			ev.Severity = plugin.SevLow
		}
	}
	ev.DedupKey = fmt.Sprintf("%s:%s:%s:%d", ev.Type, c.fingerprint, c.ip, c.port)
	return ev
}

// Run implements plugin.Runner: periodic certificate expiry check.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	c := load(rc.Settings)
	if !c.types[plugin.EvCertExpiring] && !c.types[plugin.EvCertExpired] {
		rc.Log.Info("Zertifikats-Events sind deaktiviert")
		return nil
	}
	now := time.Now()
	rows, err := rc.DB.R.QueryContext(ctx, `SELECT device_id, ip, port, subject_cn, fingerprint, not_after FROM certificates
		WHERE gone_at IS NULL AND not_after <= ? ORDER BY not_after`, now.Add(time.Duration(c.warnDays)*24*time.Hour).UnixMilli())
	if err != nil {
		return err
	}
	var certs []certRow
	for rows.Next() {
		var (
			cr certRow
			na int64
		)
		if err := rows.Scan(&cr.deviceID, &cr.ip, &cr.port, &cr.cn, &cr.fingerprint, &na); err != nil {
			rows.Close()
			return err
		}
		cr.notAfter = time.UnixMilli(na)
		certs = append(certs, cr)
	}
	rows.Close()
	cache := &deviceCache{rc: rc, devs: map[int64]*plugin.DeviceInfo{}}
	emitted := 0
	sort.Slice(certs, func(i, j int) bool { return certs[i].notAfter.Before(certs[j].notAfter) })
	for _, cr := range certs {
		dev := cache.get(ctx, cr.deviceID)
		if c.skipDevice(dev) {
			continue
		}
		ev := expiryEvent(cr, now, c.warnDays, c.certRepeat, name(dev, cr.deviceID))
		if ev == nil || !c.types[ev.Type] {
			continue
		}
		id, err := rc.Events.Emit(ctx, *ev)
		if err != nil {
			rc.Log.Warn("Event konnte nicht gespeichert werden", "err", err)
			continue
		}
		if id > 0 {
			emitted++
		}
	}
	rc.SetStat("certificates_checked", len(certs))
	rc.SetStat("events", emitted)
	rc.Log.Info("Zertifikatsablauf geprüft", "certificates", len(certs), "events", emitted)
	return nil
}
