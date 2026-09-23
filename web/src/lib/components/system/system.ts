// Helpers for the system pages: tab catalogue, labels, validation and a JSON diff.
import { errorMessage, fieldErrors } from '$lib/api';
import type { IconName } from '$lib/components/ui';
import type { Tone } from '$lib/utils/labels';

export interface SystemTab {
	id: string;
	label: string;
	icon: IconName;
	description: string;
}

export const SYSTEM_TABS: SystemTab[] = [
	{
		id: 'overview',
		label: 'Überblick',
		icon: 'monitor',
		description: 'Version, Laufzeit, Datenbank und Umgebung'
	},
	{
		id: 'settings',
		label: 'Einstellungen',
		icon: 'system',
		description: 'Systemweite Einstellungen und Log-Level'
	},
	{ id: 'account', label: 'Konto', icon: 'user', description: 'Passwort des Administrators ändern' },
	{ id: 'tokens', label: 'API-Tokens', icon: 'key', description: 'Tokens für Skripte und Automatisierung' },
	{
		id: 'subnets',
		label: 'Subnetze',
		icon: 'network',
		description: 'Netze, die gescannt und zugeordnet werden'
	},
	{ id: 'groups', label: 'Gruppen', icon: 'layers', description: 'Manuelle und regelbasierte Gerätegruppen' },
	{ id: 'fields', label: 'Custom Fields', icon: 'tag', description: 'Eigene Geräteattribute' },
	{ id: 'backups', label: 'Backups', icon: 'disk', description: 'Datenbank sichern und wiederherstellen' },
	{ id: 'vault', label: 'Vault', icon: 'lock', description: 'Master-Key der verschlüsselten Credentials' },
	{ id: 'logs', label: 'Log-Viewer', icon: 'terminal', description: 'Anwendungsprotokoll mit Live-Ansicht' },
	{ id: 'audit', label: 'Audit-Log', icon: 'history', description: 'Protokoll aller manuellen Änderungen' }
];

// ---------------------------------------------------------------- log levels

export const LOG_LEVELS = ['debug', 'info', 'warn', 'error'] as const;

export const logLevelLabel: Record<string, string> = {
	debug: 'Debug',
	info: 'Info',
	warn: 'Warnung',
	error: 'Fehler'
};

const levelRank: Record<string, number> = { debug: 0, info: 1, warn: 2, error: 3 };

/** Numeric rank of a slog level string ("INFO", "WARN", "DEBUG-4" …). */
export function logRank(level: string): number {
	const l = (level ?? '').toLowerCase();
	for (const k of ['error', 'warn', 'info', 'debug']) if (l.startsWith(k)) return levelRank[k];
	return 1;
}

export function logTone(level: string): Tone {
	const r = logRank(level);
	return r >= 3 ? 'danger' : r === 2 ? 'warn' : r === 1 ? 'info' : 'neutral';
}

// ---------------------------------------------------------------- audit

export const entityLabel: Record<string, string> = {
	device: 'Gerät',
	credential: 'Credential',
	customfield: 'Custom Field',
	group: 'Gruppe',
	subnet: 'Subnetz',
	healthcheck: 'Health-Check',
	plugin: 'Plugin',
	rule: 'Regel',
	run: 'Lauf',
	event: 'Event',
	report: 'Bericht',
	backup: 'Backup',
	settings: 'Einstellungen',
	token: 'API-Token',
	user: 'Benutzer',
	relation: 'Topologie-Kante',
	file: 'Datei',
	vault: 'Vault',
	view: 'Ansicht'
};

/** Link to the UI page of an audited entity (null if none). */
export function entityHref(type: string, id: string): string | null {
	if (!id) return null;
	switch (type) {
		case 'device':
			return `/devices/${id}`;
		case 'plugin':
			return `/plugins/${id}`;
		case 'healthcheck':
			return `/health?check=${id}`;
		case 'rule':
			return `/rules`;
		case 'credential':
			return `/credentials`;
		default:
			return null;
	}
}

export type DiffKind = 'added' | 'removed' | 'changed' | 'same';

export interface DiffRow {
	path: string;
	before: unknown;
	after: unknown;
	kind: DiffKind;
}

function flatten(v: unknown, prefix: string, out: Map<string, unknown>) {
	if (v !== null && typeof v === 'object' && !Array.isArray(v)) {
		const entries = Object.entries(v as Record<string, unknown>);
		if (!entries.length && prefix) out.set(prefix, {});
		for (const [k, x] of entries) flatten(x, prefix ? `${prefix}.${k}` : k, out);
		return;
	}
	out.set(prefix || '(Wert)', v);
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/** Flat before/after comparison of two JSON values (arrays are compared as a whole). */
export function jsonDiff(before: unknown, after: unknown): DiffRow[] {
	const a = new Map<string, unknown>();
	const b = new Map<string, unknown>();
	if (before !== undefined && before !== null) flatten(before, '', a);
	if (after !== undefined && after !== null) flatten(after, '', b);
	const keys = [...new Set([...a.keys(), ...b.keys()])];
	return keys.map((path) => {
		const hasA = a.has(path);
		const hasB = b.has(path);
		const kind: DiffKind = !hasA
			? 'added'
			: !hasB
				? 'removed'
				: same(a.get(path), b.get(path))
					? 'same'
					: 'changed';
		return { path, before: a.get(path), after: b.get(path), kind };
	});
}

export function fmtValue(v: unknown): string {
	if (v === undefined) return '';
	if (typeof v === 'string') return v === '' ? '""' : v;
	return JSON.stringify(v);
}

// ---------------------------------------------------------------- validation helpers

const ipv4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;

export function isIPv4(s: string): boolean {
	return ipv4.test(s);
}

export function isIP(s: string): boolean {
	return isIPv4(s) || (s.includes(':') && /^[0-9a-fA-F:.]+$/.test(s));
}

/** Validates "a.b.c.d/nn" or an IPv6 prefix; returns an error message or null. */
export function cidrError(s: string): string | null {
	const v = s.trim();
	const [addr, bits, extra] = v.split('/');
	if (!addr || bits === undefined || extra !== undefined)
		return 'CIDR-Notation erwartet, z. B. 192.168.1.0/24';
	const n = Number(bits);
	if (isIPv4(addr)) {
		if (!Number.isInteger(n) || n < 0 || n > 32) return 'Präfixlänge 0–32';
		if (n < 16) return 'Maximal /16 (65.536 Adressen)';
		return null;
	}
	if (isIP(addr)) return Number.isInteger(n) && n >= 0 && n <= 128 ? null : 'Präfixlänge 0–128';
	return 'Ungültige IP-Adresse';
}

/** Whether an IPv4 address lies in an IPv4 CIDR. */
export function ipInCidr(ip: string, cidr: string): boolean {
	const [addr, bits] = cidr.split('/');
	if (!isIPv4(ip) || !isIPv4(addr)) return true; // let the server decide for IPv6
	const toNum = (x: string) => x.split('.').reduce((a, o) => a * 256 + Number(o), 0);
	const shift = 2 ** (32 - Number(bits));
	return Math.floor(toNum(ip) / shift) === Math.floor(toNum(addr) / shift);
}

/**
 * Splits a failed save into field errors for the given form fields (API field names as in
 * the JSON body; `rename` maps them to form names) and a general message for everything
 * else (errors without field list or for fields the form does not show).
 */
export function apiErrors(
	e: unknown,
	fields: string[],
	rename: Record<string, string> = {}
): { errors: Record<string, string>; general: string | null } {
	const errors: Record<string, string> = {};
	const rest: string[] = [];
	for (const [k, msg] of Object.entries(fieldErrors(e))) {
		const f = rename[k] ?? k;
		if (fields.includes(f)) errors[f] = msg;
		else rest.push(msg);
	}
	const general = rest.length ? rest.join(' · ') : Object.keys(errors).length ? null : errorMessage(e);
	return { errors, general };
}

/** Quotes a query value if needed: group:"Mein Netz". */
export function queryValue(v: string): string {
	return /[\s"]/.test(v) ? `"${v.replace(/"/g, '\\"')}"` : v;
}
