# Future Requests

Wünsche für spätere Versionen, die noch nicht umgesetzt sind. Jeder Eintrag beschreibt
Anlass, Problem und einen Lösungsvorschlag, damit die Umsetzung ohne Vorgeschichte starten
kann.

---

## FR-002: Mehrere NetScope-Instanzen bündeln (Zentrale und Standorte)

**Status:** offen · erfasst am 24.09.2026

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

### Abnahme

- Tests: gleiche IP an zwei Standorten ergibt zwei Geräte; gleiche MAC ergibt ein Gerät;
  Anwesenheit in der Zentrale entspricht der am Standort; Pufferung und Nachlieferung nach
  Verbindungsabbruch; keine doppelten Events.
- Ende-zu-Ende: zwei Instanzen (Zuhause als Zentrale, Colo als Standort) mit einem
  gemeinsamen Inventar in der Zentrale.
- Doku: README (Abschnitt „Mehrere Standorte“), ARCHITECTURE (Rollen, Protokoll).
