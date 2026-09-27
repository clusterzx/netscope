package meraki

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"Cisco Meraki": "Cisco Meraki",
		"Liest Clients und Geräte aus Meraki-Netzen über die Dashboard-API: Namen, Adressen, VLAN, SSID, Switch-Port bzw. Access Point, Betriebssystem, feste IP-Zuweisungen der MX-Appliances.":                                                                                                                                  "Reads clients and devices from Meraki networks via the Dashboard API: names, addresses, VLAN, SSID, switch port or access point, operating system, fixed IP assignments of the MX appliances.",
		"API-Token mit dem API-Schlüssel eines Dashboard-Benutzers (Mein Profil → API-Zugang); Lesezugriff auf die Organisation genügt. Der Geltungsbereich des Credentials bezieht sich auf api.meraki.com.":                                                                                                                     "API token with the API key of a Dashboard user (My profile → API access); read access to the organization is sufficient. The credential's scope refers to api.meraki.com.",
		"API-Token mit dem API-Schlüssel eines Dashboard-Benutzers (Mein Profil → API-Zugang); Lesezugriff auf die Organisation genügt. Der Geltungsbereich des Credentials bezieht sich auf api.meraki.com. Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "API token with the API key of a Dashboard user (My profile → API access); read access to the organization is sufficient. The credential's scope refers to api.meraki.com. Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",
		"Organisationen": "Organizations",
		"Name oder ID; leer = alle, auf die der Schlüssel Zugriff hat.": "Name or ID; empty = all the key has access to.",
		"Netzwerke": "Networks",
		"Name oder ID; leer = alle Netzwerke der Organisationen.": "Name or ID; empty = all networks of the organizations.",
		"Clients der letzten": "Clients from the last",
		"Zeitraum, in dem Clients gesehen worden sein müssen (höchstens 31 Tage).": "Period in which clients must have been seen (at most 31 days).",
		"Meraki-Geräte übernehmen": "Import Meraki devices",
		"Access Points, Switches und Appliances selbst; nötig, damit Clients ihrem Access Point bzw. Switch zugeordnet werden.": "The access points, switches and appliances themselves; needed so that clients are assigned to their access point or switch.",
		"API-URL": "API URL",
		"Nur für andere Meraki-Regionen ändern (z. B. https://api.meraki.cn/api/v1).": "Only change for other Meraki regions (e.g. https://api.meraki.cn/api/v1).",

		// run log, run errors
		"zu viele Weiterleitungen":                    "too many redirects",
		"Organisationen: %w":                          "Organizations: %w",
		"Netzwerke von %s: %w":                        "Networks of %s: %w",
		"Geräte von %s: %w":                           "Devices of %s: %w",
		"Clients von %s: %w":                          "Clients of %s: %w",
		"Feste IP-Zuweisungen nicht lesbar":           "Cannot read fixed IP assignments",
		"keine passende Organisation (vorhanden: %s)": "no matching organization (available: %s)",
	})
}
