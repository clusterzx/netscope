package api

import (
	"encoding"
	"encoding/json"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

type schemaGen struct {
	components map[string]any
	names      map[reflect.Type]string
}

var (
	timeType          = reflect.TypeOf(time.Time{})
	durationType      = reflect.TypeOf(time.Duration(0))
	rawType           = reflect.TypeOf(json.RawMessage{})
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
)

func (g *schemaGen) name(t reflect.Type) string {
	if n, ok := g.names[t]; ok {
		return n
	}
	pkg := t.PkgPath()
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	tn := t.Name()
	if i := strings.Index(tn, "["); i > 0 {
		tn = tn[:i] // generic instantiation
	}
	base := strings.ToUpper(pkg[:1]) + pkg[1:] + strings.ToUpper(tn[:1]) + tn[1:]
	n := base
	for i := 2; ; i++ {
		taken := false
		for _, v := range g.names {
			if v == n {
				taken = true
			}
		}
		if !taken {
			break
		}
		n = base + strings.Repeat("_", i-1)
	}
	g.names[t] = n
	return n
}

func (g *schemaGen) schema(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == timeType:
		return map[string]any{"type": "string", "format": "date-time"}
	case t == durationType:
		return map[string]any{"type": "integer", "description": "Nanosekunden"}
	case t == rawType:
		return map[string]any{}
	case t.Kind() != reflect.Struct && (t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType)):
		return map[string]any{"type": "string"}
	case t.Kind() == reflect.Struct && (t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType)) &&
		!t.Implements(jsonMarshalerType):
		return map[string]any{"type": "string"}
	}
	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{"type": "array", "items": g.schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": g.schema(t.Elem())}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		if t.Name() == "" {
			return g.structSchema(t)
		}
		n := g.name(t)
		if _, done := g.components[n]; !done {
			g.components[n] = map[string]any{} // placeholder against recursion
			g.components[n] = g.structSchema(t)
		}
		return map[string]any{"$ref": "#/components/schemas/" + n}
	}
	return map[string]any{}
}

func (g *schemaGen) structSchema(t reflect.Type) map[string]any {
	props := map[string]any{}
	var required []string
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			if f.Anonymous && name == "" {
				ft := f.Type
				for ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if ft.Kind() == reflect.Struct {
					walk(ft)
					continue
				}
			}
			if name == "" {
				name = f.Name
			}
			props[name] = g.schema(f.Type)
			if !strings.Contains(opts, "omitempty") && f.Type.Kind() != reflect.Pointer {
				required = append(required, name)
			}
		}
	}
	walk(t)
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		s["required"] = required
	}
	return s
}

var pathParamRe = regexp.MustCompile(`\{([a-zA-Z_]+)\}`)

// Spec returns the generated OpenAPI 3.1 document.
func (s *Server) Spec() map[string]any {
	g := &schemaGen{components: map[string]any{}, names: map[reflect.Type]string{}}
	paths := map[string]map[string]any{}
	errSchema := map[string]any{"type": "object", "properties": map[string]any{"error": map[string]any{"type": "object",
		"properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"},
			"fields": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}}}}}
	for _, rt := range s.routes {
		op := map[string]any{"summary": rt.Summary, "tags": []string{rt.Tag},
			"operationId": strings.ToLower(rt.Method) + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(rt.Path, "/api/v1", ""), "/", "_"), "{", "")}
		op["operationId"] = strings.ReplaceAll(op["operationId"].(string), "}", "")
		var params []map[string]any
		for _, m := range pathParamRe.FindAllStringSubmatch(rt.Path, -1) {
			params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
		for _, p := range rt.Params {
			in := p.In
			if in == "" {
				in = "query"
			}
			typ := p.Type
			if typ == "" {
				typ = "string"
			}
			params = append(params, map[string]any{"name": p.Name, "in": in, "required": p.Required, "description": p.Desc,
				"schema": map[string]any{"type": typ}})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if rt.Body != nil {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{
				"schema": g.schema(reflect.TypeOf(rt.Body))}}}
		}
		status := rt.Status
		if status == 0 {
			status = http.StatusOK
		}
		resp := map[string]any{"description": "Erfolg"}
		switch {
		case rt.Content != "":
			resp["content"] = map[string]any{rt.Content: map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}}
		case rt.Resp != nil:
			resp["content"] = map[string]any{"application/json": map[string]any{"schema": g.schema(reflect.TypeOf(rt.Resp))}}
		}
		errResp := map[string]any{"description": "Fehler", "content": map[string]any{"application/json": map[string]any{"schema": errSchema}}}
		op["responses"] = map[string]any{itoa(status): resp, "default": errResp}
		if rt.Scope == scopePublic {
			op["security"] = []any{}
		} else {
			desc := "Lesezugriff"
			if rt.Scope == scopeWrite || rt.Method != http.MethodGet {
				desc = "Schreibzugriff (Token-Scope write)"
			}
			if rt.Perm != "" {
				desc += " · Berechtigung „" + permLabel(rt.Perm) + "“ (" + rt.Perm + ")"
			}
			op["description"] = desc
		}
		if paths[rt.Path] == nil {
			paths[rt.Path] = map[string]any{}
		}
		paths[rt.Path][strings.ToLower(rt.Method)] = op
	}
	paths["/api/v1/stream"] = map[string]any{"get": map[string]any{"summary": "Live-Updates (Server-Sent Events)", "tags": []string{"System"},
		"parameters": []any{map[string]any{"name": "topics", "in": "query", "schema": map[string]any{"type": "string"},
			"description": "Kommagetrennt: run, run.log, device, event, plugin, notification, health, system, log"}},
		"responses": map[string]any{"200": map[string]any{"description": "text/event-stream"}}}}
	paths["/metrics"] = map[string]any{"get": map[string]any{"summary": "Prometheus-Metriken", "tags": []string{"System"},
		"responses": map[string]any{"200": map[string]any{"description": "text/plain; version=0.0.4"}}}}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": "NetScope API", "version": s.Version,
			"description": "JSON-API von NetScope. Authentifizierung per Session-Cookie (Web-UI, zusätzlich Header X-NetScope-CSRF bei schreibenden Anfragen) oder per API-Token (Authorization: Bearer ns_…; Scope read oder write). Ein Token hat höchstens die Rechte der Rolle seines Benutzers; die nötige Berechtigung steht bei jedem Endpunkt."},
		"servers": []any{map[string]any{"url": "/"}},
		"paths":   paths,
		"components": map[string]any{
			"schemas": g.components,
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"},
				"cookieAuth": map[string]any{"type": "apiKey", "in": "cookie", "name": sessionCookie},
			},
		},
		"security": []any{map[string]any{"bearerAuth": []any{}}, map[string]any{"cookieAuth": []any{}}},
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Spec())
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'")
	_, _ = w.Write([]byte(docsHTML))
}

const docsHTML = `<!doctype html>
<html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>NetScope API</title>
<style>
:root{--bg:#0b1020;--panel:#111831;--line:#243052;--text:#e5e9f5;--muted:#8d97b5;--acc:#5eb1ff;--get:#3fb68b;--post:#e0a23a;--put:#7b8cff;--patch:#b07bff;--delete:#e5596b}
@media (prefers-color-scheme: light){:root{--bg:#f6f7fb;--panel:#fff;--line:#dfe3ee;--text:#1b2233;--muted:#5d6784;--acc:#1f6feb}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 system-ui,-apple-system,Segoe UI,Roboto,sans-serif}
header{padding:20px 24px;border-bottom:1px solid var(--line)}h1{margin:0;font-size:20px}header p{margin:6px 0 0;color:var(--muted);max-width:900px}
main{max-width:1100px;margin:0 auto;padding:16px 24px 60px}h2{font-size:15px;margin:28px 0 8px;color:var(--acc);text-transform:uppercase;letter-spacing:.06em}
details{background:var(--panel);border:1px solid var(--line);border-radius:8px;margin:6px 0}summary{cursor:pointer;padding:9px 12px;display:flex;gap:10px;align-items:center}
.m{font:600 11px ui-monospace,monospace;padding:2px 7px;border-radius:4px;color:#fff;min-width:58px;text-align:center}
.GET{background:var(--get)}.POST{background:var(--post)}.PUT{background:var(--put)}.PATCH{background:var(--patch)}.DELETE{background:var(--delete)}
code,pre{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:12.5px}.path{font-family:ui-monospace,monospace}
.sum{color:var(--muted);margin-left:auto;text-align:right}.body{padding:4px 14px 14px;border-top:1px solid var(--line)}
pre{background:var(--bg);border:1px solid var(--line);border-radius:6px;padding:10px;overflow:auto;max-height:420px}
table{border-collapse:collapse;width:100%;margin:6px 0}td,th{border-bottom:1px solid var(--line);padding:4px 6px;text-align:left;vertical-align:top}
input{width:100%;padding:8px 10px;border-radius:6px;border:1px solid var(--line);background:var(--panel);color:var(--text);margin-top:10px}
a{color:var(--acc)}
</style></head><body>
<header><h1>NetScope API</h1><p id="desc">Lade Spezifikation …</p><p><a href="/api/openapi.json">openapi.json</a> · <a href="/">zur Oberfläche</a></p>
<input id="filter" placeholder="Endpunkte filtern (z. B. devices, POST, events)"></header>
<main id="main"></main>
<script>
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
let spec;
function resolve(s,depth){depth=depth||0;if(!s)return{};if(s.$ref){const n=s.$ref.split('/').pop();if(depth>6)return{type:n};return resolve(spec.components.schemas[n],depth+1)}
 if(s.type==='array')return{type:'array',items:resolve(s.items,depth+1)};if(s.type==='object'&&s.properties){const o={};for(const k in s.properties)o[k]=resolve(s.properties[k],depth+1);return{type:'object',properties:o}}return s}
function example(s,d){d=d||0;s=resolve(s,0);if(d>5)return'…';if(s.type==='object'&&s.properties){const o={};for(const k in s.properties)o[k]=example(s.properties[k],d+1);return o}
 if(s.type==='object')return{};if(s.type==='array')return[example(s.items,d+1)];if(s.type==='integer')return 0;if(s.type==='number')return 0.0;if(s.type==='boolean')return false;
 if(s.format==='date-time')return'2026-01-01T00:00:00Z';if(s.type==='string')return'string';return null}
function render(filter){const byTag={};for(const p in spec.paths)for(const m in spec.paths[p]){const op=spec.paths[p][m];const t=(op.tags||['Sonstiges'])[0];
 const hay=(m+' '+p+' '+op.summary+' '+t).toLowerCase();if(filter&&!hay.includes(filter.toLowerCase()))continue;(byTag[t]=byTag[t]||[]).push([m.toUpperCase(),p,op])}
 let h='';for(const t of Object.keys(byTag).sort()){h+='<h2>'+esc(t)+'</h2>';for(const [m,p,op] of byTag[t]){
  h+='<details><summary><span class="m '+m+'">'+m+'</span><span class="path">'+esc(p)+'</span><span class="sum">'+esc(op.summary)+'</span></summary><div class="body">';
  if(op.description)h+='<p>'+esc(op.description)+'</p>';
  if(op.parameters){h+='<table><tr><th>Parameter</th><th>Ort</th><th>Typ</th><th>Beschreibung</th></tr>';for(const x of op.parameters)h+='<tr><td><code>'+esc(x.name)+'</code>'+(x.required?' *':'')+'</td><td>'+esc(x.in)+'</td><td>'+esc(x.schema&&x.schema.type)+'</td><td>'+esc(x.description)+'</td></tr>';h+='</table>'}
  if(op.requestBody){const s=op.requestBody.content['application/json'].schema;h+='<p><b>Request-Body</b></p><pre>'+esc(JSON.stringify(example(s),null,2))+'</pre>'}
  for(const c in op.responses){const r=op.responses[c];if(c==='default')continue;const ct=r.content&&Object.keys(r.content)[0];h+='<p><b>Antwort '+esc(c)+'</b> '+esc(ct||'')+'</p>';
   if(ct==='application/json')h+='<pre>'+esc(JSON.stringify(example(r.content[ct].schema),null,2))+'</pre>'}
  h+='</div></details>'}}
 document.getElementById('main').innerHTML=h||'<p>Keine Treffer.</p>'}
fetch('/api/openapi.json').then(r=>r.json()).then(s=>{spec=s;document.getElementById('desc').textContent=s.info.description+' Version '+s.info.version;render('')});
document.getElementById('filter').addEventListener('input',e=>render(e.target.value));
</script></body></html>`
