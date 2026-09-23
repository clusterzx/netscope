# Plugin-Entwicklung

Jede Fähigkeit von NetScope ist ein Plugin – auch alle mitgelieferten Scanner, Importer,
Processor und Publisher. Der Core stellt Datenmodell, Scheduler, Event-Bus, Vault,
Regel-Engine, API und UI bereit. Ein Plugin ist ein Go-Paket unter
`internal/plugins/<id>/`, das sich in `init()` registriert und in
`internal/plugins/all/all.go` importiert wird.

## Grundgerüst

```go
package example

import (
	"context"
	"time"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

type Plugin struct{}

func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "example",           // ^[a-z][a-z0-9_]{1,31}$
		Kind:               plugin.KindScanner,  // scanner | importer | processor | publisher
		Name:               "Beispiel",
		Description:        "Was das Plugin tut (deutsch, ein Satz).",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "*/30 * * * *",      // Cron; "" = nur manuell
		DefaultTimeout:     10 * time.Minute,
		DefaultConcurrency: 16,                  // interne Parallelität (rc.Parallelism())
		DefaultRetries:     1,
		Targets:            plugin.TargetDevices, // "", TargetSubnets, TargetDevices
		Presence:           false,               // true = Läufe bestimmen online/offline
		Binaries:           nil,                 // benötigte Systemprogramme, z. B. {"nmap"}
	}
}

func (p *Plugin) Schema() plugin.Schema { return plugin.Schema{Fields: []plugin.Field{ /* … */ }} }

func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error { /* … */ return nil }
```

Fähigkeiten werden über Interfaces angeboten (alle in `internal/plugin/plugin.go`):

| Interface | Methode | Wer |
|---|---|---|
| `plugin.Runner` | `Run(ctx, *RunContext) error` | Scanner, Importer, Processor-Jobs |
| `plugin.Publisher` | `Publish(ctx, *PublishContext, *Notification) error` | Publisher |
| `plugin.ChangeHandler` | `HandleChanges(ctx, *RunContext, []Change) error` | Processor (asynchron, geordnet) |
| `plugin.RunFinishedHandler` | `HandleRunFinished(ctx, *RunContext, RunSummary) error` | Processor |
| `plugin.ActionProvider` | `Actions()`, `RunAction(...)` | Buttons (Plugin-Seite oder pro Gerät) |
| `plugin.SettingsValidator` | `ValidateSettings(Settings) error` | feldübergreifende Prüfung |

## Settings-Schema

Die UI rendert das Formular ausschließlich aus dem Schema. Feldtypen:

| Typ | Go-Wert (`rc.Settings`) | Hinweise |
|---|---|---|
| `string` | `String(key)` | `Multiline`, `Validation.Pattern`, `Validation.Format` (url, host, hostport, ip, email, path, mac, header) |
| `secret` | `String(key)` | verschlüsselt gespeichert, API liefert nur `********` |
| `int` | `Int(key)` | `Validation.Min/Max` |
| `bool` | `Bool(key)` | |
| `cron` | `String(key)` | wird validiert, UI zeigt Klartext-Vorschau |
| `enum` | `String(key)` / `StringList(key)` | `Options` Pflicht, `Multi` für Mehrfachauswahl |
| `string-list` | `StringList(key)` | eine Zeile pro Eintrag, Pattern/Format je Eintrag |
| `subnet-list` | `StringList(key)`, `Prefixes(key)` | CIDR oder einzelne IP, wird normalisiert |
| `credential-ref` | `CredentialID(key)` / `CredentialIDs(key)` | `CredentialTypes` begrenzt die Auswahl, `Multi` für mehrere |
| `duration` | `Duration(key)` | Go-Dauer (`30s`, `5m`); `Validation.Min/Max` in Sekunden |

Weitere Feld-Attribute: `Label` (Pflicht, deutsch), `Description`, `Default`, `Required`,
`Placeholder`, `Group` (Abschnitt im Formular), `Advanced` (eingeklappt),
`VisibleIf` (nur anzeigen, wenn ein anderes Feld bestimmte Werte hat),
`Widget: "file"` (String-Feld mit Datei-Upload, Wert = Pfad unter `/data/uploads`).

Schema-Fehler (doppelte Keys, Enum ohne Optionen, ungültiger Default …) lassen
`plugin.Register` beim Start panicen – sie fallen also sofort auf.

Allgemeine Einstellungen verwaltet der Core selbst, sie gehören **nicht** ins Schema:
aktiv/inaktiv, Cron-Zeitplan, Timeout, Wiederholungen + Backoff, Parallelität, Scope.

## RunContext

| Feld | Bedeutung |
|---|---|
| `Settings` | validierte Einstellungen (typisierte Getter) |
| `Targets` | aufgelöster Scope: `Subnets` (CIDR + Interface), `Devices` (inkl. IPs, MACs, offene Ports), `DeviceMode` |
| `Sink` | `Observe(ctx, *Observation)` – schreibt **sofort** (eine Transaktion pro Aufruf) |
| `Events` | `Emit(ctx, Event)` – nur Processor |
| `Creds` | `Get(ctx, id)` – entschlüsselt ein Vault-Credential |
| `Inventory` | Lesezugriff auf Geräte (`Devices(query)`, `DeviceByIP` …) |
| `DB` | direkter DB-Zugriff – **nur Processor** für eigene Tabellen |
| `DataDir` | persistentes Verzeichnis des Plugins (`/data/plugins/<id>`) |
| `Log` | `slog`-Logger; Einträge landen im Laufprotokoll (UI) |
| `Progress(done, total)`, `AddStat`, `SetStat` | Live-Fortschritt und Laufstatistik |
| `Live(topic, typ, data)` | Live-Nachricht an die Oberfläche (SSE); nur freigegebene Topics (derzeit `health`) |
| `PresenceIncomplete(prefixes…)` | Subnetze, die in diesem Lauf nicht zuverlässig gescannt wurden – dort zählt niemand als „verpasst“ |
| `Parallelism()` | konfigurierte Parallelität, mit `plugin.ForEach` nutzen |
| `Env.PublicURL` | Basis-URL für Deep-Links |

Regeln für `Run`:

- **Streaming:** Ergebnisse pro Host sofort über `rc.Sink.Observe` schreiben, nie bis zum
  Ende sammeln. Systemprogramme über `execx.Stream` aufrufen und die Ausgabe streamend parsen.
- **Kontext respektieren:** Bei `ctx.Done()` zügig zurückkehren (Timeout, Abbruch, Shutdown).
- **Fehler:** Ein zurückgegebener Fehler markiert den Lauf als fehlgeschlagen (Retry laut
  Konfiguration). Einzelne Host-Fehler loggen (`rc.Log.Warn`) und weitermachen; nur wenn
  gar nichts ging, einen Fehler zurückgeben.
- **Nur lesen:** Scanner verändern nichts auf Zielsystemen.

## Observation – was ein Scanner/Importer meldet

`plugin.Observation` (Datei `internal/plugin/observation.go`) beschreibt ein Gerät.

**Identität** (mindestens eins): `DeviceID` (bekanntes Gerät), `MACs`, `Ref`
(Fremdsystem, z. B. Proxmox-VMID), `IP`. Der Core löst in dieser Reihenfolge auf:
DeviceID → MAC → Ref → IP.

- `Present: true` – das Gerät hat *jetzt aktiv geantwortet*. Setzt Letztsichtung/online,
  zählt für die Anwesenheitserkennung und legt unbekannte Geräte an.
- `Create: true` – unbekanntes Gerät auch ohne Anwesenheit anlegen (Importer).
- Leere Felder bedeuten „keine Information“ und löschen nie etwas.

**Vollständigkeit:** Damit der Core „Port geschlossen“, „Zertifikat weg“ usw. erkennen
kann, melden Abschnitte, was geprüft wurde:

- `Ports.Scanned` – exakte gescannte Portbereiche. Offene Ports in diesem Bereich, die
  nicht mehr gemeldet werden, gelten als geschlossen. Leer = nie schließen.
- `HTTP.Scanned`, `TLS.Scanned` – geprüfte Ports.
- `Packages`, `Containers` – immer die vollständige Liste (pro Paketmanager / Engine).

Weitere Abschnitte: `Hostname`, `Vendor`, `Model`, `DeviceType`, `OS` (mit `Accuracy`,
`CPEs`), `Attrs` (skalare Fakten wie `snmp.sysDescr`), `Inventory` (strukturierte Daten,
im Gerät unter „Rohdaten/Inventar“ sichtbar), `Metrics` (Zeitreihen), `Relations`
(Kanten zu anderen, bereits bekannten Geräten), `Manual` (vom Benutzer gepflegte Daten aus
Importen; nur leere Felder werden gefüllt, außer `Overwrite`), `Raw` (Rohausgabe für den
Tab „Rohdaten“).

**Hostname-Priorität:** Der effektive Hostname wird nach Quelle gewählt
(manuell > DHCP (`openwrt`) > `ssh` > `snmp` > `proxmox` > DNS > mDNS > NetBIOS > UPnP …,
in den Systemeinstellungen änderbar). Die Quelle ist die Plugin-ID.

## Changes und Events (Processor)

Aus Observations leitet der Core `plugin.Change`s ab (`device.created`, `port.opened`,
`cert.changed`, `packages.changed`, `device.offline` …, siehe `change.go`). Processor
bekommen sie über `HandleChanges` asynchron und in Reihenfolge. `Initial: true`
kennzeichnet Erstdaten (z. B. der erste Portscan eines Geräts) – dafür werden
üblicherweise keine Events erzeugt.

Events werden mit `rc.Events.Emit` erzeugt. Erlaubte Typen stehen im Katalog
(`internal/plugin/events.go`, API `GET /api/v1/events/types`). `DedupKey` +
`DedupWindow` unterdrücken Wiederholungen (z. B. ein Ablauf-Hinweis pro Tag).
Benachrichtigungen verschickt nie ein Processor selbst – das macht die Regel-Engine.

## Aktionen

`ActionProvider.Actions()` liefert Buttons. `Scope: plugin.ActionPlugin` erscheint auf der
Plugin-Seite (z. B. „OUI-Datei aktualisieren“), `plugin.ActionDevice` beim Gerät bzw. als
Massenaktion (z. B. „Wake-on-LAN“); die Geräte stehen dann in `rc.Targets.Devices`.

## Publisher

`Publish(ctx, pc, n)` erhält eine gebündelte `plugin.Notification` (Titel, Priorität,
Events mit Deep-Links, bei Berichten ein Markdown-`Body`). `n.PlainText()` liefert eine
fertige Textdarstellung. Fehler zurückgeben – der Dispatcher wiederholt.

## Tests

`internal/plugin/plugintest` stellt Fakes bereit:

```go
rc, sink, _ := plugintest.RunContext(t, &Plugin{}, map[string]any{"timeout": "2s"})
rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 1, PrimaryIP: "192.168.8.1"}}}
if err := (&Plugin{}).Run(ctx, rc); err != nil { t.Fatal(err) }
obs := sink.All()
```

Parser werden mit **echten Fixtures** unter `testdata/` getestet
(`plugintest.Fixture(t, "nmap-192.168.8.1.xml")`).

## Checkliste

- [ ] ID, deutsches Label/Beschreibung, sinnvolle Defaults (Zeitplan, Timeout, Parallelität)
- [ ] Schema vollständig, Secrets als `secret`, Zugangsdaten als `credential-ref`
- [ ] Ergebnisse pro Host sofort über den Sink, `Scanned`-Bereiche gesetzt
- [ ] `ctx` wird respektiert, Fehler pro Host geloggt statt abgebrochen
- [ ] Parser-Tests mit echten Fixtures
- [ ] Import in `internal/plugins/all/all.go`
