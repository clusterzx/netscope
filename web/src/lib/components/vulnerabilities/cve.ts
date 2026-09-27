// Helpers for the vulnerability pages: labels, severity filters and a readable CVSS
// vector breakdown (v2, v3.x and v4.0 base metrics).
import { t } from '$lib/i18n';
import { formatPercent } from '$lib/utils/format';
import type { Tone } from '$lib/utils/labels';

/** Match types of the CVE matcher. */
export const matchTypeLabel: Record<string, string> = {
	exact: t('Exakt'),
	range: t('Versionsbereich'),
	heuristic: t('Heuristisch')
};

export const matchTypeHint: Record<string, string> = {
	exact: t('Die erkannte Version steht exakt in den CPE-Angaben der NVD.'),
	range: t('Die erkannte Version liegt in einem betroffenen Versionsbereich der NVD.'),
	heuristic: t('Produkt/Version wurden unscharf zugeordnet – häufig Fehlalarme (Backports).')
};

export function matchTypeTone(type: string): Tone {
	return type === 'exact' ? 'danger' : type === 'range' ? 'warn' : 'neutral';
}

/** Severities as returned by the CVE API (incl. none/unknown). */
export const cveSeverityLabel: Record<string, string> = {
	critical: t('Kritisch'),
	high: t('Hoch'),
	medium: t('Mittel'),
	low: t('Niedrig'),
	none: t('Keine'),
	unknown: t('Unbekannt')
};

/** Minimum CVSS per severity (for the "min" filter). */
export const severityMin: Record<string, number> = { critical: 9, high: 7, medium: 4, low: 0.1 };

export const MIN_OPTIONS = [
	{ value: '9', label: t('Kritisch (≥ 9,0)') },
	{ value: '7', label: t('Hoch und höher (≥ 7,0)') },
	{ value: '4', label: t('Mittel und höher (≥ 4,0)') },
	{ value: '0.1', label: t('Niedrig und höher (> 0)') }
];

/** Default order: exploited (CISA KEV) first, then EPSS, then CVSS. */
export const DEFAULT_SORT = '-priority';

export const SORT_OPTIONS = [
	{ value: '-priority', label: t('Dringlichkeit (ausgenutzt, EPSS, CVSS)') },
	{ value: '-epss', label: t('EPSS (höchster zuerst)') },
	{ value: '-score', label: t('CVSS (höchster zuerst)') },
	{ value: 'score', label: t('CVSS (niedrigster zuerst)') },
	{ value: '-published', label: t('Veröffentlicht (neueste zuerst)') },
	{ value: 'published', label: t('Veröffentlicht (älteste zuerst)') },
	{ value: '-devices', label: t('Betroffene Geräte (meiste zuerst)') },
	{ value: '-first_seen', label: t('Erstmals gefunden (neueste zuerst)') },
	{ value: 'first_seen', label: t('Erstmals gefunden (älteste zuerst)') },
	{ value: 'cve', label: t('CVE-ID (aufsteigend)') },
	{ value: '-cve', label: t('CVE-ID (absteigend)') }
];

/** EPSS filter thresholds (probability of exploitation within 30 days). */
export const EPSS_OPTIONS = [
	{ value: '0.5', label: t('ab {percent}', { percent: formatPercent(50, 0) }) },
	{ value: '0.1', label: t('ab {percent}', { percent: formatPercent(10, 0) }) },
	{ value: '0.01', label: t('ab {percent}', { percent: formatPercent(1, 0) }) }
];

/** EPSS as a percentage: 0.93 → "93 %", 0.0004 → "< 0,1 %" (English "93%", "< 0.1%"). */
export function epssLabel(v: number): string {
	const p = v * 100;
	if (p > 0 && p < 0.1) return `< ${formatPercent(0.1, 1)}`;
	return formatPercent(p, p < 10 ? 1 : 0);
}

export function epssTone(v: number): Tone {
	return v >= 0.5 ? 'danger' : v >= 0.1 ? 'warn' : 'neutral';
}

export function epssTitle(v: number, percentile?: number | null): string {
	if (percentile !== undefined && percentile !== null)
		return t(
			'Wahrscheinlichkeit einer Ausnutzung in den nächsten 30 Tagen: {epss} – höher als bei {percentile} aller bewerteten CVEs (Quelle: FIRST EPSS)',
			{ epss: epssLabel(v), percentile: epssLabel(percentile) }
		);
	return t('Wahrscheinlichkeit einer Ausnutzung in den nächsten 30 Tagen: {epss} (Quelle: FIRST EPSS)', {
		epss: epssLabel(v)
	});
}

/** Names of the extra rows in the feed list. */
export const feedName: Record<string, string> = {
	modified: t('Geänderte CVEs'),
	kev: t('CISA KEV (ausgenutzte CVEs)'),
	epss: 'FIRST EPSS'
};

/** NVD vulnStatus labels. */
export const nvdStatusLabel: Record<string, string> = {
	Analyzed: t('Analysiert'),
	Modified: t('Geändert'),
	'Awaiting Analysis': t('Analyse ausstehend'),
	'Undergoing Analysis': t('In Analyse'),
	Received: t('Eingegangen'),
	Rejected: t('Zurückgezogen'),
	Deferred: t('Zurückgestellt')
};

/** Feed sync status. */
export const feedStatusLabel: Record<string, string> = {
	ok: 'OK',
	syncing: t('Synchronisiert …'),
	error: t('Fehler'),
	pending: t('Ausstehend')
};

export function feedStatusTone(s: string | undefined): Tone {
	return s === 'ok' ? 'ok' : s === 'error' ? 'danger' : s === 'syncing' ? 'accent' : 'neutral';
}

export const syncModeLabel: Record<string, string> = {
	initial: t('Erstimport'),
	full: t('Vollständig'),
	incremental: t('Inkrementell')
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
	H: [t('Hoch'), 'danger'],
	L: [t('Niedrig'), 'warn'],
	N: [t('Keine'), 'ok']
};

const v3: Record<string, MetricDef> = {
	AV: {
		name: t('Angriffsvektor'),
		values: {
			N: [t('Netzwerk'), 'danger'],
			A: [t('Benachbartes Netz'), 'warn'],
			L: [t('Lokal'), 'ok'],
			P: [t('Physisch'), 'ok']
		}
	},
	AC: { name: t('Angriffskomplexität'), values: { L: [t('Niedrig'), 'danger'], H: [t('Hoch'), 'ok'] } },
	PR: {
		name: t('Benötigte Rechte'),
		values: { N: [t('Keine'), 'danger'], L: [t('Niedrig'), 'warn'], H: [t('Hoch'), 'ok'] }
	},
	UI: {
		name: t('Benutzerinteraktion'),
		values: { N: [t('Keine'), 'danger'], R: [t('Erforderlich'), 'ok'] }
	},
	S: {
		name: t('Auswirkungsbereich'),
		values: { U: [t('Unverändert'), 'neutral'], C: [t('Verändert'), 'danger'] }
	},
	C: { name: t('Vertraulichkeit'), values: impact3 },
	I: { name: t('Integrität'), values: impact3 },
	A: { name: t('Verfügbarkeit'), values: impact3 }
};

const v4: Record<string, MetricDef> = {
	AV: v3.AV,
	AC: v3.AC,
	AT: {
		name: t('Angriffsvoraussetzungen'),
		values: { N: [t('Keine'), 'danger'], P: [t('Vorhanden'), 'ok'] }
	},
	PR: v3.PR,
	UI: {
		name: t('Benutzerinteraktion'),
		values: { N: [t('Keine'), 'danger'], P: [t('Passiv'), 'warn'], A: [t('Aktiv'), 'ok'] }
	},
	VC: { name: t('Vertraulichkeit (System)'), values: impact3 },
	VI: { name: t('Integrität (System)'), values: impact3 },
	VA: { name: t('Verfügbarkeit (System)'), values: impact3 },
	SC: { name: t('Vertraulichkeit (Folgesysteme)'), values: impact3 },
	SI: { name: t('Integrität (Folgesysteme)'), values: impact3 },
	SA: { name: t('Verfügbarkeit (Folgesysteme)'), values: impact3 }
};

const impact2: Record<string, [string, Tone]> = {
	C: [t('Vollständig'), 'danger'],
	P: [t('Teilweise'), 'warn'],
	N: [t('Keine'), 'ok']
};

const v2: Record<string, MetricDef> = {
	AV: {
		name: t('Angriffsvektor'),
		values: { N: [t('Netzwerk'), 'danger'], A: [t('Benachbartes Netz'), 'warn'], L: [t('Lokal'), 'ok'] }
	},
	AC: {
		name: t('Angriffskomplexität'),
		values: { L: [t('Niedrig'), 'danger'], M: [t('Mittel'), 'warn'], H: [t('Hoch'), 'ok'] }
	},
	Au: {
		name: t('Authentifizierung'),
		values: { N: [t('Keine'), 'danger'], S: [t('Einfach'), 'warn'], M: [t('Mehrfach'), 'ok'] }
	},
	C: { name: t('Vertraulichkeit'), values: impact2 },
	I: { name: t('Integrität'), values: impact2 },
	A: { name: t('Verfügbarkeit'), values: impact2 }
};

/**
 * Parses a CVSS vector ("CVSS:3.1/AV:N/AC:L/…", "AV:N/AC:L/Au:N/C:P/I:P/A:P" for v2,
 * "CVSS:4.0/…") into its base metrics with labels in the UI language. Unknown metrics are skipped.
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
	if (from && to) return t('{from} und {to}', { from, to });
	if (from || to) return from || to;
	if (!m.version || m.version === '*') return t('alle Versionen');
	if (m.version === '-') return t('ohne Version');
	return `= ${m.version}`;
}
