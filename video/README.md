# NetScope – Intro-Video

Ein ca. 80 Sekunden langes Intro-/Info-Video zu NetScope, gebaut mit [Remotion](https://www.remotion.dev)
(React → MP4) und einem Voice-over von [ElevenLabs](https://elevenlabs.io). Zwei Fassungen:
`NetScopeIntro-de` und `NetScopeIntro-en`, 1920 × 1080, 30 fps.

| Szene      | Inhalt                                                       |
| ---------- | ------------------------------------------------------------ |
| hook       | „Wie viele Geräte hängen gerade in deinem Netzwerk?“         |
| questions  | Die sieben Fragen aus dem README                             |
| reveal     | Logo, Wortmarke, Kernaussagen – hier setzt der Beat ein      |
| discovery  | Radar findet Geräte, 15 Scanner, nmap-Ergebnis               |
| sources    | Importer (Proxmox, OPNsense, UniFi …) und Agent-Installation |
| inventory  | Geräteliste, neues Gerät, Änderungshistorie                  |
| vulns      | CVE-Liste, Umsortierung nach CISA KEV und EPSS               |
| topology   | Topologie mit Health-Check-Ausfall, Rack mit Patchkabel      |
| alerts     | Regel und Benachrichtigungen auf dem Handy                   |
| selfhosted | 1 Binary · 41 Plugins · 0 Cloud                              |
| outro      | „NetScope. Kenne dein Netzwerk.“                             |

## Ablauf

```bash
cd video
npm install
cp .env.example .env              # ELEVENLABS_API_KEY eintragen
npm run voiceover:de              # public/voiceover/de/*.mp3 + manifest.json
npm run voiceover:en
npm run sound                     # Musik und Effekte (synthetisiert, ohne Samples)
npm run studio                    # Vorschau im Browser
npm run render:de                 # out/netscope-intro-de.mp4
npm run render:en
```

Die Szenenlängen ergeben sich aus den gemessenen Längen der Sprachaufnahmen
(`public/voiceover/<lang>/manifest.json`). Wer einen Satz in `src/voiceover.json` ändert oder eine
andere Stimme wählt, erzeugt nur das Voice-over neu (`node scripts/voiceover.mjs --lang de --only reveal`)
– das Timing passt sich an. Die Texte im Bild stehen in `src/i18n.ts`.

- **Stimme:** `ELEVENLABS_VOICE_ID` (Standard: Brian), Modell `eleven_multilingual_v2`.
- **Ohne ElevenLabs:** `--engine piper` mit `PIPER_MODEL=/pfad/zu/de_DE-thorsten-high.onnx` nutzt die
  lokale Open-Source-TTS [Piper](https://github.com/rhasspy/piper) (`pip install piper-tts`).
- **Ohne Voice-over** rendern die Szenen mit geschätzten Längen und ohne Ton; ohne `npm run sound`
  fehlen Musik und Effekte.
- **Musik:** `scripts/sound.mjs` erzeugt 100 BPM, Am–F–C–G; der Beat setzt nach 6 Takten ein und wird
  im Video auf den Logo-Moment gelegt (`MUSIC_DROP_SECONDS` in `src/timeline.ts`).
- **Icons und Farben** kommen aus der Web-UI (`web/src/lib/components/ui/icons.ts`, Dark-Theme).
- **Chrome:** Remotion lädt beim ersten Render eine Headless-Chrome herunter; eine vorhandene lässt sich
  mit `REMOTION_BROWSER_EXECUTABLE=/pfad/zu/chrome` verwenden.

Die gezeigten Daten (Geräte, CVEs mit CVSS/EPSS, Benachrichtigungen) sind Beispielwerte.
