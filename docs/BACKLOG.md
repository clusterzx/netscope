# Backlog

Zurückgestellte Wünsche. Sie sind vollständig beschrieben, stehen aber hinter den
[Future Requests](FUTURE_REQUESTS.md) zurück und werden bei Bedarf wieder hochgeholt.

---

## FR-001: Empfindliche Geräte schonend scannen

**Status:** zurückgestellt am 24.09.2026 · erfasst am 23.09.2026

### Anlass

Am 23.09.2026 wurden alle Scanner von Hand gleichzeitig gestartet (21:43 Uhr). Wenige
Minuten später war der Proxmox-Host stromlos. Er hängt an der smarten Steckdose
**„Proxmox-Büro“ (192.168.8.33)**, einem Shelly der zweiten Generation (ESP32, lwIP-Stack,
Server-Header `ShellyHTTP/1.0.0`).

Zeitleiste aus den Laufdaten von NetScope:

| Zeit | Ereignis |
|---|---|
| 21:43:16–23 | alle Scanner gestartet |
| 21:43:57 | HTTP-Fingerprinting fertig |
| 21:45:24 | nmap UDP fertig |
| 21:45:42 | Proxmox-Import fertig – der Server lief noch |
| danach | nur noch nmap TCP aktiv (`-sV`, `-O`, 35 Hosts) |
| 21:50 | regulärer ICMP-Lauf fehlt – Server aus |
| 21:53:55 | LXC startet neu, nmap-Lauf wird als „abgebrochen“ markiert |

nmap lief an diesem Tag sieben Mal einzeln ohne Probleme. Wahrscheinlichste Erklärung: Die
parallelen Scanner (HTTP, UDP, mDNS …) haben den Speicher der Steckdose ausgelastet, und die
Dienst- und OS-Erkennung von nmap hat sie zum Absturz gebracht. Beim Neustart des Chips fällt
das Relais ab, und der angeschlossene Server verliert den Strom. Nicht endgültig bewiesen –
bestätigen lässt es sich über die Uptime in der Shelly-Oberfläche (Neustart gegen
21:46–21:50).

### Problem

Mikrocontroller-Geräte mit Relais (Shelly, Tasmota, andere ESP8266/ESP32-Geräte) können
unter aggressiven Scans abstürzen und dabei schalten. Das kann angeschlossene Geräte
ausschalten – im schlimmsten Fall den Server, auf dem NetScope selbst läuft.

NetScope bietet dafür heute nur eine Notlösung: die Ausschlussliste von nmap (IP-Adressen
unter *Einstellungen → Erweitert → Ausschlüsse*). nmap UDP, HTTP und TLS haben keine
Ausschlüsse, und Plugin-Scopes können Geräte nur auswählen, nicht ausschließen.

Einschätzung der Scanner:

| Belastung | Scanner |
|---|---|
| hoch | nmap TCP (Diensterkennung `-sV`, OS-Erkennung `-O`), nmap UDP |
| mittel | HTTP-Fingerprinting (viele Anfragen), TLS |
| gering | ICMP, ARP, mDNS, NetBIOS, UPnP, DNS, OUI |

Betroffene Geräte im Heimnetz (Hersteller laut OUI: Espressif):

- 192.168.8.33 „Proxmox-Büro“ (Shelly Gen2, versorgt den Proxmox-Host)
- 192.168.8.40 Shelly 3EM
- 192.168.8.79 Tasmota „growswitchbig“
- 192.168.8.212 smardencore

### Vorschlag

1. **Schalter pro Gerät „Schonend scannen“.** Er wird auf der Geräteseite und als
   Massenaktion in der Geräteliste gesetzt und im Gerät gespeichert, als eigenes Feld statt
   Tag.
2. **Zentrale Umsetzung im Plugin-Host statt in jedem Plugin:**
   - Plugins bekommen ein Merkmal `Intrusive` (bzw. eine Belastungsstufe) in `plugin.Info`.
   - Für solche Plugins entfernt der Host schonend markierte Geräte aus den Zielen.
   - Subnetz-Scanner bekommen deren Adressen als Ausschlüsse übergeben (nmap `--exclude`).
   - Im Laufprotokoll steht: „N Geräte übersprungen (schonend)“.
   - Betroffen: nmap, nmap UDP, HTTP, TLS. ICMP, ARP und die passiven Scanner bleiben
     unverändert.
3. **Automatischer Vorschlag.** Geräte mit OUI-Hersteller Espressif oder erkannten
   IoT-Relais (HTTP-Server `ShellyHTTP`, `Tasmota`, Mongoose …) zeigen auf der Geräteseite
   den Hinweis „Empfohlen: schonend scannen“. Eine Systemeinstellung
   „ESP-Geräte automatisch schonen“ setzt den Schalter selbst.
4. **Optional: leichter Modus statt Überspringen.** Nur wenige Ports, TCP-Connect-Scan ohne
   `-sV`/`-O`, langsames Timing (T2), eine Verbindung gleichzeitig.
5. **Optional: nie zwei belastende Plugins gleichzeitig auf demselben Gerät.** Laufen
   mehrere belastende Plugins parallel, arbeitet der Host die Geräte pro Host nacheinander
   ab – auch wenn jemand „alle Scanner“ von Hand startet.

**Abnahme:**

- Tests: Der Host filtert schonende Geräte nur für belastende Plugins; nmap erhält die
  Ausschlüsse; der Gerätemodus bleibt korrekt.
- Oberfläche: Schalter und Hinweis auf der Geräteseite, Massenaktion in der Geräteliste.
- Doku: README (Scanner-Tabelle), PLUGINS.md (`Intrusive`).

### Übergangslösung bis dahin

- nmap: *Einstellungen → Erweitert → Ausschlüsse* – die Adressen der Geräte oben eintragen.
- Shelly „Proxmox-Büro“: „Einschaltverhalten: letzten Zustand wiederherstellen“.
- Server-BIOS: „Nach Stromausfall: einschalten“.
- Am sichersten: den Server nicht über ein schaltbares Relais versorgen oder eine USV
  vorschalten.
