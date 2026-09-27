package n8n

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin info and settings
		"n8n": "n8n",
		"Übergibt Benachrichtigungen als dokumentierten JSON-Payload an einen n8n-Workflow (Webhook-Node).": "Passes notifications as a documented JSON payload to an n8n workflow (Webhook node).",
		"Webhook-URL": "Webhook URL",
		"Production-URL des n8n-Webhook-Nodes (…/webhook/…, nicht …/webhook-test/…). Der Workflow muss aktiv sein; HTTP-Methode im Node: POST.": "Production URL of the n8n Webhook node (…/webhook/…, not …/webhook-test/…). The workflow must be active; HTTP method in the node: POST.",
		"Authentifizierung": "Authentication",
		"Auth-Header-Name":  "Auth header name",
		"Optional, für „Header Auth“ im Webhook-Node, z. B. X-N8N-Key.": "Optional, for “Header Auth” in the Webhook node, e.g. X-N8N-Key.",
		"Auth-Header-Wert": "Auth header value",
		"Wert des Auth-Headers (verschlüsselt gespeichert).": "Value of the auth header (stored encrypted).",
		"Timeout": "Timeout",
		"Maximale Dauer einer Zustellung. Im Webhook-Node „Respond: Immediately“ wählen, damit n8n nicht erst am Ende des Workflows antwortet.": "Maximum duration of a delivery. Choose “Respond: Immediately” in the Webhook node so that n8n does not answer only at the end of the workflow.",
		"TLS-Zertifikat prüfen": "Verify TLS certificate",
		"Nur für n8n-Instanzen mit selbstsigniertem Zertifikat im eigenen Netz abschalten.": "Turn off only for n8n instances with a self-signed certificate in your own network.",

		// validation and delivery errors
		"Wert für den Auth-Header fehlt":               "value for the auth header missing",
		"Name des Auth-Headers fehlt":                  "name of the auth header missing",
		"n8n: %w":                                      "n8n: %w",
		"n8n: Payload konnte nicht erzeugt werden: %w": "n8n: could not build the payload: %w",
		"n8n-Webhook zugestellt":                       "n8n webhook delivered",
		"%w (Test-URL: funktioniert nur, solange im Editor „Listen for test event“ läuft – Production-URL verwenden)": "%w (test URL: only works while “Listen for test event” is running in the editor – use the production URL)",
		"%w (Workflow aktiv? HTTP-Methode im Webhook-Node auf POST gestellt?)":                                        "%w (is the workflow active? is the HTTP method in the Webhook node set to POST?)",
		"%w (Auth-Header-Name und -Wert mit der Header-Auth-Credential im Webhook-Node vergleichen)":                  "%w (compare the auth header name and value with the Header Auth credential in the Webhook node)",
	})
}
