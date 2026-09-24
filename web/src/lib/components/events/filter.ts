// Event list filter: URL <-> state, API query and a client-side matcher for live events.
import type { Event } from '$lib/api';
import { severityRank } from '$lib/utils/labels';
import { intParam, listParam } from '$lib/utils/url';

/** Parses an ISO timestamp from the URL; invalid → ''. */
export function isoParam(v: string | null): string {
	if (!v) return '';
	const d = new Date(v);
	return isNaN(d.getTime()) ? '' : d.toISOString();
}

export type AckMode = 'open' | 'acked' | 'all';

export interface EventFilter {
	types: string[];
	categories: string[];
	/** minimum severity ('' = all) */
	severity: string;
	acked: AckMode;
	device: number | null;
	run: number | null;
	q: string;
	/** relative time range preset ('' = none/custom) */
	range: string;
	/** custom range (ISO) */
	from: string;
	to: string;
}

export const RANGES: { value: string; label: string; ms: number }[] = [
	{ value: '1h', label: 'Letzte Stunde', ms: 3600e3 },
	{ value: '24h', label: 'Letzte 24 h', ms: 86400e3 },
	{ value: '7d', label: 'Letzte 7 Tage', ms: 7 * 86400e3 },
	{ value: '30d', label: 'Letzte 30 Tage', ms: 30 * 86400e3 }
];

/** Reads the filter from the URL (?type&category&severity&acked&device&run&q&range&from&to). */
export function filterFromParams(sp: URLSearchParams): EventFilter {
	const a = sp.get('acked');
	const range = sp.get('range') ?? '';
	const device = intParam(sp, 'device', 0);
	const run = intParam(sp, 'run', 0);
	return {
		types: listParam(sp, 'type'),
		categories: listParam(sp, 'category'),
		severity: sp.get('severity') && sp.get('severity')! in severityRank ? sp.get('severity')! : '',
		acked: a === '1' || a === 'true' ? 'acked' : a === 'all' ? 'all' : 'open',
		device: device > 0 ? device : null,
		run: run > 0 ? run : null,
		q: sp.get('q') ?? '',
		range: RANGES.some((r) => r.value === range) ? range : '',
		from: isoParam(sp.get('from')),
		to: isoParam(sp.get('to'))
	};
}

/** Start of the time window (ISO) – relative presets are evaluated now. */
export function windowFrom(f: EventFilter, now = Date.now()): string {
	const r = RANGES.find((x) => x.value === f.range);
	if (r) return new Date(now - r.ms).toISOString();
	return f.from;
}

/** Query for GET /api/v1/events. */
export function apiQuery(f: EventFilter, offset: number, limit: number) {
	return {
		type: f.types.length ? f.types.join(',') : null,
		category: f.categories.length ? f.categories.join(',') : null,
		severity: f.severity || null,
		acked: f.acked === 'all' ? null : f.acked === 'acked',
		device: f.device,
		run: f.run,
		q: f.q.trim() || null,
		from: windowFrom(f) || null,
		to: f.range ? null : f.to || null,
		limit,
		offset
	};
}

/** Body.filter for POST /api/v1/events/ack – exactly the filter of the list (only open events are acked). */
export function ackFilter(f: EventFilter, now = Date.now(), site = '') {
	return {
		site: site || undefined,
		types: f.types.length ? f.types : undefined,
		categories: f.categories.length ? f.categories : undefined,
		minSeverity: f.severity || undefined,
		deviceId: f.device ?? undefined,
		runId: f.run ?? undefined,
		text: f.q.trim() || undefined,
		from: windowFrom(f, now) || undefined,
		to: f.range ? undefined : f.to || undefined
	};
}

function typeMatches(type: string, patterns: string[]): boolean {
	return patterns.some((p) => (p.endsWith('.*') ? type.startsWith(p.slice(0, -1)) : type === p));
}

/** Client-side check whether a (new) event matches the filter – mirrors the backend filter.
 *  siteId: -1 = all sites, 0 = this instance, otherwise the site id. */
export function matchesEvent(ev: Event, f: EventFilter, now = Date.now(), siteId = -1): boolean {
	if (siteId >= 0 && (ev.siteId ?? 0) !== siteId) return false;
	if (f.types.length && !typeMatches(ev.type, f.types)) return false;
	if (f.categories.length && !f.categories.includes(ev.category)) return false;
	if (f.severity && (severityRank[ev.severity] ?? 0) < (severityRank[f.severity] ?? 0)) return false;
	if (f.device && ev.deviceId !== f.device) return false;
	if (f.run && ev.runId !== f.run) return false;
	const acked = !!ev.ackedAt;
	if (f.acked === 'open' && acked) return false;
	if (f.acked === 'acked' && !acked) return false;
	const ts = new Date(ev.ts).getTime();
	const from = windowFrom(f, now);
	if (from && ts < new Date(from).getTime()) return false;
	if (!f.range && f.to && ts > new Date(f.to).getTime()) return false;
	const q = f.q.trim().toLowerCase();
	if (q) {
		const hay = `${ev.title}\n${ev.message}\n${JSON.stringify(ev.payload ?? {})}`.toLowerCase();
		if (!hay.includes(q)) return false;
	}
	return true;
}

/** True if any filter besides the default "open" status is set. */
export function hasFilter(f: EventFilter): boolean {
	return !!(
		f.types.length ||
		f.categories.length ||
		f.severity ||
		f.device ||
		f.run ||
		f.q ||
		f.range ||
		f.from ||
		f.to ||
		f.acked !== 'open'
	);
}
