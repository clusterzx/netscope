#!/usr/bin/env bash
# End-to-end smoke test against a running NetScope instance (curl + jq).
# Exits != 0 on the first failure and prints the container log.
#
#   BASE_URL      default http://127.0.0.1:8080
#   CONTAINER     container name for token creation/logs (default netscope)
#   NETSCOPE_TOKEN  use this API token instead of creating one via docker exec
#   SMOKE_SCANS   1 (default) = run real scans against the LAN, 0 = API checks only
#   SMOKE_TARGETS hosts that the ARP scan must find (default "192.168.8.1 192.168.8.123")
set -Eeuo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
CONTAINER="${CONTAINER:-netscope}"
SMOKE_SCANS="${SMOKE_SCANS:-1}"
SMOKE_TARGETS="${SMOKE_TARGETS:-192.168.8.1 192.168.8.123}"
TOKEN="${NETSCOPE_TOKEN:-}"
PASSED=0

red() { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }

on_error() {
	local line=$1
	red "SMOKE FEHLGESCHLAGEN (Zeile $line)"
	if command -v docker >/dev/null 2>&1 && docker inspect "$CONTAINER" >/dev/null 2>&1; then
		echo "---- docker logs --tail 150 $CONTAINER ----"
		docker logs --tail 150 "$CONTAINER" 2>&1 || true
	fi
	exit 1
}
trap 'on_error $LINENO' ERR

for bin in curl jq; do
	command -v "$bin" >/dev/null || { red "$bin fehlt"; exit 1; }
done

ok() { PASSED=$((PASSED + 1)); green "✓ $*"; }
fail() { red "✗ $*"; false; }

api() { # api METHOD PATH [JSON]
	local method=$1 path=$2 body=${3:-}
	if [[ -n "$body" ]]; then
		curl -fsS -X "$method" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$body" "$BASE_URL$path"
	else
		curl -fsS -X "$method" -H "Authorization: Bearer $TOKEN" "$BASE_URL$path"
	fi
}

wait_run() { # wait_run RUN_ID TIMEOUT_SECONDS -> prints final status
	local id=$1 timeout=$2 status
	for ((i = 0; i < timeout; i += 2)); do
		status=$(api GET "/api/v1/runs/$id" | jq -r .status)
		case "$status" in
		queued | running) sleep 2 ;;
		*) echo "$status"; return 0 ;;
		esac
	done
	echo "timeout"
}

run_plugin() { # run_plugin ID TIMEOUT [SCOPE_JSON] -> run id
	local scope=${3:-}
	local body='{}'
	[[ -n "$scope" ]] && body="{\"scope\":$scope}"
	api POST "/api/v1/plugins/$1/run" "$body" | jq -r .id
}

# ---------------------------------------------------------------- health
for i in $(seq 1 60); do
	if curl -fsS "$BASE_URL/api/v1/health" >/dev/null 2>&1; then break; fi
	sleep 2
done
health=$(curl -fsS "$BASE_URL/api/v1/health")
[[ $(jq -r .status <<<"$health") == ok ]] || fail "health: $health"
ok "Health-Endpoint: $(jq -r .version <<<"$health")"

code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/api/v1/devices")
[[ $code == 401 ]] || fail "API ohne Anmeldung liefert $code statt 401"
ok "API verlangt Anmeldung"

# ---------------------------------------------------------------- token
if [[ -z "$TOKEN" ]]; then
	TOKEN=$(docker exec "$CONTAINER" netscope token create --name "smoke-$(date +%s)" --scope write --ttl 2h | tail -1)
fi
[[ $TOKEN == ns_* ]] || fail "kein API-Token"
me=$(api GET /api/v1/auth/me)
[[ $(jq -r .principal.scope <<<"$me") == write ]] || fail "Token-Scope: $me"
ok "API-Token (write) funktioniert"

# ---------------------------------------------------------------- basics
spec=$(curl -fsS "$BASE_URL/api/openapi.json")
paths=$(jq '.paths | length' <<<"$spec")
((paths > 50)) || fail "OpenAPI hat nur $paths Pfade"
ok "OpenAPI-Spezifikation: $paths Pfade"
curl -fsS "$BASE_URL/api/docs" | grep -q "NetScope API" || fail "/api/docs"
ok "API-Doku unter /api/docs"
html=$(curl -fsS "$BASE_URL/")
grep -q '/_app/immutable/' <<<"$html" || fail "Web-UI: gebautes SvelteKit-Bundle fehlt"
asset=$(grep -o '/_app/immutable/[^"]*\.js' <<<"$html" | head -1)
ctype=$(curl -fsS -o /dev/null -w '%{content_type}' "$BASE_URL$asset")
[[ $ctype == *javascript* ]] || fail "Web-UI-Asset $asset: $ctype"
curl -fsSI "$BASE_URL$asset" | grep -qi '^cache-control: .*immutable' || fail "Asset ohne Cache-Header"
curl -fsS "$BASE_URL/devices/1?tab=ports" | grep -q '/_app/immutable/' || fail "SPA-Fallback für Unterseiten"
ok "Web-UI: Bundle, Assets ($ctype) und SPA-Routing"
api GET /metrics | grep -q '^netscope_build_info' || fail "/metrics"
ok "Prometheus-Metriken"

plugins=$(api GET /api/v1/plugins)
expected="arpscan icmp oui dns nmap nmap_udp http tls snmp ssh mdns netbios upnp wol proxmox openwrt docker netalertx csv diff cve healthcheck topology cleanup report telegram webhook ntfy email n8n"
for id in $expected; do
	jq -e --arg id "$id" 'map(.info.id) | index($id) != null' <<<"$plugins" >/dev/null || fail "Plugin $id fehlt"
done
missing=$(jq -r '[.[] | select(.missingBinaries != null) | .info.id] | join(",")' <<<"$plugins")
[[ -z "$missing" ]] || fail "fehlende Binaries bei: $missing"
ok "$(jq length <<<"$plugins") Plugins registriert, alle Binaries vorhanden"

cron=$(api GET "/api/v1/cron/describe?expr=*/5%20*%20*%20*%20*")
[[ $(jq -r .text <<<"$cron") == "Alle 5 Minuten" ]] || fail "Cron-Beschreibung: $cron"
ok "Cron-Klartext: $(jq -r .text <<<"$cron")"

# ---------------------------------------------------------------- vault roundtrip
cred=$(api POST /api/v1/credentials '{"name":"smoke-vault","type":"password","values":{"username":"u","password":"geheim-123"}}')
cid=$(jq -r .id <<<"$cred")
grep -q 'geheim-123' <<<"$cred" && fail "Secret in API-Antwort"
[[ $(jq -r '.secretsSet[0]' <<<"$cred") == password ]] || fail "Secret nicht gespeichert: $cred"
api GET "/api/v1/credentials/$cid" | grep -q 'geheim-123' && fail "Secret im GET"
api DELETE "/api/v1/credentials/$cid" >/dev/null
ok "Vault: Credential verschlüsselt gespeichert, Secret nie ausgeliefert"

subnets=$(api GET /api/v1/subnets)
jq -e 'length > 0' <<<"$subnets" >/dev/null || fail "keine Subnetze"
ok "Subnetze: $(jq -r 'map(.cidr) | join(", ")' <<<"$subnets")"

# ---------------------------------------------------------------- rules + events
rules=$(api GET /api/v1/rules)
[[ $(jq length <<<"$rules") -ge 4 ]] || fail "Standardregeln fehlen"
rid=$(jq -r '.[] | select(.builtin == "critical-cve") | .id' <<<"$rules")
sim=$(api POST "/api/v1/rules/$rid/test" '{"type":"cve.new","payload":{"cvss":9.8,"cve":"CVE-2024-6387"}}')
[[ $(jq -r .matched <<<"$sim") == true ]] || fail "Regelsimulation: $sim"
sim=$(api POST "/api/v1/rules/$rid/test" '{"type":"cve.new","payload":{"cvss":5.0}}')
[[ $(jq -r .matched <<<"$sim") == false ]] || fail "Regelsimulation (negativ): $sim"
ok "Regel-Engine: Simulation positiv/negativ"
api GET "/api/v1/events?limit=5" | jq -e .total >/dev/null
api GET /api/v1/events/types | jq -e 'length > 20' >/dev/null || fail "Event-Katalog"
ok "Events und Event-Katalog"

sse=$(curl -sN -m 3 -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/v1/stream" || true)
grep -q 'connected' <<<"$sse" || fail "SSE-Stream"
ok "SSE-Stream"

if [[ "$SMOKE_SCANS" != 1 ]]; then
	green "Smoke ohne Scans bestanden ($PASSED Prüfungen)"
	exit 0
fi

# ---------------------------------------------------------------- real scans
id=$(run_plugin arpscan 300)
status=$(wait_run "$id" 300)
[[ $status == success ]] || fail "arpscan-Lauf $id: $status"
for ip in $SMOKE_TARGETS; do
	n=$(api GET "/api/v1/devices?q=ip:$ip" | jq -r .total)
	[[ $n -ge 1 ]] || fail "arpscan hat $ip nicht gefunden"
done
total=$(api GET "/api/v1/devices" | jq -r .total)
ok "ARP-Scan: $total Geräte, darunter $SMOKE_TARGETS"

for p in icmp oui dns; do
	id=$(run_plugin $p 300)
	status=$(wait_run "$id" 300)
	[[ $status == success ]] || fail "$p-Lauf $id: $status"
	ok "Scanner $p: Lauf erfolgreich"
done
rtt=$(api GET "/api/v1/devices?q=ip:${SMOKE_TARGETS%% *}" | jq -r '.items[0].id')
series=$(api GET "/api/v1/devices/$rtt/timeseries?metric=icmp.rtt_ms")
[[ $(jq '.points | length' <<<"$series") -ge 1 ]] || fail "keine Ping-Zeitreihe"
ok "Ping-Zeitreihe vorhanden"

ids=$(for ip in $SMOKE_TARGETS; do api GET "/api/v1/devices?q=ip:$ip" | jq -r '.items[0].id'; done | paste -sd, -)
scope="{\"devices\":[${ids}]}"
id=$(run_plugin nmap 1800 "$scope")
status=$(wait_run "$id" 1800)
[[ $status == success ]] || fail "nmap-Lauf $id: $status"
ports=$(for dev in ${ids//,/ }; do api GET "/api/v1/devices/$dev/ports" | jq length; done | awk '{s += $1} END {print s + 0}')
[[ $ports -ge 1 ]] || fail "nmap hat keine Ports gefunden"
ok "nmap: $ports offene Ports auf $SMOKE_TARGETS"

for p in http tls; do
	id=$(run_plugin $p 600 "$scope")
	status=$(wait_run "$id" 600)
	[[ $status == success ]] || fail "$p-Lauf $id: $status"
	ok "Scanner $p: Lauf erfolgreich"
done

diff=$(api GET "/api/v1/diff?from=$(date -u -d '-1 hour' +%Y-%m-%dT%H:%M:%SZ)")
jq -e '.items' <<<"$diff" >/dev/null || fail "Diff"
ok "Diff (letzte Stunde): $(jq '.items | length' <<<"$diff") Änderungen"

topo=$(api GET /api/v1/topology)
ok "Topologie: $(jq '.nodes | length' <<<"$topo") Knoten, $(jq '.edges | length' <<<"$topo") Kanten in $(jq .tookMs <<<"$topo") ms"

list=$(api GET "/api/v1/devices?ports=1")
ok "Geräteliste in $(jq .tookMs <<<"$list") ms"

vs=$(api GET /api/v1/vulnerabilities/status)
jq -e '.cveCount >= 0 and (.disclaimer | length > 0)' <<<"$vs" >/dev/null || fail "Schwachstellen-Status: $vs"
if [[ $(jq .cveCount <<<"$vs") -gt 0 ]]; then
	ok "NVD-Spiegel: $(jq .cveCount <<<"$vs") CVEs, $(jq .summary.total <<<"$vs") Treffer auf $(jq .summary.devices <<<"$vs") Geräten"
else
	ok "NVD-Spiegel noch leer (Sync läuft täglich bzw. über „NVD jetzt synchronisieren“)"
fi

green "Smoke bestanden ($PASSED Prüfungen)"
