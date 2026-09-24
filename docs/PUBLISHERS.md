# Publisher

Publisher verschicken Benachrichtigungen. Sie werden nie direkt von Events ausgelöst,
sondern von der **Regel-Engine**: Eine Regel-Aktion bündelt Events zu genau einer
Benachrichtigung (`plugin.Notification`) – bei „sofort“ pro Scan-Lauf, bei „sammeln“ über
N Minuten – und übergibt sie dem Publisher.

| Publisher | ID | Kanal | Format |
|---|---|---|---|
| Telegram | `telegram` | Bot-API `sendMessage` | MarkdownV2, gebündelt, Deep-Links |
| Webhook | `webhook` | HTTP `POST`/`PUT` | JSON – NetScope-Payload v1, optional HMAC-signiert |
| ntfy | `ntfy` | ntfy.sh oder eigener Server | JSON-Publishing, Markdown, Emoji-Tags |
| E-Mail | `email` | SMTP (STARTTLS/TLS) | `multipart/alternative` (Text + HTML), Berichte zusätzlich mit PDF-Anhang |
| n8n | `n8n` | n8n-Webhook-Node | JSON – NetScope-Payload v1 |

Alle Publisher sind nach der Installation **deaktiviert** und haben keinen Zeitplan. Aktivieren
und konfigurieren unter *Plugins → Publisher*; der Button „Testnachricht senden“ verschickt eine
Benachrichtigung vom Typ `test`.

## Gemeinsames Verhalten

**Arten von Benachrichtigungen** (`kind`):

| `kind` | Bedeutung | Darstellung |
|---|---|---|
| `event` | gebündelte Events einer Regel | Titel, danach ein Block pro Event |
| `escalation` | erneuter Versand, weil nicht rechtzeitig quittiert | deutlicher Hinweis „⏰ Eskalation – nicht quittiert“ |
| `test` | Testnachricht aus der UI | Text aus `body` |
| `report` | Wochen-/Änderungsbericht | Markdown-Text aus `body`, meist ohne Events |

**Priorität:** `low`, `normal`, `high`, `urgent` (aus der Regel-Aktion). **Schweregrad** je
Event: `info`, `low`, `medium`, `high`, `critical`.

**Deep-Links:** Jedes Event hat einen Link auf das Event und auf das Gerät, die
Benachrichtigung einen Link in die UI. Links gibt es nur, wenn unter *System* eine öffentliche
URL eingetragen ist – sonst fehlen sie in allen Nachrichten.

**Fehler und Wiederholung:** Schlägt eine Zustellung fehl, gibt der Publisher einen Fehler
zurück und der Dispatcher wiederholt sie (Standard: 3 Wiederholungen). Fehlermeldungen
landen im Laufprotokoll und enthalten nie Geheimnisse: kein Bot-Token, kein Passwort, keine
Header-Werte, bei Webhooks nur Schema und Host der Ziel-URL (Pfad und Query können Tokens
enthalten). Weiterleitungen (HTTP 3xx) werden nicht verfolgt, sondern als Fehler gemeldet,
weil ein `POST` sonst still zu einem `GET` ohne Inhalt würde.

**Secrets:** Felder vom Typ *secret* (Bot-Token, HMAC-Secret, ntfy-Token, n8n-Auth-Wert)
werden verschlüsselt gespeichert und von der API nur als `********` ausgeliefert.
Zugangsdaten für SMTP und ntfy liegen als *Benutzer/Passwort*-Credential im Vault.

---

## Telegram

### Einrichtung

1. In Telegram **@BotFather** öffnen, `/newbot` senden, Namen vergeben. BotFather antwortet
   mit dem **Bot-Token** (`123456789:AAH…`).
2. **Chat-ID** ermitteln:
   - *Privatchat:* dem Bot einmal `/start` schreiben, dann
     `https://api.telegram.org/bot<TOKEN>/getUpdates` im Browser öffnen und `message.chat.id`
     ablesen (positive Zahl).
   - *Gruppe:* Bot zur Gruppe hinzufügen, eine Nachricht in die Gruppe schreiben, wieder
     `getUpdates` aufrufen. Gruppen-IDs sind negativ, Supergruppen beginnen mit `-100`.
   - *Kanal:* Bot als Administrator mit Schreibrecht hinzufügen; Chat-ID ist `@kanalname`
     oder die numerische `-100…`-ID.
3. *Themen (Forum-Gruppen):* Die ID eines Themas steht im Link einer Nachricht darin
   (`https://t.me/c/<gruppe>/<themen-id>/<nachricht>`) und gehört in „Themen-ID“.

### Einstellungen

| Feld | Typ | Standard | Bedeutung |
|---|---|---|---|
| `bot_token` | secret, Pflicht | – | Token von @BotFather; Format wird geprüft (ohne es in Fehlern zu zeigen) |
| `chat_id` | Text, Pflicht | – | numerische ID (`42`, `-1001234567890`) oder `@kanalname` |
| `message_thread_id` | Zahl | `0` | Thema in Forum-Gruppen, `0` = allgemein |
| `silent_below` | Auswahl | `normal` | Benachrichtigungen mit niedrigerer Priorität kommen **ohne Ton** (`disable_notification`). `low` = nie lautlos, `urgent` = nur „Dringend“ mit Ton |
| `disable_preview` | Ja/Nein | Ja | Link-Vorschau unterdrücken |
| `api_url` | URL | `https://api.telegram.org` | nur für einen selbst betriebenen Bot-API-Server |
| `timeout` | Dauer | `15s` | pro Anfrage an die Bot-API |

### Nachrichtenformat

Gesendet wird mit `parse_mode: "MarkdownV2"`. In allen dynamischen Texten (Titel,
Gerätenamen, Meldungen, Linktexte) werden sämtliche Sonderzeichen escaped:

```text
_ * [ ] ( ) ~ ` > # + - = | { } . ! \
```

In Link-URLs werden nur `)` und `\` escaped. Gerätenamen wie `nas_01` oder Meldungen mit
`(v1.2)` können die Formatierung also nicht zerstören.

Beispiel – zwei gesammelte Events, so wie Telegram sie anzeigt (Titel fett, Metazeilen
kursiv, „nas_01“, „Öffnen“ und „In NetScope öffnen“ sind Links):

```text
🔴 2 Ereignisse: Port & CVE auf nas-01
Regel: Ports (bekannt) + CVE>=9 · 2 Ereignisse

🟡 Neuer Port 8080/tcp (http-alt) auf nas_01!
Port neu · Mittel · 22.09. 12:03
🖥 nas_01 · 192.168.8.10
Server: nginx/1.25
Öffnen

🔴 CVE-2024-6387 auf nas_01
Neue Schwachstelle · Kritisch · 22.09. 12:04
🖥 nas_01 · 192.168.8.10
Öffnen · ⏰ eskaliert · ✅ quittiert

In NetScope öffnen
```

Der gesendete Text dazu (Auszug):

```text
🔴 *2 Ereignisse: Port & CVE auf nas\-01*
_Regel: Ports \(bekannt\) \+ CVE\>\=9 · 2 Ereignisse_

🟡 *Neuer Port 8080/tcp \(http\-alt\) auf nas\_01\!*
_Port neu · Mittel · 22\.09\. 12:03_
🖥 [nas\_01](http://192.168.8.123:8080/devices/17) · 192\.168\.8\.10
Server: nginx/1\.25
[Öffnen](http://192.168.8.123:8080/events/1)
```

Aufbau:

- **Kopfzeile:** Emoji + fetter Titel. Emoji: 🚨 bei Priorität „Dringend“, sonst der höchste
  Schweregrad (🔴 kritisch, 🟠 hoch, 🟡 mittel, 🔵 niedrig, ⚪ info); 🧪 Test, 📊 Bericht.
  Darunter kursiv Regelname und Anzahl der Events.
- **Eskalation:** zusätzliche erste Zeile **⏰ Eskalation – nicht quittiert**.
- **Pro Event:** Schweregrad-Emoji + Titel, Typ · Schweregrad · Uhrzeit, Gerät (verlinkt) + IP,
  Meldung, Link „Öffnen“, Markierungen „⏰ eskaliert“ / „✅ quittiert“.
- **Höchstens 20 Events** pro Benachrichtigung, danach „… und N weitere Ereignisse – Alle
  anzeigen“ (Link auf die Benachrichtigung in der UI).
- **Berichte und Tests:** Der Markdown-Text aus `body` wird umgesetzt: Überschriften und
  `**fett**` erscheinen fett, Listenpunkte mit „•“; alles andere wird escaped (unpaarige
  Markdown-Zeichen erscheinen wörtlich).
- **Lange Nachrichten** (> 4096 Zeichen, gezählt in UTF-16 wie bei Telegram) werden an
  Event-Grenzen bzw. Zeilen geteilt. Folgenachrichten beginnen mit „*Titel* (Fortsetzung)“, alle
  Teile enden mit „Teil i/n“. Teile werden im Abstand von 1 s gesendet.

### Fehlerbilder

| Meldung | Ursache |
|---|---|
| `Ratenlimit der Bot-API erreicht (HTTP 429) – erneuter Versuch frühestens in 30s möglich` | Telegram drosselt (`retry_after`). Der Dispatcher wiederholt später. Tritt das Limit erst bei Teil 2+ einer geteilten Nachricht auf, wartet der Publisher Verzögerungen bis 30 s selbst ab, damit bereits gesendete Teile nicht doppelt kommen. |
| `Chat nicht gefunden (400: Bad Request: chat not found)` | falsche Chat-ID, oder der Bot wurde nie mit `/start` angeschrieben bzw. ist nicht Mitglied |
| `Bot-Token ungültig oder widerrufen` | Token bei @BotFather neu erzeugen und eintragen |
| `die Gruppe wurde in eine Supergruppe umgewandelt – neue Chat-ID -100… eintragen` | Telegram hat die Gruppe migriert; neue ID übernehmen |
| `Bot darf in diesen Chat nicht schreiben` | Bot aus Gruppe entfernt oder im Kanal ohne Schreibrecht |

---

## Webhook

Sendet jede Benachrichtigung als JSON ([NetScope-Payload v1](#netscope-payload-v1)) an eine
beliebige URL.

### Einstellungen

| Feld | Typ | Standard | Bedeutung |
|---|---|---|---|
| `url` | URL, Pflicht | – | Ziel (`http://` oder `https://`) |
| `method` | Auswahl | `POST` | `POST` oder `PUT` |
| `headers` | Liste | – | zusätzliche Header, eine Zeile pro Header: `Name: Wert`. Nicht erlaubt: `Host`, `Content-Type`, `Content-Length`, `Transfer-Encoding`, `Connection`, `User-Agent`, `X-NetScope-*`. Werte werden **unverschlüsselt** gespeichert. |
| `hmac_secret` | secret | – | aktiviert die Signatur (siehe unten); empfohlen ≥ 32 zufällige Zeichen |
| `timeout` | Dauer | `10s` | maximale Dauer einer Zustellung |
| `verify_tls` | Ja/Nein | Ja | TLS-Zertifikat prüfen; nur für selbstsignierte Zertifikate im LAN abschalten |

### Anfrage

```http
POST /hooks/netscope HTTP/1.1
Host: example.org
Content-Type: application/json
User-Agent: NetScope/1.0
X-NetScope-Event: event
X-NetScope-Notification-Id: 42
X-NetScope-Timestamp: 1758542620
X-NetScope-Signature: sha256=5f0c…
Authorization: Bearer …            (aus „Zusätzliche Header“)

{"version":1,"source":"netscope",…}
```

| Header | Inhalt |
|---|---|
| `Content-Type` | immer `application/json` (UTF-8) |
| `User-Agent` | `NetScope/1.0` |
| `X-NetScope-Event` | `kind` der Benachrichtigung: `event`, `escalation`, `test`, `report` |
| `X-NetScope-Notification-Id` | ID der Benachrichtigung – bleibt bei Wiederholungen gleich und eignet sich zur **Deduplizierung** |
| `X-NetScope-Timestamp` | Unix-Sekunden des Versands (nur mit HMAC-Secret) |
| `X-NetScope-Signature` | `sha256=` + HEX(HMAC-SHA256(Secret, Timestamp + `.` + Body)) (nur mit HMAC-Secret) |

Jede Antwort mit Status **2xx** gilt als Erfolg. Alle anderen Status sind Fehler; die Meldung
enthält Status und die ersten 200 Zeichen der Antwort, z. B.
`Webhook: Empfänger antwortete mit HTTP 500 Internal Server Error: {"error":"…"}`.

### HMAC-Signatur prüfen

Signiert wird `Timestamp + "." + Body` – der **unveränderte** Request-Body als Bytes, nicht ein
neu serialisiertes JSON. Empfänger sollten

1. den Zeitstempel prüfen (z. B. höchstens 5 Minuten alt – schützt vor Replays),
2. die Signatur selbst berechnen und **zeitkonstant** vergleichen,
3. erst dann das JSON parsen.

Testvektor: Secret `geheim`, Timestamp `1758542620`, Body `{"version":1}` →
`sha256=03c1a28b2650ebb177d50893f4f563f6e139d4b60a0dcced9699d7d4ca4fa268`.

Kommandozeile:

```sh
printf '%s' '1758542620.{"version":1}' | openssl dgst -sha256 -hmac geheim
```

**Python** (Standardbibliothek; `raw_body` = Request-Body als `bytes`, z. B. Flask
`request.get_data()`, FastAPI `await request.body()`):

```python
import hashlib
import hmac
import time


def verify_netscope(secret: str, headers, raw_body: bytes, max_age: int = 300) -> bool:
    ts = headers.get("X-NetScope-Timestamp", "")
    sig = headers.get("X-NetScope-Signature", "")
    if not ts.isdigit() or abs(time.time() - int(ts)) > max_age:
        return False
    mac = hmac.new(secret.encode(), ts.encode() + b"." + raw_body, hashlib.sha256)
    return hmac.compare_digest("sha256=" + mac.hexdigest(), sig)


# Flask:
# @app.post("/hooks/netscope")
# def netscope():
#     if not verify_netscope(SECRET, request.headers, request.get_data()):
#         abort(401)
#     payload = request.get_json()
#     ...
#     return "", 204
```

**Node.js** (ab Node 18; `headers` mit kleingeschriebenen Namen wie in `req.headers`):

```js
const crypto = require("node:crypto");

function verifyNetScope(secret, headers, rawBody, maxAgeSeconds = 300) {
  const ts = headers["x-netscope-timestamp"] ?? "";
  const sig = headers["x-netscope-signature"] ?? "";
  if (!/^\d+$/.test(ts) || Math.abs(Date.now() / 1000 - Number(ts)) > maxAgeSeconds) {
    return false;
  }
  const expected =
    "sha256=" +
    crypto.createHmac("sha256", secret).update(`${ts}.`).update(rawBody).digest("hex");
  const a = Buffer.from(expected);
  const b = Buffer.from(sig);
  return a.length === b.length && crypto.timingSafeEqual(a, b);
}

// Express: Body roh einlesen, sonst stimmt die Signatur nicht.
// app.post("/hooks/netscope", express.raw({ type: "application/json" }), (req, res) => {
//   if (!verifyNetScope(process.env.NETSCOPE_SECRET, req.headers, req.body)) return res.sendStatus(401);
//   const payload = JSON.parse(req.body);
//   res.sendStatus(204);
// });
```

---

## ntfy

Push-Benachrichtigungen über [ntfy](https://ntfy.sh) – öffentlich auf ntfy.sh oder auf einem
eigenen Server.

### Einstellungen

| Feld | Typ | Standard | Bedeutung |
|---|---|---|---|
| `server` | URL | `https://ntfy.sh` | Basis-URL, auch mit Pfad (z. B. `https://example.org/ntfy`) |
| `topic` | Text, Pflicht | – | 1–64 Zeichen `A–Z a–z 0–9 - _`. Auf ntfy.sh ohne Zugriffsschutz ist jedes Topic öffentlich – schwer erratbaren Namen wählen |
| `token` | secret | – | Access-Token (`tk_…`) → `Authorization: Bearer`. **Hat Vorrang** vor dem Credential |
| `credential` | Credential *Benutzer/Passwort* | – | Basic-Auth, wenn kein Token gesetzt ist (Benutzername Pflicht) |
| `markdown` | Ja/Nein | Ja | Nachricht als Markdown (Fettschrift, Links). Die Web-App stellt Markdown dar; Clients ohne Markdown-Unterstützung zeigen die Zeichen |
| `tags_by_severity` | Ja/Nein | Ja | Emoji-Tag nach höchstem Schweregrad |
| `timeout` | Dauer | `10s` | maximale Dauer einer Zustellung |

### Versand

NetScope nutzt das **JSON-Publishing** von ntfy: ein `POST` auf die Server-Wurzel
(`https://ntfy.sh/`) mit einem JSON-Objekt. Anders als beim Header-Publishing sind Umlaute und
Sonderzeichen in Titel und Nachricht ohne weitere Kodierung möglich.

```json
{
  "topic": "netscope-a8f3k2",
  "title": "2 Ereignisse auf nas_01",
  "message": "**Neuer Port 22/tcp auf nas\\_01** (Mittel)  \nnas\\_01 · 192.168.8.10 · 22.09. 12:03  \n[Öffnen](http://192.168.8.123:8080/events/1)\n\n…",
  "priority": 4,
  "tags": ["rotating_light"],
  "click": "http://192.168.8.123:8080/events?notification=8",
  "markdown": true
}
```

| NetScope | ntfy |
|---|---|
| Priorität `low` / `normal` / `high` / `urgent` | `priority` 2 / 3 / 4 / 5 |
| höchster Schweregrad `critical` / `high` / `medium` / `low` / `info` | Tag `rotating_light` 🚨 / `warning` ⚠️ / `large_orange_diamond` 🔶 / `large_blue_diamond` 🔷 / `information_source` ℹ️ |
| `escalation` / `test` / `report` | zusätzlicher Tag `alarm_clock` ⏰ / `test_tube` 🧪 / `bar_chart` 📊; bei Eskalation beginnt der Titel mit „Eskalation – nicht quittiert:“ |
| Klick auf die Benachrichtigung | bei genau einem Event dessen Link, sonst der Link der Benachrichtigung, sonst der erste Event-Link |

Die Nachricht bleibt unter ntfys Grenze von 4096 Bytes (größere Nachrichten würden zu einem
Anhang): Es werden höchstens 20 Events aufgeführt, solange sie hineinpassen, danach folgt
„… und N weitere Ereignisse“ mit Link. Event-Texte werden für Markdown escaped.

Fehler werden mit der Meldung des Servers zurückgegeben, z. B.
`ntfy: Server antwortete mit HTTP 403 Forbidden: forbidden (Code 40301) – Token bzw. Zugangsdaten und Schreibrecht auf das Topic prüfen`.

---

## E-Mail (SMTP)

### Einstellungen

| Feld | Typ | Standard | Bedeutung |
|---|---|---|---|
| `host` | Hostname/IP, Pflicht | – | SMTP-Server |
| `port` | Zahl | `587` | üblich: 587 STARTTLS, 465 TLS, 25 interne Relays |
| `security` | Auswahl | `starttls` | `starttls`, `tls` (SMTPS) oder `none` |
| `credential` | Credential *Benutzer/Passwort* | – | SMTP-AUTH; ohne Benutzername wird die Absenderadresse verwendet |
| `from` | E-Mail, Pflicht | – | Absender, optional mit Namen: `NetScope <netscope@example.org>` |
| `to` | Liste von E-Mails, Pflicht | – | eine Adresse pro Zeile (max. 50) |
| `subject_prefix` | Text | `[NetScope]` | vor jedem Betreff |
| `helo` | Hostname | Hostname des Systems | Name für EHLO |
| `timeout` | Dauer | `20s` | gesamte SMTP-Sitzung |
| `verify_tls` | Ja/Nein | Ja | Zertifikat prüfen |

### Transportsicherheit

- **`starttls`:** Bietet der Server kein STARTTLS an, wird **abgebrochen** – es gibt keinen
  stillen Rückfall auf Klartext.
- **`tls`:** TLS ab dem ersten Byte (SMTPS, Port 465).
- **`none`:** unverschlüsselt, nur für vertrauenswürdige Relays im eigenen Netz.
- **Anmeldung** (AUTH PLAIN, sonst AUTH LOGIN) nur über TLS. Einzige Ausnahme: `localhost`,
  `127.0.0.1`, `::1`. `none` mit Credential zu einem anderen Host wird bereits beim Speichern
  abgelehnt.
- TLS mindestens 1.2; das Zertifikat wird gegen den eingetragenen Hostnamen geprüft.

### Nachricht

`multipart/alternative` mit

1. `text/plain; charset=utf-8` – die Textdarstellung der Benachrichtigung (bei Eskalationen mit
   Hinweiszeile), quoted-printable,
2. `text/html; charset=utf-8` – HTML im NetScope-Design, nur Tabellen und Inline-Styles
   (Outlook, Gmail, Apple Mail): dunkles Kopfband mit Wortmarke und Art der Nachricht, Titel,
   Tabelle der Events (Schweregrad-Chips, Titel mit Link, Meldung, Gerät mit Link und IP,
   Zeit), Fußzeile mit Button „In NetScope öffnen“. Eskalationen bekommen einen roten Balken
   „⏰ Eskalation – nicht quittiert“. Texte (z. B. Testnachrichten) werden aus Markdown
   dargestellt. Auf Smartphones greifen Media-Queries (Kacheln 2 × 2, Nebenspalten
   ausgeblendet); Clients ohne `<style>`-Unterstützung zeigen das Desktop-Layout.

**Berichte** (Wochenbericht, „Jetzt versenden“) kommen als `multipart/mixed`: die obige
Nachricht mit eigenem Berichtslayout – Kennzahl-Kacheln (Geräte, online, neu, unbekannt),
offene Events und aktive Schwachstellen als farbige Chips, Tabellen für wichtige Ereignisse,
neue Geräte, neue Schwachstellen, ablaufende Zertifikate, Ausfälle und fehlgeschlagene Läufe,
Balken für die Ereignisse nach Typ – plus der vollständige Bericht als **PDF-Anhang**. Lange
Listen sind in der Mail gekürzt („… und N weitere im PDF-Anhang“).

Header: `From`, `To`, `Subject` (RFC 2047, Umlaute kodiert), `Date`, `Message-ID`,
`MIME-Version`, `Auto-Submitted: auto-generated`, `X-Mailer: NetScope/1.0`,
`X-NetScope-Kind`, `X-NetScope-Notification-Id`, bei hoher/dringender bzw. niedriger Priorität
`X-Priority` und `Importance`. Zeilenenden sind CRLF.

Betreff-Beispiele:

```text
[NetScope] Neues Gerät: Küchen-Thermometer
[NetScope] Eskalation – nicht quittiert: CVE-2024-6387 auf nas
```

Fehler nennen den SMTP-Schritt und die Serverantwort, z. B.
`E-Mail: Anmeldung fehlgeschlagen: 535 5.7.8 Authentication credentials invalid`, bei typischen
Port-Verwechslungen mit Hinweis („Port 465 erwartet üblicherweise Verschlüsselung „TLS““).

---

## n8n

Ein vorkonfigurierter Webhook für [n8n](https://n8n.io): sendet immer `POST` mit der
[NetScope-Payload v1](#netscope-payload-v1) – derselbe Payload wie beim Webhook-Publisher,
ohne HMAC, optional mit einem Auth-Header für „Header Auth“.

### Einstellungen

| Feld | Typ | Standard | Bedeutung |
|---|---|---|---|
| `url` | URL, Pflicht | – | **Production-URL** des Webhook-Nodes (`…/webhook/…`) |
| `auth_header_name` | Text | – | z. B. `X-N8N-Key` |
| `auth_header_value` | secret | – | Wert des Headers; Name und Wert nur gemeinsam |
| `timeout` | Dauer | `10s` | maximale Dauer einer Zustellung |
| `verify_tls` | Ja/Nein | Ja | Zertifikat prüfen |

Zusätzlich werden `X-NetScope-Event` und `X-NetScope-Notification-Id` gesendet.

### n8n: Workflow einrichten

1. **Neuer Workflow → Node „Webhook“** hinzufügen:
   - *HTTP Method:* `POST`
   - *Path:* z. B. `netscope`
   - *Authentication:* `Header Auth` (empfohlen) → neue Credential mit *Name* `X-N8N-Key` und
     einem zufälligen *Value*. Dieselben Werte in NetScope bei „Auth-Header-Name/-Wert“
     eintragen.
   - *Respond:* **`Immediately`** – n8n antwortet sofort mit 200, NetScope wartet nicht auf den
     ganzen Workflow (sonst droht ein Timeout und eine doppelte Zustellung durch Wiederholung).
2. Die **Production URL** aus dem Node (`https://n8n.example.org/webhook/netscope`) in NetScope
   bei „Webhook-URL“ eintragen. Die *Test URL* (`…/webhook-test/…`) funktioniert nur, solange im
   Editor „Listen for test event“ läuft.
3. Workflow **aktivieren** (Schalter oben rechts). Ohne Aktivierung antwortet n8n mit 404
   („webhook … is not registered“); NetScope weist in der Fehlermeldung darauf hin.
4. In NetScope beim n8n-Publisher „Testnachricht senden“ – im n8n-Tab *Executions* erscheint
   die Ausführung.

Der Webhook-Node legt den Request-Body unter `body` ab. Nützliche Ausdrücke:

| Ausdruck | Wert |
|---|---|
| `{{ $json.body.notification.kind }}` | `event`, `escalation`, `test`, `report` |
| `{{ $json.body.notification.title }}` | Titel |
| `{{ $json.body.summary.maxSeverity }}` | höchster Schweregrad, z. B. `critical` |
| `{{ $json.body.summary.maxSeverityRank }}` | derselbe als Zahl 0 (info) … 4 (critical) |
| `{{ $json.body.text }}` | fertiger Klartext für Chat-/Mail-Nodes |
| `{{ $json.body.events[0].device.name }}` | Gerät des ersten Events |

**Beispiel: nur hohe und kritische Meldungen weiterleiten** – nach dem Webhook-Node einen
**IF**-Node einfügen:

- *Conditions:* Wert 1 `{{ $json.body.summary.maxSeverityRank }}`, Operator
  *Number → is greater than or equal to*, Wert 2 `3`
- Ausgang **true** → z. B. Node „Send Email“, „Slack“ oder „Home Assistant“ mit
  `{{ $json.body.text }}` als Nachricht; Ausgang **false** leer lassen.

Alternativ auf den Typ prüfen: *String → is equal to* mit
`{{ $json.body.summary.maxSeverity }}` und `critical`. Um jedes Event einzeln zu verarbeiten,
einen **Split Out**-Node mit *Field To Split Out* `body.events` verwenden; danach stehen die
Felder direkt unter `$json` (`{{ $json.severity }}`, `{{ $json.device.ip }}`,
`{{ $json.payload.port }}` …).

---

## NetScope-Payload v1

Webhook und n8n senden denselben JSON-Payload. Er ist in
`internal/plugins/webhook/payload.go` als Go-Structs definiert (`webhook.Payload`,
`webhook.BuildPayload`); eine Referenzdatei liegt unter
`internal/plugins/webhook/testdata/payload_v1.json` und wird durch einen Test gegen den Code
geprüft.

**Stabilität:** Alle Schlüssel sind immer vorhanden (fehlende Werte sind `""`, `0`, `false`,
`[]` oder `null`). Innerhalb von Version 1 kommen höchstens neue Felder hinzu; inkompatible
Änderungen erhöhen `version`. Zeitstempel sind RFC 3339 in UTC.

### Felder

| Feld | Typ | Bedeutung |
|---|---|---|
| `version` | Zahl | immer `1` |
| `source` | Text | immer `netscope` |
| `sentAt` | Zeit | Versandzeitpunkt (bei signierten Webhooks = `X-NetScope-Timestamp`) |
| `notification.id` | Zahl | ID der Benachrichtigung (bei Wiederholungen gleich) |
| `notification.kind` | Text | `event`, `escalation`, `test`, `report` |
| `notification.priority` | Text | `low`, `normal`, `high`, `urgent` |
| `notification.title` | Text | Titel |
| `notification.ruleId` / `ruleName` | Zahl / Text | auslösende Regel (`0`/`""` bei Tests) |
| `notification.link` | URL | Link in die UI (`""` ohne öffentliche URL) |
| `notification.createdAt` | Zeit | Erzeugung der Benachrichtigung |
| `summary.events` | Zahl | Anzahl der Events |
| `summary.maxSeverity` | Text | höchster Schweregrad (`info` ohne Events) |
| `summary.maxSeverityRank` | Zahl | 0 `info`, 1 `low`, 2 `medium`, 3 `high`, 4 `critical` |
| `summary.bySeverity` | Objekt | Anzahl je Schweregrad, immer alle fünf Schlüssel |
| `events[]` | Liste | gebündelte Events (Reihenfolge wie in der Regel-Engine) |
| `events[].id` | Zahl | Event-ID |
| `events[].type` | Text | Typ aus dem Event-Katalog, z. B. `port.opened`, `cve.new` (`GET /api/v1/events/types`) |
| `events[].label` | Text | deutscher Name des Typs |
| `events[].category` | Text | `device`, `port`, `cert`, `container`, `software`, `vulnerability`, `health`, `system` |
| `events[].severity` / `severityRank` / `severityLabel` | Text / Zahl / Text | Schweregrad, als Zahl 0–4 und deutsch |
| `events[].title` / `message` | Text | Titel und Detailtext |
| `events[].at` | Zeit | Zeitpunkt des Events |
| `events[].acknowledged` | Bool | inzwischen quittiert |
| `events[].escalated` | Bool | wurde eskaliert |
| `events[].link` | URL | Deep-Link auf das Event |
| `events[].device` | Objekt/`null` | `id`, `name`, `ip`, `mac`, `link` – `null` bei Events ohne Gerät (z. B. `plugin.failed`) |
| `events[].payload` | Objekt | typspezifische Felder laut Event-Katalog (z. B. `port`, `proto`, `cve`, `cvss`) |
| `events[].site` | Text | nur in einer Zentrale: NetScope-Standort, der das Event gemeldet hat (fehlt bei eigenen Events); auch als `payload.site` |
| `text` | Text | fertige Klartextdarstellung der ganzen Benachrichtigung |
| `body` | Text | Markdown-Text bei Berichten und Tests, sonst `""` |

### Vollständiges Beispiel

```json
{
  "version": 1,
  "source": "netscope",
  "sentAt": "2026-09-22T12:03:40Z",
  "notification": {
    "id": 42,
    "kind": "event",
    "priority": "high",
    "title": "2 neue Ereignisse auf nas",
    "ruleId": 3,
    "ruleName": "Neuer Port auf bekanntem Gerät",
    "link": "http://192.168.8.123:8080/events?notification=42",
    "createdAt": "2026-09-22T12:03:34Z"
  },
  "summary": {
    "events": 2,
    "maxSeverity": "critical",
    "maxSeverityRank": 4,
    "bySeverity": {
      "critical": 1,
      "high": 0,
      "info": 0,
      "low": 0,
      "medium": 1
    }
  },
  "events": [
    {
      "id": 1001,
      "type": "port.opened",
      "label": "Port neu",
      "category": "port",
      "severity": "medium",
      "severityRank": 2,
      "severityLabel": "Mittel",
      "title": "Neuer Port 22/tcp (ssh) auf nas",
      "message": "OpenSSH 9.6p1 Ubuntu 3ubuntu13",
      "at": "2026-09-22T12:03:04Z",
      "acknowledged": false,
      "escalated": false,
      "link": "http://192.168.8.123:8080/events/1001",
      "device": {
        "id": 17,
        "name": "nas",
        "ip": "192.168.8.10",
        "mac": "aa:bb:cc:dd:ee:ff",
        "link": "http://192.168.8.123:8080/devices/17"
      },
      "payload": {
        "device_ip": "192.168.8.10",
        "device_mac": "aa:bb:cc:dd:ee:ff",
        "device_name": "nas",
        "device_state": "known",
        "ip": "192.168.8.10",
        "port": 22,
        "product": "OpenSSH",
        "proto": "tcp",
        "service": "ssh",
        "version": "9.6p1 Ubuntu 3ubuntu13"
      }
    },
    {
      "id": 1002,
      "type": "cve.new",
      "label": "Neue Schwachstelle",
      "category": "vulnerability",
      "severity": "critical",
      "severityRank": 4,
      "severityLabel": "Kritisch",
      "title": "CVE-2024-6387 (CVSS 8.1) auf nas",
      "message": "OpenSSH: Race Condition im Signal-Handler (regreSSHion)",
      "at": "2026-09-22T12:03:06Z",
      "acknowledged": false,
      "escalated": false,
      "link": "http://192.168.8.123:8080/events/1002",
      "device": {
        "id": 17,
        "name": "nas",
        "ip": "192.168.8.10",
        "mac": "aa:bb:cc:dd:ee:ff",
        "link": "http://192.168.8.123:8080/devices/17"
      },
      "payload": {
        "cpe": "cpe:2.3:a:openbsd:openssh:9.6:p1:*:*:*:*:*:*",
        "cve": "CVE-2024-6387",
        "cvss": 8.1,
        "device_ip": "192.168.8.10",
        "device_mac": "aa:bb:cc:dd:ee:ff",
        "device_name": "nas",
        "device_state": "known",
        "match_type": "range",
        "product": "openssh",
        "vector": "CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:H/A:H",
        "version": "9.6p1"
      }
    }
  ],
  "text": "2 neue Ereignisse auf nas\n\n[Mittel] Neuer Port 22/tcp (ssh) auf nas (192.168.8.10)\n  OpenSSH 9.6p1 Ubuntu 3ubuntu13\n  http://192.168.8.123:8080/events/1001\n\n[Kritisch] CVE-2024-6387 (CVSS 8.1) auf nas (192.168.8.10)\n  OpenSSH: Race Condition im Signal-Handler (regreSSHion)\n  http://192.168.8.123:8080/events/1002\n\nhttp://192.168.8.123:8080/events?notification=42\n",
  "body": ""
}
```

Auf der Leitung wird das JSON kompakt (ohne Einrückung) gesendet; `&` in Links wird nicht als
`&` maskiert.

Bericht (`kind: report`) – typischerweise ohne Events:

```json
{
  "version": 1,
  "source": "netscope",
  "sentAt": "2026-09-28T07:00:02Z",
  "notification": {"id": 57, "kind": "report", "priority": "low", "title": "Wochenbericht KW 39",
    "ruleId": 0, "ruleName": "", "link": "http://192.168.8.123:8080/reports", "createdAt": "2026-09-28T07:00:00Z"},
  "summary": {"events": 0, "maxSeverity": "info", "maxSeverityRank": 0,
    "bySeverity": {"critical": 0, "high": 0, "info": 0, "low": 0, "medium": 0}},
  "events": [],
  "text": "Wochenbericht KW 39\n\n## Änderungen\n- 3 neue Geräte\n- 1 Zertifikat läuft ab\n\nhttp://192.168.8.123:8080/reports\n",
  "body": "## Änderungen\n- 3 neue Geräte\n- 1 Zertifikat läuft ab"
}
```
