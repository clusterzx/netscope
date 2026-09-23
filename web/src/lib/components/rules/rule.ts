// Helpers for the rule pages: labels, summaries, drafts and client side validation
// (mirrors rules.Rule.Validate in the backend).
import type { EventSpec, PublisherInfo, Rule, RuleAction, RuleConditions } from '$lib/api';
import { isCidr } from '$lib/components/schema';
import { severityLabel, stateLabel, type Tone } from '$lib/utils/labels';

export const priorityLabel: Record<string, string> = {
	low: 'Niedrig',
	normal: 'Normal',
	high: 'Hoch',
	urgent: 'Dringend'
};
export const PRIORITIES = ['low', 'normal', 'high', 'urgent'] as const;

export function priorityTone(p: string | undefined): Tone {
	switch (p) {
		case 'urgent':
			return 'critical';
		case 'high':
			return 'high';
		case 'low':
			return 'info';
		default:
			return 'neutral';
	}
}

export const modeLabel: Record<string, string> = { immediate: 'sofort', batch: 'gesammelt' };

export const opLabel: Record<string, string> = {
	'=': 'gleich',
	'!=': 'ungleich',
	'>': 'größer als',
	'>=': 'größer/gleich',
	'<': 'kleiner als',
	'<=': 'kleiner/gleich',
	contains: 'enthält'
};
export const OPS = ['=', '!=', '>', '>=', '<', '<=', 'contains'] as const;

/** Week days in German order (Mo–So) with the backend numbers (0 = Sunday). */
export const WEEKDAYS: { value: number; short: string; long: string }[] = [
	{ value: 1, short: 'Mo', long: 'Montag' },
	{ value: 2, short: 'Di', long: 'Dienstag' },
	{ value: 3, short: 'Mi', long: 'Mittwoch' },
	{ value: 4, short: 'Do', long: 'Donnerstag' },
	{ value: 5, short: 'Fr', long: 'Freitag' },
	{ value: 6, short: 'Sa', long: 'Samstag' },
	{ value: 0, short: 'So', long: 'Sonntag' }
];

export const notificationStatusLabel: Record<string, string> = {
	pending: 'Geplant',
	sending: 'Wird gesendet',
	sent: 'Gesendet',
	failed: 'Fehlgeschlagen',
	skipped: 'Übersprungen'
};

export function notificationStatusTone(s: string | undefined): Tone {
	switch (s) {
		case 'sent':
			return 'ok';
		case 'failed':
			return 'danger';
		case 'pending':
		case 'sending':
			return 'accent';
		case 'skipped':
			return 'warn';
		default:
			return 'neutral';
	}
}

export const notificationKindLabel: Record<string, string> = {
	event: 'Event',
	escalation: 'Eskalation',
	test: 'Test',
	report: 'Bericht'
};

// ---------------------------------------------------------------- event type catalog

/** Label of an event type or pattern ("port.*", "*"). */
export function eventTypeName(t: string, catalog: EventSpec[]): string {
	if (t === '*') return 'Alle Events';
	if (t.endsWith('.*')) return `Alle ${t.slice(0, -2)}-Events`;
	return catalog.find((e) => e.type === t)?.label ?? t;
}

/** Event types matched by a pattern list (for payload field suggestions). */
export function matchedTypes(patterns: string[], catalog: EventSpec[]): EventSpec[] {
	if (!patterns.length || patterns.includes('*')) return catalog;
	return catalog.filter((e) =>
		patterns.some((p) => p === e.type || (p.endsWith('.*') && e.type.startsWith(p.slice(0, -1))))
	);
}

/** Wildcard patterns derived from the catalog (one per type prefix). */
export function typePatterns(catalog: EventSpec[]): string[] {
	const out = new Set<string>();
	for (const e of catalog) {
		const i = e.type.indexOf('.');
		if (i > 0) out.add(e.type.slice(0, i) + '.*');
	}
	return [...out].sort();
}

// ---------------------------------------------------------------- summaries

export function publisherName(id: string, publishers: PublisherInfo[]): string {
	return publishers.find((p) => p.id === id)?.name ?? id;
}

/** Short German description of the conditions ("Wenn …"). */
export function conditionSummary(
	c: RuleConditions,
	catalog: EventSpec[],
	groupName?: (id: number) => string
): string[] {
	const parts: string[] = [];
	const types = c.eventTypes ?? [];
	parts.push(types.length ? types.map((t) => eventTypeName(t, catalog)).join(' oder ') : 'jedes Event');
	if (c.minSeverity) parts.push(`ab Schweregrad ${severityLabel[c.minSeverity] ?? c.minSeverity}`);
	if (c.onlyUnknown) parts.push('nur nicht bekannte Geräte');
	if (c.deviceStates?.length)
		parts.push('Gerätezustand ' + c.deviceStates.map((s) => stateLabel[s] ?? s).join('/'));
	if (c.tags?.length) parts.push((c.tags.length === 1 ? 'Tag ' : 'Tags ') + c.tags.join(', '));
	if (c.groups?.length)
		parts.push(
			(c.groups.length === 1 ? 'Gruppe ' : 'Gruppen ') +
				c.groups.map((g) => groupName?.(g) ?? `#${g}`).join(', ')
		);
	if (c.subnets?.length) parts.push('Subnetz ' + c.subnets.join(', '));
	if (c.deviceQuery) parts.push(`Filter „${c.deviceQuery}“`);
	const sym: Record<string, string> = { '>=': '≥', '<=': '≤', '!=': '≠', contains: 'enthält' };
	for (const p of c.payload ?? []) parts.push(`${p.field} ${sym[p.op] ?? p.op} ${p.value}`);
	if (c.timeWindow) {
		const days = (c.timeWindow.days ?? []).length
			? WEEKDAYS.filter((d) => c.timeWindow?.days?.includes(d.value))
					.map((d) => d.short)
					.join(', ') + ' '
			: '';
		parts.push(`${days}${c.timeWindow.from}–${c.timeWindow.to} Uhr`);
	}
	return parts;
}

/** Short German description of one action ("Telegram · Hoch · sofort"). */
export function actionSummary(a: RuleAction, publishers: PublisherInfo[]): string {
	const parts = [publisherName(a.publisher, publishers), priorityLabel[a.priority] ?? a.priority];
	parts.push(a.mode === 'batch' ? `gesammelt ${a.batchMinutes ?? 0} min` : 'sofort');
	if (a.throttle) parts.push(`höchstens alle ${durationText(a.throttle)}`);
	if (a.quietHours) parts.push(`Ruhezeit ${a.quietHours.from}–${a.quietHours.to}`);
	if (a.escalateAfterMinutes)
		parts.push(
			`Eskalation nach ${a.escalateAfterMinutes} min` +
				(a.escalatePublisher ? ` an ${publisherName(a.escalatePublisher, publishers)}` : '')
		);
	return parts.join(' · ');
}

/** "24h" → "24 h", "90m" → "90 min" (Go duration strings). */
export function durationText(d: string): string {
	return d
		.replace(/(\d+(?:\.\d+)?)h/g, '$1 h ')
		.replace(/(\d+(?:\.\d+)?)m(?!s)/g, '$1 min ')
		.replace(/(\d+(?:\.\d+)?)s/g, '$1 s ')
		.trim();
}

// ---------------------------------------------------------------- drafts

export function emptyAction(publisher = ''): RuleAction {
	return { publisher, priority: 'normal', mode: 'immediate' };
}

export function emptyRule(): Rule {
	return {
		id: 0,
		name: '',
		description: '',
		enabled: true,
		sortOrder: 0,
		stop: false,
		conditions: { eventTypes: [] },
		actions: [emptyAction()],
		createdAt: '',
		updatedAt: ''
	};
}

/** Deep copy with all optional lists present (editor state). */
export function normRule(r: Rule): Rule {
	const c = r.conditions ?? { eventTypes: [] };
	return {
		...r,
		description: r.description ?? '',
		conditions: {
			eventTypes: [...(c.eventTypes ?? [])],
			minSeverity: c.minSeverity ?? '',
			tags: [...(c.tags ?? [])],
			groups: [...(c.groups ?? [])],
			subnets: [...(c.subnets ?? [])],
			deviceQuery: c.deviceQuery ?? '',
			onlyUnknown: !!c.onlyUnknown,
			deviceStates: [...(c.deviceStates ?? [])],
			payload: (c.payload ?? []).map((p) => ({ ...p })),
			timeWindow: c.timeWindow ? { ...c.timeWindow, days: [...(c.timeWindow.days ?? [])] } : undefined
		},
		actions: (r.actions ?? []).map((a) => ({
			...a,
			quietHours: a.quietHours ? { ...a.quietHours } : undefined
		}))
	};
}

/** Rule body for the API (empty optionals removed). */
export function rulePayload(r: Rule): Rule {
	const c = r.conditions;
	const cond: RuleConditions = { eventTypes: c.eventTypes ?? [] };
	if (c.minSeverity) cond.minSeverity = c.minSeverity;
	if (c.tags?.length) cond.tags = c.tags;
	if (c.groups?.length) cond.groups = c.groups;
	if (c.subnets?.length) cond.subnets = c.subnets;
	if (c.deviceQuery?.trim()) cond.deviceQuery = c.deviceQuery.trim();
	if (c.onlyUnknown) cond.onlyUnknown = true;
	if (c.deviceStates?.length) cond.deviceStates = c.deviceStates;
	if (c.payload?.length)
		cond.payload = c.payload.map((p) => ({ field: p.field.trim(), op: p.op, value: p.value }));
	if (c.timeWindow) cond.timeWindow = { ...c.timeWindow, days: c.timeWindow.days ?? [] };
	// timestamps are read-only; an empty string would not parse as time on the server
	const { createdAt, updatedAt, ...rest } = r;
	const stamps = (createdAt ? { createdAt, updatedAt } : {}) as Pick<Rule, 'createdAt' | 'updatedAt'>;
	return {
		...rest,
		...stamps,
		name: r.name.trim(),
		conditions: cond,
		actions: r.actions.map((a) => {
			const out: RuleAction = {
				publisher: a.publisher,
				priority: a.priority || 'normal',
				mode: a.mode || 'immediate'
			};
			if (out.mode === 'batch') out.batchMinutes = Number(a.batchMinutes) || 0;
			if (a.throttle?.trim()) out.throttle = a.throttle.trim();
			if (a.quietHours) out.quietHours = { ...a.quietHours };
			if (a.escalateAfterMinutes && Number(a.escalateAfterMinutes) > 0) {
				out.escalateAfterMinutes = Number(a.escalateAfterMinutes);
				if (a.escalatePublisher) out.escalatePublisher = a.escalatePublisher;
				if (a.escalatePriority) out.escalatePriority = a.escalatePriority;
			}
			return out;
		})
	};
}

// ---------------------------------------------------------------- validation

const hm = /^([01]?\d|2[0-3]):[0-5]\d$/;
const goDuration = /^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/;

function durationMs(d: string): number {
	let total = 0;
	const re = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g;
	let m: RegExpExecArray | null;
	const f: Record<string, number> = { ns: 1e-6, us: 1e-3, µs: 1e-3, ms: 1, s: 1000, m: 60000, h: 3600000 };
	while ((m = re.exec(d))) total += parseFloat(m[1]) * f[m[2]];
	return total;
}

export type RuleErrors = Record<string, string>;

/**
 * Client side validation. Keys are the JSON paths the backend uses for its field errors
 * (e.g. name, conditions.subnets.0, conditions.payload.1.field, actions.0.throttle), so
 * server and client errors land on the same controls.
 */
export function validateRule(r: Rule): RuleErrors {
	const e: RuleErrors = {};
	if (!r.name.trim()) e.name = 'Name erforderlich';
	(r.conditions.subnets ?? []).forEach((s, i) => {
		if (!isCidr(s)) e[`conditions.subnets.${i}`] = `„${s}“ ist kein gültiges Subnetz (CIDR)`;
	});
	(r.conditions.payload ?? []).forEach((p, i) => {
		if (!p.field.trim()) e[`conditions.payload.${i}.field`] = 'Feld fehlt';
	});
	const tw = r.conditions.timeWindow;
	if (tw && !hm.test(tw.from)) e['conditions.timeWindow.from'] = 'HH:MM erwartet';
	if (tw && !hm.test(tw.to)) e['conditions.timeWindow.to'] = 'HH:MM erwartet';
	if (!r.actions.length) e.actions = 'Mindestens eine Aktion erforderlich';
	r.actions.forEach((a, i) => {
		const k = (f: string) => `actions.${i}.${f}`;
		if (!a.publisher) e[k('publisher')] = 'Publisher wählen';
		if (a.mode === 'batch') {
			const n = Number(a.batchMinutes);
			if (!Number.isInteger(n) || n < 1 || n > 1440) e[k('batchMinutes')] = '1–1440 Minuten';
		}
		if (a.throttle?.trim()) {
			const t = a.throttle.trim();
			if (!goDuration.test(t) || durationMs(t) < 60000) e[k('throttle')] = 'Dauer ≥ 1 Minute, z. B. 30m, 24h';
		}
		if (a.quietHours && !hm.test(a.quietHours.from)) e[k('quietHours.from')] = 'HH:MM erwartet';
		if (a.quietHours && !hm.test(a.quietHours.to)) e[k('quietHours.to')] = 'HH:MM erwartet';
		if (a.escalateAfterMinutes !== undefined) {
			const n = Number(a.escalateAfterMinutes);
			if (a.escalateAfterMinutes === null || !Number.isInteger(n) || n < 1 || n > 10080)
				e[k('escalateAfterMinutes')] = '1–10080 Minuten';
		}
	});
	return e;
}

/**
 * Server field messages repeat the position ("Aktion 2: …", "Payload-Bedingung 1: …");
 * next to the control that prefix is redundant.
 */
export function fieldMessage(msg: string | undefined): string | undefined {
	return msg?.replace(/^(Aktion|Payload-Bedingung) \d+:\s*/, '');
}

/** First error whose key starts with the given path prefix (e.g. "conditions.subnets."). */
export function firstError(errors: RuleErrors, prefix: string): string | undefined {
	const k = Object.keys(errors).find((x) => x === prefix || x.startsWith(prefix + '.'));
	return k ? fieldMessage(errors[k]) : undefined;
}
