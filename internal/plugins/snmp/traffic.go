package snmp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"

	"netscope/internal/plugin"
)

// Traffic polls the interface counters of IF-MIB and stores traffic (bit/s), utilisation,
// errors and discards per interface as time series. Rates are computed from the counter
// difference to the previous poll (kept in the plugin's data directory), so the first
// poll of a device only records the counters.

func init() { plugin.Register(&Traffic{}) }

// counter OIDs (64-bit ifXTable, 32-bit ifTable as fallback)
const (
	oidIfHCInOctets  = ".1.3.6.1.2.1.31.1.1.1.6"
	oidIfHCOutOctets = ".1.3.6.1.2.1.31.1.1.1.10"
	oidIfInOctets    = ".1.3.6.1.2.1.2.2.1.10"
	oidIfOutOctets   = ".1.3.6.1.2.1.2.2.1.16"
	oidIfInDiscards  = ".1.3.6.1.2.1.2.2.1.13"
	oidIfInErrors    = ".1.3.6.1.2.1.2.2.1.14"
	oidIfOutDiscards = ".1.3.6.1.2.1.2.2.1.19"
	oidIfOutErrors   = ".1.3.6.1.2.1.2.2.1.20"
)

// Metric names.
const (
	MetricInBps    = "if.in_bps"
	MetricOutBps   = "if.out_bps"
	MetricUtil     = "if.util_pct"
	MetricErrors   = "if.errors"   // per minute, in + out
	MetricDiscards = "if.discards" // per minute, in + out
)

// rates are only computed for polls this far apart
const (
	minGap = 20 * time.Second
	maxGap = 2 * time.Hour
)

// retryAfter spaces out attempts at devices that do not answer, so that phones and PCs
// without SNMP are not asked every five minutes.
const retryAfter = time.Hour

// physicalTypes are the IANAifTypes of real ports.
var physicalTypes = map[int]bool{6: true, 62: true, 69: true, 117: true, 161: true, 71: true, 7: true}

// Traffic is the SNMP traffic plugin.
type Traffic struct {
	now func() time.Time

	mu     sync.Mutex
	state  map[int64]*devState // loaded from the data directory on the first run
	misses map[int64]time.Time // last failed poll of devices without recent counters
	file   string
}

type ifState struct {
	In, Out, Errors, Discards uint64
	HC                        bool   // In/Out are 64-bit counters
	Oper                      string // up | down | …
}

type devState struct {
	At     time.Time
	Uptime uint32 // sysUpTime in hundredths of a second
	Ifs    map[int]ifState
}

// stateFile is the content of traffic-state.json.
type stateFile struct {
	Devices map[int64]*devState `json:"devices"`
	Misses  map[int64]time.Time `json:"misses,omitempty"`
}

// Info implements plugin.Plugin.
func (t *Traffic) Info() plugin.Info {
	return plugin.Info{
		ID:   "snmp_traffic",
		Kind: plugin.KindScanner,
		Name: "SNMP-Traffic",
		Description: "Misst per SNMP den Datenverkehr, die Auslastung, Fehler und Verwürfe je Interface (IF-MIB) und meldet " +
			"Port-Ausfälle und überlastete Ports.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "*/5 * * * *",
		DefaultTimeout:     4 * time.Minute,
		DefaultConcurrency: 16,
		DefaultRetries:     0,
		Targets:            plugin.TargetDevices,
	}
}

// Schema implements plugin.Plugin.
func (t *Traffic) Schema() plugin.Schema {
	base := (&Plugin{}).Schema().Fields
	var fields []plugin.Field
	for _, f := range base {
		if f.Group == "Verbindung" {
			fields = append(fields, f)
		}
	}
	return plugin.Schema{Fields: append(fields,
		plugin.Field{Key: "interfaces", Type: plugin.FieldEnum, Label: "Interfaces", Default: "physical", Group: "Umfang", Options: []plugin.Option{
			{Value: "physical", Label: "Physische Ports, WLAN und Link-Aggregationen"},
			{Value: "up", Label: "Alle, die gerade verbunden sind"},
			{Value: "all", Label: "Alle (auch VLAN- und virtuelle Interfaces)"},
		}, Description: "Welche Interfaces gemessen werden; abgeschaltete (admin down) nie."},
		plugin.Field{Key: "exclude", Type: plugin.FieldStringList, Label: "Interfaces auslassen", Default: []string{}, Group: "Umfang",
			Description: "Namen mit Platzhaltern, z. B. vlan* oder Loopback*; Groß-/Kleinschreibung egal."},
		plugin.Field{Key: "saturation_pct", Type: plugin.FieldInt, Label: "Überlastet ab (%)", Default: 90, Group: "Events",
			Description: "Event, wenn ein Port in eine Richtung mindestens so stark ausgelastet ist (0 = aus).",
			Validation:  &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(100)}},
		plugin.Field{Key: "port_events", Type: plugin.FieldBool, Label: "Port-Ausfälle melden", Default: true, Group: "Events",
			Description: "Event, wenn ein eingeschalteter Port die Verbindung verliert oder wiederbekommt."},
		plugin.Field{Key: "port_events_described", Type: plugin.FieldBool, Label: "Nur Ports mit Beschreibung", Default: true, Group: "Events",
			Description: "Nur Ports mit Beschreibung (ifAlias) melden – typischerweise Uplinks und Server; Arbeitsplätze, die abends ausgehen, bleiben still.",
			VisibleIf:   &plugin.Condition{Field: "port_events", Equals: []any{true}}},
	)}
}

func (t *Traffic) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// load reads the counter state of the previous polls.
func (t *Traffic) load(dir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	file := filepath.Join(dir, "traffic-state.json")
	if t.state != nil && t.file == file {
		return
	}
	var sf stateFile
	if data, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(data, &sf)
	}
	t.file, t.state, t.misses = file, sf.Devices, sf.Misses
	if t.state == nil {
		t.state = map[int64]*devState{}
	}
	if t.misses == nil {
		t.misses = map[int64]time.Time{}
	}
}

func (t *Traffic) save() error {
	t.mu.Lock()
	data, err := json.Marshal(stateFile{Devices: t.state, Misses: t.misses})
	file := t.file
	t.mu.Unlock()
	if err != nil || file == "" {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// waiting reports whether a device that failed recently is left out of this run. A device
// with fresh counters is always asked again, so a switch that reboots is not skipped.
func (t *Traffic) waiting(id int64, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	miss, ok := t.misses[id]
	if !ok {
		return false
	}
	if st := t.state[id]; st != nil && now.Sub(st.At) < maxGap {
		return false
	}
	return now.Sub(miss) < retryAfter
}

// Run implements plugin.Runner.
func (t *Traffic) Run(ctx context.Context, rc *plugin.RunContext) error {
	cfg := loadConfig(rc.Settings, rc.DataDir)
	cfg.storeFile = filepath.Join(rc.DataDir, "credentials.json")
	t.load(rc.DataDir)
	picker := &plugin.CredentialPicker{Creds: rc.Creds, Types: []string{plugin.CredSNMPv2c, plugin.CredSNMPv3}, Allowed: cfg.credIDs, Log: rc.Log,
		Check: func(c *plugin.Credential) error { _, err := newClient(ctx, "127.0.0.1", c, cfg.client); return err }}
	store, err := loadCredStore(cfg.storeFile)
	if err != nil {
		rc.Log.Warn("gemerkte Credentials nicht lesbar – beginne neu", "error", err)
	}
	var targets []target
	now := t.clock()
	skipped := 0
	for _, d := range rc.Targets.Devices {
		if t.waiting(d.ID, now) {
			skipped++
			continue
		}
		ip := d.PrimaryIP
		if ip == "" && len(d.IPs) > 0 {
			ip = d.IPs[0]
		}
		if ip != "" {
			targets = append(targets, target{dev: d, ip: ip})
		}
	}
	rc.SetStat("targets", len(targets))
	if skipped > 0 {
		rc.SetStat("waiting", skipped)
	}
	var ok, failed atomic.Int64
	runErr := plugin.ForEach(ctx, rc.Parallelism(), targets, func(ctx context.Context, tg target) error {
		err := t.poll(ctx, rc, cfg, picker, store, tg)
		switch {
		case err == nil:
			ok.Add(1)
		case ctx.Err() != nil:
		case errors.Is(err, errNoCredential):
			rc.AddStat("no_credential", 1)
		default:
			failed.Add(1)
			t.mu.Lock()
			t.misses[tg.dev.ID] = t.clock()
			t.mu.Unlock()
			rc.Log.Debug("keine SNMP-Antwort", "device", tg.dev.Name, "ip", tg.ip, "error", err)
		}
		return nil
	})
	if err := store.save(); err != nil {
		rc.Log.Warn("gemerkte Credentials nicht speicherbar", "error", err)
	}
	if err := t.save(); err != nil {
		rc.Log.Warn("Zählerstände nicht speicherbar", "error", err)
	}
	if runErr != nil {
		return runErr
	}
	rc.SetStat("answered", ok.Load())
	rc.SetStat("no_answer", failed.Load())
	if ok.Load() == 0 && failed.Load() > 0 {
		return fmt.Errorf("kein Gerät hat per SNMP geantwortet (%d ohne Antwort)", failed.Load())
	}
	return nil
}

// sample is one poll of a device.
type sample struct {
	uptime uint32
	ifs    []Interface
	ctr    map[int]ifState
}

// poll reads one device and records metrics and events.
func (t *Traffic) poll(ctx context.Context, rc *plugin.RunContext, cfg config, picker *plugin.CredentialPicker, store *credStore, tg target) error {
	creds, err := picker.For(ctx, plugin.CredentialTarget{DeviceID: tg.dev.ID, IP: tg.ip})
	if err != nil {
		return err
	}
	if len(creds) == 0 {
		return errNoCredential
	}
	var s *sample
	var errs []string
	for _, c := range orderedCredentials(store.get(tg.dev.ID), creds, func(c *plugin.Credential) int64 { return c.ID }) {
		g, err := newClient(ctx, tg.ip, c, cfg.client)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		s, err = readCounters(g)
		g.Close()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.Name, err))
			continue
		}
		store.set(tg.dev.ID, c.ID)
		break
	}
	if s == nil {
		return errors.New(strings.Join(errs, "; "))
	}
	now := t.clock()
	t.mu.Lock()
	prev := t.state[tg.dev.ID]
	t.state[tg.dev.ID] = &devState{At: now, Uptime: s.uptime, Ifs: s.ctr}
	delete(t.misses, tg.dev.ID)
	t.mu.Unlock()
	metrics, events := evaluate(rc.Settings, tg.dev, prev, s, now)
	rc.AddStat("interfaces", len(s.ctr))
	if len(metrics) > 0 {
		if _, err := rc.Sink.Observe(ctx, &plugin.Observation{DeviceID: tg.dev.ID, Target: tg.ip, Metrics: metrics}); err != nil {
			return fmt.Errorf("Messwerte speichern: %w", err)
		}
		rc.AddStat("metrics", len(metrics))
	}
	for _, ev := range events {
		ev.RunID = rc.RunID
		if rc.Events == nil {
			break
		}
		if _, err := rc.Events.Emit(ctx, ev); err != nil {
			rc.Log.Warn("Event konnte nicht gespeichert werden", "type", ev.Type, "error", err)
		}
	}
	return nil
}

// readCounters fetches uptime, interface table and counters.
func readCounters(g *gosnmp.GoSNMP) (*sample, error) {
	if err := g.Connect(); err != nil {
		return nil, err
	}
	res, err := g.Get([]string{oidSysUpTime})
	if err != nil {
		return nil, err
	}
	s := &sample{ctr: map[int]ifState{}}
	if len(res.Variables) == 1 {
		if n, ok := pduInt(res.Variables[0]); ok {
			s.uptime = uint32(n)
		}
	}
	w := &walker{g: g}
	if !w.table(TableInterfaces, oidIfName, oidIfDescr, oidIfAlias, oidIfType, oidIfSpeed, oidIfHighSpeed, oidIfAdminStatus, oidIfOperStatus) {
		return nil, fmt.Errorf("Interface-Tabelle: %s", w.errors[TableInterfaces])
	}
	// 64-bit counters where the agent has them, 32-bit ones otherwise
	w.table("hc", oidIfHCInOctets, oidIfHCOutOctets)
	w.table("counters", oidIfInOctets, oidIfOutOctets, oidIfInErrors, oidIfOutErrors, oidIfInDiscards, oidIfOutDiscards)
	s.ifs = decodeInterfaces(w.pdus)
	s.ctr = decodeCounters(w.pdus)
	// the link state is compared with the next poll
	for _, ifc := range s.ifs {
		st := s.ctr[ifc.Index]
		st.Oper = ifc.OperStatus
		s.ctr[ifc.Index] = st
	}
	return s, nil
}

func pduUint(p gosnmp.SnmpPDU) (uint64, bool) {
	switch v := p.Value.(type) {
	case uint64:
		return v, true
	case uint:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case int:
		return uint64(v), v >= 0
	case int64:
		return uint64(v), v >= 0
	}
	return 0, false
}

// decodeCounters collects the counters per ifIndex (64-bit ones win).
func decodeCounters(pdus []gosnmp.SnmpPDU) map[int]ifState {
	out := map[int]ifState{}
	upd := func(oid string, fn func(st *ifState, v uint64)) {
		for idx, p := range column(pdus, oid) {
			n, err := strconv.Atoi(idx)
			v, ok := pduUint(p)
			if err != nil || !ok || isException(p) {
				continue
			}
			st := out[n]
			fn(&st, v)
			out[n] = st
		}
	}
	upd(oidIfInOctets, func(st *ifState, v uint64) {
		if !st.HC {
			st.In = v
		}
	})
	upd(oidIfOutOctets, func(st *ifState, v uint64) {
		if !st.HC {
			st.Out = v
		}
	})
	upd(oidIfHCInOctets, func(st *ifState, v uint64) { st.In, st.HC = v, true })
	upd(oidIfHCOutOctets, func(st *ifState, v uint64) { st.Out = v })
	upd(oidIfInErrors, func(st *ifState, v uint64) { st.Errors += v })
	upd(oidIfOutErrors, func(st *ifState, v uint64) { st.Errors += v })
	upd(oidIfInDiscards, func(st *ifState, v uint64) { st.Discards += v })
	upd(oidIfOutDiscards, func(st *ifState, v uint64) { st.Discards += v })
	return out
}

// delta is the increase of a counter; ok is false after a reset (64-bit) – a 32-bit
// counter that went backwards wrapped once.
func delta(prev, cur uint64, hc bool) (uint64, bool) {
	if cur >= prev {
		return cur - prev, true
	}
	if hc || prev > 1<<32 {
		return 0, false
	}
	return cur + (1 << 32) - prev, true
}

// ifKey is the series key of an interface: its name, made unique with the index.
func ifKeys(ifs []Interface) map[int]string {
	seen := map[string]int{}
	for _, ifc := range ifs {
		seen[ifc.Name]++
	}
	out := map[int]string{}
	for _, ifc := range ifs {
		k := ifc.Name
		if k == "" || seen[k] > 1 {
			k = fmt.Sprintf("%s#%d", ifc.Name, ifc.Index)
		}
		out[ifc.Index] = k
	}
	return out
}

// selected reports whether an interface is measured.
func selected(s plugin.Settings, ifc Interface) bool {
	if ifc.AdminStatus == "down" || ifc.Type == 24 { // switched off, loopback
		return false
	}
	for _, pat := range s.StringList("exclude") {
		if ok, _ := path.Match(strings.ToLower(strings.TrimSpace(pat)), strings.ToLower(ifc.Name)); ok {
			return false
		}
	}
	switch s.String("interfaces") {
	case "all":
		return true
	case "up":
		return ifc.OperStatus == "up"
	}
	return physicalTypes[ifc.Type]
}

// evaluate turns two polls into metrics and events.
func evaluate(s plugin.Settings, dev plugin.DeviceInfo, prev *devState, cur *sample, now time.Time) ([]plugin.Metric, []plugin.Event) {
	if prev == nil {
		return nil, nil // the first poll only records the counters
	}
	gap := now.Sub(prev.At)
	rebooted := cur.uptime < prev.Uptime
	rates := gap >= minGap && gap <= maxGap && !rebooted
	keys := ifKeys(cur.ifs)
	secs := gap.Seconds()
	sat := float64(s.Int("saturation_pct"))
	var metrics []plugin.Metric
	var events []plugin.Event
	add := func(name, key, unit string, v float64) {
		metrics = append(metrics, plugin.Metric{Name: name, Key: key, Unit: unit, Min: v, Avg: v, Max: v, At: now})
	}
	for _, ifc := range cur.ifs {
		if !selected(s, ifc) {
			continue
		}
		c, ok := cur.ctr[ifc.Index]
		p, had := prev.Ifs[ifc.Index]
		if !ok || !had {
			continue
		}
		key := keys[ifc.Index]
		if !rates || p.HC != c.HC {
			continue
		}
		in, ok1 := delta(p.In, c.In, c.HC)
		out, ok2 := delta(p.Out, c.Out, c.HC)
		if !ok1 || !ok2 {
			continue
		}
		inBps, outBps := float64(in)*8/secs, float64(out)*8/secs
		add(MetricInBps, key, "bit/s", inBps)
		add(MetricOutBps, key, "bit/s", outBps)
		if e, ok := delta(p.Errors, c.Errors, true); ok {
			add(MetricErrors, key, "1/min", float64(e)/secs*60)
		}
		if d, ok := delta(p.Discards, c.Discards, true); ok {
			add(MetricDiscards, key, "1/min", float64(d)/secs*60)
		}
		if ifc.SpeedMbps > 0 {
			util := max(inBps, outBps) / (float64(ifc.SpeedMbps) * 1e6) * 100
			util = min(util, 100)
			add(MetricUtil, key, "%", util)
			if sat > 0 && util >= sat {
				dir := "eingehend"
				if outBps > inBps {
					dir = "ausgehend"
				}
				events = append(events, plugin.Event{Type: plugin.EvInterfaceSaturated, Severity: plugin.SevMedium, DeviceID: dev.ID,
					Title:   fmt.Sprintf("%s auf %s zu %.0f %% ausgelastet", label(ifc), dev.Name, util),
					Message: fmt.Sprintf("%s %s bei %s Mbit/s Portgeschwindigkeit.", formatBps(max(inBps, outBps)), dir, strconv.FormatInt(ifc.SpeedMbps, 10)),
					Payload: map[string]any{"interface": ifc.Name, "alias": ifc.Alias, "util_pct": util, "direction": dir, "speed_mbps": ifc.SpeedMbps,
						"in_bps": inBps, "out_bps": outBps},
					DedupKey: fmt.Sprintf("interface.saturated:%d:%s", dev.ID, key), DedupWindow: time.Hour})
			}
		}
	}
	events = append(events, linkEvents(s, dev, prev, cur, keys)...)
	return metrics, events
}

// linkEvents compares the operational state of described (or all) switched-on ports.
func linkEvents(s plugin.Settings, dev plugin.DeviceInfo, prev *devState, cur *sample, keys map[int]string) []plugin.Event {
	if !s.Bool("port_events") {
		return nil
	}
	var out []plugin.Event
	for _, ifc := range cur.ifs {
		if !selected(s, ifc) || (s.Bool("port_events_described") && ifc.Alias == "") {
			continue
		}
		p, had := prev.Ifs[ifc.Index]
		if !had || p.Oper == "" || ifc.OperStatus == "" || p.Oper == ifc.OperStatus {
			continue
		}
		payload := map[string]any{"interface": ifc.Name, "alias": ifc.Alias, "speed_mbps": ifc.SpeedMbps, "previous": p.Oper, "status": ifc.OperStatus}
		switch {
		case p.Oper == "up" && ifc.OperStatus != "up":
			out = append(out, plugin.Event{Type: plugin.EvInterfaceDown, Severity: plugin.SevMedium, DeviceID: dev.ID,
				Title:   fmt.Sprintf("%s auf %s hat keine Verbindung mehr", label(ifc), dev.Name),
				Message: fmt.Sprintf("Port-Status: %s (vorher up).", ifc.OperStatus), Payload: payload,
				DedupKey: fmt.Sprintf("interface.down:%d:%s", dev.ID, keys[ifc.Index])})
		case p.Oper != "up" && ifc.OperStatus == "up":
			out = append(out, plugin.Event{Type: plugin.EvInterfaceUp, Severity: plugin.SevInfo, DeviceID: dev.ID,
				Title:    fmt.Sprintf("%s auf %s ist wieder verbunden", label(ifc), dev.Name),
				Message:  fmt.Sprintf("Port-Status: up (vorher %s).", p.Oper),
				Payload:  payload,
				DedupKey: fmt.Sprintf("interface.up:%d:%s", dev.ID, keys[ifc.Index])})
		}
	}
	return out
}

func label(ifc Interface) string {
	if ifc.Alias != "" && ifc.Alias != ifc.Name {
		return fmt.Sprintf("Port %s (%s)", ifc.Name, ifc.Alias)
	}
	return "Port " + ifc.Name
}

func formatBps(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1f Gbit/s", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1f Mbit/s", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1f kbit/s", v/1e3)
	}
	return fmt.Sprintf("%.0f bit/s", v)
}
