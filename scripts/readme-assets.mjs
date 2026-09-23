#!/usr/bin/env node
// Generates the README graphics (docs/assets/<name>-{dark,light}.svg) in the NetScope
// design: brand colours of the web UI and its icon set (web/src/lib/components/ui/icons.ts).
// No dependencies:  node scripts/readme-assets.mjs
//
// The SVGs are shown through <img>/<picture> on GitHub, which blocks web fonts: text uses
// system font stacks and is centred or given generous room, since widths differ per OS.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const outDir = path.join(root, 'docs', 'assets');

// ---------------------------------------------------------------- building blocks

function loadIcons() {
	let src = fs.readFileSync(path.join(root, 'web/src/lib/components/ui/icons.ts'), 'utf8');
	src = src
		.replace(/^export type .*$/m, '')
		.replace(/:\s*number\b/g, '') // parameter types of the helper
		.replace('export const icons =', 'return')
		.replace(/\}\s*satisfies[^;]*;/, '};');
	return new Function(src)();
}
const icons = loadIcons();

const themes = {
	dark: {
		bg0: '#0b1017', bg1: '#0f1826', card: '#121a25', card2: '#172131', border: '#253245', borderStrong: '#34435c',
		fg: '#e8ecf2', muted: '#a3adbd', subtle: '#6f7a8c', accent: '#4cb4ec', accentSoft: 'rgba(76,180,236,0.14)',
		accentLine: 'rgba(76,180,236,0.38)', amber: '#f5a524', amberSoft: 'rgba(245,165,36,0.15)', ok: '#3ec27a',
		okSoft: 'rgba(62,194,122,0.15)', violet: '#a78bfa', violetSoft: 'rgba(167,139,250,0.16)', danger: '#fb5f7e',
		dangerSoft: 'rgba(251,95,126,0.16)', offline: '#5d6677', grid: 'rgba(148,163,184,0.09)', glow: 0.22
	},
	light: {
		bg0: '#fbfcfe', bg1: '#eaf2fa', card: '#ffffff', card2: '#f4f7fb', border: '#dbe2ec', borderStrong: '#c3cddb',
		fg: '#141922', muted: '#4d5768', subtle: '#7b8596', accent: '#0673b0', accentSoft: 'rgba(6,115,176,0.08)',
		accentLine: 'rgba(6,115,176,0.30)', amber: '#c26a04', amberSoft: 'rgba(217,119,6,0.10)', ok: '#15803d',
		okSoft: 'rgba(22,163,74,0.10)', violet: '#7c3aed', violetSoft: 'rgba(124,58,237,0.09)', danger: '#be123c',
		dangerSoft: 'rgba(190,18,60,0.08)', offline: '#9aa3b2', grid: 'rgba(15,23,42,0.06)', glow: 0.14
	}
};

const SANS = `'Segoe UI', -apple-system, BlinkMacSystemFont, 'Helvetica Neue', Helvetica, Arial, sans-serif`;
const MONO = `'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace`;

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

// rough text width (system sans fonts vary; callers leave room)
function tw(text, size, { bold = false, mono = false } = {}) {
	if (mono) return text.length * size * 0.61;
	let w = 0;
	for (const ch of text) {
		if (" iljtfrI.,:;|!'’·".includes(ch)) w += 0.3;
		else if ('mwMW@–'.includes(ch)) w += 0.84;
		else if ((ch >= 'A' && ch <= 'Z') || 'ÄÖÜ'.includes(ch)) w += 0.64;
		else if (ch >= '0' && ch <= '9') w += 0.56;
		else w += 0.53;
	}
	return w * size * (bold ? 1.07 : 1);
}

function text(x, y, s, { size = 16, fill, weight = 400, anchor = 'start', mono = false, ls = 0, opacity } = {}) {
	return `<text x="${x}" y="${y}" font-family="${mono ? MONO : SANS}" font-size="${size}" font-weight="${weight}"${
		anchor !== 'start' ? ` text-anchor="${anchor}"` : ''
	}${ls ? ` letter-spacing="${ls}"` : ''}${opacity !== undefined ? ` opacity="${opacity}"` : ''} fill="${fill}">${esc(s)}</text>`;
}

function icon(name, x, y, size, color, { width = 2 } = {}) {
	const paths = icons[name];
	if (!paths) throw new Error(`unknown icon ${name}`);
	const k = size / 24;
	return `<g transform="translate(${x} ${y}) scale(${k})" fill="none" stroke="${color}" stroke-width="${width}" stroke-linecap="round" stroke-linejoin="round">${paths
		.map((d) => `<path d="${d}"/>`)
		.join('')}</g>`;
}

// brand logo (same as the favicon), size = edge length
function logo(x, y, size) {
	const k = size / 64;
	return `<g transform="translate(${x} ${y}) scale(${k})"><rect width="64" height="64" rx="14" fill="#0f172a"/><rect x="0.5" y="0.5" width="63" height="63" rx="13.5" fill="none" stroke="#38bdf8" stroke-opacity="0.25"/><circle cx="32" cy="32" r="18" fill="none" stroke="#38bdf8" stroke-width="4"/><circle cx="32" cy="32" r="8" fill="none" stroke="#38bdf8" stroke-width="3" opacity=".7"/><path d="M32 32 L46 18" stroke="#f59e0b" stroke-width="4" stroke-linecap="round"/><circle cx="46" cy="18" r="4" fill="#f59e0b"/></g>`;
}

// pill centred on its text; returns [svg, width]
function pill(x, y, label, t, { size = 14, h = 30, fill, stroke, color, mono = false, iconName, bold = false } = {}) {
	const iw = iconName ? size + 6 : 0;
	const w = Math.round(tw(label, size, { mono, bold }) + 26 + iw);
	const parts = [
		`<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${h / 2}" fill="${fill ?? t.card2}" stroke="${stroke ?? t.border}"/>`
	];
	if (iconName) parts.push(icon(iconName, x + 12, y + (h - size) / 2, size, color ?? t.muted));
	parts.push(
		text(x + iw / 2 + w / 2, y + h / 2 + size * 0.35, label, {
			size,
			fill: color ?? t.muted,
			anchor: 'middle',
			mono,
			weight: bold ? 600 : 500
		})
	);
	return [parts.join(''), w];
}

// lays out pills left to right, wrapping at maxX; returns [svg, bottom]
function pillFlow(x0, y0, maxX, labels, t, opts = {}) {
	const gap = opts.gap ?? 8;
	const h = opts.h ?? 30;
	let x = x0;
	let y = y0;
	const out = [];
	for (const l of labels) {
		const w = Math.round(tw(l, opts.size ?? 14, opts) + 26 + (opts.iconName ? (opts.size ?? 14) + 6 : 0));
		if (x > x0 && x + w > maxX) {
			x = x0;
			y += h + gap;
		}
		const [svg] = pill(x, y, l, t, { ...opts, h });
		out.push(svg);
		x += w + gap;
	}
	return [out.join(''), y + h];
}

function svgDoc(w, h, title, desc, body, css = '') {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" role="img" aria-labelledby="t d">
<title id="t">${esc(title)}</title>
<desc id="d">${esc(desc)}</desc>
<style>
${css}
@media (prefers-reduced-motion: reduce) { * { animation: none !important; } }
</style>
${body}
</svg>
`;
}

const polar = (cx, cy, r, deg) => {
	const a = (deg * Math.PI) / 180;
	return [cx + r * Math.cos(a), cy + r * Math.sin(a)].map((v) => Math.round(v * 10) / 10);
};

// ---------------------------------------------------------------- banner

function banner(t, mode) {
	const W = 1280;
	const H = 400;
	const cx = 1018;
	const cy = 200;
	const b = [];
	b.push(`<defs>
<linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${t.bg0}"/><stop offset="1" stop-color="${t.bg1}"/></linearGradient>
<radialGradient id="glow" cx="${cx}" cy="${cy}" r="330" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="${t.accent}" stop-opacity="${t.glow}"/><stop offset="1" stop-color="${t.accent}" stop-opacity="0"/></radialGradient>
<pattern id="dots" width="22" height="22" patternUnits="userSpaceOnUse"><circle cx="2" cy="2" r="1.1" fill="${t.grid}"/></pattern>
<linearGradient id="sweep" gradientUnits="userSpaceOnUse" x1="${polar(cx, cy, 120, -58).join('" y1="')}" x2="${polar(cx, cy, 120, 0).join('" y2="')}"><stop offset="0" stop-color="${t.accent}" stop-opacity="0"/><stop offset="1" stop-color="${t.accent}" stop-opacity="${mode === 'dark' ? 0.32 : 0.22}"/></linearGradient>
<clipPath id="frame"><rect width="${W}" height="${H}" rx="28"/></clipPath>
</defs>`);
	b.push(`<g clip-path="url(#frame)">`);
	b.push(`<rect width="${W}" height="${H}" fill="url(#bg)"/><rect width="${W}" height="${H}" fill="url(#dots)"/><rect width="${W}" height="${H}" fill="url(#glow)"/>`);

	// radar rings and sweep
	for (const [r, dash, op] of [
		[62, '', 0.55],
		[112, '', 0.4],
		[164, '3 7', 0.45]
	]) {
		b.push(`<circle cx="${cx}" cy="${cy}" r="${r}" fill="none" stroke="${t.accentLine}" stroke-width="1.5"${dash ? ` stroke-dasharray="${dash}"` : ''} opacity="${op}"/>`);
	}
	const [sx1, sy1] = polar(cx, cy, 170, -58);
	const [sx2, sy2] = polar(cx, cy, 170, 0);
	b.push(`<g class="sweep"><path d="M${cx} ${cy} L${sx1} ${sy1} A170 170 0 0 1 ${sx2} ${sy2} Z" fill="url(#sweep)"/><path d="M${cx} ${cy} L${sx2} ${sy2}" stroke="${t.accent}" stroke-width="2" stroke-linecap="round" opacity="0.8"/></g>`);

	// devices around the router
	const nodes = [
		{ a: -152, r: 112, s: 'ok', ic: 'server', label: 'nas' },
		{ a: -104, r: 150, s: 'ok', ic: 'camera', label: 'cam-hof' },
		{ a: -40, r: 116, s: 'danger', ic: 'server', label: 'pve', tag: ['CVSS 9,8', 'danger'] },
		{ a: 14, r: 150, s: 'ok', ic: 'tv', label: 'tv' },
		{ a: 62, r: 118, s: 'violet', ic: 'phone', label: '', tag: ['Neues Gerät', 'violet'], pulse: true },
		{ a: 112, r: 150, s: 'offline', ic: 'laptop', label: 'laptop' },
		{ a: 160, r: 116, s: 'ok', ic: 'zap', label: 'iot-plug' }
	];
	const color = { ok: t.ok, danger: t.danger, violet: t.violet, offline: t.offline };
	for (const n of nodes) {
		const [x, y] = polar(cx, cy, n.r, n.a);
		b.push(`<line x1="${cx}" y1="${cy}" x2="${x}" y2="${y}" stroke="${n.s === 'offline' ? t.offline : t.accentLine}" stroke-width="1.5"${n.s === 'offline' ? ' stroke-dasharray="4 5"' : ''}/>`);
	}
	// containers running on "pve"
	const [px, py] = polar(cx, cy, 116, -40);
	for (const [dx, dy] of [
		[-44, -30],
		[-8, -46]
	]) {
		b.push(`<line x1="${px}" y1="${py}" x2="${px + dx}" y2="${py + dy}" stroke="${t.amber}" stroke-width="1.3" stroke-dasharray="3 3" opacity="0.8"/>`);
		b.push(`<rect x="${px + dx - 7}" y="${py + dy - 7}" width="14" height="14" rx="3.5" fill="${t.card}" stroke="${t.amber}" stroke-width="1.6"/>`);
	}
	for (const n of nodes) {
		const [x, y] = polar(cx, cy, n.r, n.a);
		const c = color[n.s];
		if (n.pulse) b.push(`<circle class="ping" cx="${x}" cy="${y}" r="18" fill="none" stroke="${c}" stroke-width="2"/>`);
		b.push(`<circle cx="${x}" cy="${y}" r="18" fill="${t.card}" stroke="${c}" stroke-width="2"/>`);
		b.push(icon(n.ic, x - 9, y - 9, 18, n.s === 'offline' ? t.subtle : t.fg, { width: 1.8 }));
		if (n.label) {
			const right = Math.cos((n.a * Math.PI) / 180) >= -0.2;
			const lx = right ? x + 26 : x - 26;
			b.push(text(lx, y + 4.5, n.label, { size: 13, fill: t.muted, anchor: right ? 'start' : 'end', mono: true }));
		}
		if (n.tag) {
			const [label, kind] = n.tag;
			const w = Math.round(tw(label, 12, { bold: true }) + 20);
			const tx = n.a > 0 ? x + 24 : x + 24;
			const ty = n.a > 0 ? y + 8 : y - 42;
			b.push(`<rect x="${tx}" y="${ty}" width="${w}" height="22" rx="11" fill="${kind === 'danger' ? t.dangerSoft : t.violetSoft}" stroke="${color[kind]}" stroke-opacity="0.6"/>`);
			b.push(text(tx + w / 2, ty + 15, label, { size: 12, fill: color[kind], anchor: 'middle', weight: 600 }));
		}
	}
	// router in the middle
	b.push(`<circle cx="${cx}" cy="${cy}" r="27" fill="${t.card2}" stroke="${t.accent}" stroke-width="2.2"/>`);
	b.push(icon('router', cx - 12, cy - 12, 24, t.accent, { width: 2 }));

	// left: logo, name, claims
	b.push(logo(72, 70, 88));
	b.push(text(182, 136, 'NetScope', { size: 62, fill: t.fg, weight: 700, ls: -1.2 }));
	b.push(text(184, 164, 'self-hosted · go · sveltekit · sqlite', { size: 15, fill: t.subtle, ls: 1.6, mono: true }));
	b.push(text(72, 222, 'Netzwerk-Inventar & Asset-Monitoring fürs Homelab', { size: 26, fill: t.fg, weight: 600, ls: -0.3 }));
	b.push(text(72, 258, 'Was ist das, wo hängt es, was läuft darauf – und ist es verwundbar?', { size: 18, fill: t.muted }));
	let x = 72;
	for (const [label, ic] of [
		['Ein Go-Binary', 'box'],
		['30 Plugins', 'plugins'],
		['Live-Updates', 'activity'],
		['CVE-Abgleich', 'shield'],
		['Dark Mode', 'moon']
	]) {
		const [svg, w] = pill(x, 300, label, t, { size: 14, h: 34, iconName: ic, color: t.fg, fill: t.card, stroke: t.borderStrong });
		b.push(svg);
		x += w + 10;
	}
	b.push(`</g>`);
	b.push(`<rect x="0.75" y="0.75" width="${W - 1.5}" height="${H - 1.5}" rx="27.5" fill="none" stroke="${t.border}" stroke-width="1.5"/>`);

	const css = `.sweep { transform-origin: ${cx}px ${cy}px; animation: spin 7s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.ping { transform-box: fill-box; transform-origin: center; animation: ping 2.6s ease-out infinite; }
@keyframes ping { 0% { transform: scale(1); opacity: .7; } 80%, 100% { transform: scale(2.1); opacity: 0; } }`;
	return svgDoc(
		W,
		H,
		'NetScope – Netzwerk-Inventar & Asset-Monitoring fürs Homelab',
		'Logo und Titel von NetScope neben einer Radar-Darstellung: ein Router in der Mitte, verbundene Geräte mit Online-, Offline- und Neu-Status sowie ein Gerät mit kritischer CVE.',
		b.join('\n'),
		css
	);
}

// ---------------------------------------------------------------- feature grid

const features = [
	['devices', 'Inventar', 'Geräte mit IPs, MACs, Hersteller, Typ, Tags,', 'Gruppen, Notizen und Custom Fields'],
	['radar', '14 Scanner', 'ARP, ICMP, nmap, HTTP, TLS, SNMP, SSH,', 'mDNS, UPnP, NetBIOS, DNS – pro Host live'],
	['diff', 'Änderungen & Events', 'Neue Geräte, Ports, Dienste, Zertifikate,', 'Container und Pakete – mit Diff je Lauf'],
	['shield', 'Schwachstellen', 'CVE-Abgleich gegen die lokal gespiegelte', 'NVD – mit CVSS, Ignorieren und Heuristik'],
	['health', 'Health-Checks', 'TCP, HTTP, TLS und ICMP mit Flap-Dämpfung,', 'Verfügbarkeit und Ausfallhistorie'],
	['topology', 'Topologie', 'Graph aus FDB, LLDP, ARP, Proxmox und', 'Docker – Zoom, Filter, eigene Kanten'],
	['bell', 'Regeln & Benachrichtigungen', 'Bündeln, Ruhezeiten, Drosselung, Eskalation', 'über Telegram, ntfy, E-Mail, Webhook, n8n'],
	['plugins', 'Plugins & API', 'Jede Funktion ein Plugin mit Settings-Schema;', 'JSON-API, OpenAPI, SSE, Prometheus']
];

function featureGrid(t) {
	const W = 960;
	const cw = 472;
	const ch = 128;
	const gap = 16;
	const rows = Math.ceil(features.length / 2);
	const H = rows * ch + (rows - 1) * gap;
	const b = [];
	features.forEach(([ic, title, l1, l2], i) => {
		const x = (i % 2) * (cw + gap);
		const y = Math.floor(i / 2) * (ch + gap);
		b.push(`<rect x="${x + 0.75}" y="${y + 0.75}" width="${cw - 1.5}" height="${ch - 1.5}" rx="16" fill="${t.card}" stroke="${t.border}" stroke-width="1.5"/>`);
		b.push(`<rect x="${x + 24}" y="${y + 24}" width="48" height="48" rx="13" fill="${t.accentSoft}"/>`);
		b.push(icon(ic, x + 34, y + 34, 28, t.accent, { width: 1.9 }));
		b.push(text(x + 92, y + 46, title, { size: 19, fill: t.fg, weight: 650 }));
		b.push(text(x + 92, y + 76, l1, { size: 15, fill: t.muted }));
		b.push(text(x + 92, y + 98, l2, { size: 15, fill: t.muted }));
	});
	return svgDoc(
		W,
		H,
		'Funktionen von NetScope',
		features.map((f) => `${f[1]}: ${f[2]} ${f[3]}`).join(' '),
		b.join('\n')
	);
}

// ---------------------------------------------------------------- architecture

function architecture(t) {
	const W = 1100;
	const top = 16;
	const colW = 244;
	const gap = 28;
	const colH = 404;
	const x0 = (W - (4 * colW + 3 * gap)) / 2;
	const xs = [0, 1, 2, 3].map((i) => x0 + i * (colW + gap));
	const b = [];
	const box = (x, title, sub, ic, accent) => {
		b.push(`<rect x="${x + 0.75}" y="${top + 0.75}" width="${colW - 1.5}" height="${colH - 1.5}" rx="18" fill="${t.card}" stroke="${t.border}" stroke-width="1.5"/>`);
		b.push(`<rect x="${x + 18}" y="${top + 18}" width="40" height="40" rx="11" fill="${accent}" fill-opacity="0.14"/>`);
		b.push(icon(ic, x + 27, top + 27, 22, accent, { width: 2 }));
		b.push(text(x + 70, top + 36, title, { size: 18, fill: t.fg, weight: 650 }));
		b.push(text(x + 70, top + 56, sub, { size: 13, fill: t.subtle }));
	};
	const section = (x, y, label) => text(x + 18, y, label.toUpperCase(), { size: 11.5, fill: t.subtle, weight: 600, ls: 1.2 });

	// 1 sources
	box(xs[0], 'Quellen', 'Scanner & Importer', 'radar', t.accent);
	b.push(section(xs[0], top + 94, 'Scanner'));
	let [svg, bottom] = pillFlow(xs[0] + 18, top + 104, xs[0] + colW - 14, ['ARP', 'ICMP', 'nmap', 'HTTP', 'TLS', 'SNMP', 'SSH', 'mDNS', 'UPnP', 'NetBIOS', 'DNS', 'OUI'], t, { size: 13, h: 26, gap: 6 });
	b.push(svg);
	b.push(section(xs[0], bottom + 32, 'Importer'));
	[svg] = pillFlow(xs[0] + 18, bottom + 42, xs[0] + colW - 14, ['Proxmox', 'OpenWrt', 'Docker', 'NetAlertX', 'CSV'], t, { size: 13, h: 26, gap: 6, color: t.violet, fill: t.violetSoft, stroke: 'none' });
	b.push(svg);

	// 2 core
	box(xs[1], 'Core', 'Inventar & Historie', 'layers', t.amber);
	const coreRows = [
		['merge', 'Identität', 'DeviceID › MAC › Ref › IP'],
		['tag', 'Quellen-Priorität', 'manuell › DHCP › DNS › mDNS'],
		['history', 'Temporale Historie', 'seit wann, bis wann, Diff'],
		['activity', 'Zeitreihen', 'roh → 5 min → 1 h'],
		['lock', 'Vault', 'AES-256-GCM'],
		['disk', 'SQLite (WAL)', 'eingebettete Migrationen']
	];
	coreRows.forEach(([ic, a, s], i) => {
		const y = top + 86 + i * 50;
		b.push(icon(ic, xs[1] + 20, y + 2, 18, t.amber, { width: 2 }));
		b.push(text(xs[1] + 48, y + 13, a, { size: 14.5, fill: t.fg, weight: 600 }));
		b.push(text(xs[1] + 48, y + 32, s, { size: 12.5, fill: t.subtle }));
	});

	// 3 processors
	box(xs[2], 'Processor', 'Auswertung', 'cpu', t.ok);
	[svg, bottom] = pillFlow(xs[2] + 18, top + 88, xs[2] + colW - 14, ['Diff', 'CVE / NVD', 'Health', 'Topologie', 'Cleanup', 'Report'], t, { size: 13.5, h: 28, gap: 7, color: t.ok, fill: t.okSoft, stroke: 'none' });
	b.push(svg);
	b.push(section(xs[2], bottom + 34, 'Ergebnis'));
	const results = [
		['events', '25 Event-Typen'],
		['shield', 'CVE-Treffer je Gerät'],
		['health', 'Verfügbarkeit'],
		['topology', 'Graph & Beziehungen']
	];
	results.forEach(([ic, l], i) => {
		const y = bottom + 48 + i * 32;
		b.push(icon(ic, xs[2] + 20, y, 17, t.muted, { width: 2 }));
		b.push(text(xs[2] + 46, y + 13, l, { size: 14, fill: t.fg }));
	});

	// 4 notifications
	box(xs[3], 'Benachrichtigung', 'Regeln & Publisher', 'bell', t.danger);
	b.push(section(xs[3], top + 94, 'Regel-Engine'));
	['Bedingungen & Zeitfenster', 'Bündeln über N Minuten', 'Ruhezeiten & Drosselung', 'Eskalation ohne Quittung', 'Simulation im Editor'].forEach((l, i) => {
		const y = top + 110 + i * 27;
		b.push(`<circle cx="${xs[3] + 23}" cy="${y + 5}" r="3" fill="${t.danger}"/>`);
		b.push(text(xs[3] + 34, y + 10, l, { size: 13.5, fill: t.fg }));
	});
	b.push(section(xs[3], top + 268, 'Publisher'));
	[svg] = pillFlow(xs[3] + 18, top + 278, xs[3] + colW - 14, ['Telegram', 'ntfy', 'E-Mail', 'Webhook', 'n8n'], t, { size: 13, h: 26, gap: 6, iconName: 'send', color: t.fg });
	b.push(svg);

	// arrows between the columns
	const arrows = ['Observations', 'Changes', 'Events'];
	arrows.forEach((label, i) => {
		const x1 = xs[i] + colW + 3;
		const x2 = xs[i + 1] - 4;
		const y = top + 200;
		b.push(`<line class="flow" x1="${x1}" y1="${y}" x2="${x2 - 6}" y2="${y}" stroke="${t.accent}" stroke-width="2" stroke-dasharray="4 4"/>`);
		b.push(`<path d="M${x2 - 8} ${y - 6} L${x2} ${y} L${x2 - 8} ${y + 6}" fill="none" stroke="${t.accent}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>`);
		b.push(`<g transform="translate(${(x1 + x2) / 2} ${y - 16}) rotate(-90)">${text(0, 4, label, { size: 11.5, fill: t.subtle, anchor: 'start', weight: 600, ls: 0.6 })}</g>`);
	});

	// base band
	const by = top + colH + 18;
	const bw = 4 * colW + 3 * gap;
	b.push(`<rect x="${x0 + 0.75}" y="${by + 0.75}" width="${bw - 1.5}" height="58.5" rx="16" fill="${t.card2}" stroke="${t.border}" stroke-width="1.5"/>`);
	const segs = [
		['box', 'Ein Go-Binary', 'Plugin-Host · Scheduler · Worker-Pool · Event-Bus'],
		['monitor', 'Web-UI & API', 'SvelteKit eingebettet · JSON · OpenAPI · SSE · /metrics']
	];
	segs.forEach(([ic, a, s], i) => {
		const x = x0 + 28 + i * (bw / 2);
		b.push(icon(ic, x, by + 18, 22, t.accent, { width: 2 }));
		b.push(text(x + 34, by + 26, a, { size: 14.5, fill: t.fg, weight: 650 }));
		b.push(text(x + 34, by + 45, s, { size: 13, fill: t.muted }));
	});
	b.push(`<line x1="${x0 + bw / 2}" y1="${by + 14}" x2="${x0 + bw / 2}" y2="${by + 46}" stroke="${t.border}" stroke-width="1.5"/>`);

	const H = by + 60 + 12;
	const css = `.flow { animation: flow 1.1s linear infinite; }
@keyframes flow { to { stroke-dashoffset: -16; } }`;
	return svgDoc(
		W,
		H,
		'Architektur von NetScope',
		'Datenfluss: Scanner und Importer liefern Observations an den Core (Inventar, Historie, Zeitreihen, Vault, SQLite). Processor wie Diff, CVE, Health und Topologie werten Changes aus und erzeugen Events. Die Regel-Engine bündelt, drosselt und eskaliert und stellt über Telegram, ntfy, E-Mail, Webhook und n8n zu. Alles läuft in einem Go-Binary mit eingebetteter Web-UI und API.',
		b.join('\n'),
		css
	);
}

// ---------------------------------------------------------------- plugin overview

const pluginLanes = [
	['Scanner', 'radar', 'accent', ['arpscan', 'icmp', 'oui', 'dns', 'mdns', 'netbios', 'upnp', 'nmap', 'nmap_udp', 'http', 'tls', 'snmp', 'ssh', 'wol']],
	['Importer', 'download', 'violet', ['proxmox', 'openwrt', 'docker', 'netalertx', 'csv']],
	['Processor', 'cpu', 'ok', ['diff', 'cve', 'healthcheck', 'topology', 'cleanup', 'report']],
	['Publisher', 'send', 'amber', ['telegram', 'webhook', 'ntfy', 'email', 'n8n']]
];

function plugins(t) {
	const W = 1100;
	const laneH = 58;
	const gap = 10;
	const b = [];
	pluginLanes.forEach(([name, ic, key, ids], i) => {
		const y = i * (laneH + gap);
		const c = t[key];
		b.push(`<rect x="0.75" y="${y + 0.75}" width="${W - 1.5}" height="${laneH - 1.5}" rx="14" fill="${t.card}" stroke="${t.border}" stroke-width="1.5"/>`);
		b.push(`<rect x="0.75" y="${y + 0.75}" width="5" height="${laneH - 1.5}" rx="2.5" fill="${c}"/>`);
		b.push(icon(ic, 22, y + 18, 22, c, { width: 2 }));
		b.push(text(54, y + 35, name, { size: 16, fill: t.fg, weight: 650 }));
		b.push(text(172, y + 35, String(ids.length), { size: 14, fill: t.subtle, anchor: 'end', weight: 600 }));
		let x = 192;
		for (const id of ids) {
			const w = Math.round(id.length * 13 * 0.61 + 20);
			b.push(`<rect x="${x}" y="${y + 15}" width="${w}" height="28" rx="8" fill="${t.card2}" stroke="${t.border}"/>`);
			b.push(text(x + w / 2, y + 34, id, { size: 13, fill: t.fg, anchor: 'middle', mono: true }));
			x += w + 7;
		}
	});
	const H = pluginLanes.length * (laneH + gap) - gap;
	return svgDoc(
		W,
		H,
		'Die 30 Plugins von NetScope',
		pluginLanes.map(([n, , , ids]) => `${n}: ${ids.join(', ')}`).join('. '),
		b.join('\n')
	);
}

// ---------------------------------------------------------------- write

fs.mkdirSync(outDir, { recursive: true });
const graphics = { banner, features: featureGrid, architecture, plugins };
for (const [name, fn] of Object.entries(graphics)) {
	for (const mode of ['dark', 'light']) {
		const file = path.join(outDir, `${name}-${mode}.svg`);
		fs.writeFileSync(file, fn(themes[mode], mode));
		console.log('wrote', path.relative(root, file));
	}
}
fs.writeFileSync(
	path.join(outDir, 'logo.svg'),
	`<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128" viewBox="0 0 128 128" role="img" aria-label="NetScope">${logo(0, 0, 128)}</svg>\n`
);
console.log('wrote docs/assets/logo.svg');
