// Package webui embeds the built SvelteKit single page application (static adapter).
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist is filled by the frontend build (web/ → internal/webui/dist).
//
//go:embed all:dist
var dist embed.FS

const missingUI = `<!doctype html><html lang="de"><head><meta charset="utf-8"><title>NetScope</title></head>
<body style="font-family:system-ui;background:#0b1020;color:#e5e9f5;padding:40px">
<h1>NetScope</h1><p>Die Weboberfläche ist in diesem Build nicht enthalten (Frontend nicht gebaut).
Mit <code>make build</code> bzw. dem Docker-Image wird sie eingebettet. Die API steht unter <a style="color:#5eb1ff" href="/api/docs">/api/docs</a> bereit.</p></body></html>`

// Handler serves the SPA: existing files directly, everything else as index.html.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	index, indexErr := fs.ReadFile(sub, "index.html")
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(sub, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "_app/immutable/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if indexErr != nil {
			_, _ = w.Write([]byte(missingUI))
			return
		}
		_, _ = w.Write(index)
	})
}
