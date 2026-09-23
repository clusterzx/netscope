// Helpers for the health check pages: labels, ordering, form model and validation
// (mirrors healthcheck.Check.Validate in the backend).
import type { HealthCheck, HealthCheckConfig } from '$lib/api';
import type { Tone } from '$lib/utils/labels';

export const CHECK_TYPES = ['tcp', 'http', 'tls', 'icmp'] as const;
export type CheckType = (typeof CHECK_TYPES)[number];

export const checkTypeLabel: Record<string, string> = {
	tcp: 'TCP',
	http: 'HTTP',
	tls: 'TLS',
	icmp: 'ICMP'
};

export const checkTypeDescription: Record<string, string> = {
	tcp: 'Verbindungsaufbau zu einem Port',
	http: 'HTTP-Status und optional Body-Inhalt',
	tls: 'TLS-Handshake und Zertifikatslaufzeit',
	icmp: 'Ping (Paketverlust und Laufzeit)'
};

/** Display order of the board sections. */
export const STATE_ORDER = ['down', 'degraded', 'unknown', 'up'] as const;

export const stateSectionLabel: Record<string, string> = {
	down: 'Down',
	degraded: 'Beeinträchtigt',
	unknown: 'Unbekannt',
	up: 'Up',
	disabled: 'Deaktiviert'
};

/** Tailwind classes for the coloured left edge of a tile. */
export const stateEdge: Record<string, string> = {
	down: 'border-l-danger',
	degraded: 'border-l-warn',
	unknown: 'border-l-unknown',
	up: 'border-l-ok',
	disabled: 'border-l-border-strong'
};

/** Board section of a check (disabled checks are listed separately). */
export function sectionOf(c: HealthCheck): string {
	if (!c.enabled) return 'disabled';
	return (STATE_ORDER as readonly string[]).includes(c.state) ? c.state : 'unknown';
}

/** Colour tone of an availability percentage. */
export function availabilityTone(v: number | null | undefined): Tone {
	if (v === null || v === undefined) return 'neutral';
	if (v >= 99.9) return 'ok';
	if (v >= 99) return 'warn';
	return 'danger';
}

export const availabilityText: Record<string, string> = {
	ok: 'text-ok',
	warn: 'text-warn',
	danger: 'text-danger',
	neutral: 'text-fg-subtle'
};

/** "host:port" / URL text of a check target. */
export function targetText(c: HealthCheck): string {
	if (c.type === 'http' && c.config?.url) return c.config.url;
	const host = c.target || (c.deviceId ? 'Geräte-IP' : '–');
	if (c.type === 'icmp' || !c.port) return host;
	return `${host}:${c.port}`;
}

/** Default port per type. */
export function defaultPort(t: string): number | null {
	return t === 'http' ? 80 : t === 'tls' ? 443 : null;
}

// ---------------------------------------------------------------- form model

export interface CheckForm {
	name: string;
	deviceId: number;
	deviceName: string;
	type: CheckType;
	target: string;
	port: number | null;
	intervalSeconds: number | null;
	timeoutSeconds: number | null;
	failThreshold: number | null;
	recoverThreshold: number | null;
	enabled: boolean;
	// type specific
	url: string;
	method: string;
	expectStatus: string;
	bodyMatch: string;
	verifyTls: boolean;
	followRedirects: boolean;
	serverName: string;
	minDays: number | null;
	count: number | null;
	degradedMs: number | null;
}

export function emptyForm(): CheckForm {
	return {
		name: '',
		deviceId: 0,
		deviceName: '',
		type: 'tcp',
		target: '',
		port: null,
		intervalSeconds: 60,
		timeoutSeconds: 10,
		failThreshold: 3,
		recoverThreshold: 2,
		enabled: true,
		url: '',
		method: 'GET',
		expectStatus: '200-399',
		bodyMatch: '',
		verifyTls: false,
		followRedirects: true,
		serverName: '',
		minDays: 14,
		count: 3,
		degradedMs: null
	};
}

export function formFromCheck(c: HealthCheck): CheckForm {
	const cfg = c.config ?? {};
	return {
		name: c.name,
		deviceId: c.deviceId ?? 0,
		deviceName: c.deviceName ?? '',
		type: (CHECK_TYPES as readonly string[]).includes(c.type) ? (c.type as CheckType) : 'tcp',
		target: c.target ?? '',
		port: c.port || null,
		intervalSeconds: c.intervalSeconds,
		timeoutSeconds: c.timeoutSeconds,
		failThreshold: c.failThreshold,
		recoverThreshold: c.recoverThreshold,
		enabled: c.enabled,
		url: cfg.url ?? '',
		method: cfg.method || 'GET',
		expectStatus: cfg.expectStatus ?? '',
		bodyMatch: cfg.bodyMatch ?? '',
		verifyTls: !!cfg.verifyTls,
		followRedirects: !!cfg.followRedirects,
		serverName: cfg.serverName ?? '',
		minDays: cfg.minDays ?? null,
		count: cfg.count ?? null,
		degradedMs: cfg.degradedMs ?? null
	};
}

const num = (v: number | null | undefined) => (v === null || v === undefined || !isFinite(v) ? 0 : v);

/** Request body for POST/PUT /api/v1/health-checks (only the fields of the chosen type). */
export function payloadFromForm(f: CheckForm): Partial<HealthCheck> {
	const config: HealthCheckConfig = {};
	if (f.type === 'http') {
		if (f.url.trim()) config.url = f.url.trim();
		config.method = f.method || 'GET';
		if (f.expectStatus.trim()) config.expectStatus = f.expectStatus.trim();
		if (f.bodyMatch) config.bodyMatch = f.bodyMatch;
		config.verifyTls = f.verifyTls;
		config.followRedirects = f.followRedirects;
	} else if (f.type === 'tls') {
		if (f.serverName.trim()) config.serverName = f.serverName.trim();
		config.verifyTls = f.verifyTls;
		if (num(f.minDays) > 0) config.minDays = num(f.minDays);
	} else if (f.type === 'icmp') {
		config.count = num(f.count) || 3;
	}
	if (num(f.degradedMs) > 0) config.degradedMs = num(f.degradedMs);
	const usesPort = f.type === 'tcp' || f.type === 'tls' || (f.type === 'http' && !f.url.trim());
	return {
		name: f.name.trim(),
		deviceId: f.deviceId || undefined,
		type: f.type,
		target: f.target.trim(),
		port: usesPort ? num(f.port) : 0,
		config,
		intervalSeconds: num(f.intervalSeconds),
		timeoutSeconds: num(f.timeoutSeconds),
		failThreshold: num(f.failThreshold),
		recoverThreshold: num(f.recoverThreshold),
		enabled: f.enabled
	};
}

const statusRe = /^\s*\d{3}(\s*-\s*\d{3})?(\s*,\s*\d{3}(\s*-\s*\d{3})?)*\s*$/;

function inRange(v: number | null, lo: number, hi: number) {
	return v !== null && isFinite(v) && Number.isInteger(v) && v >= lo && v <= hi;
}

/** Client side validation; keys are form field names. */
export function validateForm(f: CheckForm): Record<string, string> {
	const e: Record<string, string> = {};
	if (!f.name.trim()) e.name = 'Name erforderlich';
	const url = f.url.trim();
	if (f.type === 'http' && url) {
		try {
			const u = new URL(url);
			if ((u.protocol !== 'http:' && u.protocol !== 'https:') || !u.host)
				e.url = 'URL muss mit http:// oder https:// beginnen';
		} catch {
			e.url = 'URL muss mit http:// oder https:// beginnen';
		}
	}
	const usesPort = f.type === 'tcp' || f.type === 'tls' || (f.type === 'http' && !url);
	if (usesPort && !inRange(f.port, 1, 65535))
		e.port = f.type === 'http' ? 'Port 1–65535 oder eine URL angeben' : 'Port 1–65535 erforderlich';
	if (f.type === 'http') {
		if (f.expectStatus.trim() && !statusRe.test(f.expectStatus))
			e.expectStatus = 'z. B. 200-399 oder 200,204';
		else if (f.expectStatus.trim()) {
			for (const part of f.expectStatus.split(',')) {
				const [a, b] = part.split('-').map((x) => parseInt(x, 10));
				if (a < 100 || (b ?? a) > 599 || (b !== undefined && a > b)) e.expectStatus = 'Statuscodes 100–599';
			}
		}
		if (f.bodyMatch) {
			try {
				new RegExp(f.bodyMatch);
			} catch {
				e.bodyMatch = 'Ungültiger regulärer Ausdruck';
			}
		}
	}
	if (f.type === 'icmp' && f.count !== null && !inRange(f.count, 1, 20)) e.count = 'Anzahl Pings 1–20';
	if (f.type === 'tls' && f.minDays !== null && !inRange(f.minDays, 0, 3650)) e.minDays = '0–3650 Tage';
	if (f.degradedMs !== null && !inRange(f.degradedMs, 0, 600000)) e.degradedMs = 'Nicht negativ';
	const target = f.target.trim();
	if (!target && !f.deviceId && !(f.type === 'http' && url))
		e.target = 'Ziel (Host/IP) oder Gerät erforderlich';
	if (target && /[\s/]/.test(target)) e.target = 'Ziel muss ein Hostname oder eine IP sein';
	if (!inRange(f.intervalSeconds, 30, 86400)) e.intervalSeconds = '30 Sekunden bis 24 Stunden';
	if (!inRange(f.timeoutSeconds, 1, 120)) e.timeoutSeconds = '1–120 Sekunden';
	if (!inRange(f.failThreshold, 1, 20)) e.failThreshold = '1–20';
	if (!inRange(f.recoverThreshold, 1, 20)) e.recoverThreshold = '1–20';
	return e;
}

/** API field errors (keys as in the JSON body, e.g. "config.url") → form field names. */
export function formErrors(fields: Record<string, string>): Record<string, string> {
	const out: Record<string, string> = {};
	for (const [k, v] of Object.entries(fields)) out[k.startsWith('config.') ? k.slice(7) : k] = v;
	return out;
}

/** Fields that live in the collapsible "Intervall & Schwellwerte" section. */
export const ADVANCED_FIELDS = ['intervalSeconds', 'timeoutSeconds', 'failThreshold', 'recoverThreshold'];

// ---------------------------------------------------------------- SSE (topic "health")

/** type "state": a check changed its state. */
export interface HealthStateMessage {
	checkId: number;
	state: string;
	previousState: string;
	deviceId?: number;
	latencyMs?: number;
	pluginId?: string;
}

/** type "round": a check round ran checks. */
export interface HealthRoundMessage {
	checks: number;
	failed: number;
	stateChanges: number;
}

/** Interval in seconds → "60 s", "5 min", "1 h". */
export function intervalText(s: number): string {
	if (s % 3600 === 0) return `${s / 3600} h`;
	if (s % 60 === 0) return `${s / 60} min`;
	return `${s} s`;
}
