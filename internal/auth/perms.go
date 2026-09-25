package auth

// Permission keys. Reading inventory, topology, events, health, vulnerabilities, rules,
// plugins and reports needs no permission: every signed-in user may read them.
const (
	PermDevicesEdit     = "devices.edit"
	PermDevicesDelete   = "devices.delete"
	PermDevicesScan     = "devices.scan"
	PermDevicesActions  = "devices.actions"
	PermInventoryConfig = "inventory.config"
	PermEventsAck       = "events.ack"
	PermHealthManage    = "health.manage"
	PermVulnsManage     = "vulns.manage"
	PermRulesManage     = "rules.manage"
	PermReportsSend     = "reports.send"
	PermPluginsManage   = "plugins.manage"
	PermCredentialsView = "credentials.view"
	PermCredentialsEdit = "credentials.manage"
	PermNetworkManage   = "network.manage"
	PermSitesManage     = "sites.manage"
	PermSystemManage    = "system.manage"
	PermBackupsManage   = "backups.manage"
	PermAuditView       = "audit.view"
	PermTokensCreate    = "tokens.create"
	PermUsersManage     = "users.manage"
)

// Permission describes a permission for the role editor.
type Permission struct {
	Key   string `json:"key"`
	Group string `json:"group"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	// Critical permissions reach secrets or the whole system (plugins use vault
	// credentials, a backup contains everything, user management can grant any right).
	Critical bool `json:"critical,omitempty"`
}

// Permissions is the catalogue in display order.
var Permissions = []Permission{
	{Key: PermDevicesEdit, Group: "Inventar", Label: "Geräte bearbeiten",
		Hint: "Anlegen, Name, Tags, Notizen, Felder, Zustand, IP-Adressen und Topologie-Verbindungen ändern"},
	{Key: PermDevicesDelete, Group: "Inventar", Label: "Geräte löschen und zusammenführen",
		Hint: "Löschen, Zusammenführen und Aufteilen von Geräten"},
	{Key: PermDevicesScan, Group: "Inventar", Label: "Scans starten",
		Hint: "Geräte scannen sowie Plugin-Läufe starten und abbrechen"},
	{Key: PermDevicesActions, Group: "Inventar", Label: "Geräteaktionen ausführen",
		Hint: "Aktionen der Plugins an Geräten, z. B. Wake-on-LAN"},
	{Key: PermInventoryConfig, Group: "Inventar", Label: "Gruppen, Felder und Ansichten verwalten",
		Hint: "Gruppen, benutzerdefinierte Felder und gespeicherte Ansichten der Geräteliste"},
	{Key: PermEventsAck, Group: "Überwachung", Label: "Events quittieren"},
	{Key: PermHealthManage, Group: "Überwachung", Label: "Health-Checks verwalten",
		Hint: "Anlegen, ändern, löschen und von Hand ausführen"},
	{Key: PermVulnsManage, Group: "Überwachung", Label: "Schwachstellen bewerten",
		Hint: "CVEs als irrelevant markieren und wieder freigeben"},
	{Key: PermRulesManage, Group: "Überwachung", Label: "Regeln verwalten",
		Hint: "Benachrichtigungsregeln anlegen, ändern, sortieren und testen"},
	{Key: PermReportsSend, Group: "Überwachung", Label: "Berichte versenden"},
	{Key: PermPluginsManage, Group: "Konfiguration", Label: "Plugins konfigurieren", Critical: true,
		Hint: "Einstellungen, Zeitpläne, Plugin-Aktionen und Uploads – Plugins nutzen die Credentials aus dem Vault"},
	{Key: PermCredentialsView, Group: "Konfiguration", Label: "Credentials einsehen",
		Hint: "Name, Typ, Benutzer und Geltungsbereich – nie die Geheimnisse"},
	{Key: PermCredentialsEdit, Group: "Konfiguration", Label: "Credentials verwalten", Critical: true,
		Hint: "Anlegen, ändern und löschen"},
	{Key: PermNetworkManage, Group: "Konfiguration", Label: "Subnetze und Tunnel verwalten", Critical: true,
		Hint: "Legt fest, welche Netze gescannt werden"},
	{Key: PermSitesManage, Group: "Konfiguration", Label: "Verbund und Standorte verwalten", Critical: true,
		Hint: "Rolle der Instanz, Standorte und ihre Tokens"},
	{Key: PermSystemManage, Group: "System", Label: "Systemeinstellungen ändern", Critical: true,
		Hint: "Einstellungen, Log-Level und Rotation des Vault-Schlüssels"},
	{Key: PermBackupsManage, Group: "System", Label: "Backups verwalten", Critical: true,
		Hint: "Erstellen, herunterladen, löschen und wiederherstellen – ein Backup enthält alle Daten"},
	{Key: PermAuditView, Group: "System", Label: "Audit-Log und Server-Protokoll einsehen"},
	{Key: PermTokensCreate, Group: "System", Label: "Eigene API-Tokens anlegen",
		Hint: "Ein Token hat höchstens die Rechte seines Benutzers"},
	{Key: PermUsersManage, Group: "System", Label: "Benutzer und Rollen verwalten", Critical: true,
		Hint: "Wer das darf, kann sich selbst jedes Recht geben"},
}

var permIndex = func() map[string]bool {
	m := make(map[string]bool, len(Permissions))
	for _, p := range Permissions {
		m[p.Key] = true
	}
	return m
}()

// ValidPermission reports whether key is a known permission.
func ValidPermission(key string) bool { return permIndex[key] }

// AllPermissions returns every permission key.
func AllPermissions() []string {
	out := make([]string, len(Permissions))
	for i, p := range Permissions {
		out[i] = p.Key
	}
	return out
}
