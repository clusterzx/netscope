// Helpers for the vulnerability pages: labels, severity filters and a readable CVSS
// vector breakdown (v2, v3.x and v4.0 base metrics).
import type { Tone } from '$lib/utils/labels';

/** Match types of the CVE matcher. */
export const matchTypeLabel: Record<string, string> = {
	exact: 'Exakt',
	range: 'Versionsbereich',
	heuristic: 'Heuristisch'
};

export const matchTypeHint: Record<string, string> = {
	exact: 'Die erkannte Version steht exakt in den CPE-Angaben der NVD.',
	range: 'Die erkannte Version liegt in einem betroffenen Versionsbereich der NVD.',
	heuristic: 'Produkt/Version wurden unscharf zugeordnet – häufig Fehlalarme (Backports).'
};

export function matchTypeTone(t: string): Tone {
	return t === 'exact' ? 'danger' : t === 'range' ? 'warn' : 'neutral';
}

/** Severities as returned by the CVE API (incl. none/unknown). */
export const cveSeverityLabel: Record<string, string> = {
	critical: 'Kritisch',
	high: 'Hoch',
	medium: 'Mittel',
	low: 'Niedrig',
	none: 'Keine',
	unknown: 'Unbekannt'
};

/** Minimum CVSS per severity (for the "min" filter). */
export const severityMin: Record<string, number> = { critical: 9, high: 7, medium: 4, low: 0.1 };

export const MIN_OPTIONS = [
	{ value: '9', label: 'Kritisch (≥ 9,0)' },
	{ value: '7', label: 'Hoch und höher (≥ 7,0)' },
	{ value: '4', label: 'Mittel und höher (≥ 4,0)' },
	{ value: '0.1', label: 'Niedrig und höher (> 0)' }
];

export const SORT_OPTIONS = [
	{ value: '-score', label: 'CVSS (höchster zuerst)' },
	{ value: 'score', label: 'CVSS (niedrigster zuerst)' },
	{ value: '-published', label: 'Veröffentlicht (neueste zuerst)' },
	{ value: 'published', label: 'Veröffentlicht (älteste zuerst)' },
	{ value: '-devices', label: 'Betroffene Geräte (meiste zuerst)' },
	{ value: '-first_seen', label: 'Erstmals gefunden (neueste zuerst)' },
	{ value: 'first_seen', label: 'Erstmals gefunden (älteste zuerst)' },
	{ value: 'cve', label: 'CVE-ID (aufsteigend)' },
	{ value: '-cve', label: 'CVE-ID (absteigend)' }
];

/** NVD vulnStatus labels. */
export const nvdStatusLabel: Record<string, string> = {
	Analyzed: 'Analysiert',
	Modified: 'Geändert',
	'Awaiting Analysis': 'Analyse ausstehend',
	'Undergoing Analysis': 'In Analyse',
	Received: 'Eingegangen',
	Rejected: 'Zurückgezogen',
	Deferred: 'Zurückgestellt'
};

/** Feed sync status. */
export const feedStatusLabel: Record<string, string> = {
	ok: 'OK',
	syncing: 'Synchronisiert …',
	error: 'Fehler',
	pending: 'Ausstehend'
};

export function feedStatusTone(s: string | undefined): Tone {
	return s === 'ok' ? 'ok' : s === 'error' ? 'danger' : s === 'syncing' ? 'accent' : 'neutral';
}

export const syncModeLabel: Record<string, string> = {
	initial: 'Erstimport',
	full: 'Vollständig',
	incremental: 'Inkrementell'
};

// ---------------------------------------------------------------- CVSS vector

export interface VectorMetric {
	key: string;
	name: string;
	value: string;
	text: string;
	tone: Tone;
}

type MetricDef = { name: string; values: Record<string, [string, Tone]> };

const impact3: Record<string, [string, Tone]> = {
	H: ['Hoch', 'danger'],
	L: ['Niedrig', 'warn'],
	N: ['Keine', 'ok']
};

const v3: Record<string, MetricDef> = {
	AV: {
		name: 'Angriffsvektor',
		values: {
			N: ['Netzwerk', 'danger'],
			A: ['Benachbartes Netz', 'warn'],
			L: ['Lokal', 'ok'],
			P: ['Physisch', 'ok']
		}
	},
	AC: { name: 'Angriffskomplexität', values: { L: ['Niedrig', 'danger'], H: ['Hoch', 'ok'] } },
	PR: {
		name: 'Benötigte Rechte',
		values: { N: ['Keine', 'danger'], L: ['Niedrig', 'warn'], H: ['Hoch', 'ok'] }
	},
	UI: { name: 'Benutzerinteraktion', values: { N: ['Keine', 'danger'], R: ['Erforderlich', 'ok'] } },
	S: { name: 'Auswirkungsbereich', values: { U: ['Unverändert', 'neutral'], C: ['Verändert', 'danger'] } },
	C: { name: 'Vertraulichkeit', values: impact3 },
	I: { name: 'Integrität', values: impact3 },
	A: { name: 'Verfügbarkeit', values: impact3 }
};

const v4: Record<string, MetricDef> = {
	AV: v3.AV,
	AC: v3.AC,
	AT: { name: 'Angriffsvoraussetzungen', values: { N: ['Keine', 'danger'], P: ['Vorhanden', 'ok'] } },
	PR: v3.PR,
	UI: {
		name: 'Benutzerinteraktion',
		values: { N: ['Keine', 'danger'], P: ['Passiv', 'warn'], A: ['Aktiv', 'ok'] }
	},
	VC: { name: 'Vertraulichkeit (System)', values: impact3 },
	VI: { name: 'Integrität (System)', values: impact3 },
	VA: { name: 'Verfügbarkeit (System)', values: impact3 },
	SC: { name: 'Vertraulichkeit (Folgesysteme)', values: impact3 },
	SI: { name: 'Integrität (Folgesysteme)', values: impact3 },
	SA: { name: 'Verfügbarkeit (Folgesysteme)', values: impact3 }
};

const impact2: Record<string, [string, Tone]> = {
	C: ['Vollständig', 'danger'],
	P: ['Teilweise', 'warn'],
	N: ['Keine', 'ok']
};

const v2: Record<string, MetricDef> = {
	AV: {
		name: 'Angriffsvektor',
		values: { N: ['Netzwerk', 'danger'], A: ['Benachbartes Netz', 'warn'], L: ['Lokal', 'ok'] }
	},
	AC: {
		name: 'Angriffskomplexität',
		values: { L: ['Niedrig', 'danger'], M: ['Mittel', 'warn'], H: ['Hoch', 'ok'] }
	},
	Au: {
		name: 'Authentifizierung',
		values: { N: ['Keine', 'danger'], S: ['Einfach', 'warn'], M: ['Mehrfach', 'ok'] }
	},
	C: { name: 'Vertraulichkeit', values: impact2 },
	I: { name: 'Integrität', values: impact2 },
	A: { name: 'Verfügbarkeit', values: impact2 }
};

/**
 * Parses a CVSS vector ("CVSS:3.1/AV:N/AC:L/…", "AV:N/AC:L/Au:N/C:P/I:P/A:P" for v2,
 * "CVSS:4.0/…") into its base metrics with German labels. Unknown metrics are skipped.
 */
export function parseVector(vector: string, version = ''): { version: string; metrics: VectorMetric[] } {
	const parts = (vector ?? '').trim().split('/').filter(Boolean);
	let ver = version;
	if (parts[0]?.startsWith('CVSS:')) ver = parts.shift()!.slice(5);
	if (!ver) ver = parts.some((p) => p.startsWith('Au:')) ? '2.0' : '3.1';
	const defs = ver.startsWith('4') ? v4 : ver.startsWith('2') ? v2 : v3;
	const metrics: VectorMetric[] = [];
	for (const p of parts) {
		const [k, v] = p.split(':');
		const def = defs[k];
		if (!def || v === undefined) continue;
		const [text, tone] = def.values[v] ?? [v, 'neutral'];
		metrics.push({ key: k, name: def.name, value: v, text, tone });
	}
	return { version: ver, metrics };
}

/** Link to the CWE definition (NVD placeholders like NVD-CWE-Other have none). */
export function cweUrl(cwe: string): string | null {
	const m = /^CWE-(\d+)$/.exec(cwe);
	return m ? `https://cwe.mitre.org/data/definitions/${m[1]}.html` : null;
}

/** Readable version range of a CPE match entry. */
export function versionRange(m: {
	version?: string;
	versionStartIncluding?: string;
	versionStartExcluding?: string;
	versionEndIncluding?: string;
	versionEndExcluding?: string;
}): string {
	const from = m.versionStartIncluding
		? `≥ ${m.versionStartIncluding}`
		: m.versionStartExcluding
			? `> ${m.versionStartExcluding}`
			: '';
	const to = m.versionEndIncluding
		? `≤ ${m.versionEndIncluding}`
		: m.versionEndExcluding
			? `< ${m.versionEndExcluding}`
			: '';
	if (from || to) return [from, to].filter(Boolean).join(' und ');
	if (!m.version || m.version === '*') return 'alle Versionen';
	if (m.version === '-') return 'ohne Version';
	return `= ${m.version}`;
}
