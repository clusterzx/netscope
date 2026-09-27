package webui

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		missingUI: missingUIEn,
	})
}

// missingUIEn is the English page shown when the build contains no web UI (missingUI).
const missingUIEn = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>NetScope</title></head>
<body style="font-family:system-ui;background:#0b1020;color:#e5e9f5;padding:40px">
<h1>NetScope</h1><p>The web UI is not included in this build (frontend not built).
<code>make build</code> or the Docker image embeds it. The API is available at <a style="color:#5eb1ff" href="/api/docs">/api/docs</a>.</p></body></html>`
