# NetScope – Architektur

## Überblick

NetScope ist ein einzelnes Go-Binary (ohne CGO) mit eingebetteter SvelteKit-Oberfläche und
SQLite-Datenbank (WAL). Der **Core** stellt Datenmodell, Scheduler, Event-Bus,
Credential-Vault, Regel-Engine, API und UI-Shell bereit; **jede Fähigkeit ist ein Plugin**.

```
               ┌──────────── Web-UI (SvelteKit, embed) ────────────┐
Browser ◄────► │  /api/v1 (JSON) · /api/v1/stream (SSE) · /metrics │
               └───────────────────────┬────────────────────────────┘
                                       │
   Scheduler (Cron) ─► Worker-Pool ─► Plugin.Run(ctx, RunContext)
                                       │  rc.Sink.Observe(obs)        (pro Host, sofort)
                                       ▼
                         Inventory: Beobachtung → Zustand (temporale Tabellen)
                                       │  Changes (port.opened, cert.changed …)
                                       ▼
                         Processor (diff, cve, …) ─► Events ─► Regel-Engine ─► Publisher
```

Datenfluss eines Scans: Der Scheduler reiht einen Lauf ein; der Worker-Pool führt ihn aus
(nie parallel zum selben Plugin, globale Parallelität einstellbar). Das Plugin schreibt
jedes Ergebnis sofort über den Sink. Der Core ordnet die Beobachtung einem Gerät zu
(DeviceID › MAC › externe Referenz › IP), speichert sie roh (Tab „Rohdaten“), überträgt sie
in die Zustandstabellen und leitet daraus *Changes* ab. Processor erhalten die Changes
asynchron und erzeugen Events; neue Events laufen durch die Regel-Engine, die gebündelte
Benachrichtigungen plant und über Publisher zustellt.

## Verzeichnisstruktur

| Pfad | Inhalt |
|---|---|
| `cmd/netscope` | Einstiegspunkt und CLI (`serve`, `token create`, `passwd`, `healthcheck`, `openapi`) |
| `internal/app` | Verdrahtung, Start/Shutdown, In-Process-Neustart nach Restore |
| `internal/config` | Bootstrap-Konfiguration (`/data/config.yaml`, `NETSCOPE_*`) |
| `internal/db` | SQLite (modernc), Schreib-/Lese-Pools, eingebettete Migrationen, Backup |
| `internal/vault` | AES-256-GCM, Master-Key aus Env/Datei, Key-Rotation, Credentials |
| `internal/auth` | Admin-Login (bcrypt, Session-Cookie), API-Tokens (read/write), Rate-Limit |
| `internal/plugin` | Plugin-SDK: Interfaces, Settings-Schema, Observation, Change, Event-Katalog |
| `internal/pluginhost` | Registry, Konfiguration, Scheduler, Worker-Pool, Laufhistorie, Hooks, Aktionen |
| `internal/inventory` | Datenmodell, Ingest, Präsenz, Query-Sprache, Merge/Split, Diff, Topologie-Graph |
| `internal/events` | Event-Speicher mit Dedup, Quittierung |
| `internal/rules` | Regel-Engine, Bündelung, Ruhezeiten, Drosselung, Eskalation, Simulation |
| `internal/timeseries` | Zeitreihen roh/5 min/1 h, Downsampling, Retention |
| `internal/reports` | Inventar-Export (CSV/JSON/PDF), Änderungsbericht |
| `internal/api` | HTTP-API, OpenAPI-Generator, SSE, Prometheus-Endpoint |
| `internal/webui` | eingebettete SPA (`dist/` wird vom Frontend-Build befüllt) |
| `internal/plugins/<id>` | alle Plugins; `internal/plugins/all` importiert sie |
| `internal/sshx`, `internal/execx`, `internal/netutil` | gemeinsame Helfer (SSH mit TOFU, Prozesse streamend, Adressen) |
| `web/` | SvelteKit-Quellen (TypeScript, Tailwind) |
| `scripts/` | `smoke.sh` (End-to-End-Test), `deploy.sh` (Deployment per SSH) |

## Datenbank

Schema: `internal/db/migrations/*.sql` (eingebettet, beim Start der Reihe nach angewendet).
Zeitstempel sind Unix-Millisekunden. Tabellen mit `first_seen/last_seen/gone_at` sind
**temporal**: `gone_at IS NULL` ist der aktuelle Zustand, geschlossene Zeilen sind Historie –
darauf beruhen Diff zu beliebigen Zeitpunkten und die Gerätehistorie.

| Bereich | Tabellen |
|---|---|
| System | `settings`, `users`, `sessions`, `api_tokens`, `audit_log` |
| Vault | `vault_meta` (Key-Prüfwert), `credentials` (öffentliche Felder + AES-GCM-Blob + Geltungsbereich `scope`) |
| Plugins | `plugin_configs` (inkl. verschlüsselter Secret-Felder), `runs`, `run_logs` |
| Netz | `subnets` |
| Geräte | `devices` (manuelle Felder + effektive Werte), `device_macs`, `device_ips`*, `device_facts`* (Hostname/Hersteller/OS/Typ/Attribute je Quelle), `device_presence`, `external_refs`, `device_inventory`, `device_tags`, `groups`, `group_members`, `custom_fields`, `saved_views`, `relations` |
| Beobachtungen | `observations` (normalisiert + Rohausgabe je Plugin und Lauf) |
| Zustand | `ports`*, `certificates`*, `http_services`*, `packages`*, `containers`*, `container_images`* |
| Events & Regeln | `events`, `rules`, `notifications`, `rule_throttle`, `escalations` |
| Health | `health_checks`, `health_outages` |
| Zeitreihen | `ts_series`, `ts_raw`, `ts_5m`, `ts_1h` |
| Schwachstellen | `nvd_cves`, `nvd_cpe_matches`, `nvd_feeds`, `device_cves`*, `cve_ignores` |

\* temporal. Manuelle Daten (Anzeigename, Standort, Besitzer, Notizen, Tags, Custom Fields,
Kritikalität, Zustand, manuelle Overrides mit Quelle `manual`) schreibt kein Scan und kein
Import um.

**Effektive Werte:** Hostname, Hersteller, Modell, Typ und OS liegen je Quelle in
`device_facts`; der angezeigte Wert folgt einer Priorität (Hostname: manuell › DHCP
(`openwrt`) › `ssh` › `snmp` › `proxmox` › DNS › mDNS › NetBIOS › UPnP › …, in den
Systemeinstellungen änderbar).

**Präsenz:** Nur Plugins mit `Presence` (arpscan, icmp, nmap) zählen verpasste Läufe je Gerät.
Ein Gerät geht offline, wenn ein solches Plugin es `offlineAfterMissed`-mal in Folge nicht
sieht und kein anderes Plugin es seitdem gesehen hat. IP-Wechsel werden am Laufende erkannt.

## Plugin-Interfaces

Siehe `internal/plugin/plugin.go` und die Anleitung [PLUGINS.md](PLUGINS.md).
Vier Typen (`scanner`, `importer`, `processor`, `publisher`); Fähigkeiten über
Interfaces: `Runner`, `Publisher`, `ChangeHandler`, `RunFinishedHandler`, `ActionProvider`,
`SettingsValidator`.

## Settings-Schema

`plugin.Schema{Fields: []plugin.Field}` – je Feld: `key`, `type` (string, secret, int, bool,
cron, enum, string-list, subnet-list, credential-ref, duration), `label`, `description`,
`default`, `required`, `validation` (min/max, pattern, format), `options`, `multi`,
`credentialTypes`, `group`, `advanced`, `visibleIf`, `widget`. Die UI erzeugt daraus die
Formulare; der Core validiert und normalisiert, speichert Secret-Felder verschlüsselt und
liefert sie nur maskiert (`********`) aus. Allgemein je Plugin (vom Core verwaltet):
aktiv, Cron-Zeitplan, Timeout, Wiederholungen + Backoff, Parallelität, Scope
(Subnetze / Gruppen / Tags / Geräte / Filter).

## Event-Katalog

| Typ | Label | Standard-Schweregrad | Quelle |
|---|---|---|---|
| `device.new` | Neues Gerät | medium | diff |
| `device.offline` | Gerät weg | low | diff |
| `device.online` | Gerät wieder da | info | diff |
| `device.ip_changed` | IP-Wechsel | low | diff |
| `device.mac_changed` | MAC-Wechsel auf bekannter IP | high (low bei privater/zufälliger MAC) | diff |
| `device.hostname_changed` | Hostname geändert | info | diff |
| `device.os_changed` | Betriebssystem geändert | low | diff |
| `port.opened` | Port neu | medium | diff |
| `port.closed` | Port geschlossen | low | diff |
| `service.version_changed` | Dienstversion geändert | low | diff |
| `cert.expiring` | Zertifikat läuft ab | medium | diff |
| `cert.expired` | Zertifikat abgelaufen | high | diff |
| `cert.changed` | Zertifikat gewechselt | low | diff |
| `tls.weak` | Schwache TLS-Konfiguration | medium | diff |
| `container.new` | Container neu | info | diff |
| `container.removed` | Container weg | info | diff |
| `container.image_changed` | Container-Image geändert | info | diff |
| `package.changed` | Paket-Änderungen | info | diff |
| `cve.new` | Neue Schwachstelle | high (aus CVSS) | cve |
| `cve.resolved` | Schwachstelle behoben | info | cve |
| `health.down` | Check ausgefallen | high | healthcheck |
| `health.degraded` | Check beeinträchtigt | medium | healthcheck |
| `health.up` | Check wieder OK | info | healthcheck |
| `plugin.failed` | Plugin-Lauf fehlgeschlagen | medium | core |
| `nvd.sync_failed` | NVD-Sync fehlgeschlagen | medium | cve |

Payload-Felder je Typ: `GET /api/v1/events/types` bzw. `internal/plugin/events.go`.

## API

JSON unter `/api/v1`, generierte OpenAPI-Spezifikation unter `/api/openapi.json`,
Dokumentation unter `/api/docs`, Live-Updates per SSE unter `/api/v1/stream`,
Prometheus-Metriken unter `/metrics`.

| Bereich | Endpunkte |
|---|---|
| Auth | `POST /auth/login`, `POST /auth/logout`, `GET /auth/me`, `PUT /auth/password`, `GET/POST /tokens`, `DELETE /tokens/{id}` |
| Geräte | `GET/POST /devices`, `GET/PATCH/DELETE /devices/{id}`, `POST /devices/bulk`, `POST /devices/merge`, `POST /devices/{id}/split`, `POST /devices/{id}/scan`, `POST /devices/{id}/actions/{plugin}/{action}`, Tabs: `…/ports`, `…/http`, `…/certificates`, `…/packages`, `…/containers`, `…/inventory`, `…/cves`, `…/health`, `…/events`, `…/timeline`, `…/relations`, `…/observations`, `…/timeseries`, `…/credentials` (passende Zugangsdaten mit Rang und Grund); `GET /tags`, `GET /certificates` |
| Stammdaten | `/subnets`, `/groups` (+ `/members`), `/custom-fields`, `/views` (CRUD) |
| Plugins | `GET /plugins`, `GET /plugins/{id}`, `PUT /plugins/{id}/config`, `POST /plugins/{id}/run`, `POST /plugins/{id}/actions/{action}` (`?wait=0` antwortet sofort mit der Lauf-ID), `POST /plugins/{id}/test`, `GET /runs` (Filter `plugin`, `status`, `kind`, `before`, `scope=full`), `GET /runs/active`, `GET /runs/{id}`, `GET /runs/{id}/logs?after=`, `POST /runs/{id}/cancel` |
| Events | `GET /events`, `GET /events/{id}`, `POST /events/ack`, `GET /events/types`, `GET /events/counts`, `GET /diff` |
| Regeln | `/rules` (CRUD), `PUT /rules/order`, `POST /rules/test`, `POST /rules/{id}/test`, `GET /notifications` (Filter `status`, `publisher`, `event`, `rule`), `GET /publishers` |
| Credentials | `/credentials` (CRUD mit `scope`, nie Secrets), `GET /credentials/types` |
| Health | `GET /health/board`, `/health-checks` (CRUD), `POST /health-checks/{id}/run`, `…/outages`, `…/latency` |
| Schwachstellen | `GET /vulnerabilities`, `GET /vulnerabilities/{cve}`, `POST /vulnerabilities/ignore`, `GET /vulnerabilities/status` |
| Topologie | `GET /topology`, `GET/POST /topology/edges`, `DELETE /topology/edges/{id}` |
| Reports | `GET /reports/inventory?format=csv|json|pdf`, `GET /reports/changes?format=json|md|pdf`, `POST /reports/send` |
| System | `GET /health` (ohne Login), `GET /system/info`, `GET/PUT /system/settings`, `PUT /system/loglevel`, `GET /system/logs`, Backups (`/system/backups`, `/system/restore`), `POST /system/vault/rotate`, `GET /audit`, `GET /cron/describe`, `GET /meta`, `POST /uploads`, `GET /dashboard` |

Fehler haben die Form `{"error": {"code", "message", "fields": [{"field", "message"}]}}`.
Validierungsfehler (`code: "validation"`) nennen in `fields` die betroffenen Felder mit den
JSON-Namen des Request-Bodys (verschachtelt mit Punkt, Listen mit Index, z. B.
`actions.1.throttle`), damit Formulare sie am richtigen Feld anzeigen.

Live-Updates (`GET /api/v1/stream?topics=…`, Server-Sent Events, Event-Name = Topic):

| Topic | Typen und Daten |
|---|---|
| `run` | `queued`, `started`, `progress` (`done`, `total`), `finished` (`status`, `error`, `durationMs`); außer bei `progress` mit der vollständigen Laufansicht in `run`. `discarded: true` = Routinelauf ohne Änderungen, nicht in der Historie |
| `run.log` | `line` (`id` der gespeicherten Zeile, `runId`, `level`, `msg`, `attrs`) – nach dem Speichern, Fortsetzung per `…/logs?after=<id>` |
| `device` | `created`, `updated`, `deleted` (nach einem Merge mit `mergedInto`) |
| `event` | `created`, `acked` |
| `notification` | `created`, `updated` (Bündel erweitert), `sent`, `failed` |
| `health` | `state` (`checkId`, `state`, `previousState`, `latencyMs`), `round` (nach jeder Prüfrunde) |
| `plugin`, `system`, `log` | Konfigurationsänderungen, Systemereignisse, Log-Zeilen |

Authentifizierung: Session-Cookie (Oberfläche; schreibende Anfragen zusätzlich mit Header
`X-NetScope-CSRF: 1`) oder `Authorization: Bearer ns_…` (API-Token mit Scope `read` oder
`write`). Hinter einem Reverse-Proxy werden `X-Forwarded-For/-Proto/-Host` nur von
vertrauenswürdigen Proxys (`trusted_proxies`) übernommen.
