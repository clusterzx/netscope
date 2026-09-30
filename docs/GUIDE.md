# NetScope – Handbuch

Die Bedienung im Detail. Überblick und Schnellstart stehen in der [README](../README.de.md)
(englisch: [README.md](../README.md)).

- [Erster Start](#erster-start)
- [Hinter einem Reverse-Proxy (Traefik)](#hinter-einem-reverse-proxy-traefik)
- [Konfiguration](#konfiguration)
- [Plugins](#plugins)
- [NetScope-Agent](#netscope-agent)
- [Entfernte Netze (Router, WireGuard)](#entfernte-netze-router-wireguard)
- [Mehrere Standorte (Verbund)](#mehrere-standorte-verbund)
- [Racks](#racks)
- [Filter-Query-Sprache](#filter-query-sprache)
- [Regeln und Benachrichtigungen](#regeln-und-benachrichtigungen)
- [Schwachstellen](#schwachstellen)
- [Benutzer, Rollen und Zwei-Faktor-Anmeldung](#benutzer-rollen-und-zwei-faktor-anmeldung)
- [Sprache: Deutsch und Englisch](#sprache-deutsch-und-englisch)
- [API](#api)
- [Backup und Wiederherstellung](#backup-und-wiederherstellung)
- [Sicherheit](#sicherheit)

## Erster Start

Eine neue Installation richtet man im Browser mit dem **Einrichtungsassistenten** ein. Bis er
abgeschlossen ist, läuft kein Plugin – NetScope scannt nichts, bevor du entschieden hast, was.

**Einrichtungscode.** Solange es kein Konto gibt, gehört der Assistent dem, der die Seite zuerst
öffnet – und die Instanz speichert später Zugangsdaten für das ganze Netz. Deshalb verlangt er
einen Code, den NetScope beim Start ins Log schreibt und in `data/setup-code.txt` ablegt:

```bash
docker logs netscope 2>&1 | grep -i einrichtung   # oder:
cat data/setup-code.txt                           # z. B. K7QM-2XRP-9BWD
```

Groß-/Kleinschreibung und Bindestriche sind egal. Fehlversuche zählen zum Rate-Limit der
Anmeldung (nach 5 Fehlversuchen wird die Adresse gesperrt, 30 s und länger). Nach dem Abschluss
wird die Datei gelöscht und der Code gilt nicht mehr. Jeder Schritt hat „Zurück“, die Eingaben
bleiben erhalten (auch beim Neuladen der Seite).

| Schritt | Inhalt |
|---|---|
| 1. Sprache und Zeitzone | Deutsch oder Englisch (Vorschlag: Browsersprache) – Sprache des Kontos, der Benachrichtigungen und Berichte. Zeitzone als durchsuchbare Liste (Vorschlag: Zeitzone des Browsers, sonst `NETSCOPE_TIMEZONE`) |
| 2. Konto | Benutzername (Vorschlag `admin`), Anzeigename, Passwort (mindestens 10 Zeichen). „Konto anlegen“ legt den Administrator an und meldet dich an; ab hier arbeitet der Assistent mit dieser Sitzung. Optional gleich TOTP einrichten |
| 3. Instanz | Rolle *allein*, *Zentrale* (mit dem Namen dieser Instanz, z. B. „Zuhause“) oder *Standort* (URL und Token der Zentrale, optional Zertifikat-Fingerprint, „Verbindung testen“). Öffentliche URL (Vorschlag: die aufgerufene Adresse) – für Links in Benachrichtigungen, den Installationsbefehl des Agents und Passkeys |
| 4. Netze | Erkannte Subnetze bestätigen, abwählen oder umbenennen, weitere hinzufügen (direkt oder über Router erreichbar; WireGuard-Tunnel später unter **System → Subnetze**). Ausschlüsse: einzelne Adressen oder Netze, die kein Scanner abfragt. DNS-Server für Reverse-DNS (leer = DNS-Server des Systems, die erkannten werden angezeigt) |
| 5. Scanner | Alle Scanner mit Kurzbeschreibung, Belastung (hoch / mittel / gering) und Zeitplan, einzeln an- und abwählbar. Vorlagen: „Nur Anwesenheit“ (ARP, Ping), „Ohne belastende Scans“ (Vorauswahl), „Vollständig“. Auswertende Plugins (CVE-Abgleich, OUI-Hersteller, Änderungen, Topologie, Aufräumen …) bleiben an |
| 6. Quellen (optional) | Systeme, die NetScope mit Zugangsdaten abfragt, in Tabs: Router und Firewalls, Netzwerk-Controller, Virtualisierung und Container, DNS und DHCP, Server und Switches. Eingeschaltet zeigt eine Karte das Formular des Plugins; Zugangsdaten legt man direkt am Feld mit „Neu anlegen“ an (sie werden gleich ausgewählt), „Verbindung testen“ prüft Adresse und Anmeldung, „Übernehmen und aktivieren“ speichert |
| 7. Abschluss | Zusammenfassung, Pfad von `master.key` zum Sichern, optional sofort ein erster ARP-Scan und Ping. „Einrichtung abschließen“ übernimmt alles und startet die gewählten Scanner |

Wird der Assistent unterbrochen, nachdem das Konto angelegt ist, meldet man sich einfach an und
landet wieder im Assistenten (der Code wird dann nicht mehr gebraucht).

**Ohne Assistent:**

- `NETSCOPE_ADMIN_PASSWORD` gesetzt (automatisierte Installationen): Der Benutzer `admin` bekommt
  dieses Passwort, die direkt angeschlossenen Netze werden übernommen und alle ab Werk aktiven
  Plugins laufen sofort. Quellen mit Zugangsdaten bleiben aus, bis sie jemand einrichtet.
- Ohne Weboberfläche (`NETSCOPE_UI=false`, z. B. ein Standort, der nur sammelt): kein
  Assistent; ohne `NETSCOPE_ADMIN_PASSWORD` bekommt `admin` ein Zufallspasswort, das man bei
  Bedarf mit `netscope passwd` setzt.
- Bestehende Installationen (es gibt schon Benutzer) gelten beim Update als eingerichtet.

## Hinter einem Reverse-Proxy (Traefik)

NetScope hat keine eigene TLS-Logik. `X-Forwarded-For/-Proto/-Host` werden von
vertrauenswürdigen Proxys übernommen (Standard: private Netze und localhost, siehe
`trusted_proxies`); Session-Cookies sind dann `Secure`. Da der Container im Host-Netz läuft,
am einfachsten über den File-Provider:

```yaml
http:
  routers:
    netscope:
      rule: Host(`netscope.example.lan`)
      entryPoints: [websecure]
      tls: { certResolver: letsencrypt }
      service: netscope
  services:
    netscope:
      loadBalancer:
        servers:
          - url: http://192.168.10.123:8080
```

Unter **System → Einstellungen** die öffentliche URL (`https://netscope.example.lan`)
eintragen – sie wird für Deep-Links in Benachrichtigungen verwendet.

**Proxy mit Anmeldung** (Pangolin, Authentik, Authelia …): Agents, Standorte und Prometheus
melden sich nicht im Browser an. Diese Pfade müssen den Proxy ohne Login passieren – NetScope
prüft dort selbst Token bzw. Schlüssel:

| Pfad | Wer |
|---|---|
| `/agent/*` | Installationsskripte und Agent-Programme (ohne Geheimnisse) |
| `/api/v1/agent/*` | NetScope-Agents |
| `/api/v1/federation/ingest` | Standorte, die an diese Zentrale liefern |
| `/metrics` | Prometheus (nur falls genutzt) |

Alternativ Agents über die interne Adresse installieren; der Installationsdialog bietet sie an
und gibt sie im Befehl weiter.

## Konfiguration

`/data/config.yaml` enthält nur Bootstrap-Werte (wird beim ersten Start angelegt), alles
andere wird in der Oberfläche gepflegt. Jeder Wert ist per Umgebungsvariable überschreibbar:

| Schlüssel | Umgebungsvariable | Standard |
|---|---|---|
| `listen` | `NETSCOPE_LISTEN` | `:8080` |
| `data_dir` | `NETSCOPE_DATA_DIR` | `/data` |
| `log_level` | `NETSCOPE_LOG_LEVEL` | `info` (debug, info, warn, error) |
| `log_format` | `NETSCOPE_LOG_FORMAT` | `json` (oder `text`) |
| `timezone` | `NETSCOPE_TIMEZONE` | `Europe/Berlin` – nur der Startwert beim ersten Start; danach gilt die Einstellung unter **System → Einstellungen** (wirkt ohne Neustart) |
| `master_key_file` | `NETSCOPE_MASTER_KEY_FILE` | `<data_dir>/master.key` |
| – | `NETSCOPE_MASTER_KEY` | Schlüssel direkt (hat Vorrang vor der Datei) |
| `trusted_proxies` | `NETSCOPE_TRUSTED_PROXIES` | private Netze, localhost |
| – | `NETSCOPE_ADMIN_PASSWORD` | Passwort des ersten Admins (nur beim allerersten Start); überspringt den [Einrichtungsassistenten](#erster-start) |
| – | `NETSCOPE_CONFIG` | Pfad der Konfigurationsdatei |
| `ui` | `NETSCOPE_UI` | `true`; `false` = nur API (Standort als reiner Sammler) |
| – | `NETSCOPE_CENTRAL_URL`, `NETSCOPE_CENTRAL_TOKEN` | Standort: Zentrale und Token (legen die Anbindung fest, siehe [Mehrere Standorte](#mehrere-standorte-verbund)) |
| – | `NETSCOPE_CENTRAL_FINGERPRINT` | Standort: SHA-256 des Zertifikats der Zentrale (optional, für selbst signierte Zertifikate) |
| – | `NETSCOPE_AGENT_DIR` | `/usr/share/netscope/agent` – Agent-Builds, die die Instanz ausliefert (im Image enthalten) |

## Plugins

Pro Plugin in der Oberfläche einstellbar: aktiv/inaktiv, Cron-Zeitplan (mit Klartext-Vorschau),
alle Einstellungen, Scope (Subnetze, Gruppen, Tags, Geräte, Filter), Timeout, Wiederholungen
mit Backoff, Parallelität, „Jetzt ausführen“, Laufhistorie mit Protokoll. Änderungen wirken
sofort, ohne Neustart.

Die Plugin-Liste zeigt bei Scannern die **Belastung** der gescannten Geräte (hoch: `nmap`,
`nmap_udp`; mittel: `http`, `tls`; gering: die übrigen) und gruppiert die Quellen mit
Zugangsdaten nach Bereich (Router und Firewalls, Netzwerk-Controller, Virtualisierung und
Container, DNS und DHCP, Server und Switches). Diese Quellen haben in den Einstellungen
**„Verbindung testen“**: NetScope prüft mit den Werten des Formulars – ohne sie zu speichern –
Adresse und Anmeldung und nennt je System, was es lesen konnte. SSH-Inventar und SNMP fragen
dafür nach der Adresse eines Geräts. Fehlen noch Zugangsdaten, legt man sie in den Plugin-Einstellungen direkt am
Feld mit **„Neu anlegen“** an; der Dialog bietet nur die passenden Typen an und wählt das neue
Credential gleich aus.

**Vom Scannen ausgenommen** (System → Einstellungen): Adressen oder Netze, die kein Scanner
abfragt, z. B. empfindliche Geräte, die bei Port-Scans ausfallen. Sie fehlen in den Zielen aller
Läufe, `arp-scan` und `nmap` lassen sie auch beim Scan ganzer Subnetze aus, UPnP lädt ihre
Gerätebeschreibung nicht und mDNS fragt nicht nach ihrem Namen. Geräte, die nur unter
ausgenommenen Adressen erreichbar sind, gelten dadurch nicht als offline.

| Plugin | Art | Standard | Beschreibung |
|---|---|---|---|
| `arpscan` | Scanner | alle 5 min | Anwesenheit per ARP (arp-scan), mehrere Subnetze/Interfaces |
| `icmp` | Scanner | alle 5 min | Ping-Latenz und Paketverlust als Zeitreihe (min/avg/max) |
| `oui` | Scanner | alle 6 h | Hersteller aus lokaler OUI-Datei; Aktion „OUI-Datei aktualisieren“ lädt die IEEE-Listen |
| `dns` | Scanner | stündlich | Reverse-Lookup gegen konfigurierbaren Resolver (Standard: DNS-Server des Systems) |
| `mdns` | Scanner | alle 30 min | Bonjour/mDNS: Namen, Dienste, Modell-Hinweise |
| `netbios` | Scanner | stündlich | NetBIOS-Namen und Arbeitsgruppe |
| `upnp` | Scanner | alle 30 min | SSDP: Friendly Name, Hersteller, Modell |
| `nmap` | Scanner | täglich 03:00 | TCP-Ports, Dienste, Versionen, OS, CPE (`-sV -O --osscan-guess`), pro Host gestreamt |
| `nmap_udp` | Scanner | sonntags 04:00 | UDP-Scan der Top-Ports |
| `http` | Scanner | täglich 03:30 | Titel, Server-Header, Redirects, Favicon-Hash, 44 Web-App-Signaturen (erweiterbar) |
| `tls` | Scanner | täglich 03:45 | Zertifikate, Aussteller, Ablauf, Selbstsigniert, schwache Protokolle/Cipher |
| `snmp` | Scanner | aus | v2c/v3: System, Interfaces, ARP, Bridge-FDB, LLDP (für die Topologie) |
| `snmp_traffic` | Scanner | aus (alle 5 min) | Traffic, Auslastung, Fehler und Discards je Interface als Zeitreihen (Tab „Traffic“ am Gerät); Events bei Port-Ausfall und Überlast |
| `ssh` | Scanner | aus | Linux-Inventar: OS, Kernel, CPU/RAM/Disks, Pakete, Dienste, Sockets, Docker, Uptime, Updates – nur feste Lesekommandos; abweichende SSH-Ports je Adresse/Netz oder aus dem Portscan |
| `wol` | Aktion | – | Wake-on-LAN pro Gerät bzw. als Massenaktion |
| `agent` | Importer | laufend | Einstellungen der NetScope-Agents (Linux und Windows): Inventar- und Messintervall, Pakete/Docker, Schwelle „Dateisystem fast voll“, Zeit bis „Agent meldet sich nicht“, Leases von Windows-DHCP-Servern; manueller Lauf fordert bei allen Agents ein Inventar an |
| `proxmox` | Importer | aus | VMs/CTs mit VMID, Status, MACs, Ressourcen, Node; verknüpft VM ↔ Gerät („läuft auf Node X“); Online-Status aus Proxmox für Gäste, die kein Scanner erreicht (Event nur bei Autostart); mehrere Hosts/Cluster; optional Docker-Container in LXCs |
| `openwrt` | Importer | aus | DHCP-Leases und statische Leases (SSH oder LuCI-RPC), mehrere Router |
| `opnsense` | Importer | aus | OPNsense über die REST-API: Leases (Kea, Dnsmasq, ISC), Reservierungen, ARP-Tabelle; alte (camelCase) und neue URLs ab 25.7 |
| `pfsense` | Importer | aus | pfSense per SSH: Leases (ISC oder Kea, bevorzugt über den Kea-Steuer-Socket), statische Zuordnungen aus config.xml, `arp -an` |
| `unifi` | Importer | aus | UniFi-Controller (UniFi OS oder selbst gehostet): Clients mit Switch-Port bzw. Access Point, SSID, VLAN, feste IPs, bekannte Offline-Clients, die UniFi-Geräte selbst; mit API-Schlüssel nur verbundene Clients |
| `mikrotik` | Importer | aus | RouterOS 7 über die REST-API: DHCP-Leases, ARP, Bridge-Hosttabelle (Port je Gerät) |
| `fortigate` | Importer | aus | FortiOS-REST-API: DHCP-Leases, ARP, erkannte Geräte (Name, OS, FortiSwitch-Port, FortiAP), VDOMs |
| `sophos` | Importer | aus | Sophos Firewall über die XML-API: nur DHCP-Reservierungen (aktuelle Leases bietet SFOS dort nicht an) |
| `meraki` | Importer | aus | Meraki-Dashboard-API: Clients (VLAN, SSID, Port bzw. AP, OS), Meraki-Geräte, feste IPs der MX |
| `fritzbox` | Importer | aus | FRITZ!Box über TR-064: Geräteliste mit LAN/WLAN, Port und Verbindungsstatus |
| `pihole` | Importer | aus | Pi-hole v6: DHCP-Leases, Reservierungen und die Netzwerk-Tabelle (Namen aus DNS-Anfragen) |
| `docker` | Importer | aus | Container, Images, Ports, Compose-Projekte (lokaler Socket, TCP oder SSH-Tunnel) |
| `netalertx` | Importer | manuell | Einmaliger Import einer NetAlertX-Datenbank oder -CSV |
| `csv` | Importer | manuell | Generischer Inventar-Import (Export: Reports) |
| `diff` | Processor | stündlich | Änderungen → Events; stündliche Prüfung ablaufender Zertifikate; Flap-Dämpfung für Online/Offline (keine Events für Handys/Tablets, max. 4 Wechsel pro Gerät in 24 h – einstellbar) |
| `cve` | Processor | täglich 04:30 | NVD-Spiegel (JSON-2.0-Feeds), CISA KEV und FIRST EPSS, CVE-Abgleich der CPEs |
| `healthcheck` | Processor | alle 30 s | TCP/HTTP/TLS/ICMP-Checks mit Flap-Dämpfung und Verfügbarkeit |
| `topology` | Processor | alle 15 min | Graph aus FDB, LLDP, ARP, Proxmox und Docker |
| `cleanup` | Processor | alle 15 min | Downsampling der Zeitreihen, Aufbewahrungsfristen je Datenart (Rohdaten 30 Tage, Anwesenheits-Scans 2 Tage, Historie 1 Jahr; Events bleiben immer) |
| `report` | Processor | aus | Geplanter Wochenbericht über Publisher |
| `telegram`, `webhook`, `ntfy`, `email`, `n8n` | Publisher | aus | Zustellung der Benachrichtigungen, siehe [PUBLISHERS.md](PUBLISHERS.md) |

### Zugangsdaten (Credentials)

Zugangsdaten werden unter **Credentials** angelegt, verschlüsselt gespeichert und in den
Plugin-Einstellungen nur referenziert. Die API liefert Secrets nie aus.

**Gilt für (Geltungsbereich):** Jedes Credential hat einen Geltungsbereich – überall, ein
oder mehrere Subnetze oder bestimmte Geräte (einzeln, per Gruppe, Tag oder Filter). Plugins,
bei denen keine Zugangsdaten fest ausgewählt sind, nehmen pro Ziel automatisch die passenden –
das spezifischste zuerst: dem Gerät zugewiesen → Gruppe/Tag/Filter → Subnetz → überall. Wird
eins abgelehnt, probieren sie das nächste; das funktionierende merken sich SSH- und
SNMP-Scanner pro Gerät. So bekommen zwei Proxmox-Hosts oder mehrere Router jeweils ihr eigenes
Token bzw. Passwort, ohne dass man Plugins doppelt konfigurieren muss. Welche Zugangsdaten für
ein Gerät gelten, zeigt die Karte **Zugangsdaten** auf der Geräteseite
(`GET /api/v1/devices/{id}/credentials`). Eine feste Auswahl im Plugin schränkt auf diese
Credentials ein (die Reihenfolge nach Geltungsbereich bleibt).

- **SSH** (`ssh`, `openwrt`, `docker` über `ssh://`): Benutzer mit privatem Schlüssel und/oder
  Passwort. Hostschlüssel werden beim ersten Kontakt gespeichert (TOFU) und danach geprüft.
  Der SSH-Scanner führt nur eine feste Liste lesender Befehle aus.
- **SNMP v2c/v3**: Community bzw. Benutzer mit Auth/Privacy-Protokollen.
- **Proxmox**: API-Token mit Leserechten, z. B.:
  ```bash
  pveum user add netscope@pve
  pveum aclmod / -user netscope@pve -role PVEAuditor
  pveum user token add netscope@pve netscope --privsep 0
  ```
  Im Credential (Typ API-Token) als Token-ID `netscope@pve!netscope` plus Secret eintragen und
  bei mehreren Hosts unter „Gilt für“ dem jeweiligen Proxmox-Gerät zuweisen.
- **OpenWrt/GL.iNet**: `root` mit dem Router-Passwort (SSH) oder LuCI-RPC (Paket `luci-mod-rpc`).

### Docker-Container in Proxmox-LXCs

Die Proxmox-API schaut nicht in Container hinein. Mit der Option **Docker-Container in LXCs
erfassen** meldet sich der Proxmox-Importer per SSH am Node an und liest mit `pct exec` die
Docker-Container aller laufenden LXCs (`docker ps`, `docker images`, `docker version` – nur
lesend). Die Container erscheinen am jeweiligen LXC-Gerät und in der Topologie.

Empfohlen ist ein eigener Schlüssel, der auf dem Node **nur** das Inventar-Skript ausführen
darf (Forced Command). Das Skript
[scripts/proxmox/netscope-docker-inventory](../scripts/proxmox/netscope-docker-inventory) aus dem
Repository auf jeden Node kopieren:

```bash
scp scripts/proxmox/netscope-docker-inventory root@pve.lan:/usr/local/sbin/
ssh root@pve.lan chmod 0755 /usr/local/sbin/netscope-docker-inventory
```

Dann auf dem Node in `/root/.ssh/authorized_keys` eine Zeile mit dem öffentlichen Schlüssel von
NetScope ergänzen – `from=` auf die IP von NetScope setzen:

```
command="/usr/local/sbin/netscope-docker-inventory",from="192.168.10.123",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA… netscope
```

Der Schlüssel kann dann weder eine Shell öffnen noch andere Befehle ausführen oder Ports
weiterleiten – egal, was NetScope sendet, der Node führt nur das Skript aus. In NetScope ein
SSH-Credential (Benutzer `root`, privater Schlüssel) anlegen und unter „Gilt für“ den
Proxmox-Node wählen. Ohne Forced Command funktioniert es auch mit einem normalen root-Zugang;
NetScope schickt dann dasselbe Skript als Befehl mit.

## NetScope-Agent

Statt per SSH abzufragen, kann ein Linux- oder Windows-System den **NetScope-Agent**
installieren. Er verbindet sich von sich aus mit NetScope (auch hinter NAT und Firewalls, ohne SSH-Zugang und
ohne Zugangsdaten in NetScope) und liefert:

- das **Inventar** wie das SSH-Inventar – dieselbe feste Liste von Lesebefehlen: OS, Kernel,
  CPU/RAM/Platten, Pakete (für den CVE-Abgleich), Dienste, offene Ports, Docker, Updates;
  standardmäßig stündlich und auf Knopfdruck,
- die **Auslastung**: CPU, Arbeitsspeicher, Swap, Last, Belegung je Dateisystem und
  Durchsatz je Netzwerkschnittstelle – als Verläufe im Tab **Auslastung** des Geräts.

**Installieren:** Unter **Agents → Agent installieren** entsteht ein Befehl mit einem
Installations-Token (gültig z. B. 30 Tage, für beliebig viele oder eine festgelegte Anzahl
Systeme, optional mit Tags für die Geräte). Auf dem System als root ausführen:

```bash
curl -fsSL http://192.168.10.123:8080/agent/install.sh | sudo sh -s -- --token nse_…
```

Das Skript lädt den Agent von der Instanz (amd64, arm64, armv7), prüft die Prüfsumme, legt den
Benutzer `netscope-agent` an und richtet einen systemd- bzw. OpenRC-Dienst ein. `--docker`
nimmt den Agent in die Gruppe `docker` auf, damit er Container sieht – das entspricht
root-Rechten auf dem System. Entfernen: dasselbe Skript mit `--uninstall`.

**Betrieb:** Der Agent läuft ohne root, führt nur die festen Lesebefehle aus und nimmt von
NetScope nichts entgegen außer „jetzt Inventar liefern“, seinen Einstellungen und einer neuen
Version von sich selbst: Nach einem NetScope-Update holt er die passende Version von der
Instanz, prüft die SHA-256-Prüfsumme und startet neu. Meldet sich ein Agent länger nicht
(Standard 5 Minuten), entsteht das Event „Agent meldet sich nicht“; scannt kein anderer
Scanner das Gerät, geht es offline. Ein Dateisystem über der Schwelle (Standard 90 %) löst
„Dateisystem fast voll“ aus. Einstellungen im Plugin **NetScope-Agent**.

Agents melden sich bei der Instanz an, deren Befehl sie ausführen – im Verbund also am
Standort, der die Daten wie alles andere an die Zentrale liefert. Wird ein Agent in NetScope
entfernt, beendet sich sein Dienst beim nächsten Kontakt; das Gerät bleibt.

### Windows

Derselbe Agent läuft als Windows-Dienst (Windows 10/11, Windows Server 2016 und neuer, amd64
und arm64). Unter **Agents → Agent installieren** auf **Windows** umschalten und den Befehl in
einer PowerShell **als Administrator** ausführen:

```powershell
& ([scriptblock]::Create((irm 'http://192.168.10.123:8080/agent/install.ps1'))) -Token nse_…
```

Das Skript lädt den Agent, prüft die Prüfsumme, legt ihn unter `C:\Program Files\NetScope Agent`
ab und richtet den Dienst `NetScopeAgent` unter dem virtuellen Konto `NT SERVICE\NetScopeAgent`
ein – ohne Administratorrechte. Konfiguration und Protokoll (`agent.log`) liegen in
`C:\ProgramData\NetScope Agent`, lesbar nur für SYSTEM und Administratoren. Nach Fehlern und
nach einem Selbst-Update startet Windows den Dienst neu. Entfernen: derselbe Befehl mit
`-Uninstall` statt `-Token …`. Ein selbst signiertes Zertifikat der Instanz pinnt
`-Fingerprint <SHA-256>`.

Das Inventar kommt aus einer festen Liste von Leseabfragen (CIM/WMI, Registry, Windows Update,
PowerShell-Module – nie `Win32_Product`):

- Windows-Version mit Build und Update-Revision, Hardware (Hersteller, Modell, Seriennummer,
  BIOS), CPU, RAM, Laufwerke, Netzwerkadapter, Domäne oder Arbeitsgruppe,
- installierte Programme (Liste **Installierte Pakete**), Hotfixes, **ausstehende Updates**
  (was Windows Update bereits kennt – ohne eigene Suche im Internet; Windows nennt sie nur
  Administratoren, also nur bei `-RunAsSystem`), „Neustart erforderlich“,
- Dienste, offene Ports mit Prozess, Firewall-Profile, Microsoft Defender und die im
  Sicherheitscenter registrierten Virenschutzprodukte.

Aus Build und Update-Revision (z. B. `10.0.22631.6199`) entsteht die CPE der Windows-Version:
Der **CVE-Abgleich** zeigt damit die Windows-Schwachstellen, deren Patch auf dem System noch
fehlt. Die Auslastung (CPU, RAM, Laufwerke, Netz) kommt ohne Last-Wert – den kennt Windows nicht.

**Windows-DHCP-Server:** Läuft der Agent auf einem DHCP-Server, liefert er zusätzlich Bereiche,
Leases und Reservierungen. Sie ergänzen wie die Router-Importer Namen und Adressen der Geräte
im Netz (Quelle „Windows-DHCP“; Einstellungen im Plugin **NetScope-Agent**, auf Wunsch mit neu
angelegten Geräten). Dafür nimmt das Installationsskript den Dienst in die lokale Gruppe
„DHCP Users“ auf. Auf einem Domänencontroller gibt es diese Gruppe nicht – dort mit
`-RunAsSystem` installieren.

## Entfernte Netze (Router, WireGuard)

Jedes Subnetz hat unter **System → Subnetze** eine **Erreichbarkeit**:

| Erreichbarkeit | Wann | Was NetScope dort kann |
|---|---|---|
| Direkt angeschlossen | NetScope hängt selbst im Netz | alles, inklusive ARP-Scan und MAC-Adressen |
| Über einen Router | anderes VLAN oder Standort, per Gateway erreichbar | Ping, Ports, Dienste, HTTP/TLS, SSH, SNMP, Health-Checks – ARP, mDNS und UPnP überspringen solche Netze; Geräte werden über die IP erkannt |
| Über WireGuard-Tunnel | Netz ist nur per VPN erreichbar, z. B. ein Rechenzentrum | wie „über einen Router“; den Tunnel baut NetScope selbst auf |

**WireGuard-Tunnel einrichten:**

1. Auf dem WireGuard-Server einen **eigenen Zugang für NetScope** anlegen, z. B. in der
   OPNsense unter *VPN → WireGuard → Peer generator*, und die Client-Konfiguration kopieren.
   Nicht die Konfiguration eines anderen Geräts verwenden – zwei Geräte mit demselben
   Schlüssel werfen sich gegenseitig aus dem Tunnel.
2. In NetScope das Subnetz anlegen, *Über WireGuard-Tunnel* wählen, die Konfiguration
   einfügen oder als `.conf` laden. NetScope zeigt Gegenstelle, Tunnel-Adresse und den
   eigenen öffentlichen Schlüssel an.
3. *Verbindung testen* (Handshake mit dem Server), speichern – fertig.

Weitere Subnetze hinter demselben Server (z. B. ein IPMI-Netz) wählen den vorhandenen Tunnel.
Die Konfiguration liegt verschlüsselt als Credential vom Typ *WireGuard-Tunnel* im Vault.

NetScope leitet **nur die zugeordneten Subnetze** durch den Tunnel – auch wenn die
Konfiguration `AllowedIPs = 0.0.0.0/0` enthält, bleibt der übrige Verkehr des Servers
unverändert. `PostUp`/`PreUp`/`PostDown`, `DNS` und `Table` werden ignoriert. Subnetze, die
sich mit einem lokal angeschlossenen Netz überschneiden oder für die der Host schon eine
Route hat, werden abgelehnt.

Fällt der Tunnel aus (kein Handshake seit 3 Minuten), gibt es das Event `tunnel.down`, die
Subnetze dahinter werden bis zur Rückkehr nicht gescannt und ihre Geräte **nicht** als
offline gewertet; danach folgt `tunnel.up` mit der Ausfalldauer. Der Zustand steht in der
Subnetz-Liste (verbunden, letzter Handshake, Traffic).

Für die Gegenseite gilt wie bei jedem VPN-Client: Firewall-Regel für die Tunnel-Adresse von
NetScope und ein Rückweg zu ihr. Geräte mit eigenem Standard-Gateway (z. B. VMs mit
öffentlicher IP) antworten sonst ins Internet statt in den Tunnel – dann auf dem
WireGuard-Server Outbound-NAT für das Tunnelnetz einrichten.

Voraussetzungen: Linux-Host mit WireGuard im Kernel (ab 5.6), Container mit Host-Netzwerk
und `NET_ADMIN` (wie in der mitgelieferten `docker-compose.yml`). Unter Docker Desktop
(Windows/macOS) ist die Option ausgegraut.

## Mehrere Standorte (Verbund)

Ein Tunnel reicht für viele entfernte Netze, hat aber Grenzen: keine MAC-Adressen, kein ARP,
mDNS oder SSDP, und jeder Host braucht Firewall-Freigaben. Für solche Netze läuft besser eine
**eigene NetScope-Instanz vor Ort (Standort)**, die an eine **Zentrale** liefert. Die Rolle
steht unter **System → Verbund**: *eigenständig* (Standard), *Standort* oder *Zentrale*.

| | Standort | Zentrale |
|---|---|---|
| Scans, Plugins, Zugangsdaten | eigene; Zugangsdaten verlassen den Standort nie | eigene, nur für ihre eigenen Netze |
| Oberfläche | vollständig, arbeitet auch ohne Zentrale | alle Netze gemeinsam oder je Standort |
| Benachrichtigungen | eigene Regeln (optional) | Events aller Standorte laufen durch ihre Regeln |
| Manuelle Angaben (Name, Tags, Notizen …) | gelten am Standort | gelten in der Zentrale; nichts wird zurückgeschrieben |

**Einrichten:**

1. In der Zentrale unter **System → Verbund** die Rolle *Zentrale* wählen (optional einen
   Namen für diese Instanz, z. B. „Zuhause“).
2. Unter **Standorte** einen Standort anlegen. Das Token wird genau einmal angezeigt.
3. Am Standort unter **System → Verbund** die Rolle *Standort* wählen, die Adresse der
   Zentrale und das Token eintragen, speichern – der Verbindungstest folgt automatisch.
   Ohne Oberfläche geht es auch per Umgebung: `NETSCOPE_CENTRAL_URL`,
   `NETSCOPE_CENTRAL_TOKEN` und `NETSCOPE_UI=false`.

Der Standort baut die Verbindung selbst auf (HTTPS empfohlen); am Standort sind keine
eingehenden Freigaben nötig. Er liefert, was er lernt: Beobachtungen der Plugins,
Offline-Wechsel, IP-Wechsel, Löschen/Zusammenführen/Aufteilen von Geräten und seine Events.
Beim ersten Kontakt (und nach einer Lücke) schickt er seinen kompletten Bestand. Ist die
Zentrale nicht erreichbar, puffert er alles in seiner Datenbank und liefert es in der
richtigen Reihenfolge nach – nichts doppelt. Die Zentrale meldet einen Standort, der sich
fünf Minuten nicht meldet, mit `site.down` (und `site.up`, sobald er wieder liefert).

In der Zentrale gilt:

- **IP-Adressen gelten je Standort:** Dasselbe `192.168.1.0/24` darf es an mehreren
  Standorten geben. MAC-Adressen und externe Referenzen (z. B. Proxmox-VMs) sind global –
  dasselbe Gerät von zwei Standorten ist ein Gerät.
- Die **Standort-Auswahl** oben schränkt Geräte, Topologie, Events, Schwachstellen,
  Dashboard und Export auf einen Standort ein; in der Filtersprache `site:colo` bzw.
  `site:local` für die Zentrale selbst. Regeln haben die Bedingung *Standorte*.
- Die **Events eines Standorts** übernimmt die Zentrale mit Standort-Kennzeichen; für
  Geräte eines Standorts erzeugt sie keine eigenen (sonst gäbe es jede Meldung doppelt).
  Ihr CVE-Abgleich und die Topologie (je Standort abgeleitet) laufen trotzdem.
- Die Zentrale **löst am Standort nichts aus**: Scans, Aktionen und Health-Checks für
  Geräte eines Standorts gibt es dort; die Geräteseite verlinkt stattdessen auf den Standort
  (dessen öffentliche URL oder die unter *Standorte* hinterlegte Adresse).
- **Standorte** zeigt je Standort Verbindung, letzte Meldung, Puffer, Version, Subnetze und
  Plugins mit Fehlern. Ein neues Token macht das alte sofort ungültig; *Entfernen* löscht die
  gelieferten Geräte und Events aus der Zentrale (am Standort bleibt alles).

Wird ein Netz bisher per Tunnel von der Zentrale gescannt, das Subnetz dort entfernen,
sobald der Standort liefert – sonst erscheinen Geräte ohne MAC doppelt (einmal über den
Tunnel, einmal vom Standort).

## Racks

Unter **Racks** dokumentierst du den physischen Aufbau: Racks mit ihren Höheneinheiten (HE),
die eingebauten Geräte und passiven Elemente, und was an welchem Port steckt. Die
Verbindungen fließen in die Topologie ein.

**Rack anlegen:** Name, Standort, Breite (19" oder 10"), Höhe in HE (1–60) und Zählung
(HE 1 unten oder oben). Die Übersicht zeigt je Rack, wie viel belegt ist und ob eingebaute
Geräte offline sind.

**Einbauen:** Klick auf eine freie Höheneinheit (oder „Einbauen“) und dann entweder ein
**Gerät aus dem Inventar** wählen oder ein passives Element: Patchfeld, Fachboden, Blende,
Kabelführung, Steckdosenleiste, Sonstiges. Jedes Element hat eine Höhe, eine Seite (vorne oder
hinten, „volle Tiefe“ belegt beide), und eine Breite: voll, halb oder ein Drittel – so passen
Mini-PCs oder Raspberry Pis nebeneinander in eine HE. Elemente lassen sich mit der Maus auf
eine andere HE (bei halber und drittel Breite auch zur Seite) ziehen, mit den Pfeiltasten im
Seitenpanel um eine HE verschieben oder beim Bearbeiten in ein anderes Rack verschieben. Ein
Gerät steckt in höchstens einem Rack. Wird es aus dem Inventar gelöscht, bleibt sein Platz mit
dem letzten Namen stehen; beim Zusammenführen übernimmt das Zielgerät den Platz.

**Ports:** Bei einem Gerät mit SNMP kommen die Ports aus der Interface-Tabelle (nur
physische Ports, mit Link-Status und Geschwindigkeit). Ohne SNMP gibst du die Anzahl und
ein Präfix an (`ether` → ether1, ether2 …); Ports, die Importe oder die Topologie nennen
(UniFi, MikroTik, LLDP …), kommen automatisch dazu. Patchfelder haben die angegebene Zahl an
Ports.

**Was an einem Port steckt:**

- **Erkannt** (gestrichelt grün): Geräte, die andere Quellen an diesem Port sehen – die
  Topologie aus SNMP (Bridge-Tabelle, LLDP) und Importe wie UniFi oder MikroTik. Namen wie
  „5“, „Port 5“ oder „ether5“ werden dem richtigen Port zugeordnet.
- **Von Hand** (blau): Port anklicken, im Panel „Gerät anschließen“. Ein erkanntes Gerät
  übernimmst du mit „Übernehmen“; „N erkannte Verbindungen übernehmen“ beim Gerät übernimmt
  alle Ports mit genau einem erkannten Gerät auf einmal. Steht ein erkanntes Gerät im Rack an
  einem anderen Port, weist das Panel darauf hin.
- **Patchkabel** (in seiner Farbe): Port anklicken, „Kabel ziehen“ und den Ziel-Port
  anklicken (Esc bricht ab) – oder „Ziel auswählen …“, auch für Ports in einem anderen Rack.
  Farbe und Beschriftung (z. B. Kabelnummer) sind optional. Ein Geräte-Port nimmt ein Kabel,
  ein Patchfeld-Port zwei (vorne und hinten). An einem Patchfeld-Port trägst du die Dose ein
  („Dose Büro 1.04“) und das Gerät, das dort angeschlossen ist. Das Panel des Switch-Ports
  zeigt dann den ganzen Weg: Kabel zum Patchfeld → Dose → Gerät.

Jede von Hand eingetragene Verbindung – direkt oder über Kabel und Patchfelder – wird eine
geschützte Beziehung „Switch-Port“ (Quelle `rack`): Die Topologie zeigt sie, und die
automatische Ableitung hängt dieses Gerät nicht mehr um. Gelöst wird sie im Rack (Zuordnung
lösen, Kabel entfernen, Element ausbauen), nicht in der Topologie. Die Geräteseite zeigt unter
**Rack**, wo das Gerät eingebaut ist und an welchem Rack-Port es hängt.

Racks anlegen und ändern erfordert das Recht `devices.edit`; ansehen darf jeder angemeldete
Benutzer.

## Filter-Query-Sprache

In der Geräteliste, in Gruppen, Scopes und Regeln. Terme werden mit UND verknüpft, `|`
trennt Alternativen, `-` negiert, Werte mit Leerzeichen in Anführungszeichen:

```
tag:iot port:22 os:linux cve>=7 seen<24h
-state:known is:online vendor:"tp-link"
port:22|80 ip:192.168.10.0/24 app:grafana cert<30d
```

| Feld | Bedeutung |
|---|---|
| (Freitext) | Name, Hostname, IP, MAC, Hersteller, Modell, OS, Aufstellort, Besitzer, Notizen, Tags |
| `tag`, `group`, `type`, `vendor`, `model`, `os`, `name`, `hostname`, `location`, `owner`, `notes` | Stammdaten |
| `ip`, `subnet`, `mac` | Adressen (CIDR und Platzhalter `*` erlaubt) |
| `port` (`:`, `>`, `<` …; `port:161/udp`), `service`, `product`, `version` | offene Ports und Dienste |
| `state` (known/unknown/ignored), `crit` (low…critical, auch `crit>=high`) | Zustand, Kritikalität |
| `is` (online, offline, new, known, unknown, ignored, randomized, critical, exploited), `online:yes` | Status; `is:exploited` = mindestens eine laut CISA aktiv ausgenutzte CVE |
| `has` (notes, cve, cert, health, ports, http, containers, packages, parent, children, tags, mac, hostname) | vorhanden |
| `cve>=7`, `cve:CVE-2024-6387` | höchster CVSS-Wert oder konkrete CVE |
| `epss>=0.1` (auch `epss>=10%`) | höchster EPSS-Wert der CVEs eines Geräts |
| `seen<24h`, `first<7d` (m, h, d, w, y) | Letzt-/Erstsichtung |
| `cert<30d`, `cert:expired`, `cert:selfsigned`, `cert:weak` | Zertifikate |
| `app`, `title`, `container`, `package`, `health`, `source`, `parent`, `id` | weitere Merkmale |
| `cf.<feld>` | Custom Fields (`cf.rack:A1`, `cf.baujahr>=2020`) |
| `site` | NetScope-Standort in der Zentrale (`site:colo`, `site:local` = die Zentrale selbst) |

## Regeln und Benachrichtigungen

Events gehen nie direkt an Publisher, sondern durch Regeln (Bedingungen: Event-Typ, Tag,
Gruppe, Subnetz, Standort (Verbund), Schweregrad, Zeitfenster, Payload, „nur nicht bekannte Geräte“ → Aktion:
Publisher, Priorität, sofort oder gesammelt über N Minuten, Drosselung, Ruhezeiten,
Eskalation ohne Quittierung). Jede Regel lässt sich mit einem simulierten Event testen.
Vorgefertigt (anpassbar):

- Neues unbekanntes Gerät → Telegram sofort
- Neuer Port auf bekanntem Gerät → Telegram gesammelt (stündlich)
- CVE ≥ 9 → Telegram sofort
- Zertifikat < 14 Tage → höchstens einmal täglich

Telegram einrichten: Bot bei `@BotFather` anlegen, Token und Chat-ID im Plugin `telegram`
eintragen, aktivieren, „Testnachricht senden“. Details zu allen Publishern und zum
Webhook/n8n-Payload: [PUBLISHERS.md](PUBLISHERS.md).

## Schwachstellen

Das Plugin `cve` spiegelt die NVD-Datenbank lokal (offizielle JSON-2.0-Feeds, danach
inkrementell über den „modified“-Feed) und gleicht die CPEs der Geräte ab (nmap-Dienste,
OS-Erkennung, erkannte Web-Apps, SSH-Pakete). Es gibt keinen Online-Lookup pro Gerät.
Der Spiegel liegt in einer eigenen Datei (`data/netscope-nvd.db`, einige hundert MB) und
ist nicht Teil der Backups.
Der Versionsabgleich ist **heuristisch** – insbesondere Distributionspakete enthalten oft
zurückportierte Sicherheitskorrekturen. Einzelne CVEs lassen sich pro Gerät als irrelevant
markieren; die Markierung übersteht jeden Abgleich.

**Was zuerst?** Mit dem NVD-Spiegel lädt das Plugin täglich den Katalog der US-Behörde CISA
mit nachweislich angegriffenen Schwachstellen (Known Exploited Vulnerabilities, KEV) und die
EPSS-Werte von FIRST (geschätzte Wahrscheinlichkeit einer Ausnutzung in den nächsten
30 Tagen). Listen, Gerätereiter und Dashboard sortieren danach: aktiv ausgenutzte CVEs zuerst
(markiert mit „Ausgenutzt“, ggf. „Ransomware“), dann nach EPSS, dann nach CVSS. Filter „Nur
aktiv ausgenutzte“ und „EPSS ab“, in der Gerätesuche `is:exploited` und `epss>=0.1`.
Ausgenutzte CVEs erzeugen `cve.new` unabhängig von der CVSS-Schwelle und als kritisch; nimmt
CISA eine CVE neu auf, die bereits ein Gerät betrifft, meldet NetScope `cve.exploited`
(beides abschaltbar im Plugin).

OS-Vermutungen aus der nmap-Fingerabdruck-Erkennung werden nur abgeglichen, wenn sie
eindeutig sind und ein konkretes Produkt nennen (z. B. eine Geräte-Firmware). Kandidatenlisten
(„Android 9 - 10“), allgemeine Betriebssysteme ohne Patchstand (iOS, Android, Windows, macOS,
BSD) und der Linux-Kernel aus einem Fingerabdruck würden jede CVE der Hauptversion melden und
bleiben deshalb außen vor (Einstellung „Auch unsichere OS-Vermutungen abgleichen“ im Plugin).
Die ersten Treffer einer neuen Datenquelle gelten als Erstinventar und lösen keine Events aus.

## Benutzer, Rollen und Zwei-Faktor-Anmeldung

Unter **System → Benutzer** legt ein Administrator weitere Konten an. Das Start-Passwort
(vorgegeben oder erzeugt, einmal angezeigt) muss beim ersten Login geändert werden. Konten
lassen sich deaktivieren – Sitzungen und API-Tokens enden dann sofort –, bekommen ein neues
Start-Passwort oder ihren zweiten Faktor zurückgesetzt (verlorenes Handy).

**Rollen** (System → Rollen) sind frei definierbare Rechte. Lesen dürfen alle Benutzer:
Inventar, Topologie, Events, Health, Schwachstellen, Regeln, Plugins und Berichte. Die Rolle
legt fest, was jemand ändern darf:

| Bereich | Rechte |
|---|---|
| Inventar | Geräte bearbeiten · Geräte löschen und zusammenführen · Scans starten · Geräteaktionen (z. B. Wake-on-LAN) · Gruppen, Felder und Ansichten |
| Überwachung | Events quittieren · Health-Checks verwalten · Schwachstellen bewerten · Regeln verwalten · Berichte versenden |
| Konfiguration | Plugins konfigurieren\* · Credentials einsehen · Credentials verwalten\* · Subnetze und Tunnel\* · Verbund und Standorte\* · Agents verwalten |
| System | Systemeinstellungen\* · Backups\* · Audit-Log und Server-Protokoll · eigene API-Tokens · Benutzer und Rollen\* |

Die mit \* markierten Rechte sind kritisch: Plugins nutzen die Credentials aus dem Vault, ein
Backup enthält alle Daten, und wer Benutzer verwalten darf, kann sich jedes Recht geben.
Vorgegeben sind *Administrator* (immer alle Rechte, auch künftige), *Bearbeiter* und
*Betrachter*; die beiden letzten lassen sich anpassen. Mindestens ein aktiver Administrator
bleibt immer bestehen. Änderungen an einer Rolle gelten sofort, auch für laufende Sitzungen.

**Zwei-Faktor-Anmeldung** richtet jeder unter **System → Konto** ein:

- *Authenticator-App (TOTP)*: QR-Code scannen, ersten Code bestätigen. Jeder Code gilt nur
  einmal.
- *Passkey*: Fingerabdruck, Gesicht, Geräte-PIN oder Sicherheitsschlüssel. Browser erlauben
  Passkeys nur über HTTPS mit einem Hostnamen (z. B. [hinter Traefik](#hinter-einem-reverse-proxy-traefik)) oder
  auf localhost, nicht über `http://<IP>:8080`. Ein Passkey gilt nur für den Hostnamen, unter
  dem er eingerichtet wurde.
- *Wiederherstellungscodes*: zehn Einmal-Codes, die mit dem ersten Faktor einmal angezeigt
  werden.

Eine Rolle kann die Zwei-Faktor-Anmeldung verlangen: Betroffene richten sie beim nächsten
Login ein, bevor sie NetScope nutzen können. Wer sich ausgesperrt hat, dem setzt ein
Administrator den zweiten Faktor zurück – oder im Container:
`docker exec netscope netscope 2fa-reset --user NAME` (Passwort:
`docker exec -it netscope netscope passwd --user NAME`).

### Zentrale Anmeldung: OIDC und LDAP

Unter **System → Anmeldung** lassen sich zwei Verfahren einrichten, einzeln oder zusammen:

- **OIDC (Single Sign-on):** Die Login-Seite zeigt „Anmelden mit …“ und leitet zum Identity
  Provider (Authentik, Keycloak, Authelia, Entra ID, Google …). Dort eine Anwendung mit der
  angezeigten Redirect-URI (`…/api/v1/auth/oidc/callback`) anlegen, Issuer-URL, Client-ID und
  Client-Secret übernehmen. Gruppen kommen aus einem Claim (Standard `groups`, auch als Pfad
  wie `realm_access.roles`), fehlt er im ID-Token, aus Userinfo. Über den zweiten Faktor
  entscheidet der Identity Provider; die 2FA-Pflicht einer Rolle gilt für diese Konten nicht.
- **LDAP / Active Directory:** Anmeldung über das normale Formular mit dem Konto aus dem
  Verzeichnis. Vorlagen für Active Directory und OpenLDAP füllen Filter und Attribute; Gruppen
  kommen aus `memberOf` oder einer Gruppensuche. Die 2FA-Pflicht der Rolle gilt wie bei lokalen
  Konten. „Prüfen“ testet Verbindung, Dienstkonto und – mit Testbenutzer – Suche, Anmeldung,
  Gruppen und die daraus folgende Rolle.

Beim ersten Anmelden entsteht das Konto automatisch. Die Rolle folgt aus einer geordneten
Liste Gruppe → Rolle (erster Treffer gewinnt), sonst aus einer Standardrolle, sonst gibt es
keinen Zugriff; auf Wunsch wird sie bei jeder Anmeldung neu übernommen. Solche Konten haben
kein NetScope-Passwort, lassen sich aber in NetScope deaktivieren. Ein lokales Konto mit
gleichem Namen hat immer Vorrang und wird nie von einer externen Anmeldung übernommen; das gilt
auch zwischen OIDC und LDAP. Mindestens ein aktiver lokaler Administrator bleibt immer bestehen
– er ist der Notzugang, wenn Verzeichnis oder Identity Provider ausfallen.

## Sprache: Deutsch und Englisch

Die Oberfläche gibt es auf Deutsch und Englisch. Jeder Benutzer wählt seine Sprache unter
**System → Konto → Sprache** (Deutsch, English oder „Wie im Browser“, Administratoren auch in
der Benutzerverwaltung); ohne Auswahl gilt die Sprache des Browsers, und ist die weder Deutsch
noch Englisch, Deutsch. Die Anmeldeseite folgt immer dem Browser. Auch alles, was der Server
liefert, kommt in dieser Sprache: Plugin-Einstellungen, Event-Katalog und Event-Titel (auch
bereits gespeicherte), Fehlermeldungen, Laufprotokolle, Audit-Log und die API-Dokumentation.
Datum und Zahlen folgen der Sprache (Englisch in der Variante des Browsers, sonst britisch:
27/09/2026 14:05).

Benachrichtigungen (E-Mail, Telegram, ntfy, Webhook, n8n) und geplante Berichte gehen in einer
Sprache für die ganze Instanz: **System → Einstellungen → Sprache für Benachrichtigungen und
Berichte** (Standard Deutsch). Berichte, die man in der Oberfläche herunterlädt, kommen in der
Sprache des Benutzers. API-Clients erhalten Texte nach der Einstellung des Token-Benutzers bzw.
`Accept-Language`; die CLI im Container antwortet mit `LANG=en_US.UTF-8` auf Englisch.

Nicht übersetzt werden Daten: Gerätenamen, Namen von Rollen (auch der vorgegebenen
„Bearbeiter“ und „Betrachter“), Werte in Event-Payloads (z. B. `direction: eingehend`, auf
die Regeln filtern) und Texte, die NetScope nicht selbst schreibt. Der Agent und seine
Installationsskripte bleiben deutsch.

**Übersetzungen ergänzen:** Texte stehen im Code auf Deutsch; die englischen stehen daneben –
in der Oberfläche in `web/src/lib/i18n/en/*.json` ([FRONTEND.md](FRONTEND.md),
Abschnitt 9), im Server in `i18n_en.go` je Paket ([PLUGINS.md](PLUGINS.md)).
`npm run check` und `go test ./internal/i18n/` schlagen fehl, sobald eine Übersetzung fehlt.

## API

- Dokumentation: `/api/docs`, Spezifikation: `/api/openapi.json`; bei jedem Endpunkt steht
  das nötige Recht.
- API-Tokens gehören einem Benutzer und haben höchstens die Rechte seiner Rolle (Scope `read`
  nur deren lesenden Teil); einen zweiten Faktor brauchen sie nicht. Anlegen unter
  **System → API-Tokens** oder per CLI (für den ersten Administrator):
  `docker exec netscope netscope token create --name skript --scope read`
- Beispiel: `curl -H "Authorization: Bearer ns_…" http://netscope:8080/api/v1/devices?q=port:22`
- Live-Updates: Server-Sent Events unter `/api/v1/stream?topics=event,device,run` (Topics und
  Nachrichten: [ARCHITECTURE.md](ARCHITECTURE.md#api))
- Metriken: `/metrics` im Prometheus-Format (Token erforderlich, außer in den
  Systemeinstellungen freigegeben):
  ```yaml
  scrape_configs:
    - job_name: netscope
      authorization: { credentials: ns_… }
      static_configs: [{ targets: ["192.168.10.123:8080"] }]
  ```

## Backup und Wiederherstellung

**System → Backups** erstellt konsistente, gzip-komprimierte Kopien der Datenbank
(`data/backups/*.db.gz`), die sich herunterladen und wiederherstellen lassen (auch per
Upload; unkomprimierte `.db`-Backups älterer Versionen gehen weiterhin). Den NVD-Spiegel
enthalten Backups nicht: Er liegt in `data/netscope-nvd.db`, bleibt bei einer
Wiederherstellung erhalten und wird auf einer neuen Installation einfach neu geladen. Beim
ersten Start nach dem Update verschiebt NetScope einen vorhandenen Spiegel einmalig aus der
Datenbank in diese Datei; dieser Start dauert entsprechend länger. Bei der Wiederherstellung
starten die Dienste im Prozess neu; die vorherige Datenbank bleibt als
`netscope.db.pre-restore-<zeit>` erhalten. Für ein vollständiges Backup zusätzlich
`data/master.key` sichern: Das Backup einer anderen Instanz lässt sich nur mit deren
Master-Key öffnen – passt der Schlüssel nicht, rollt NetScope automatisch auf die bisherige
Datenbank zurück und protokolliert den Grund.

CLI im Container: `netscope passwd [--user NAME]` (Passwort zurücksetzen),
`netscope 2fa-reset [--user NAME]` (zweiten Faktor entfernen), `netscope token create`,
`netscope healthcheck`, `netscope openapi`, `netscope version`.

## Sicherheit

- Login mit bcrypt-Passwort und Session-Cookie (HttpOnly, SameSite=Lax, Secure hinter
  HTTPS), Schutz gegen CSRF und Rate-Limit bei Fehlversuchen (je Adresse; für den zweiten
  Faktor zusätzlich je Benutzer, das richtige Passwort setzt es nicht zurück).
- Eine neue Installation nimmt nur in Besitz, wer den Einrichtungscode vom Host kennt (Log,
  `data/setup-code.txt`); die Einrichtungs-Endpunkte antworten nur mit Code oder der Sitzung des
  im Assistenten angelegten Administrators und nur bis zum Abschluss. Bis dahin läuft kein
  Plugin.
- Rechte prüft der Server bei jedem Aufruf; ein Test stellt sicher, dass kein ändernder
  Endpunkt ohne Recht bleibt. Deaktivierte Konten verlieren sofort Sitzungen und Tokens.
- OIDC: Authorization Code Flow mit PKCE, State (an den Browser gebunden) und Nonce; das
  ID-Token wird mit den Schlüsseln des Providers geprüft (nur asymmetrische Verfahren),
  dazu Issuer, Audience, Ablauf und `azp`. LDAP: Benutzernamen werden im Filter maskiert,
  leere Passwörter abgelehnt (sonst „anonymer Bind“), Verbindungen per LDAPS oder StartTLS mit
  Zertifikatsprüfung (eigene CA möglich). Client-Secret und Bind-Passwort liegen verschlüsselt
  im Vault und werden bei der Key-Rotation mit umgeschlüsselt.
- TOTP-Geheimnisse liegen verschlüsselt im Vault, Wiederherstellungscodes nur gehasht;
  Passkeys speichern nur öffentliche Schlüssel.
- API-Tokens werden nur gehasht gespeichert und genau einmal angezeigt.
- Secrets (Credentials, geheime Plugin-Einstellungen, TOTP) liegen AES-256-GCM-verschlüsselt in
  der Datenbank; Key-Rotation unter **System**.
- Der Container braucht `NET_RAW`/`NET_ADMIN` für ARP, ICMP, OS-Erkennung und die eigenen
  WireGuard-Tunnel; das Einbinden des Docker-Sockets ist optional und gibt Root-Rechte auf
  dem Host.
- WireGuard-Tunnel führen keine Befehle aus der Konfiguration aus und leiten nur die
  zugeordneten Subnetze um; ihre Interfaces (`nswg<ID>`) räumt NetScope beim Beenden auf.
- NetScope-Agents laufen als eigener Benutzer ohne root und führen nur feste Lesebefehle
  aus. Installations-Tokens (`nse_…`) und die Secrets der Agents (`nsag_…`) speichert NetScope
  nur als Hash; beide gelten nur für die Agent-Schnittstelle. Automatische Updates bedeuten:
  Wer die NetScope-Instanz kontrolliert, kann den Agents neuen Code geben – mit den Rechten
  des Benutzers `netscope-agent` (mit `--docker` faktisch root). Ohne HTTPS laufen Befehl
  und Agent-Download unverschlüsselt durchs Netz.
- Standort-Tokens (`nss_…`) gelten nur für das Einliefern bei der Zentrale, nie für die
  übrige API; die Zentrale speichert nur ihren Hash, der Standort das Token verschlüsselt.
  Zugangsdaten eines Standorts verlassen ihn nie, und die Zentrale kann am Standort nichts
  auslösen. Mit selbst signiertem Zertifikat der Zentrale dessen Fingerprint hinterlegen.
