# Future Requests

Wünsche für spätere Versionen, die noch nicht umgesetzt sind. Jeder Eintrag beschreibt
Anlass, Problem und einen Lösungsvorschlag, damit die Umsetzung ohne Vorgeschichte starten
kann.

FR-005 bis FR-012 stammen aus dem Marktvergleich vom 27.09.2026 (17 Produkte aus Discovery,
Monitoring und Security); sie schließen die Lücken, in denen NetScope dort zurücklag.

---

## FR-005: Kommerzielle Lizenz und Hinweise auf Fremdsoftware

**Status:** umgesetzt am 27.09.2026 – `LICENSE`, `THIRD_PARTY_NOTICES.md` (erzeugt mit
`make notices`), README „Lizenz“

### Anlass

Im Repo lag keine Lizenzdatei. NetScope soll kein Open Source sein, sondern ein kommerzielles
Produkt.

### Festgelegt

| Frage | Entscheidung |
|---|---|
| Lizenz | **Proprietär**, alle Rechte vorbehalten. |
| Fremdsoftware | Go- und npm-Abhängigkeiten sind permissiv (MIT, BSD, Apache) und brauchen nur Namensnennung: `THIRD_PARTY_NOTICES`. |
| nmap | Bleibt. nmap steht unter der Nmap Public Source License 0.95; sie wertet Software, die nmap gezielt ausführt und die Ausgabe auswertet, als abgeleitetes Werk. Wer NetScope mit nmap vertreibt, braucht eine **Nmap-OEM-Lizenz** (nmap.org/oem). Lizenzfreie Alternative: FR-011. |
| arp-scan | GPL-3.0, als eigenständiges Programm aufgerufen (Aggregation). Unkritischer als nmap, wird aber ebenfalls durch den eigenen Scanner ersetzbar. |

---

## FR-006: CVEs nach tatsächlicher Ausnutzung priorisieren

**Status:** umgesetzt am 27.09.2026 – README „Schwachstellen“, ARCHITECTURE „NVD-Spiegel“

### Anlass

Der CVE-Abgleich ist heuristisch und liefert viele Treffer; sortiert wird nur nach CVSS.
runZero stuft aktiv ausgenutzte Lücken hoch.

### Festgelegt

- CISA-Katalog „Known Exploited Vulnerabilities“ (KEV) und FIRST EPSS (Wahrscheinlichkeit
  einer Ausnutzung in 30 Tagen) täglich laden, lokal wie der NVD-Spiegel.
- Treffer tragen „bekannt ausgenutzt“ (mit Datum und Frist aus KEV, Ransomware-Hinweis) und den
  EPSS-Wert; Sortierung, Filter (`kev:yes`, `epss>=0.1`) und Regeln können darauf aufbauen.

### Umsetzung – Entscheidungen und Abweichungen

- **Speicherort:** `nvd_kev` und `nvd_epss` in der Spiegel-Datei; sie entstehen beim nächsten
  Start ohne erneuten NVD-Download. Beide Dateien werden bei jedem Sync geladen (zusammen
  ~5 MB), die Tabelle aber nur bei geändertem Inhalt ersetzt; der Import der ~380.000 EPSS-Werte
  dauert rund eine Sekunde.
- **Suche:** `is:exploited` statt `kev:yes` (passt zu den übrigen `is:`-Werten), dazu `epss>=0.1`
  bzw. `epss>=10%`.
- **Sortierung** überall „Dringlichkeit“: ausgenutzt, dann EPSS, dann CVSS – Liste, Gerätereiter,
  Top-CVEs im Dashboard.
- **Events:** Ausgenutzte CVEs lösen `cve.new` unabhängig von der CVSS-Schwelle und als kritisch
  aus; neu: `cve.exploited`, wenn CISA eine bereits gefundene CVE aufnimmt (nicht beim ersten
  Laden des Katalogs). Beides lässt sich im Plugin abschalten.
- **Quellen:** KEV steht unter CC0; EPSS ist frei nutzbar, FIRST bittet um Namensnennung
  (Detailseite und `THIRD_PARTY_NOTICES.md`).

---

## FR-007: Zentrale Anmeldung per OIDC und LDAP

**Status:** umgesetzt am 27.09.2026 – README „Zentrale Anmeldung: OIDC und LDAP“,
ARCHITECTURE „Benutzer und Rechte“

### Anlass

13 der 17 verglichenen Produkte bieten LDAP oder SSO; in Firmen ist das ein
Ausschlusskriterium.

### Festgelegt

| Frage | Entscheidung |
|---|---|
| Verfahren | **OIDC** (Authentik, Keycloak, Authelia, Entra ID, Google …) **und LDAP** (Active Directory, OpenLDAP). |
| Konten | Anlage beim ersten Login; Rolle über Gruppen-Zuordnung (Gruppe → NetScope-Rolle), sonst Standardrolle. |
| Lokale Konten | Bleiben; mindestens ein lokaler Administrator als Notzugang. |
| 2FA | Bei OIDC Sache des Identity Providers; bei LDAP gilt die 2FA-Pflicht der Rolle wie bei lokalen Konten. |

### Umsetzung – Entscheidungen und Abweichungen

- **Kein Übernehmen fremder Konten:** Ein lokales Konto gleichen Namens hat immer Vorrang; eine
  externe Anmeldung legt nie ein bestehendes Konto an eine neue Quelle. Das gilt auch zwischen
  OIDC und LDAP – wer beides nutzt, meldet sich je Person über ein Verfahren an.
- **Rollen:** geordnete Liste Gruppe → Rolle, dann Standardrolle, sonst kein Zugriff (403
  `no_role`). „Rolle bei jeder Anmeldung übernehmen“ ist Standard; aus, zählt die Gruppe nur beim
  ersten Mal. LDAP-Gruppen passen per DN oder CN.
- **Notzugang:** Der letzte aktive lokale Administrator lässt sich nicht löschen, deaktivieren
  oder herabstufen – auch wenn externe Administratoren existieren.
- **Bibliotheken:** go-ldap (MIT) für LDAP; OIDC selbst implementiert auf golang-jwt (Discovery,
  JWKS mit RSA/EC/Ed25519, PKCE) statt go-oidc + oauth2, um keine weiteren Abhängigkeiten
  einzuführen.
- **Getestet:** Unit-Tests mit einem eingebetteten LDAP-Server und einem Test-Provider (u. a.
  fremde Audience, falsche Nonce, abgelaufen, HS256, `alg=none`, fremder Issuer, Replay); dazu
  End-to-End auf dem LXC gegen OpenLDAP (memberOf und Gruppensuche, Filter-Injection) und Dex
  als OIDC-Provider mit echter Browser-Anmeldung.

---

## FR-008: Firewalls, Router und DHCP-Server als Quellen

**Status:** umgesetzt am 27.09.2026 (Windows-DHCP über den Windows-Agent, FR-010) – README
„Plugins“, ARCHITECTURE `internal/plugins/netsrc`

### Anlass

NetAlertX gewinnt im Homelab mit vielen Router-Importern; in Firmen stehen Firewalls und
DHCP-Server, deren Leases und ARP-Tabellen Namen, MACs und Anwesenheit liefern – auch aus
Netzen, die NetScope nicht selbst scannt.

### Festgelegt

Importer für **OPNsense, pfSense, UniFi, MikroTik RouterOS, Fortinet FortiGate, Sophos
Firewall, Cisco Meraki, Fritz!Box, Pi-hole und Windows-DHCP-Server** (über den Windows-Agent,
FR-010). Echte Geräte zum Testen: OPNsense und UniFi; die übrigen gegen Beispieldaten aus der
Hersteller-Dokumentation.

### Umsetzung – Entscheidungen und Abweichungen

- **Gemeinsamer Unterbau** `internal/plugins/netsrc`: Jeder Importer liefert nur Einträge
  (Lease, Reservierung, ARP, Controller-Client); zusammengeführt wird je MAC, Namen von
  Administratoren (Reservierung, Alias) gehen vor gemeldeten Hostnamen. Keine Anwesenheit –
  Importer haben keinen Scan-Bereich, aus dem sich „offline“ ableiten ließe; der
  Verbindungsstatus steht im Inventar der Quelle.
- **Topologie:** UniFi, Meraki (Access Point bzw. Switch-Port) und MikroTik (Bridge-Port)
  liefern Beziehungen; die Netzwerkgeräte selbst werden vor den Clients übernommen.
- **Versionen:** OPNsense in beiden URL-Schreibweisen (camelCase bis 25.1, snake_case ab 25.7)
  und allen drei DHCP-Diensten; pfSense mit ISC oder Kea (Socket-Pfade vor und nach 2025).
- **UniFi:** Benutzer/Passwort (volle Daten) oder API-Schlüssel (offizielle Integration-API,
  nur verbundene Clients ohne VLAN/Port/Hersteller).
- **Sophos:** Die XML-API liefert keine aktuellen Leases und keine ARP-Tabelle, nur die
  Reservierungen – das Plugin übernimmt diese. Leases gäbe es nur über Syslog (DHCP-Events).
- **Getestet** gegen nachgebaute Geräte-APIs aus Hersteller-Dokumentation, Quellcode und
  Beispielantworten (Recherche 27.09.2026); an echten Geräten noch zu prüfen: OPNsense, UniFi.

---

## FR-009: SNMP-Interface-Metriken

**Status:** umgesetzt am 27.09.2026 – README „Plugins“ (`snmp_traffic`), ARCHITECTURE
„SNMP-Traffic“

### Anlass

SNMP liefert bisher nur Inventar und Topologie. Traffic und Fehler je Switch-Port sind der Kern
von LibreNMS, PRTG und Domotz.

### Festgelegt

Zähler je Interface (ifHCIn/OutOctets, Fehler, Discards, Status, Geschwindigkeit) als
Zeitreihen mit Raten und Auslastung; Anzeige am Gerät, Events bei Port down und hoher
Auslastung.

### Umsetzung – Entscheidungen und Abweichungen

- **Eigenes Plugin** `snmp_traffic` neben `snmp`: Inventar und Topologie brauchen keinen
  5-Minuten-Takt, Traffic schon. Es nutzt dieselben SNMP-Credentials und merkt sich je Gerät,
  welches passt.
- **Welche Ports:** standardmäßig physische Ports, WLAN und Link-Aggregationen; VLAN- und
  virtuelle Interfaces wahlweise, abgeschaltete (admin down) nie. Namen lassen sich mit
  Platzhaltern ausschließen.
- **Port-Ausfall** meldet standardmäßig nur Ports mit Beschreibung (ifAlias) – Uplinks und
  Server sind in der Regel beschriftet, Arbeitsplätze, die abends ausgehen, nicht.
- **Überlast** je Richtung gegen die Portgeschwindigkeit (Schwelle Standard 90 %, ein Event
  je Port und Stunde).
- **Geräte ohne SNMP** werden nach einem Fehlversuch nur noch stündlich gefragt, damit nicht
  alle 5 Minuten jedes Handy angesprochen wird.
- **Getestet** mit einem nachgebauten SNMP-Agenten (Zählerüberlauf, Neustart, Port-Ausfall,
  Überlast); an echten Switches noch zu prüfen.

---

## FR-010: NetScope-Agent für Windows

**Status:** umgesetzt am 27.09.2026 – README „NetScope-Agent → Windows“, ARCHITECTURE
„NetScope-Agent“

### Festgelegt

| Frage | Entscheidung |
|---|---|
| Weg | **Der vorhandene Agent als Windows-Dienst**, installiert mit einem PowerShell-Befehl; nur ausgehend wie unter Linux. Kein WinRM. |
| Umfang | Inventar (OS, Hardware, installierte Software, Updates, Dienste, offene Ports) und Auslastung; auf DHCP-Servern zusätzlich die Leases (FR-008). |

### Umsetzung – Entscheidungen und Abweichungen

- **Ein Agent, zwei Plattformen:** dasselbe Programm, unter Windows als Dienst (Neustart nach
  Fehlern und nach Selbst-Updates über die Wiederherstellungsoptionen des Dienstes). Das
  Leseskript ist PowerShell mit festen Abfragen und gibt JSON aus; es erreicht PowerShell über
  stdin, also ohne Skriptdatei.
- **Rechte:** virtuelles Dienstkonto `NT SERVICE\NetScopeAgent` statt LocalSystem – wie unter Linux
  ein eigener Benutzer ohne Administratorrechte. Für die DHCP-Leases Mitglied der lokalen Gruppe
  „DHCP Users“; auf Domänencontrollern (keine lokalen Gruppen) mit `-RunAsSystem`.
- **Updates:** ausstehende Updates aus der Offline-Suche von Windows Update (was der Rechner schon
  kennt, keine eigene Suche im Internet, mit Zeitlimit), dazu Hotfixes, letzte Installation und
  „Neustart erforderlich“.
- **CVE-Abgleich** (über den Umfang hinaus): Aus Build und Update-Revision entsteht die CPE der
  Windows-Version; der vorhandene Abgleich zeigt damit fehlende Windows-Patches. Programme der
  Softwareliste werden (noch) nicht auf CPEs abgebildet.
- **Kein Last-Wert** unter Windows; die Auslagerungsdatei wird nicht als Swap gemeldet.
- **Getestet** auf Windows 11 23H2 gegen eine lokale Testinstanz: Leseskript, Messwerte,
  Konsolenbetrieb, Anmeldung, Inventar auf Knopfdruck, Geräteseite; das Installationsskript bis
  zur Admin-Prüfung. Noch zu prüfen: Einrichtung als Dienst, Selbst-Update und ein
  DHCP-Server – das braucht ein Windows-System mit Administratorrechten.

---

## FR-011: Eigener Scanner als Alternative zu nmap

**Status:** erfasst am 27.09.2026

### Anlass

nmap lässt sich in einem proprietären Produkt nur mit OEM-Lizenz nutzen (FR-005).

### Festgelegt

- nmap bleibt wie es ist; dazu kommt ein eigener, lizenzfreier Scanner in Go mit dem Anspruch,
  nmap gleichwertig zu sein: TCP-SYN- und Connect-Scan, UDP, Dienst- und Versionserkennung,
  CPEs für den CVE-Abgleich, OS-Erkennung.
- nmaps Signaturdatenbanken (`nmap-service-probes`, `nmap-os-db`) stehen selbst unter der NPSL
  und dürfen nicht übernommen werden; die Erkennung beruht auf eigenen Proben und Signaturen.
- Die Erkennungsleistung wird im echten Netz gegen nmap gemessen und dokumentiert.

---

## FR-012: Englische Oberfläche

**Status:** umgesetzt am 27.09.2026 – README „Sprache: Deutsch und Englisch“, ARCHITECTURE
„Sprachen“, FRONTEND.md Abschnitt 9, PLUGINS.md „Übersetzungen“

### Festgelegt

Oberfläche auf Deutsch und Englisch, umschaltbar je Benutzer (Standard: Browsersprache);
dazu die Texte, die der Server liefert (Plugin-Einstellungen, Events, Fehlermeldungen,
Berichte).

### Umsetzung – Entscheidungen und Abweichungen

- **Deutsch bleibt Quelle, der deutsche Text ist der Schlüssel:** keine Schlüssel wie
  `devices.delete.title`, sondern `t('Gerät löschen')` in der Oberfläche und ein Katalog
  „deutscher Text → Englisch“ (`web/src/lib/i18n/en/*.json`, rund 2 800 Einträge). Im Server
  registriert jedes Paket seine Übersetzungen in `i18n_en.go` (`internal/i18n`, rund 3 000
  Einträge). So bleibt im Code lesbar, was angezeigt wird, und die vorhandenen Texte mussten
  nicht umbenannt werden. Preis: Wer einen deutschen Text ändert, muss den Katalogeintrag
  mitziehen – die Prüfungen melden das.
- **Formatierte Texte als Muster:** `fmt`-Texte stehen mit ihren Verben im Katalog
  (`"Neues Gerät: %s"` → `"New device: %s"`, Reihenfolge per `%[2]s`). Damit übersetzt die API
  auch Texte, die zur Laufzeit auf Deutsch entstehen und gespeichert werden: Event-Titel und
  -Nachrichten, Lauf-Fehler, Run-Logs und Server-Log, Audit-Log, Health-Fehler,
  Benachrichtigungsverlauf. Fehler in `%w`/`%v` werden mitübersetzt, Namen in `%s` bleiben.
- **Gespeicherte Events:** Titel bleiben in der Datenbank deutsch und werden beim Lesen
  übersetzt – das gilt auch für Events aus der Zeit vor FR-012. Passt kein Muster (eigene
  Titel, Texte älterer Versionen), bleibt der gespeicherte Text stehen. Die Suche (`q`)
  durchsucht weiter den deutschen Text.
- **Sprache einer Anfrage:** Einstellung des Benutzers (neue Spalte `users.locale`, Migration
  0012; `PUT /api/v1/auth/preferences`, Administratoren auch unter Benutzer), sonst
  `Accept-Language`, sonst Deutsch. Die Oberfläche schickt ihre Sprache als
  `Accept-Language` mit; die Anmeldeseite folgt immer dem Browser.
- **Umschalten lädt die Seite neu:** Die Sprache steht für die Lebensdauer der Seite fest.
  Das ist einfacher als ein reaktiver Wechsel und holt zwischengespeicherte Server-Kataloge
  (Plugins, Event-Typen) gleich mit in der neuen Sprache. Gilt für einen Benutzer eine andere
  Sprache als die, mit der die Seite gestartet ist, lädt sie einmal neu.
- **Formate:** Deutsch unverändert (27.09.2026 14:05, 1.234,5, „12 %“); Englisch in der
  englischen Variante des Browsers, sonst en-GB (27/09/2026 14:05) – ein deutscher Browser mit
  englischer Oberfläche bekommt so kein US-Datum.
- **Benachrichtigungen und Berichte (über den Auftrag hinaus):** Sie gehen an Empfänger ohne
  Sitzung, daher eine Systemeinstellung „Sprache für Benachrichtigungen und Berichte“
  (Standard Deutsch). E-Mail, Telegram, ntfy, Webhook, n8n und geplante Berichte schreiben in
  dieser Sprache; in der Oberfläche heruntergeladene Berichte in der Sprache des Benutzers.
  Die CLI im Container spricht Englisch mit `LANG=en_…`, `cron`-Beschreibungen gibt es in
  beiden Sprachen, ebenso die API-Dokumentation.
- **Prüfungen:** `go test ./internal/i18n/` verlangt für jedes Label, jede Beschreibung und
  Option der ausgelieferten Kataloge (Plugins, Aktionen, Events, Credential-Typen,
  Berechtigungen, Filterfelder) eine Übersetzung und sucht im Quelltext deutsche Texte ohne
  Eintrag (Heuristik: Umlaute und typische deutsche Wörter; deutsche Daten wie CSV-Spaltennamen
  tragen `// i18n:ignore`). Die Oberfläche: `t()` ist typisiert, ein fehlender Eintrag ist ein
  Fehler in `npm run check`; dort prüft `scripts/i18n-check.mjs` zusätzlich doppelte und
  ungenutzte Schlüssel, Platzhalter und deutschen Text außerhalb von `t()`.
- **Kleine Änderungen am deutschen Text:** Wo ein deutsches Wort in zwei Bedeutungen vorkam,
  bekam eine Stelle einen eindeutigen Text (z. B. „Laufprotokoll“ statt „Protokoll“, „Abgleich
  läuft“, „Token „…“ wurde widerrufen“); Zahlen mit Einzahl heißen jetzt richtig „1 Gerät“,
  „1 Versuch“ usw.
- **Nicht übersetzt:**
  - Daten: Gerätenamen, Namen und Beschreibungen von Rollen (auch der vorgegebenen „Bearbeiter“
    und „Betrachter“ – eine Übersetzung beim Anzeigen würde sie beim nächsten Speichern
    umbenennen), Werte in Event-Payloads (`direction: eingehend`), auf die Regeln filtern.
  - Der Agent und seine Installationsskripte (laufen auf dem Zielsystem ohne Sitzung).
  - Log-Attribute: Nur Fehlertexte werden übersetzt, Schlüssel wie `zeile` bleiben; das
    Server-Log auf stdout bleibt deutsch (übersetzt wird die Anzeige in der Oberfläche).
  - Teilweise deutsch bleiben zusammengesetzte Texte ohne passendes Muster: die
    Zusammenfassung der CVE-Aktion mit mehreren Teilen („… · …“), der Tunnelzustand
    „Konfiguration ungültig: …“, deutsche Plugin-Namen innerhalb von Titeln wie
    „%s: Lauf fehlgeschlagen“ und in Audit-Einträgen, eigene Berichtstitel.
- **Getestet:** Muster, Auswahl der Sprache und Katalogvollständigkeit als Unit-Tests
  (`internal/i18n`), API-Tests für Präferenz, `Accept-Language`, Feldfehler und gespeicherte
  Events, englische Cron-Beschreibungen, Berichte (Markdown, E-Mail, PDF) und CLI-Texte;
  Oberfläche per svelte-check und i18n-check.

---

## FR-004: NetScope-Agent für überwachte Systeme

**Status:** umgesetzt am 26.09.2026 · erfasst am 25.09.2026 – Bedienung im README
(„NetScope-Agent“), Technik in ARCHITECTURE („NetScope-Agent“)

### Anlass

Linux-Systeme lassen sich bisher nur per SSH inventarisieren: NetScope braucht dafür Zugang,
Zugangsdaten und Erreichbarkeit (keine NAT, passende Ports). Gewünscht: ein Befehl auf dem
System, danach meldet es sich von selbst bei NetScope und lässt sich darüber abfragen.

### Festgelegt

| Frage | Entscheidung |
|---|---|
| Plattformen | **Linux** (amd64, arm64, armv7): VMs, LXCs, Server, Raspberry Pis. |
| Umfang | **Inventar wie per SSH und Auslastung** (CPU, RAM, Platten, Netz) als Verläufe, mit Events für volle Dateisysteme. |
| Darf NetScope etwas ausführen? | **Nein, nur lesen.** Einzige Anstöße: „jetzt Inventar liefern“ und neue Einstellungen. |
| Updates | **Automatisch** von der Instanz, geprüft per SHA-256. |

### Umsetzung – Entscheidungen und Abweichungen

- **Gleiche Erfassung wie SSH:** Die feste Befehlsliste liegt in `internal/hostscript`; der Agent
  führt sie lokal aus und schickt die Ausgabe, NetScope zerlegt sie mit dem Code des
  SSH-Inventars. Dadurch bleibt der Agent klein (~6 MB) und beide Wege liefern dasselbe.
- **Ohne root:** Der Agent läuft als Benutzer `netscope-agent`; ohne root fehlen nur die
  Prozessnamen fremder Benutzer an offenen Ports. Docker nur mit `--docker` (Gruppe docker).
- **Updates vs. „nur lesen“:** Automatische Updates geben der Instanz die Möglichkeit, neuen
  Code auf die Systeme zu bringen – begrenzt auf die Rechte von `netscope-agent`. Das ist im
  README unter Sicherheit beschrieben.
- **Verbindung:** Long Poll statt dauerhafter WebSocket-Verbindung – funktioniert durch
  Proxys, „Inventar jetzt“ kommt trotzdem sofort an.
- **Installations-Tokens** mit Ablauf, Nutzungszahl und Tags; bereits installierte Agents
  laufen nach dem Widerruf weiter. Entfernen in NetScope beendet den Dienst beim nächsten
  Kontakt (Exit-Code 3, systemd startet ihn nicht neu).
- **Nicht umgesetzt:** Windows-Agent, beliebige Befehle oder Aktionen über den Agent.

---

## FR-003: Mehrere Benutzer mit Rollen und Zwei-Faktor-Anmeldung

**Status:** umgesetzt am 25.09.2026 · erfasst am 25.09.2026 – Bedienung im README
(„Benutzer, Rollen und Zwei-Faktor-Anmeldung“), Technik in ARCHITECTURE („Benutzer und Rechte“)

### Anlass

NetScope kannte nur einen Administrator. Wer weiteren Personen Einblick geben wollte, musste
das Admin-Passwort teilen oder ein API-Token herausgeben. Gewünscht: mehrere Benutzer mit
unterschiedlichen Rechten (RBAC) und eine Zwei-Faktor-Anmeldung.

### Festgelegt

| Frage | Entscheidung |
|---|---|
| Rollenmodell | **Frei definierbare Rollen** mit einer Berechtigungsmatrix; vorgegeben sind Administrator (fest, alle Rechte), Bearbeiter und Betrachter. |
| Art der 2FA | **TOTP und Passkeys** (WebAuthn), dazu Wiederherstellungscodes. |
| 2FA-Pflicht | **Pro Rolle einstellbar**; Betroffene richten sie beim nächsten Login ein. |
| Beschränkung auf Standorte | **Nein, vorerst nicht.** Jeder Benutzer sieht alle Standorte; jede Instanz hat eigene Benutzer. |

### Umsetzung – Entscheidungen und Abweichungen

- **Lesen braucht kein Recht.** 20 Rechte in vier Bereichen regeln nur Änderungen sowie das
  Einsehen von Credentials, Backups und Protokollen. Kritische Rechte (Plugins, Credentials,
  Subnetze, Verbund, System, Backups, Benutzer) sind im Rollen-Editor markiert.
- **Start-Passwort:** Ein vom Administrator angelegtes oder zurückgesetztes Konto muss beim
  ersten Login ein eigenes Passwort wählen; bis dahin ist die Sitzung auf die Einrichtung
  beschränkt – ebenso bei fehlendem zweiten Faktor trotz 2FA-Pflicht.
- **Passkeys als zweiter Faktor** nach dem Passwort, nicht passwortlos. Sie funktionieren nur
  über HTTPS mit Hostnamen (Browser-Vorgabe); über `http://<IP>` bietet die Oberfläche nur
  TOTP an.
- **API-Tokens** bleiben ohne zweiten Faktor, haben aber höchstens die Rechte der Rolle ihres
  Benutzers; Anlegen braucht das Recht „Eigene API-Tokens anlegen“.
- **Sperre gegen Aussperren:** Mindestens ein aktiver Administrator bleibt immer; das eigene
  Konto lässt sich weder löschen noch deaktivieren. Im Notfall: `netscope 2fa-reset` und
  `netscope passwd --user` im Container.
- **Nicht umgesetzt:** Anmeldung über OIDC/SSO und Benutzer je Standort; beides lässt sich
  auf dem Rechte-Modell später ergänzen.

---

## FR-002: Mehrere NetScope-Instanzen bündeln (Zentrale und Standorte)

**Status:** umgesetzt am 24.09.2026 · erfasst am 24.09.2026 – Bedienung im README
(„Mehrere Standorte“), Technik in ARCHITECTURE („Verbund“)

### Anlass

Entfernte Netze werden heute über einen WireGuard-Tunnel von NetScope gescannt (z. B. das
Rechenzentrum 192.168.1.0/24). Das funktioniert, hat aber prinzipielle Grenzen:

- Durch den Tunnel gibt es keine MAC-Adressen, kein ARP, kein mDNS und kein SSDP.
- Proxmox-VMs ohne Gast-Agent lassen sich deshalb nicht mit ihren gescannten IPs verbinden.
- Geräte mit eigenem Standard-Gateway (VMs mit öffentlicher IP) antworten nicht in den
  Tunnel zurück.
- Jeder Host braucht Firewall-Freigaben für die Tunnel-Adresse (Ping, SSH).

Die Idee: In jedem Netz läuft eine eigene NetScope-Instanz, die lokal scannt. Eine Instanz
wird als Zentrale eingestellt und bündelt die anderen. Man kann gesammelt auf alles schauen
oder gezielt auf einen Standort.

### Festgelegt

| Frage | Entscheidung |
|---|---|
| Funktioniert ein Standort allein? | **Ja**, vollwertig mit Oberfläche, Scannern und Inventar – auch ohne Verbindung zur Zentrale. |
| Wer benachrichtigt? | **Der Standort meldet seine Events an die Zentrale**, die Zentrale stellt über ihre Regeln und Publisher zu. |
| Wo liegen Zugangsdaten? | **Nur am Standort.** Sie verlassen ihn nie. |
| Darf die Zentrale am Standort etwas auslösen? | **Nein.** Die Zentrale liest nur; Scans, Einstellungen und Zugangsdaten werden am Standort gepflegt. |

### Konzept

**Rollen.** Jede Instanz hat unter *System → Einstellungen* eine Rolle: *eigenständig*
(heutiges Verhalten), *Standort* (meldet an eine Zentrale) oder *Zentrale*.

**Datenfluss: Beobachtungen weiterreichen, keine Datenbank-Synchronisation.** Der Standort
verarbeitet alles wie heute und schickt zusätzlich an die Zentrale:

- seine **Beobachtungen** (die Ergebnisse der Plugins),
- die **Lauf-Zusammenfassungen** (welche Netze ein Lauf abgedeckt hat – nötig, damit die
  Zentrale Anwesenheit und Offline korrekt auswertet),
- seine **Events**.

Die Zentrale verarbeitet die Beobachtungen wie die eigenen, mit dem Standort als Etikett. Die
Events des Standorts übernimmt sie, statt aus denselben Beobachtungen eigene zu erzeugen –
sonst gäbe es jede Meldung doppelt. Ihre Regeln entscheiden dann über die Zustellung.

**Verbindung.**

- Der Standort baut die Verbindung nach außen auf (HTTPS, eigenes Token pro Standort mit der
  einzigen Berechtigung „Daten einliefern“). Am Standort sind keine eingehenden Freigaben,
  kein NAT und kein Tunnel nötig.
- Ist die Zentrale nicht erreichbar, puffert der Standort Beobachtungen und Events in seiner
  Datenbank und liefert sie nach.
- Das Protokoll trägt eine Versionsnummer, damit Zentrale und Standorte nicht gleichzeitig
  aktualisiert werden müssen.

**Datenmodell.**

- *Standort* wird ein eigener Begriff: an Subnetzen, Geräten, Beobachtungen und Events.
- **IP-Adressen und Subnetze gelten pro Standort.** Dasselbe `192.168.1.0/24` kann es an
  mehreren Standorten geben; ein IP-Treffer zählt nur innerhalb desselben Standorts.
- MAC-Adressen und externe Referenzen (z. B. Proxmox-VMIDs mit Node) bleiben
  standortübergreifend eindeutig.
- Manuelle Angaben (Namen, Tags, Notizen) pflegt jede Instanz für ihre Sicht. Die Zentrale
  schreibt nichts an die Standorte zurück.

**Oberfläche der Zentrale.**

- Standort-Auswahl im Kopf („Alle“, „Zuhause“, „Colo“ …) und `site:colo` in der
  Filtersprache; jede Seite (Geräte, Topologie, Events, Schwachstellen, Berichte) lässt sich
  so auf einen Standort einschränken.
- Übersicht der Standorte: verbunden oder nicht, letzte Meldung, Puffergröße, Version,
  Plugins mit Fehlern.
- Direktlink in die Oberfläche des jeweiligen Standorts.

**Standort ohne Oberfläche.** Optional kann ein Standort ohne Web-Oberfläche laufen und
dient dann als reiner Sammler.

### Etappen

1. Standort im Datenmodell (Subnetze und IP-Zuordnung pro Standort), Rollen,
   Einliefer-Schnittstelle der Zentrale, Pufferung am Standort, Standort-Auswahl und
   Standort-Übersicht.
2. Events der Standorte in der Regel-Engine der Zentrale (Bedingung „Standort“), Direktlinks,
   Übersicht der Plugin-Zustände aller Standorte.
3. Standort ohne Oberfläche, gemeinsame Anmeldung (Single Sign-on) für die Direktlinks.

### Offene Punkte

- Zentrale länger nicht erreichbar: Events nur puffern, oder nach einer Wartezeit zusätzlich
  über die Publisher des Standorts zustellen?
- Dasselbe Gerät von zwei Standorten gesehen (z. B. per Tunnel und lokal): Welcher Standort
  „besitzt“ es in der Zentrale?
- Aufbewahrung: Gelten die Fristen der Zentrale auch für eingelieferte Daten?

### Umsetzung – Entscheidungen und Abweichungen

- **Offene Punkte entschieden:**
  - Zentrale länger nicht erreichbar: Der Standort puffert (bis 250 000 Einträge, Events
    bleiben immer erhalten) und liefert nach. Eine Ersatzzustellung gibt es nicht; wer sie
    will, legt am Standort eigene Regeln an – der Standort arbeitet ohnehin eigenständig.
  - Dasselbe Gerät von zwei Standorten: MAC und externe Referenz gelten global, also ein
    Gerät; es gehört dem Standort, der es zuerst geliefert hat. Über die IP allein wird nie
    standortübergreifend zugeordnet.
  - Aufbewahrung: Es gelten die Fristen der Zentrale, auch für eingelieferte Daten.
- **Anwesenheit:** Statt Lauf-Zusammenfassungen liefert der Standort die Ergebnisse seiner
  Auswertung (Offline-Wechsel, IP-Wechsel). So gilt seine Einstellung „offline nach N
  verpassten Läufen“, und die Zentrale muss seine Scan-Bereiche nicht kennen.
- **Erstabgleich:** Beim ersten Kontakt und nach Lücken schickt der Standort seinen
  vollständigen Bestand (Zustand je Quelle als Beobachtungen) und am Ende die Liste seiner
  Geräte; Geräte, die er nicht mehr hat, entfernt die Zentrale.
- **Identität:** Löschen, Zusammenführen und Aufteilen am Standort werden in der Zentrale
  nachvollzogen. Nach einer Wiederherstellung am Standort ordnet die Zentrale alle Geräte
  neu zu (die Geräte-IDs sind dann andere).
- **Zusätzlich:** Events `site.down`/`site.up`, Standort-Kennzeichen in allen Publishern,
  Topologie je Standort, Anbindung per Umgebungsvariablen, Fingerprint-Pinning für selbst
  signierte Zertifikate der Zentrale.
- **Nicht umgesetzt:** gemeinsame Anmeldung (Single Sign-on) für die Direktlinks. Sie würde
  der Zentrale eine Sitzung am Standort geben und widerspricht damit der Festlegung, dass die
  Zentrale am Standort nichts auslösen darf. Die Direktlinks öffnen den Standort mit seiner
  eigenen Anmeldung.
- Das manuelle Gerätefeld „Standort“ heißt jetzt „Aufstellort“, damit es nicht mit den
  NetScope-Standorten verwechselt wird (CSV-Spalte „Standort“ wird weiter erkannt).

### Abnahme

- Tests: gleiche IP an zwei Standorten ergibt zwei Geräte; gleiche MAC ergibt ein Gerät;
  Anwesenheit in der Zentrale entspricht der am Standort; Pufferung und Nachlieferung nach
  Verbindungsabbruch; keine doppelten Events.
- Ende-zu-Ende: zwei Instanzen (Zuhause als Zentrale, Colo als Standort) mit einem
  gemeinsamen Inventar in der Zentrale.
- Doku: README (Abschnitt „Mehrere Standorte“), ARCHITECTURE (Rollen, Protokoll).
