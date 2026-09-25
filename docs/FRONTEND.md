# NetScope web UI – developer guide

Guide for working on the SvelteKit UI in `web/` (built into `internal/webui/dist` and
embedded into the Go binary). Plugin development: [PLUGINS.md](PLUGINS.md).

```text

Stack: SvelteKit 2 SPA (adapter-static, ssr=false) · Svelte 5 runes · TypeScript strict · Tailwind v4
(CSS-first, tokens in src/app.css). UI texts German, code English. No component library.

Quality gates: `npm run check` (0 errors/0 warnings, a11y warnings fail!), `npm run lint` (prettier:
tabs, single quotes, printWidth 110), `npm run build` (→ ../internal/webui/dist, then
`touch ../internal/webui/dist/.keep`).

Dev server: `make dev` (Go backend on 127.0.0.1:18080 + vite), or only the UI against a running
instance: NETSCOPE_API=http://<host>:8080 npm run dev (add --port <n> if 5173 is taken).
Tailwind (dev only): classes that appear in a *newly created* file sometimes show up only after the
next edit/HMR of any file or a server restart; the production build always scans everything.


1. Routing / ownership
----------------------
src/routes/+layout.ts       auth guard: resolves the session once (auth.check), else redirect
                            /login?next=<path>. ssr=false.
src/routes/+layout.svelte   imports app.css, theme.init(), renders <AppShell> (not on /login),
                            <ConfirmHost/>, <Toaster/>.
src/routes/+error.svelte    404/errors.
Pages: / (dashboard), /devices, /devices/[id], /topology, /events, /diff, /health,
/vulnerabilities(/[cve]), /plugins(/[id](/runs/[run])), /rules(/new, /[id]), /credentials,
/reports, /system (?tab=…), /login. Page-specific components live in
src/lib/components/<area>/. The sidebar is src/lib/nav.ts. Every page renders
<PageHeader title=…> (sets <title>).


2. API client  (src/lib/api)
----------------------------
import { api, ApiError, errorMessage, fieldErrors } from '$lib/api';
import type { PluginView, Rule, HealthCheck, … } from '$lib/api';     // friendly aliases, types.ts

Typed against ApiPaths (generated.ts, generated from /api/openapi.json – DO NOT EDIT;
regenerate: `npm run gen:api -- http://192.168.8.123:8080` or `-- path/to/openapi.json`):

  const list = await api.get('/api/v1/plugins');                                   // PluginView[]
  const p    = await api.get('/api/v1/plugins/{id}', { path: { id } });
  await api.put('/api/v1/plugins/{id}/config', { path: { id }, body: { enabled: true } });
  await api.post('/api/v1/rules/{id}/test', { path: { id }, body: simInput });
  const runs = await api.get('/api/v1/runs', { query: { plugin: id, limit: 20 }, signal });
  await api.delete('/api/v1/credentials/{id}', { path: { id } });

- path params are required when the path has {…}; query/body are type checked.
- Options: signal (AbortController), auth:false (no login redirect on 401), fetch (load fetch).
- Mutations automatically send `X-NetScope-CSRF: 1`. 401 → redirect to /login?next=….
- Errors: ApiError { status, code, message, fields[] }, e.fieldMap → { field: message }.
  fieldErrors(e) returns that map (or {}), errorMessage(e) any error as text.
- Not in the spec (yet) / generic: api.raw.get<T>(url, query?), .post<T>(url, body?), .put, .patch,
  .delete. Planned CVE endpoints: types DeviceCVE / CVEIgnoreRequest in types.ts.
- api.upload(file) → { path, name, size } (POST /api/v1/uploads multipart).
- api.download(url, query?) fetches and saves a file (Content-Disposition name);
  api.url(path, query) builds "path?query" for plain <a href download> links.
- NOTE: Go encodes nil slices as null – list responses can be null (e.g. /devices/{id}/health,
  /health-checks when empty). Always `?? []`.
- Constants/types: SECRET_MASK ('********'), Severity, SEVERITIES, DeviceState, LiveMessage, …


3. Stores  (src/lib/stores, all Svelte 5 runes classes – read properties directly)
----------------------------------------------------------------------------------
auth.svelte.ts      auth.me {user, principal}, auth.can(perm) (Rechte der Rolle – Aktionen ohne Recht
                    ausblenden), auth.restricted, auth.login() (+ zweiter Schritt), auth.logout()
live.svelte.ts      one shared SSE connection (/api/v1/stream, all topics, auto reconnect):
                      $effect(() => live.on('plugin', (m) => …));      // returns unsubscribe
                      $effect(() => live.on<RunLogMessageData>('run.log', (m) => …));
                      $effect(() => live.onReconnect(() => reload()));
                    live.status: idle|connecting|open|reconnecting. Message = {id, topic, type, at, data}
                    Topics/types: run (queued|started|progress|finished; data.run = RunView,
                    finished may be discarded = routine run not kept), run.log (line with stored id),
                    device (created|updated|deleted, deleted may carry mergedInto), event
                    (created|acked), plugin (updated), notification (created|updated|sent|failed),
                    health (state|round), system, log (line → LogEntry). Payload types in
                    api/types.ts; full list in docs/ARCHITECTURE.md.
runs.svelte.ts      runs.active (queued+running RunView[], live progress), runs.recent,
                    runs.forPlugin(id), runs.get(id), runs.onFinished(fn) → unsubscribe.
catalog.svelte.ts   cached Resources: meta, groups, tags, subnets, customFields, credentials,
                    eventTypes. `x.load()` (cached), `x.refresh()`, `x.value`, `x.loading`, `x.error`.
                    Call credentials.refresh() after creating/deleting credentials, groups.refresh() …
                    eventTypeLabel(type).
eventCounts.svelte.ts  open events per severity (live), eventCounts.urgent.
resource.svelte.ts  Resource<T> (shared cache) and AsyncData<T> (per page request state):
                      const data = new AsyncData<HealthBoard>();
                      $effect(() => { data.run((signal) => api.get('/api/v1/health/board', { signal })); });
                      data.data / data.loading / data.error / data.reload() / data.set(v)
                    Reactive reads inside the run callback are tracked by the surrounding $effect.
toast.svelte.ts     toast.success(msg) / .info / .warning / .error(errOrMsg, { title, timeout, action })
confirm.svelte.ts   if (await confirm({ title, message, confirmLabel: 'Löschen', danger: true })) …
theme.svelte.ts     theme.mode system|light|dark, theme.set(), theme.resolved (dark class on <html>)


4. UI kit  (import { … } from '$lib/components/ui')
-----------------------------------------------------
Every component documents its props in a comment at the top of the file. Summary:
Button        variant primary|secondary|ghost|danger|subtle, size xs|sm|md, icon, iconRight,
              loading, href (renders <a>), label (REQUIRED for icon-only: aria-label + tooltip), active
Input         label, hint, error, icon, size sm|md, mono, trailing snippet, bind:value, bind:ref, + native attrs
Textarea      label, hint, error, mono, rows, bind:value
Select        options (string | {value,label,disabled}), placeholder (adds "" option), bind:value
MultiSelect   options (string | {value,label,description,group}), bind:value (string[]), onchange
TagInput      bind:value (string[]), suggestions, normalize
Checkbox      bind:checked, indeterminate, label, hideLabel, description
Toggle        bind:checked or checked+onchange(v), label (required), hideLabel, description, size
FormField     wrapper for custom controls: {#snippet children(id, describedby)}…{/snippet}
Badge         tone neutral|accent|ok|warn|danger|unknown|info|low|medium|high|critical, variant, dot
SeverityBadge severity | cvss (shows the score, colour from CVSS)
StatusDot     status online|offline|up|down|degraded|unknown|running|…, label
Card          title, description, icon, padding none|sm|md, actions/header/footer snippets
Tabs          items [{id,label,count,icon}], bind:active; panels: role="tabpanel" id="panel-<id>"
              aria-labelledby="tab-<id>" (use idPrefix for several tab bars)
Modal         bind:open, title, description, size sm|md|lg|xl, as="form" + onsubmit, busy, footer snippet
Drawer        bind:open, title, size md|lg|xl, footer/headerExtra snippets (right side panel)
Popover       bind:open, anchor (element), placement, matchWidth – fixed positioned, outside click/Esc
Menu          items [{label, icon, onclick|href, danger, disabled, hint} | {separator:true,label?}],
              label (aria), text (text button) or icon-only
Table<T>      columns [{key,label,sortable,sortDesc,align,width,hideBelow,value,cell,class}], rows,
              key(row), sort ("f"/"-f") + onsort(next), selectable + bind:selected (keys),
              onrowclick, rowClass, loading, dense, maxHeight (sticky header), cell/empty/expanded
              snippets. Cell precedence: column.cell › table cell snippet › column.value › row[key].
Pagination    total, bind:offset, bind:limit, sizes, onchange(offset, limit)
EmptyState    title, description, icon, compact, actions snippet
ErrorState    error, onretry, title, compact (inline Alert)
Alert         tone info|ok|warn|danger, title, actions snippet
Spinner, Skeleton (lines | rows | class for one bar), ProgressBar (done/total, indeterminate)
CodeBlock     code, label, maxHeight, wrap (copy button)
JsonView      value, openDepth (tree + raw JSON toggle + copy)
MarkdownView  source (marked + DOMPurify)
Sparkline     values | points (SeriesPoint[]) + band, label (aria)
TimeSeriesChart points (SeriesPoint[] from …/timeseries or /health-checks/{id}/latency), label,
              format(v), height, band (min–max), yMin, from/to (ms); crosshair tooltip, table view
RelativeTime  value (RFC3339) → "vor 5 Minuten" (auto-updating), absolute → dd.MM.yyyy HH:mm
StatCard      label, value, icon, tone, href, children = detail line
DescList/DescItem  key/value lists (<dl>), DescItem label, value | children, mono, hint
CopyButton    text
PageHeader    title, description, meta/actions/breadcrumb snippets (also sets document title)
Icon          name (see ui/icons.ts; add paths there), size, label
Toaster / ConfirmHost are mounted in the root layout already.


5. SchemaForm  (import { SchemaForm, schemaInitial, schemaPayload, validateSchema } from '$lib/components/schema')
------------------------------------------------------------------------------------------------------------------
Plugin settings (plugin.schema.fields) and credential types (credentialTypes[].schema.fields):

  let values = $state(schemaInitial(p.schema.fields, p.config.settings));          // plugin
  let values = $state(schemaInitial(t.schema.fields, cred.public, cred.secretsSet)); // credential edit
  let errors = $state<Record<string, string>>({});
  <SchemaForm fields={p.schema.fields} bind:values {errors} idPrefix="pl-{p.info.id}" />

  async function save() {
    errors = validateSchema(fields, values);            // client check (visible fields only)
    if (Object.keys(errors).length) return;
    try { await api.put('/api/v1/plugins/{id}/config', { path: { id }, body: { settings: schemaPayload(fields, values) } }); }
    catch (e) { errors = fieldErrors(e); toast.error(e); }
  }

- values must be a $state object; canonical types: int → number|null, bool, lists → string[],
  credential-ref → number (0 = none) or number[] (multi), enum multi → string[].
- Secrets: "gesetzt"/"nicht gesetzt" badge, Ändern/Entfernen. SECRET_MASK keeps the stored secret,
  "" clears it, anything else replaces it (works for plugin settings and credentials PUT).
- Supports string (multiline → textarea, widget "file" → upload to /api/v1/uploads and store the
  path), secret, int, bool, cron (live description + next runs via /api/v1/cron/describe, presets),
  enum single/multi, string-list, subnet-list (CIDR check), credential-ref (credentials filtered by
  field.credentialTypes, multi), duration; label/description/placeholder/required/default/group/
  advanced (collapsible)/visibleIf/validation (min/max/pattern/format).
- Server errors: pass fieldErrors(e); prefixed keys supported via errorPrefix="settings."; unknown
  keys are listed below the form.
- CronField and CredentialField can also be used standalone (e.g. the plugin schedule field:
  <CronField id="sched" label="Zeitplan" bind:value={schedule} />).


6. QueryInput  (src/lib/components/QueryInput.svelte)
-----------------------------------------------------
<QueryInput bind:value={q} onsubmit={(q) => apply(q)} error={queryError} />
Device filter language (meta.queryFields): autocomplete for fields and values (tags, groups, types,
subnets, states, custom fields cf.<key> …), help popover with clickable examples, `error` shows the
API message (catch the 400 of the request that used the query: errorMessage(e)). Use it for
scopes/conditions (plugin scope.query, rules conditions.deviceQuery, report filters).


7. Utilities  (src/lib/utils)
-----------------------------
format.ts   formatDateTime (dd.MM.yyyy HH:mm), formatDate, formatTime, formatRelative ("vor 5 Minuten"),
            formatDuration(ms), formatSeconds, formatNumber (de-DE), formatPercent, formatBytes, formatMs,
            plural, toDateTimeLocal / fromDateTimeLocal (for <input type="datetime-local">), toDate
labels.ts   German labels + tones: severityLabel/severityTone/severityFromCvss, stateLabel/stateTone,
            criticalityLabel, pluginKindLabel, runStatusLabel/runStatusTone, runTriggerLabel,
            healthStateLabel/healthTone, deviceTypeName, relationKindLabel, diffKindLabel,
            eventCategoryLabel, customFieldTypeLabel, label(map, value)
url.ts      setParams({ key: value|null }) (replaceState, keeps focus), intParam, listParam,
            debounce, loadPref/savePref (localStorage "netscope.*")
chart.ts    niceTicks, timeTicks, timeTickLabel, splitGaps
markdown.ts renderMarkdown(source) → sanitized HTML


8. Conventions
--------------
- Page skeleton: <PageHeader/>, then content; loading → Skeleton, error → <ErrorState onretry/>,
  empty → <EmptyState/>. Filters synced to the URL via page.url.searchParams + setParams.
- Colours only via tokens (bg-surface, bg-surface-2/3, border-border, text-fg / text-fg-muted /
  text-fg-subtle, text-accent, bg-accent-soft, text-ok/warn/danger(+ -soft), sev-*, online/offline,
  unknown, chart-1..4). Both themes work automatically; never hard-code hex colours.
- IPs/MACs/ports/IDs/CPEs: class "mono". Numbers in tables: "tabular".
- Links to devices: /devices/<id>; events: /events?…; diff: /diff?runA=&runB=&device=.
- Icon-only buttons need `label`. Every input needs a label (use label/hideLabel/aria-label).
- Live refresh: subscribe with $effect(() => live.on(topic, handler)) and debounce refetches.
- German UI texts, "Du" is not used; short imperative labels ("Speichern", "Jetzt ausführen").
```
