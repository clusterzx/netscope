package telegram

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin info and settings
		"Telegram": "Telegram",
		"Sendet gebündelte Benachrichtigungen über einen Telegram-Bot in einen Chat, eine Gruppe oder einen Kanal.": "Sends bundled notifications through a Telegram bot to a chat, a group or a channel.",
		"Verbindung":  "Connection",
		"Darstellung": "Display",
		"Bot-Token":   "Bot token",
		"Token von @BotFather im Format 123456789:AA…": "Token from @BotFather in the format 123456789:AA…",
		"Chat-ID": "Chat ID",
		"Numerische ID des Chats bzw. der Gruppe (Gruppen/Kanäle beginnen mit -100…) oder @kanalname. Der Bot muss Mitglied sein bzw. vom Benutzer mit /start angeschrieben worden sein.": "Numeric ID of the chat or group (groups and channels start with -100…) or @channelname. The bot must be a member, or the user must have sent it /start.",
		"Themen-ID (Forum)": "Topic ID (forum)",
		"Optional: ID des Themas in Gruppen mit aktivierten Themen. 0 = allgemeiner Bereich.": "Optional: ID of the topic in groups with topics enabled. 0 = general topic.",
		"Lautlos unterhalb von": "Silent below",
		"Benachrichtigungen mit niedrigerer Priorität werden ohne Ton zugestellt.": "Notifications with a lower priority are delivered without sound.",
		"Niedrig (nie lautlos)":                                   "Low (never silent)",
		"Normal (nur Niedrig lautlos)":                            "Normal (only low silent)",
		"Hoch (Niedrig und Normal lautlos)":                       "High (low and normal silent)",
		"Dringend (nur Dringend mit Ton)":                         "Urgent (only urgent with sound)",
		"Link-Vorschau unterdrücken":                              "Suppress link preview",
		"Bot-API-URL":                                             "Bot API URL",
		"Nur ändern für einen selbst betriebenen Bot-API-Server.": "Change only for a self-hosted Bot API server.",
		"Timeout": "Timeout",
		"Maximale Dauer einer Anfrage an die Bot-API.": "Maximum duration of a request to the Bot API.",

		// validation and delivery errors
		"ungültiges Format (erwartet 123456789:ABC…, wie von @BotFather ausgegeben)": "invalid format (expected 123456789:ABC…, as issued by @BotFather)",
		"kein Bot-Token konfiguriert":                                         "no bot token configured",
		"Bot-Token hat ein ungültiges Format":                                 "bot token has an invalid format",
		"keine Chat-ID konfiguriert":                                          "no chat ID configured",
		"ungültige Bot-API-URL":                                               "invalid Bot API URL",
		"Telegram: %w":                                                        "Telegram: %w",
		"Telegram: abgebrochen nach %d von %d Teilen: %w":                     "Telegram: aborted after %d of %d parts: %w",
		"Telegram: Teil %d von %d nicht zugestellt (%d bereits gesendet): %w": "Telegram: part %d of %d not delivered (%d already sent): %w",
		"Telegram-Nachricht gesendet":                                         "Telegram message sent",
		"Ratenlimit der Bot-API erreicht (HTTP 429) – erneuter Versuch frühestens in %s möglich":                              "Bot API rate limit reached (HTTP 429) – retry possible in %s at the earliest",
		"Ratenlimit der Bot-API erreicht (HTTP 429)":                                                                          "Bot API rate limit reached (HTTP 429)",
		"Anfrage an die Bot-API konnte nicht erstellt werden":                                                                 "could not create the request to the Bot API",
		"Bot-API %s: Zeitüberschreitung nach %s":                                                                              "Bot API %s: timed out after %s",
		"Bot-API %s nicht erreichbar: %s":                                                                                     "Bot API %s not reachable: %s",
		"Antwort der Bot-API unvollständig: %s":                                                                               "incomplete response from the Bot API: %s",
		"unerwartete Antwort der Bot-API (HTTP %s): %s":                                                                       "unexpected response from the Bot API (HTTP %s): %s",
		"die Gruppe wurde in eine Supergruppe umgewandelt – neue Chat-ID %d eintragen":                                        "the group was converted into a supergroup – enter the new chat ID %d",
		"Bot-Token ungültig oder widerrufen (%d: %s)":                                                                         "bot token invalid or revoked (%d: %s)",
		"Chat nicht gefunden (%d: %s) – Chat-ID prüfen; der Bot muss Mitglied sein bzw. mit /start angeschrieben worden sein": "chat not found (%d: %s) – check the chat ID; the bot must be a member or must have been sent /start",
		"Bot darf in diesen Chat nicht schreiben (%d: %s)":                                                                    "bot may not write to this chat (%d: %s)",
		"Bot-API-Fehler %d: %s":                                                                                               "Bot API error %d: %s",
		"(ungültig)":                                                                                                          "(invalid)",

		// message texts
		"1 Ereignis":                   "1 event",
		"%d Ereignisse":                "%d events",
		"Eskalation – nicht quittiert": "Escalation – not acknowledged",
		"Regel: %s":                    "Rule: %s",
		"(Fortsetzung)":                "(continued)",
		"Öffnen":                       "Open",
		"eskaliert":                    "escalated",
		"quittiert":                    "acknowledged",
		"Standort %s":                  "Site %s",
		"… und 1 weiteres Ereignis":    "… and 1 more event",
		"… und %d weitere Ereignisse":  "… and %d more events",
		"Alle anzeigen":                "Show all",
		"In NetScope öffnen":           "Open in NetScope",
		"Teil %d/%d":                   "Part %d/%d",
	})
}
