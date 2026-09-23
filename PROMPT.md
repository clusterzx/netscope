# Projekt: NetScope – Netzwerk-Inventar und Asset-Monitoring für mein Homelab

Self-hosted Netzwerk-Inventar als Web-App. Entwicklung: mein Windows-PC (`D:\netscope`,
192.168.8.11). Zielumgebung für Deployment und Abnahme: Ubuntu-LXC auf Proxmox, Docker,
Host-Netzwerk, IP 192.168.8.123, Zugriff per SSH als root. Du entwickelst lokal und
spielst jeden Stand per SSH auf den LXC; gebaut, gestartet und getestet wird dort.
Ports 20211/20212 sind belegt (NetAlertX) – tabu. Nutze Port 8080. Primäres Netz
192.168.8.0/24 auf eth0, weitere Subnetze müssen per UI ergänzbar sein. Router ist ein
GL.iNet (OpenWrt) auf 192.168.8.1, Hypervisor ist Proxmox VE. Zeitzone Europe/Berlin.

Ziel: eine einzige Oberfläche, die für jedes Gerät im Netz beantwortet – was ist das, wo
hängt es, was läuft darauf, seit wann, was hat sich geändert, ist es erreichbar, ist es
verwundbar.

## Architektur: schlanker Core, alles andere Plugins

Der Core stellt bereit: Datenmodell, Scheduler, Event-Bus, Credential-Vault, Regel-Engine,
API, UI-Shell. Jede Fähigkeit ist ein Plugin – auch die mitgelieferten. Vier Plugin-Typen:

- **Scanner** erzeugen Beobachtungen über Geräte (aktiv im Netz).
- **Importer** holen Bestandsdaten aus fremden Systemen (API/SSH), ohne zu scannen.
- **Processor** verarbeiten Beobachtungen zu Zustand und Events.
- **Publisher** verschicken Benachrichtigungen.

Jedes Plugin implementiert ein Go-Interface und deklariert sein **Settings-Schema** als
Datenstruktur (Feld, Typ, Label, Beschreibung, Default, Validierung, Auswahlwerte, Secret-
Flag, Credential-Referenz). Typen: string, secret, int, bool, cron, enum, string-list,
subnet-list, credential-ref, duration. Die UI rendert aus dem Schema automatisch das
Formular – **keine handgeschriebenen Settings-Seiten pro Plugin.** Pro Plugin per UI:
aktiv/inaktiv, Cron-Zeitplan mit Klartext-Vorschau, alle Schema-Felder, Scope (welche
Subnetze/Geräte-Gruppen), „Jetzt ausführen“, Laufhistorie mit Status/Dauer/Log, Timeout,
Fehlerverhalten (Retry, Backoff). Änderungen greifen ohne Container-Neustart.

## Scanner (initial)

- `arpscan` – Anwesenheit per ARP, Default alle 5 Min, mehrere Subnetze/Interfaces.
- `icmp` – Ping-Latenz und Paketverlust pro Gerät als Zeitreihe (Min/Avg/Max pro Lauf).
- `nmap` – Ports, Dienste, Versionen, OS (`-sV -O --osscan-guess`), CPE-Strings
  extrahieren; Port-Range und Timing per UI; Ergebnisse pro Host streamend schreiben.
- `nmap_udp` – separater, seltener UDP-Scan der Top-Ports.
- `dns` – Reverse-Lookup gegen konfigurierbaren Resolver (Default 192.168.8.1).
- `mdns`, `netbios`, `upnp` (SSDP-Discovery: Modell, Hersteller, Friendly Name).
- `oui` – Hersteller aus lokaler OUI-Datei; Update-Job lädt die Datei per Button neu.
- `http` – auf allen HTTP(S)-Ports: Titel, Server-Header, Redirects, Favicon-Hash,
  erkannte Web-Apps (kleine, erweiterbare Signaturliste: Proxmox, Frigate, Coolify,
  Portainer, Grafana, Home Assistant, Synology, OpenWrt/LuCI, Jellyfin, Paperless …).
- `tls` – Zertifikate auf allen TLS-Ports: CN/SAN, Aussteller, Ablauf, Selbstsigniert,
  Schwache Protokolle/Cipher. Ablauf-Warnung als Event.
- `snmp` – v2c/v3 mit Credential aus dem Vault: sysDescr, sysName, Uptime, Interfaces,
  ARP-Tabelle, Bridge-FDB (MAC→Port) und LLDP-Nachbarn für Topologie.
- `ssh` – Linux-Inventar per SSH-Key/Passwort aus dem Vault: OS-Release, Kernel,
  Hostname, CPU/RAM/Disks, Netzwerk-Interfaces, installierte Pakete (dpkg/rpm/apk),
  laufende Dienste (systemd), lauschende Sockets (`ss -tulpn`), Docker-Container falls
  vorhanden, Uptime, letzte Updates. Nur Lesekommandos, fest definierte Liste.
- `wol` – kein Scanner im engeren Sinn: Wake-on-LAN-Aktion pro Gerät aus der UI.

## Importer (initial)

- `proxmox` – Proxmox-API (Token im Vault): alle VMs und CTs mit Name, VMID, Status,
  MACs, Ressourcen, Node. MAC-Match verknüpft VM ↔ gescanntes Gerät und setzt die
  Eltern-Beziehung „läuft auf Node X“.
- `openwrt` – DHCP-Leases und statische Leases vom GL.iNet per SSH (`dhcp.leases`,
  `uci show dhcp`) oder LuCI-RPC: liefert Hostnamen und Lease-Zeiten, füllt
  Namenslücken.
- `docker` – Docker-Socket auf konfigurierbaren Hosts (lokal oder per SSH-Tunnel):
  Container, Images, exponierte Ports, Compose-Projekte. Verknüpft Container als
  Kind-Objekte des Hosts.
- `netalertx` – einmaliger Import der bestehenden NetAlertX-Datenbank/CSV (Namen,
  Typen, Erstsichtung), damit ich nicht von vorn anfange.
- `csv` – generischer Import/Export des Inventars.

## Processor (initial)

- `diff` – Zustandsvergleich pro Lauf. Events: Gerät neu / weg / wieder da / IP-Wechsel
  / MAC-Wechsel auf bekannter IP; Port neu / geschlossen; Dienstversion geändert;
  OS-Guess geändert; Hostname geändert; Zertifikat läuft in N Tagen ab / abgelaufen /
  gewechselt; Docker-Container neu / weg / Image geändert; Paket-Änderungen auf
  SSH-Hosts (Anzahl + Liste).
- `cve` – Abgleich der CPE-Strings (aus nmap, ssh-Pakete, http-Erkennung) gegen eine
  **lokal gespiegelte NVD-Datenbank** (JSON-Feeds, Update-Job per Cron, kein Live-
  Lookup pro Gerät). Pro Gerät: CVEs mit CVSS, Vektor, Beschreibung, Referenzen.
  Events bei neuer CVE ≥ konfigurierbarer Score. Ehrlich kennzeichnen, dass Version-
  Matching heuristisch ist; „Als irrelevant markieren“ pro CVE+Gerät.
- `healthcheck` – konfigurierbare Checks pro Gerät/Dienst: TCP-Connect, HTTP-Status/
  Body-Match, TLS-Handshake, ICMP. Zustand Up/Down/Degraded mit Flap-Dämpfung,
  Verfügbarkeit in % über 24h/7d/30d, Zeitreihe.
- `topology` – baut aus SNMP-FDB, LLDP, ARP, Proxmox-Zuordnung und Docker-Zuordnung
  einen Graphen: Gerät → Switch-Port, VM → Host, Container → Host. Manuelle Kanten
  ergänzbar und geschützt.
- `cleanup` – Rohdaten-Retention pro Datentyp (Scans, Zeitreihen, Logs), Events bleiben.

## Publisher (initial)

- `telegram` (Bot-Token, Chat-ID, gebündelt pro Lauf, Markdown, Deep-Link ins UI),
- `webhook` (JSON-POST, Header konfigurierbar, HMAC-Signatur),
- `ntfy`, `email` (SMTP),
- `n8n` – nur ein vorkonfigurierter Webhook mit dokumentiertem Payload.

## Regel-Engine

Benachrichtigungen laufen nicht direkt vom Event zum Publisher, sondern über **Regeln**
(UI-gepflegt): Bedingung (Event-Typ, Gerätegruppe/Tag, Subnetz, Schweregrad, Zeitfenster,
„nur wenn Gerät nicht als bekannt markiert“) → Aktion (Publisher, Priorität, Sammeln über
N Minuten, Ruhezeiten, Eskalation nach X Minuten ohne Quittierung). Events sind
quittierbar. Vorgefertigte Standardregeln: „neues unbekanntes Gerät → Telegram sofort“,
„neuer Port auf bekanntem Gerät → Telegram gesammelt“, „CVE ≥ 9 → Telegram sofort“,
„Zertifikat < 14 Tage → täglich einmal“.

## Datenmodell & Inventar

Gerät: IP(s), MAC(s), Hersteller, Hostname aus Quellen mit Priorität (manuell > DHCP >
DNS > mDNS > NetBIOS > UPnP), Anzeigename, Typ, Standort, Besitzer, Tags (frei), Gruppen
(regelbasiert oder manuell), Notizen (Markdown), Custom Fields (per UI definierbar:
Text, Zahl, Datum, URL, Bool), Eltern/Kinder (Host↔VM, Host↔Container, Switch↔Port),
Kritikalität, Zustand bekannt/unbekannt/ignoriert, Erst-/Letztsichtung, Online-Status.
Ports/Dienste, Zertifikate, Pakete, Container, CVEs, Health-Checks, Zeitreihen und Events
als eigene Tabellen mit FK auf das Gerät. Jede Beobachtung trägt Plugin-Quelle, Lauf-ID,
Zeitstempel. Manuelle Daten überleben jeden Scan und jeden Import.
Geräte-Merge und -Split per UI (wenn zwei MACs ein Gerät sind oder umgekehrt).
Vollständiger Audit-Log für manuelle Änderungen.

## Web-UI

- **Dashboard**: online/offline/neu/unbekannt, offene kritische Events, Plugin-Status,
  Verfügbarkeit, CVE-Übersicht nach Score, Zertifikats-Ablauf, laufende Scans live.
- **Geräte**: Tabelle mit Spaltenauswahl, Volltext, Filter-Query-Sprache
  (`tag:iot port:22 os:linux cve>=7 seen<24h`), gespeicherte Ansichten, Massenaktionen
  (Tags, Gruppe, bekannt markieren, WOL).
- **Gerätedetail**: Tabs Überblick / Ports & Dienste / Software / Container / Zertifikate
  / CVEs / Health / Historie / Beziehungen / Rohdaten pro Plugin. Timeline aller Events.
  Aktionen: WOL, Scan jetzt, Health-Check anlegen, Notiz.
- **Topologie**: interaktiver Graph (Force-Layout, Zoom, Klick → Gerät), Filter nach
  Subnetz/Tag, manuelle Kanten.
- **Events**: Liste mit Filter, Quittieren, Bulk, Link zum Diff.
- **Diff**: zwei beliebige Läufe oder zwei Zeitpunkte gegeneinander.
- **Health**: Statusboard aller Checks, Verfügbarkeit, Ausfallhistorie.
- **Vulnerabilities**: CVE-Liste über alle Geräte, nach Score/Gerät/Produkt, Irrelevant-
  Markierung, NVD-Sync-Status.
- **Plugins**: Liste nach Typ, Toggle, schemagenerierte Settings, Laufhistorie, Logs.
- **Regeln**: Regel-Editor mit Testfunktion („simuliere Event X“).
- **Credentials**: Vault-Verwaltung (SSH-Key, Passwort, SNMP-Community, API-Token),
  verschlüsselt at rest (AES-GCM, Master-Key aus Env oder Datei), Secrets werden nie
  in der API ausgeliefert, nur referenziert.
- **Reports**: Inventar-Export (CSV/JSON/PDF), Änderungsbericht für Zeitraum, geplanter
  Wochenbericht per Publisher.
- **System**: Passwort, API-Tokens (scoped: read/write), Backup/Restore der DB per UI,
  Log-Viewer, Metriken.
- Login mit Passwort (bcrypt, Session-Cookie), API-Tokens für Skripte. Hinter Traefik
  mit TLS: keine eigene TLS-Logik, `X-Forwarded-*` korrekt.

## Tech-Stack – bitte genau so

- **Backend: Go**, `net/http` + `http.ServeMux`, **SQLite** via `modernc.org/sqlite`
  (kein CGO), WAL, eingebettete SQL-Migrations. Zeitreihen in eigener Tabelle mit
  Downsampling-Job (roh 7 Tage, 5-Min-Aggregate 90 Tage, Stunden-Aggregate 1 Jahr).
  Scheduler mit Context-Cancellation, Worker-Pool mit konfigurierbarer Parallelität pro
  Plugin, ein Plugin nie parallel zu sich selbst. Scanner rufen Systembinaries
  (`nmap`, `arp-scan`) mit `-oX -` auf und parsen streamend; SNMP/SSH über reine
  Go-Bibliotheken (`gosnmp`, `golang.org/x/crypto/ssh`).
- **Frontend: SvelteKit** (Static Adapter), TypeScript, Tailwind, Graph-Rendering mit
  einer kleinen, eingebetteten Library (kein CDN). Per `embed` ins Go-Binary –
  **ein Binary**, kein Node zur Laufzeit. Dark-Mode, keine Component-Library.
- **API**: JSON `/api/v1/...`, OpenAPI-Spec generiert und unter `/api/docs` ausgeliefert,
  SSE `/api/v1/stream` für Live-Updates, `/metrics` im Prometheus-Format.
- **Config**: `/data/config.yaml` nur Bootstrap (Port, Datenpfad, Log-Level, Master-Key-
  Quelle), per `NETSCOPE_*` überschreibbar. Alles andere in der DB, per UI gepflegt.
- **Docker**: Multi-Stage (Node-Build → Go-Build → `alpine` mit `nmap`, `nmap-scripts`,
  `arp-scan`, `ca-certificates`). `docker-compose.yml`: `network_mode: host`,
  `cap_add: [NET_RAW, NET_ADMIN]`, Volume `./data:/data`, `restart: unless-stopped`.

## Nicht-funktionale Anforderungen

- Kein Scan und kein Import blockiert UI oder andere Plugins; Ergebnisse werden pro
  Host geschrieben, sobald sie vorliegen.
- Geräteliste mit 500 Geräten × 50 Ports in < 100 ms; Topologie-Graph mit 500 Knoten
  in < 500 ms aus der DB; NVD-Abgleich für 500 Geräte < 30 s.
- Sauberes Shutdown auf SIGTERM: laufende Plugins abbrechen, DB-Checkpoint.
- `log/slog`, strukturiert, Level konfigurierbar, Plugin-Name als Attribut.
- `Makefile`: `build`, `dev`, `test`, `lint`, `docker-build`, `docker-up`, `docker-down`,
  `smoke`.
- Unit-Tests: alle Parser mit echten Fixtures (nmap, arp-scan, snmp-walk, ss-Output,
  dpkg-Liste, Proxmox-JSON, dhcp.leases), Diff-Engine tabellengetrieben, Regel-Engine,
  Schema-Validierung, CVE-Matching mit bekannten Positiv-/Negativfällen, Vault
  (Roundtrip, Key-Rotation).

## Testen: jeder Meilenstein wird im Docker-Container abgenommen

Nach jedem Meilenstein: Stand auf den LXC 192.168.8.123 übertragen, dort Image bauen,
`make docker-up`, `make smoke` gegen `http://127.0.0.1:8080` auf dem LXC (curl + jq,
Exit ≠ 0 bei Fehler, Container-Logs bei Fehler ausgeben). Scanner-Plugins gelten als
grün, wenn ein echter Lauf gegen 192.168.8.0/24 aus dem Container mindestens
192.168.8.1 und 192.168.8.123 findet.
Credential-basierte Plugins (ssh, snmp, proxmox, openwrt) testest du gegen den LXC
192.168.8.123 selbst bzw. Ziele, für die ich dir Credentials in den Vault lege – frag danach, wenn
du dort ankommst. Ein Meilenstein ist erst abgeschlossen, wenn `make test`, `make lint`
und `make smoke` grün sind.

## Meilensteine – in dieser Reihenfolge, nach jedem Halt und Zusammenfassung

0. Plan: Verzeichnisstruktur, vollständiges DB-Schema, Plugin-Interfaces, Settings-
   Schema-Format, Event-Typen-Katalog, API-Endpunkte. **Warte auf mein OK.**
1. Core-Skelett: Binary im Container, Migrations, Login, API-Tokens, Health-Endpoint,
   SSE-Grundgerüst, Vault mit Roundtrip-Test.
2. Plugin-System: Interfaces, Registry, Settings-Schema, Scheduler, Worker-Pool,
   Laufhistorie, Plugin-API. Dummy-Plugin beweist den Kreislauf.
3. Scanner `arpscan`, `icmp`, `oui`, `dns` + Geräte-API + Zeitreihen. Smoke: echter Scan.
4. Scanner `nmap`, `nmap_udp`, `http`, `tls` + Ports/Zertifikate-API.
5. Processor `diff` + Event-API + Event-Typen-Katalog vollständig.
6. Regel-Engine + Publisher `telegram`, `webhook`, `ntfy`, `email` + Standardregeln.
7. Frontend Basis: Dashboard, Geräte (mit Query-Sprache), Gerätedetail, Events, Diff,
   Plugins, Regeln, Credentials, System. **Manuelle Abnahme durch mich.**
8. Importer `proxmox`, `openwrt`, `docker`, `netalertx`, `csv` + Beziehungen +
   Merge/Split.
9. Scanner `snmp`, `ssh`, `mdns`, `netbios`, `upnp` + Software/Container-Tabs.
10. Processor `healthcheck` + Health-Board; `wol`.
11. Processor `cve` mit NVD-Mirror + Vulnerabilities-Seite.
12. Processor `topology` + Graph-Ansicht; Reports; Cleanup/Downsampling; OpenAPI;
    Metriken; README mit Compose-Beispiel und Plugin-Entwicklungsanleitung.

Keine Platzhalter, keine TODOs, keine Mock-Daten im Produktionspfad. Wenn etwas unklar
ist, frag – rate nicht. Wenn ein Meilenstein zu groß ist, schlag eine Teilung vor,
statt ihn halb zu liefern.