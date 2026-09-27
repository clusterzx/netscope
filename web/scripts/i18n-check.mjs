// Checks the English catalog (src/lib/i18n/en/*.json) against the sources:
//
//   - every key in exactly one catalog file, no empty translations
//   - placeholders ({name}) identical in key and translation
//   - no unused keys (a key counts as used when it appears as a string literal in src/)
//   - no untranslated German text: German-looking literals must be the direct argument of
//     t(), tn() or msg(), German-looking markup text and static attribute values are not
//     allowed at all. "German-looking" means umlauts, common German words or words that
//     occur in catalog keys but in no translation. A line containing "i18n-ignore" is
//     skipped (for German that is data, e.g. parser keywords).
//
// Missing translations of t('…') keys are type errors and reported by svelte-check.
// Usage: node scripts/i18n-check.mjs [--list-untranslated]
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const src = join(root, 'src');
const catalogDir = join(src, 'lib', 'i18n', 'en');
const errors = [];
const err = (msg) => errors.push(msg);

// ---------------------------------------------------------------- catalog

const catalog = new Map(); // key → { value, file }
for (const f of readdirSync(catalogDir)
	.filter((f) => f.endsWith('.json'))
	.sort()) {
	const text = readFileSync(join(catalogDir, f), 'utf8');
	let obj;
	try {
		obj = JSON.parse(text);
	} catch (e) {
		err(`${f}: invalid JSON: ${e.message}`);
		continue;
	}
	// duplicates inside one file are silently merged by JSON.parse
	const seen = new Set();
	for (const m of text.matchAll(/^\s*("(?:[^"\\]|\\.)*")\s*:/gm)) {
		const k = JSON.parse(m[1]);
		if (seen.has(k)) err(`${f}: key ${m[1]} appears twice`);
		seen.add(k);
	}
	for (const [k, v] of Object.entries(obj)) {
		if (catalog.has(k))
			err(
				`${f}: key "${k}" is also in ${catalog.get(k).file} (keep it in one file, shared texts in common.json)`
			);
		else catalog.set(k, { value: v, file: f });
		if (typeof v !== 'string' || !v.trim()) err(`${f}: empty translation for "${k}"`);
		const ph = (s) =>
			[...String(s).matchAll(/\{(\w+)\}/g)]
				.map((m) => m[1])
				.sort()
				.join(',');
		if (ph(k) !== ph(v)) err(`${f}: placeholders differ: "${k}" → "${v}"`);
	}
}

// ---------------------------------------------------------------- German detection

const stopwords = new Set(
	(
		'der das und oder nicht kein keine keinen keiner keinem ist sind wird werden wurde wurden für mit auf bei ' +
		'nach vom zum zur aus ein eine einen einem einer eines dem des den als auch noch nur schon bitte erst ' +
		'ungültig ungültige erwartet fehlt gefunden konnte kann darf muss soll wählen löschen speichern anlegen ' +
		'abbrechen zurück weiter hier diese dieser dieses sich wieder alle allen jede jeder jedes zwischen über ' +
		'unter ohne seit bis wenn dann sonst weil dass haben habe sein mehr weniger neu neue neuer neues ja nein ' +
		'gerät geräte geräten fehler datei zeit lauf läufe benutzer passwort eintrag einträge wert werte leer ' +
		'unbekannt verbindung antwort dienst dienste netz subnetz standort standorte regel regeln bericht ' +
		'schwachstelle hersteller modell zeile spalte feld felder gesetzt gespeichert gelöscht angelegt entfernt ' +
		'aktiviert deaktiviert abgelaufen erreichbar verfügbar jetzt heute immer gruppe gruppen rolle rollen ' +
		'zeitraum zustand quelle anzeigen bearbeiten schließen hinzufügen ändern keine treffer'
	).split(/\s+/)
);
// technical words that are the same in both languages (never German markers)
const neutral = new Set(
	'diff schema export filter status name tags plugin plugins event events health scanner cipher parameter bit'.split(
		' '
	)
);
// words of German source texts that no translation uses
const englishWords = new Set();
for (const { value } of catalog.values()) for (const w of words(value)) englishWords.add(w);
const lexicon = new Set();
for (const k of catalog.keys())
	for (const w of words(k)) if (!englishWords.has(w) && !neutral.has(w) && w.length > 2) lexicon.add(w);

function words(s) {
	return (
		String(s)
			.toLowerCase()
			.match(/[a-zäöüß]+/g) ?? []
	).filter(Boolean);
}

function looksGerman(s) {
	if (/[äöüÄÖÜß„“]/.test(s)) return true;
	for (const w of words(s)) if (stopwords.has(w) || lexicon.has(w)) return true;
	return false;
}

// ---------------------------------------------------------------- sources

function walk(dir, out = []) {
	for (const name of readdirSync(dir)) {
		const p = join(dir, name);
		if (statSync(p).isDirectory()) {
			if (p === join(src, 'lib', 'i18n')) continue;
			walk(p, out);
		} else if (/\.(svelte|ts)$/.test(name) && name !== 'generated.ts') out.push(p);
	}
	return out;
}

const used = new Set();
const findings = [];

/**
 * Tokenizes JavaScript/TypeScript code: returns the string literals with their offsets and
 * whether they are the direct argument of t(), tn() or msg(). Comments are skipped,
 * template literals count as literals (their ${} parts are scanned as code).
 */
function scanCode(code, base, file, lineOf) {
	let i = 0;
	let pendingTn = 0; // literals of a tn() call still to accept
	let tnDepth = -1;
	let depth = 0;
	const n = code.length;
	const lastToken = () => {
		let j = i - 1;
		while (j >= 0 && /\s/.test(code[j])) j--;
		return j;
	};
	while (i < n) {
		const c = code[i];
		if (c === '/' && code[i + 1] === '/') {
			i = code.indexOf('\n', i);
			if (i < 0) break;
			continue;
		}
		if (c === '/' && code[i + 1] === '*') {
			i = code.indexOf('*/', i + 2);
			if (i < 0) break;
			i += 2;
			continue;
		}
		if (c === '/') {
			// a regular expression literal starts where an operand is expected
			const j = lastToken();
			const prev = j >= 0 ? code[j] : '';
			const word = /(\w+)$/.exec(code.slice(Math.max(0, j - 10), j + 1))?.[1] ?? '';
			if (j < 0 || '(,=:[!&|?{};+-*%<>~^'.includes(prev) || /^(return|typeof|case|in|of)$/.test(word)) {
				let k = i + 1;
				let cls = false;
				while (k < n && code[k] !== '\n') {
					if (code[k] === '\\') k++;
					else if (code[k] === '[') cls = true;
					else if (code[k] === ']') cls = false;
					else if (code[k] === '/' && !cls) break;
					k++;
				}
				i = k + 1;
				continue;
			}
		}
		if (c === '(') depth++;
		if (c === ')') {
			depth--;
			if (depth < tnDepth) {
				pendingTn = 0;
				tnDepth = -1;
			}
		}
		if (c === 't' && /^tn\s*\(/.test(code.slice(i, i + 5)) && !/[\w$.]/.test(code[i - 1] ?? '')) {
			pendingTn = 2;
			tnDepth = depth + 1;
		}
		if (c === "'" || c === '"' || c === '`') {
			const start = i;
			let j = i + 1;
			let text = '';
			while (j < n && code[j] !== c) {
				if (code[j] === '\\') {
					const nx = code[j + 1];
					text += nx === 'n' ? '\n' : nx === 't' ? '\t' : nx;
					j += 2;
					continue;
				}
				if (c === '`' && code[j] === '$' && code[j + 1] === '{') {
					// scan the expression inside the template literal as code
					let d = 1;
					let k = j + 2;
					while (k < n && d > 0) {
						if (code[k] === '{') d++;
						else if (code[k] === '}') d--;
						k++;
					}
					scanCode(code.slice(j + 2, k - 1), base + j + 2, file, lineOf);
					text += '${}';
					j = k;
					continue;
				}
				text += code[j];
				j++;
			}
			const before = code.slice(Math.max(0, lastToken() - 5), lastToken() + 1);
			const direct = /(?:^|[^\w$.])(?:t|msg)\s*\($/.test(before);
			let ok = direct;
			if (!ok && pendingTn > 0 && depth === tnDepth && /[,(]$/.test(before)) {
				ok = true;
				pendingTn--;
			}
			used.add(text);
			// module specifiers and paths are code
			const importSpec = /(?:\bfrom|\bimport)\s*\(?$/.test(
				code.slice(Math.max(0, lastToken() - 7), lastToken() + 1)
			);
			const path = /^(?:\$lib|\.{1,2}\/|\/|https?:)/.test(text);
			if (!ok && !importSpec && !path && looksGerman(text)) {
				const line = lineOf(base + start);
				if (!ignored(file, line)) findings.push(`${rel(file)}:${line}: untranslated ${JSON.stringify(text)}`);
			}
			i = j + 1;
			continue;
		}
		i++;
	}
}

const fileLines = new Map();
function ignored(file, line) {
	const lines = fileLines.get(file);
	return (
		lines?.[line - 1]?.includes('i18n-ignore') || lines?.[line - 2]?.trim().startsWith('<!-- i18n-ignore')
	);
}
const rel = (f) => relative(root, f).replaceAll('\\', '/');

function lineIndex(text) {
	const starts = [0];
	for (let i = 0; i < text.length; i++) if (text[i] === '\n') starts.push(i + 1);
	return (off) => {
		let lo = 0;
		let hi = starts.length - 1;
		while (lo < hi) {
			const mid = (lo + hi + 1) >> 1;
			if (starts[mid] <= off) lo = mid;
			else hi = mid - 1;
		}
		return lo + 1;
	};
}

// attributes whose static values are never display text
const codeAttrs =
	/^(class|href|src|type|id|for|name|role|variant|tone|icon|size|autocomplete|inputmode|method|rel|target|style|d|fill|stroke|viewBox|xmlns|pattern|lang|accept|mode|align|kind|key|width|height|x|y|cx|cy|r|rx|ry|points|transform|dir|spellcheck|autocapitalize|enterkeyhint|data-[\w-]+|bind:[\w]+|on[\w]+|use:[\w]+|transition:[\w]+|slot|stroke-[\w-]+|font-[\w-]+|text-anchor|dominant-baseline|opacity|offset|stop-[\w-]+|gradientUnits|patternUnits|mask|clip-path|preserveAspectRatio|aria-hidden|tabindex|loading|decoding|placement|position|side|layout|format|as|el|step|min|max|minlength|maxlength|rows|cols)$/;

function scanSvelte(file, text) {
	const lineOf = lineIndex(text);
	// blank out comments and style blocks, keep offsets
	const blank = (s) => s.replace(/[^\n]/g, ' ');
	let t = text.replace(/<!--[\s\S]*?-->/g, blank).replace(/<style[\s\S]*?<\/style>/g, blank);
	// scripts are code
	t = t.replace(/(<script[^>]*>)([\s\S]*?)(<\/script>)/g, (m, open, body, close, off) => {
		scanCode(body, off + open.length, file, lineOf);
		return blank(m);
	});
	// markup: expressions in braces are code, the rest is text and tags
	let out = '';
	let i = 0;
	while (i < t.length) {
		if (t[i] === '{') {
			let d = 1;
			let k = i + 1;
			let q = null;
			while (k < t.length && d > 0) {
				const ch = t[k];
				if (q) {
					if (ch === '\\') k++;
					else if (ch === q) q = null;
				} else if (ch === "'" || ch === '"' || ch === '`') q = ch;
				else if (ch === '{') d++;
				else if (ch === '}') d--;
				k++;
			}
			const expr = t.slice(i + 1, k - 1).replace(/^[#:/@]\w*\s*/, '');
			scanCode(expr, i + 1 + (t.slice(i + 1, k - 1).length - expr.length), file, lineOf);
			out += blank(t.slice(i, k));
			i = k;
			continue;
		}
		out += t[i];
		i++;
	}
	// static attribute values
	for (const m of out.matchAll(/<[\w:.-]+((?:\s+[\w:.|-]+(?:=(?:"[^"]*"|'[^']*'|[^\s>]+))?)*)\s*\/?>/g)) {
		for (const a of m[1].matchAll(/([\w:.|-]+)=("([^"]*)"|'([^']*)')/g)) {
			const name = a[1];
			const value = a[3] ?? a[4] ?? '';
			if (codeAttrs.test(name) || !value.trim()) continue;
			if (looksGerman(value)) {
				const line = lineOf(m.index + m[0].indexOf(a[0]));
				if (!ignored(file, line))
					findings.push(`${rel(file)}:${line}: untranslated attribute ${name}="${value}"`);
			}
		}
	}
	// text nodes
	const textOnly = out.replace(/<[^>]*>/g, (m) => blank(m));
	for (const m of textOnly.matchAll(/[^\s][^\n]*/g)) {
		const s = m[0].trim();
		if (s && looksGerman(s)) {
			const line = lineOf(m.index);
			if (!ignored(file, line)) findings.push(`${rel(file)}:${line}: untranslated text ${JSON.stringify(s)}`);
		}
	}
}

for (const file of walk(src)) {
	const text = readFileSync(file, 'utf8');
	fileLines.set(file, text.split('\n'));
	if (file.endsWith('.svelte')) scanSvelte(file, text);
	else scanCode(text, 0, file, lineIndex(text));
}

for (const [k, { file }] of catalog) {
	if (!used.has(k)) err(`${file}: unused key "${k}"`);
}

const listOnly = process.argv.includes('--list-untranslated');
if (findings.length) {
	for (const f of findings) console.error(f);
	if (!listOnly)
		err(
			`${findings.length} untranslated German text(s) – wrap them in t()/tn()/msg() or mark the line with i18n-ignore`
		);
}
for (const e of errors) console.error('i18n: ' + e);
if (errors.length) {
	console.error(`i18n check failed (${errors.length} problem(s), ${catalog.size} keys)`);
	process.exit(1);
}
console.log(`i18n check ok: ${catalog.size} keys`);
