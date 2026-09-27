#!/usr/bin/env bash
# Regenerates THIRD_PARTY_NOTICES.md from the code that actually ships:
#   - Go modules compiled into the server (linux) and the agent (linux, windows),
#   - npm packages whose code ends up in the built web UI,
#   - programs and data in the container image (fixed text below).
# Needs go and node, and web/node_modules (npm ci). Run via `make notices`.
set -euo pipefail
cd "$(dirname "$0")/.."

OUT=THIRD_PARTY_NOTICES.md
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# npm packages whose code is part of the web bundle: the runtime dependencies plus the parts
# of svelte, SvelteKit and Tailwind that are compiled into it. Keep in sync with web/src.
NPM_BUNDLED=(svelte @sveltejs/kit clsx devalue esm-env d3-force d3-dispatch d3-quadtree d3-timer dompurify marked tailwindcss)

classify() {
	local f=$1
	if grep -q "Mozilla Public License" "$f" && grep -q "Apache License" "$f"; then echo "MPL-2.0 oder Apache-2.0"
	elif grep -q "Apache License" "$f" && grep -qi "Permission is hereby granted, free of charge" "$f"; then echo "MIT und Apache-2.0"
	elif grep -q "Apache License" "$f"; then echo "Apache-2.0"
	elif grep -q "Mozilla Public License" "$f"; then echo "MPL-2.0"
	elif grep -qi "Permission is hereby granted, free of charge" "$f"; then echo "MIT"
	elif grep -qi "Permission to use, copy, modify, and/or distribute" "$f"; then echo "ISC"
	elif grep -q "Redistribution and use in source and binary forms" "$f"; then
		if grep -qiE "Neither the name|names of (its|the) contributors" "$f"; then echo "BSD-3-Clause"; else echo "BSD-2-Clause"; fi
	else echo "siehe Lizenztext"; fi
}

license_files() { # dir
	find "$1" -maxdepth 1 -type f \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'notice*' -o -iname 'patents*' \) | sort
}

# ---------------------------------------------------------------- Go
go_mods() { # GOOS package
	GOOS=$1 CGO_ENABLED=0 go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}' "$2"
}
{ go_mods linux ./cmd/netscope | sed 's/$/ server/'
  go_mods linux ./cmd/netscope-agent | sed 's/$/ agent/'
  go_mods windows ./cmd/netscope-agent | sed 's/$/ agent/'
} | sort -u >"$TMP/go.raw"
# path version -> "Server, Agent"
awk '{k=$1" "$2; if (!(k in u)) {u[k]=""; o[++n]=k} if (index(u[k],$3)==0) u[k]=u[k] (u[k]==""?"":",") $3}
     END {for (i=1;i<=n;i++) {s=u[o[i]]; gsub("server","Server",s); gsub("agent","Agent",s); gsub(",",", ",s); print o[i]" "s}}' \
	"$TMP/go.raw" | sort >"$TMP/go.mods"

# ---------------------------------------------------------------- npm
: >"$TMP/npm.mods"
for p in "${NPM_BUNDLED[@]}"; do
	d="web/node_modules/$p"
	[ -f "$d/package.json" ] || { echo "notices: $d fehlt – erst 'npm ci' in web/" >&2; exit 1; }
	node -e 'const p=require(process.argv[1]); console.log(p.name, p.version, (typeof p.license==="string"?p.license:"").replace(/^\((.*)\)$/,"$1").replace(/ OR /g," oder "))' "$PWD/$d/package.json" >>"$TMP/npm.mods"
done

# ---------------------------------------------------------------- write
{
cat <<'EOF'
# Software und Daten Dritter

NetScope ist proprietäre Software (siehe [LICENSE](LICENSE)). Es enthält bzw. nutzt die
folgenden Komponenten Dritter, die unter ihren eigenen Lizenzen stehen. Diese Datei wird mit
`make notices` aus den tatsächlich eingebauten Abhängigkeiten erzeugt – nicht von Hand ändern.

## Programme im Container-Image

NetScope ruft diese Programme als eigenständige Prozesse auf; sie werden unverändert aus den
Paketquellen von Alpine Linux installiert.

| Programm | Lizenz | Hinweis |
|---|---|---|
| nmap, nmap-scripts | Nmap Public Source License 0.95 | Die NPSL wertet Software, die nmap gezielt ausführt und die Ergebnisse auswertet, als abgeleitetes Werk. Der Vertrieb von NetScope zusammen mit nmap setzt eine **Nmap-OEM-Lizenz** voraus (https://nmap.org/oem/). Ohne sie ist das Plugin `nmap` zu entfernen bzw. durch den eigenen Scanner zu ersetzen. Quelle: https://nmap.org |
| arp-scan | GPL-3.0 | Unverändert, separat aufgerufen. Quellcode: https://github.com/royhills/arp-scan |
| Alpine Linux (musl, BusyBox, ca-certificates, tzdata u. a.) | je Paket (MIT, GPL-2.0, MPL-2.0, Public Domain …) | Quellen und Lizenzen: https://gitlab.alpinelinux.org/alpine/aports |

## Daten

| Quelle | Hinweis |
|---|---|
| NIST National Vulnerability Database (NVD) | This product uses data from the NVD API but is not endorsed or certified by the NVD. |
| IEEE Registration Authority (OUI-Listen) | Öffentliche Herstellerkennungen der MAC-Adressen, von https://standards-oui.ieee.org |
| SQLite | Public Domain; als Go-Übersetzung über modernc.org/sqlite eingebaut. |

## Bibliotheken

| Komponente | Version | Lizenz | Enthalten in |
|---|---|---|---|
EOF
printf '| Go-Standardbibliothek | %s | BSD-3-Clause | Server, Agent |\n' "$(go env GOVERSION)"
while read -r path ver where; do
	dir=$(go list -m -f '{{.Dir}}' "$path@$ver" 2>/dev/null || go list -m -f '{{.Dir}}' "$path")
	f=$(license_files "$dir" | head -1)
	lic=$([ -n "$f" ] && classify "$f" || echo "unbekannt")
	printf '| %s | %s | %s | %s |\n' "$path" "$ver" "$lic" "$where"
done <"$TMP/go.mods"
while read -r name ver lic; do
	printf '| %s | %s | %s | Oberfläche |\n' "$name" "$ver" "$lic"
done <"$TMP/npm.mods"

printf '\n## Lizenztexte\n'
printf '\n### Go-Standardbibliothek\n\n```text\n%s\n```\n' "$(cat "$(go env GOROOT)/LICENSE")"
while read -r path ver _; do
	dir=$(go list -m -f '{{.Dir}}' "$path@$ver" 2>/dev/null || go list -m -f '{{.Dir}}' "$path")
	printf '\n### %s %s\n' "$path" "$ver"
	files=$(license_files "$dir")
	[ -n "$files" ] || { printf '\nKein Lizenztext im Modul gefunden.\n'; continue; }
	while read -r f; do
		printf '\n```text\n%s\n```\n' "$(tr -d '\r' <"$f")"
	done <<<"$files"
done <"$TMP/go.mods"
while read -r name ver _; do
	d="web/node_modules/$name"
	printf '\n### %s %s\n' "$name" "$ver"
	files=$(license_files "$d")
	[ -n "$files" ] || { printf '\nKein Lizenztext im Paket gefunden.\n'; continue; }
	while read -r f; do
		printf '\n```text\n%s\n```\n' "$(tr -d '\r' <"$f")"
	done <<<"$files"
done <"$TMP/npm.mods"
} >"$OUT"

echo "notices: $(grep -c '^| ' "$OUT") Tabellenzeilen, $(wc -c <"$OUT") Bytes → $OUT"
