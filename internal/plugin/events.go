package plugin

import (
	"sort"
	"strings"
	"time"
)

// Severity of an event.
type Severity string

const (
	SevInfo     Severity = "info"
	SevLow      Severity = "low"
	SevMedium   Severity = "medium"
	SevHigh     Severity = "high"
	SevCritical Severity = "critical"
)

// Severities lists all severities in ascending order.
var Severities = []Severity{SevInfo, SevLow, SevMedium, SevHigh, SevCritical}

// Rank returns 0 (info) … 4 (critical), -1 if unknown.
func (s Severity) Rank() int {
	for i, v := range Severities {
		if v == s {
			return i
		}
	}
	return -1
}

// Valid reports whether s is a known severity.
func (s Severity) Valid() bool { return s.Rank() >= 0 }

// Label returns the German label.
func (s Severity) Label() string {
	switch s {
	case SevInfo:
		return "Info"
	case SevLow:
		return "Niedrig"
	case SevMedium:
		return "Mittel"
	case SevHigh:
		return "Hoch"
	case SevCritical:
		return "Kritisch"
	}
	return string(s)
}

// SeverityFromCVSS maps a CVSS base score to a severity.
func SeverityFromCVSS(score float64) Severity {
	switch {
	case score >= 9:
		return SevCritical
	case score >= 7:
		return SevHigh
	case score >= 4:
		return SevMedium
	case score > 0:
		return SevLow
	}
	return SevInfo
}

// Event is emitted by processors. Severity defaults to the catalog default.
type Event struct {
	Type     string         `json:"type"`
	Severity Severity       `json:"severity,omitempty"`
	DeviceID int64          `json:"deviceId,omitempty"`
	RunID    int64          `json:"runId,omitempty"`
	Title    string         `json:"title"`
	Message  string         `json:"message,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
	// DedupKey suppresses the event if an event with the same key exists within
	// DedupWindow (default 24h when a key is set).
	DedupKey    string        `json:"dedupKey,omitempty"`
	DedupWindow time.Duration `json:"-"`
	At          time.Time     `json:"at,omitempty"`
}

// PayloadField documents a payload key of an event type.
type PayloadField struct {
	Key         string `json:"key"`
	Type        string `json:"type"` // string | number | bool | list | object
	Description string `json:"description"`
}

// EventSpec is an entry of the event type catalog.
type EventSpec struct {
	Type            string         `json:"type"`
	Category        string         `json:"category"`
	Label           string         `json:"label"`
	Description     string         `json:"description"`
	DefaultSeverity Severity       `json:"defaultSeverity"`
	Source          string         `json:"source"` // plugin that emits it
	Payload         []PayloadField `json:"payload"`
}

// Event types.
const (
	EvDeviceNew             = "device.new"
	EvDeviceOffline         = "device.offline"
	EvDeviceOnline          = "device.online"
	EvDeviceIPChanged       = "device.ip_changed"
	EvDeviceMACChanged      = "device.mac_changed"
	EvDeviceHostnameChanged = "device.hostname_changed"
	EvDeviceOSChanged       = "device.os_changed"
	EvPortOpened            = "port.opened"
	EvPortClosed            = "port.closed"
	EvServiceChanged        = "service.version_changed"
	EvCertExpiring          = "cert.expiring"
	EvCertExpired           = "cert.expired"
	EvCertChanged           = "cert.changed"
	EvTLSWeak               = "tls.weak"
	EvContainerNew          = "container.new"
	EvContainerRemoved      = "container.removed"
	EvContainerImageChanged = "container.image_changed"
	EvPackagesChanged       = "package.changed"
	EvCVENew                = "cve.new"
	EvCVEResolved           = "cve.resolved"
	EvHealthDown            = "health.down"
	EvHealthDegraded        = "health.degraded"
	EvHealthUp              = "health.up"
	EvPluginFailed          = "plugin.failed"
	EvNVDSyncFailed         = "nvd.sync_failed"
	EvTunnelDown            = "tunnel.down"
	EvTunnelUp              = "tunnel.up"
	EvSiteDown              = "site.down"
	EvSiteUp                = "site.up"
)

var deviceFields = []PayloadField{
	{"device_name", "string", "Anzeigename des Geräts"},
	{"device_ip", "string", "Primäre IP-Adresse"},
	{"device_mac", "string", "Primäre MAC-Adresse"},
	{"device_state", "string", "known | unknown | ignored"},
}

func withDevice(fields ...PayloadField) []PayloadField {
	return append(append([]PayloadField{}, deviceFields...), fields...)
}

var catalog = []EventSpec{
	{EvDeviceNew, "device", "Neues Gerät", "Ein bisher unbekanntes Gerät wurde im Netz gefunden.", SevMedium, "diff",
		withDevice(PayloadField{"vendor", "string", "Hersteller laut OUI"}, PayloadField{"source", "string", "Plugin, das das Gerät gefunden hat"})},
	{EvDeviceOffline, "device", "Gerät weg", "Ein Gerät antwortet bei mehreren Anwesenheits-Scans in Folge nicht mehr.", SevLow, "diff",
		withDevice(PayloadField{"last_seen", "string", "Zeitpunkt der letzten Sichtung (RFC3339)"})},
	{EvDeviceOnline, "device", "Gerät wieder da", "Ein als offline geführtes Gerät antwortet wieder.", SevInfo, "diff",
		withDevice(PayloadField{"offline_since", "string", "Seit wann das Gerät offline war (RFC3339)"}, PayloadField{"offline_seconds", "number", "Dauer der Abwesenheit"})},
	{EvDeviceIPChanged, "device", "IP-Wechsel", "Ein Gerät ist unter einer neuen IP-Adresse erreichbar.", SevLow, "diff",
		withDevice(PayloadField{"old_ip", "string", "Bisherige IP"}, PayloadField{"new_ip", "string", "Neue IP"})},
	{EvDeviceMACChanged, "device", "MAC-Wechsel auf bekannter IP", "Eine bekannte IP-Adresse wird von einer anderen MAC-Adresse beantwortet (Gerätetausch oder ARP-Spoofing).", SevHigh, "diff",
		withDevice(PayloadField{"ip", "string", "Betroffene IP"}, PayloadField{"old_mac", "string", "Bisherige MAC"}, PayloadField{"new_mac", "string", "Neue MAC"}, PayloadField{"old_device_id", "number", "Bisheriges Gerät"})},
	{EvDeviceHostnameChanged, "device", "Hostname geändert", "Der effektive Hostname eines Geräts hat sich geändert.", SevInfo, "diff",
		withDevice(PayloadField{"old", "string", "Bisheriger Hostname"}, PayloadField{"new", "string", "Neuer Hostname"})},
	{EvDeviceOSChanged, "device", "Betriebssystem geändert", "Die OS-Erkennung liefert ein anderes Ergebnis.", SevLow, "diff",
		withDevice(PayloadField{"old", "string", "Bisheriges OS"}, PayloadField{"new", "string", "Neues OS"})},
	{EvPortOpened, "port", "Port neu", "Auf einem Gerät ist ein neuer Port offen.", SevMedium, "diff",
		withDevice(PayloadField{"ip", "string", "IP"}, PayloadField{"port", "number", "Port"}, PayloadField{"proto", "string", "tcp | udp"}, PayloadField{"service", "string", "Dienst"}, PayloadField{"product", "string", "Produkt"}, PayloadField{"version", "string", "Version"})},
	{EvPortClosed, "port", "Port geschlossen", "Ein zuvor offener Port ist geschlossen.", SevLow, "diff",
		withDevice(PayloadField{"ip", "string", "IP"}, PayloadField{"port", "number", "Port"}, PayloadField{"proto", "string", "tcp | udp"}, PayloadField{"service", "string", "Dienst"})},
	{EvServiceChanged, "port", "Dienstversion geändert", "Produkt oder Version eines Dienstes hat sich geändert.", SevLow, "diff",
		withDevice(PayloadField{"ip", "string", "IP"}, PayloadField{"port", "number", "Port"}, PayloadField{"proto", "string", "tcp | udp"}, PayloadField{"old", "string", "Bisher (Produkt Version)"}, PayloadField{"new", "string", "Neu (Produkt Version)"})},
	{EvCertExpiring, "cert", "Zertifikat läuft ab", "Ein Zertifikat läuft in wenigen Tagen ab.", SevMedium, "diff",
		withDevice(PayloadField{"ip", "string", "IP"}, PayloadField{"port", "number", "Port"}, PayloadField{"subject", "string", "CN"}, PayloadField{"not_after", "string", "Ablaufdatum"}, PayloadField{"days_left", "number", "Verbleibende Tage"}, PayloadField{"fingerprint", "string", "SHA-256"})},
	{EvCertExpired, "cert", "Zertifikat abgelaufen", "Ein Zertifikat ist abgelaufen.", SevHigh, "diff",
		withDevice(PayloadField{"ip", "string", "IP"}, PayloadField{"port", "number", "Port"}, PayloadField{"subject", "string", "CN"}, PayloadField{"not_after", "string", "Ablaufdatum"}, PayloadField{"days_left", "number", "Tage (negativ)"}, PayloadField{"fingerprint", "string", "SHA-256"})},
	{EvCertChanged, "cert", "Zertifikat gewechselt", "Auf einem Endpunkt wird ein anderes Zertifikat ausgeliefert.", SevLow, "diff",
		withDevice(PayloadField{"ip", "string", "IP"}, PayloadField{"port", "number", "Port"}, PayloadField{"old_fingerprint", "string", "Bisher"}, PayloadField{"new_fingerprint", "string", "Neu"}, PayloadField{"subject", "string", "CN"}, PayloadField{"not_after", "string", "Ablaufdatum"})},
	{EvTLSWeak, "cert", "Schwache TLS-Konfiguration", "Ein Endpunkt akzeptiert veraltete Protokolle oder Cipher.", SevMedium, "diff",
		withDevice(PayloadField{"ip", "string", "IP"}, PayloadField{"port", "number", "Port"}, PayloadField{"weak_protocols", "list", "Veraltete Protokolle"}, PayloadField{"weak_ciphers", "list", "Schwache Cipher"})},
	{EvContainerNew, "container", "Container neu", "Auf einem Host läuft ein neuer Container.", SevInfo, "diff",
		withDevice(PayloadField{"name", "string", "Containername"}, PayloadField{"image", "string", "Image"}, PayloadField{"compose_project", "string", "Compose-Projekt"})},
	{EvContainerRemoved, "container", "Container weg", "Ein Container ist nicht mehr vorhanden.", SevInfo, "diff",
		withDevice(PayloadField{"name", "string", "Containername"}, PayloadField{"image", "string", "Image"})},
	{EvContainerImageChanged, "container", "Container-Image geändert", "Ein Container läuft mit einem anderen Image.", SevInfo, "diff",
		withDevice(PayloadField{"name", "string", "Containername"}, PayloadField{"old_image", "string", "Bisher"}, PayloadField{"new_image", "string", "Neu"})},
	{EvPackagesChanged, "software", "Paket-Änderungen", "Auf einem SSH-Host wurden Pakete installiert, entfernt oder aktualisiert.", SevInfo, "diff",
		withDevice(PayloadField{"manager", "string", "dpkg | rpm | apk"}, PayloadField{"count", "number", "Anzahl"}, PayloadField{"added", "list", "Neu"}, PayloadField{"removed", "list", "Entfernt"}, PayloadField{"updated", "list", "Aktualisiert"})},
	{EvCVENew, "vulnerability", "Neue Schwachstelle", "Für ein Gerät wurde eine neue CVE gefunden (heuristischer Versionsabgleich).", SevHigh, "cve",
		withDevice(PayloadField{"cve", "string", "CVE-ID"}, PayloadField{"cvss", "number", "CVSS-Basiswert"}, PayloadField{"vector", "string", "CVSS-Vektor"}, PayloadField{"product", "string", "Produkt"}, PayloadField{"version", "string", "Version"}, PayloadField{"cpe", "string", "CPE"}, PayloadField{"match_type", "string", "exact | range | heuristic"})},
	{EvCVEResolved, "vulnerability", "Schwachstelle behoben", "Eine CVE trifft nach Update/Änderung nicht mehr zu.", SevInfo, "cve",
		withDevice(PayloadField{"cve", "string", "CVE-ID"}, PayloadField{"cvss", "number", "CVSS-Basiswert"})},
	{EvHealthDown, "health", "Check ausgefallen", "Ein Health-Check ist nach Flap-Dämpfung im Zustand Down.", SevHigh, "healthcheck",
		withDevice(PayloadField{"check_id", "number", "Check-ID"}, PayloadField{"check_name", "string", "Name"}, PayloadField{"check_type", "string", "tcp | http | tls | icmp"}, PayloadField{"error", "string", "Fehler"})},
	{EvHealthDegraded, "health", "Check beeinträchtigt", "Ein Health-Check ist erreichbar, aber langsam oder fehlerhaft.", SevMedium, "healthcheck",
		withDevice(PayloadField{"check_id", "number", "Check-ID"}, PayloadField{"check_name", "string", "Name"}, PayloadField{"latency_ms", "number", "Latenz"}, PayloadField{"error", "string", "Grund"})},
	{EvHealthUp, "health", "Check wieder OK", "Ein Health-Check ist wieder Up.", SevInfo, "healthcheck",
		withDevice(PayloadField{"check_id", "number", "Check-ID"}, PayloadField{"check_name", "string", "Name"}, PayloadField{"down_seconds", "number", "Dauer des Ausfalls"})},
	{EvPluginFailed, "system", "Plugin-Lauf fehlgeschlagen", "Ein Plugin-Lauf ist nach allen Wiederholungen fehlgeschlagen.", SevMedium, "core",
		[]PayloadField{{"plugin", "string", "Plugin-ID"}, {"run_id", "number", "Lauf"}, {"error", "string", "Fehler"}, {"attempt", "number", "Versuch"}}},
	{EvNVDSyncFailed, "system", "NVD-Sync fehlgeschlagen", "Der Abgleich der lokalen NVD-Kopie ist fehlgeschlagen.", SevMedium, "cve",
		[]PayloadField{{"error", "string", "Fehler"}, {"feed", "string", "Feed"}}},
	{EvTunnelDown, "system", "Tunnel getrennt", "Ein WireGuard-Tunnel in ein entferntes Subnetz ist getrennt oder lässt sich nicht aufbauen. Die Geräte dahinter werden solange nicht als offline gewertet.", SevHigh, "core",
		[]PayloadField{{"tunnel", "string", "Name des Tunnels"}, {"credential_id", "number", "Credential des Tunnels"}, {"subnets", "list", "Subnetze hinter dem Tunnel"}, {"endpoint", "string", "Gegenstelle"}, {"error", "string", "Fehler (leer = keine Antwort der Gegenstelle)"}}},
	{EvTunnelUp, "system", "Tunnel wieder verbunden", "Ein getrennter WireGuard-Tunnel steht wieder.", SevInfo, "core",
		[]PayloadField{{"tunnel", "string", "Name des Tunnels"}, {"credential_id", "number", "Credential des Tunnels"}, {"subnets", "list", "Subnetze hinter dem Tunnel"}, {"down_seconds", "number", "Dauer der Unterbrechung"}}},
	{EvSiteDown, "system", "Standort meldet sich nicht", "Ein NetScope-Standort hat sich mehrere Minuten nicht bei der Zentrale gemeldet (Netz, Dienst oder Token). Er puffert seine Daten und liefert sie nach.", SevHigh, "core",
		[]PayloadField{{"site", "string", "Name des Standorts"}, {"site_id", "number", "Standort"}, {"last_contact", "string", "Letzte Meldung (RFC3339)"}}},
	{EvSiteUp, "system", "Standort meldet sich wieder", "Ein NetScope-Standort liefert wieder an die Zentrale.", SevInfo, "core",
		[]PayloadField{{"site", "string", "Name des Standorts"}, {"site_id", "number", "Standort"}, {"down_seconds", "number", "Dauer ohne Meldung"}}},
}

// Catalog returns the event type catalog sorted by category and type.
func Catalog() []EventSpec {
	out := append([]EventSpec(nil), catalog...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Type < out[j].Type
	})
	return out
}

// LookupEvent returns the catalog entry of an event type.
func LookupEvent(typ string) (EventSpec, bool) {
	for _, e := range catalog {
		if e.Type == typ {
			return e, true
		}
	}
	return EventSpec{}, false
}

// MatchEventType matches an event type against a pattern ("port.*", "*", exact).
func MatchEventType(pattern, typ string) bool {
	if pattern == "*" || pattern == typ {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		return strings.HasPrefix(typ, strings.TrimSuffix(pattern, "*"))
	}
	return false
}
