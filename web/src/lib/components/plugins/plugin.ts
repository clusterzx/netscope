// Helpers for the plugin pages (list, detail, runs).
import { api, ApiError } from '$lib/api';
import type { PluginScope, PluginView, RunView } from '$lib/api';
import { formatBytes, formatNumber } from '$lib/utils/format';

export const KIND_ORDER = ['scanner', 'importer', 'processor', 'publisher'] as const;

export const kindDescription: Record<string, string> = {
	scanner: 'Erzeugen Beobachtungen über Geräte durch aktive Abfragen im Netz.',
	importer: 'Holen Bestandsdaten aus fremden Systemen (API/SSH), ohne zu scannen.',
	processor: 'Verarbeiten Beobachtungen zu Zustand, Events und Berichten.',
	publisher: 'Verschicken Benachrichtigungen, die von Regeln ausgelöst werden.'
};

export const capabilityLabel: Record<string, string> = {
	run: 'Läufe',
	publish: 'Versand',
	changes: 'Änderungs-Hook',
	runFinished: 'Folgelauf-Hook',
	actions: 'Aktionen'
};

export const capabilityHint: Record<string, string> = {
	run: 'Kann geplant und manuell ausgeführt werden',
	publish: 'Stellt Benachrichtigungen zu',
	changes: 'Reagiert auf Zustandsänderungen anderer Plugins',
	runFinished: 'Wird nach Läufen anderer Plugins aktiv',
	actions: 'Bietet Aktionen (Schaltflächen) an'
};

export const targetsLabel: Record<string, string> = {
	subnets: 'Arbeitet auf Subnetzen',
	devices: 'Arbeitet auf Geräten'
};

/** Plugin can be scheduled / run manually. */
export function canRun(p: PluginView): boolean {
	return (p.capabilities ?? []).includes('run');
}

export function isPublisher(p: PluginView): boolean {
	return p.info.kind === 'publisher' || (p.capabilities ?? []).includes('publish');
}

/** Plugins with a run history (everything except pure publishers). */
export function hasRuns(p: PluginView): boolean {
	return !isPublisher(p) || (p.capabilities ?? []).some((c) => c !== 'publish');
}

/** Diffs make sense for plugins that write observations. */
export function diffable(kind: string | undefined): boolean {
	return kind === 'scanner' || kind === 'importer';
}

export function isActive(status: string | undefined): boolean {
	return status === 'queued' || status === 'running';
}

// ---------------------------------------------------------------- config errors

/** Keys of the generic plugin configuration (the rest belongs to the settings schema). */
const GENERIC = new Set([
	'enabled',
	'schedule',
	'timeoutSeconds',
	'retries',
	'retryBackoffSeconds',
	'concurrency'
]);

export function isGenericKey(k: string): boolean {
	return GENERIC.has(k) || k === 'scope' || k.startsWith('scope.');
}

/**
 * Splits API field errors of PUT /plugins/{id}/config into generic ones and schema ones.
 * Schema errors get the "settings." prefix, so SchemaForm (errorPrefix="settings.") also
 * lists unknown keys.
 */
export function splitConfigErrors(e: unknown): {
	generic: Record<string, string>;
	settings: Record<string, string>;
} {
	const generic: Record<string, string> = {};
	const settings: Record<string, string> = {};
	if (e instanceof ApiError) {
		for (const f of e.fields) {
			if (isGenericKey(f.field)) generic[f.field] ??= f.message;
			else {
				const k = f.field.startsWith('settings.') ? f.field : 'settings.' + f.field;
				settings[k] ??= f.message;
			}
		}
	}
	return { generic, settings };
}

// ---------------------------------------------------------------- scope

export function emptyScope(): PluginScope {
	return { allSubnets: true, subnets: [], groups: [], tags: [], devices: [], query: '' };
}

/** Normalized copy (lists never null) – also used for dirty checks. */
export function normScope(s: PluginScope | null | undefined): PluginScope {
	return {
		allSubnets: !!s?.allSubnets,
		subnets: [...(s?.subnets ?? [])],
		groups: [...(s?.groups ?? [])],
		tags: [...(s?.tags ?? [])],
		devices: [...(s?.devices ?? [])],
		query: s?.query ?? ''
	};
}

/** Short German description of a scope. */
export function scopeSummary(s: PluginScope | null | undefined, groupName?: (id: number) => string): string {
	if (!s) return 'Alle aktiven Subnetze';
	const parts: string[] = [];
	if (s.allSubnets) parts.push('alle aktiven Subnetze');
	else if (s.subnets?.length) parts.push(s.subnets.join(', '));
	if (s.groups?.length)
		parts.push(
			(s.groups.length === 1 ? 'Gruppe ' : 'Gruppen ') +
				s.groups.map((g) => groupName?.(g) ?? `#${g}`).join(', ')
		);
	if (s.tags?.length) parts.push((s.tags.length === 1 ? 'Tag ' : 'Tags ') + s.tags.join(', '));
	if (s.devices?.length) parts.push(s.devices.length === 1 ? '1 Gerät' : `${s.devices.length} Geräte`);
	if (s.query) parts.push(`Filter „${s.query}“`);
	if (!parts.length) return 'Keine Ziele';
	const text = parts.join(' · ');
	return text.charAt(0).toUpperCase() + text.slice(1);
}

// ---------------------------------------------------------------- stats

/** Display value of a run statistic. */
export function statValue(key: string, v: unknown): string {
	if (v === null || v === undefined) return '–';
	if (typeof v === 'number') return /bytes?$/.test(key) ? formatBytes(v) : formatNumber(v, 2);
	if (typeof v === 'boolean') return v ? 'ja' : 'nein';
	if (typeof v === 'string') return v;
	return JSON.stringify(v);
}

/** Simple (scalar) statistics of a run, in key order; `result` (action outcome) excluded. */
export function scalarStats(stats: Record<string, unknown> | null | undefined): [string, unknown][] {
	return Object.entries(stats ?? {})
		.filter(([k, v]) => k !== 'result' && (v === null || typeof v !== 'object'))
		.sort(([a], [b]) => a.localeCompare(b));
}

/** Nested (object/array) statistics of a run. */
export function complexStats(stats: Record<string, unknown> | null | undefined): [string, unknown][] {
	return Object.entries(stats ?? {}).filter(
		([k, v]) => k !== 'result' && v !== null && typeof v === 'object'
	);
}

/** Human label for a stat key ("cves_changed" → "cves changed"). */
export function statLabel(key: string): string {
	return key.replace(/_/g, ' ');
}

// ---------------------------------------------------------------- runs

/** A run over whole subnets (not restricted to devices, groups, tags or a filter). */
export function isFullScope(s: PluginScope | null | undefined): boolean {
	return !(s?.devices?.length || s?.groups?.length || s?.tags?.length || s?.query);
}

/**
 * The previous successful run of the same plugin for "Diff zum vorherigen Lauf"
 * (GET /runs?before=). A full run is compared with the previous full run (scope=full), so
 * a single-device scan in between does not show every other device as removed. Action
 * runs (e.g. an OUI update) are skipped.
 */
export async function previousSuccessfulRun(run: RunView): Promise<RunView | null> {
	const res = await api.get('/api/v1/runs', {
		query: {
			plugin: run.pluginId,
			status: 'success',
			before: run.id,
			scope: isFullScope(run.scope) ? 'full' : null,
			limit: 10
		}
	});
	return (res.items ?? []).find((r) => r.trigger !== 'action') ?? null;
}

/** Diff URL between the previous run and `run` (single-device runs are limited to that device). */
export function diffUrl(prev: RunView, run: RunView): string {
	const devices = run.scope?.devices ?? [];
	const device = !isFullScope(run.scope) && devices.length === 1 ? `&device=${devices[0]}` : '';
	return `/diff?runA=${prev.id}&runB=${run.id}${device}`;
}

/** Log level → CSS classes and German label. */
export function logLevelClass(level: string): string {
	switch (level.toUpperCase()) {
		case 'ERROR':
			return 'text-danger';
		case 'WARN':
		case 'WARNING':
			return 'text-warn';
		case 'DEBUG':
			return 'text-fg-subtle';
		default:
			return 'text-accent';
	}
}

export const LOG_LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR'] as const;

export function levelRank(level: string): number {
	const i = LOG_LEVELS.indexOf(level.toUpperCase() as (typeof LOG_LEVELS)[number]);
	return i < 0 ? (level.toUpperCase() === 'WARNING' ? 2 : 1) : i;
}
