// Helpers for plugin settings schemas (plugin.Schema) – used by SchemaForm and its callers.
//
//   let values = $state(schemaInitial(plugin.schema.fields, plugin.config.settings));
//   const errs = validateSchema(fields, values);           // client side check before submit
//   await api.put('/api/v1/plugins/{id}/config', { path: { id }, body: { settings: schemaPayload(fields, values) } });
import { SECRET_MASK, type SchemaField } from '$lib/api/types';

export type SchemaValues = Record<string, unknown>;

/** Empty value of a field type (mirrors plugin.zeroValue). */
export function zeroValue(f: SchemaField): unknown {
	switch (f.type) {
		case 'int':
			return 0;
		case 'bool':
			return false;
		case 'string-list':
		case 'subnet-list':
			return [];
		case 'credential-ref':
			return f.multi ? [] : 0;
		case 'enum':
			return f.multi ? [] : '';
		default:
			return '';
	}
}

/** Coerces a raw (JSON) value into the canonical client type of the field. */
export function coerce(f: SchemaField, v: unknown): unknown {
	if (v === undefined || v === null) return zeroValue(f);
	switch (f.type) {
		case 'int': {
			const n = typeof v === 'number' ? v : Number(v);
			return isFinite(n) ? n : zeroValue(f);
		}
		case 'bool':
			return v === true || v === 'true';
		case 'string-list':
		case 'subnet-list':
			if (Array.isArray(v)) return v.map(String);
			if (typeof v === 'string') return v.split(/\r?\n/).filter((s) => s.trim());
			return [];
		case 'credential-ref':
			if (f.multi) return Array.isArray(v) ? v.map(Number).filter((n) => n > 0) : [];
			return Number(v) || 0;
		case 'enum':
			if (f.multi) return Array.isArray(v) ? v.map(String) : [];
			return String(v);
		default:
			return typeof v === 'string' ? v : String(v);
	}
}

/** Default values of all fields. */
export function schemaDefaults(fields: SchemaField[]): SchemaValues {
	const out: SchemaValues = {};
	for (const f of fields) out[f.key] = coerce(f, f.default ?? zeroValue(f));
	return out;
}

/**
 * Initial form values: stored values (e.g. plugin.config.settings) over defaults, coerced.
 * `secretsSet` marks secret fields as set (credentials API: names of set secrets); they get
 * the SECRET_MASK value, which the backend treats as "unchanged".
 */
export function schemaInitial(
	fields: SchemaField[],
	stored?: Record<string, unknown> | null,
	secretsSet?: string[] | null
) {
	const out = schemaDefaults(fields);
	for (const f of fields) {
		if (stored && f.key in stored && stored[f.key] !== null && stored[f.key] !== undefined) {
			out[f.key] = coerce(f, stored[f.key]);
		}
		if (isSecret(f)) {
			const set = secretsSet?.includes(f.key) || stored?.[f.key] === SECRET_MASK;
			out[f.key] = set
				? SECRET_MASK
				: typeof out[f.key] === 'string' && out[f.key] !== SECRET_MASK
					? out[f.key]
					: '';
		}
	}
	return out;
}

export function isSecret(f: SchemaField): boolean {
	return f.type === 'secret' || !!f.secret;
}

/** visibleIf evaluation (hidden fields are neither validated nor required). */
export function isVisible(f: SchemaField, values: SchemaValues, fields: SchemaField[]): boolean {
	const c = f.visibleIf;
	if (!c) return true;
	const dep = fields.find((x) => x.key === c.field);
	let cur = values[c.field];
	if (cur === undefined && dep) cur = dep.default;
	return (c.equals ?? []).some((e) => String(e) === String(cur));
}

const durationRe = /^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/;
const ipv4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;

/** true for an IPv4/IPv6 address or CIDR prefix */
export function isCidr(s: string): boolean {
	const [addr, bits, ...rest] = s.trim().split('/');
	if (rest.length) return false;
	if (ipv4.test(addr)) return bits === undefined || (/^\d+$/.test(bits) && Number(bits) <= 32);
	if (addr.includes(':') && /^[0-9a-fA-F:.]+$/.test(addr))
		return bits === undefined || (/^\d+$/.test(bits) && Number(bits) <= 128);
	return false;
}

function checkFormat(format: string | undefined, s: string): string | null {
	switch (format) {
		case 'url':
			try {
				const u = new URL(s);
				if (u.protocol !== 'http:' && u.protocol !== 'https:') return 'nur http:// oder https:// erlaubt';
			} catch {
				return 'gültige URL erwartet (z. B. https://host/pfad)';
			}
			return null;
		case 'email':
			return /^[^\s@]+@[^\s@]+$/.test(s) ? null : 'E-Mail-Adresse erwartet';
		case 'ip':
			return ipv4.test(s) || (s.includes(':') && /^[0-9a-fA-F:.]+$/.test(s)) ? null : 'IP-Adresse erwartet';
		case 'hostport':
			return /^.+:\d{1,5}$/.test(s) ? null : 'host:port erwartet';
		case 'mac':
			return /^([0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}$/.test(s) ? null : 'MAC-Adresse erwartet';
		case 'header':
			return /^[^\s:]+\s*:.*/.test(s) ? null : 'Format "Name: Wert" erwartet';
		case 'path':
			return s.startsWith('/') || /^[a-zA-Z]:/.test(s) ? null : 'absoluter Pfad erwartet';
	}
	return null;
}

function isEmpty(v: unknown): boolean {
	if (v === null || v === undefined) return true;
	if (typeof v === 'string') return v.trim() === '';
	if (Array.isArray(v)) return v.length === 0;
	if (typeof v === 'number') return false;
	return false;
}

/**
 * Client side validation (the backend validates again and returns field errors).
 * Returns field key → message for visible fields only.
 */
export function validateSchema(fields: SchemaField[], values: SchemaValues): Record<string, string> {
	const errs: Record<string, string> = {};
	for (const f of fields) {
		if (!isVisible(f, values, fields)) continue;
		const v = values[f.key];
		const val = f.validation;
		if (f.required && f.type !== 'bool' && f.type !== 'int') {
			const empty = f.type === 'credential-ref' && !f.multi ? !v : isEmpty(v);
			if (empty && !(isSecret(f) && v === SECRET_MASK)) {
				errs[f.key] = 'Pflichtfeld';
				continue;
			}
		}
		switch (f.type) {
			case 'int': {
				if (v === null || v === '' || v === undefined) {
					if (f.required) errs[f.key] = 'Pflichtfeld';
					break;
				}
				const n = Number(v);
				if (!Number.isInteger(n)) errs[f.key] = 'Ganzzahl erwartet';
				else if (val?.min !== undefined && n < val.min) errs[f.key] = `muss mindestens ${val.min} sein`;
				else if (val?.max !== undefined && n > val.max) errs[f.key] = `darf höchstens ${val.max} sein`;
				break;
			}
			case 'string':
			case 'secret': {
				if (typeof v !== 'string' || v === '' || v === SECRET_MASK) break;
				const len = [...v].length;
				if (val?.min !== undefined && len < val.min) errs[f.key] = `mindestens ${val.min} Zeichen`;
				else if (val?.max !== undefined && len > val.max) errs[f.key] = `höchstens ${val.max} Zeichen`;
				else if (val?.pattern && !new RegExp(val.pattern).test(v))
					errs[f.key] = 'entspricht nicht dem erwarteten Muster';
				else {
					const fe = checkFormat(val?.format, f.multiline ? v : v.trim());
					if (fe) errs[f.key] = fe;
				}
				break;
			}
			case 'duration':
				if (typeof v === 'string' && v.trim() && !durationRe.test(v.trim()))
					errs[f.key] = 'Dauer erwartet (z. B. 30s, 5m, 2h)';
				break;
			case 'string-list':
			case 'subnet-list': {
				const list = (Array.isArray(v) ? v : []) as string[];
				if (val?.min !== undefined && list.length < val.min) errs[f.key] = `mindestens ${val.min} Einträge`;
				else if (val?.max !== undefined && list.length > val.max)
					errs[f.key] = `höchstens ${val.max} Einträge`;
				for (const it of list) {
					if (errs[f.key]) break;
					if (f.type === 'subnet-list' && !isCidr(it)) errs[f.key] = `ungültiges Subnetz „${it}“`;
					else if (val?.pattern && !new RegExp(val.pattern).test(it))
						errs[f.key] = `„${it}“ entspricht nicht dem Muster`;
					else {
						const fe = checkFormat(val?.format, it);
						if (fe) errs[f.key] = `„${it}“: ${fe}`;
					}
				}
				break;
			}
		}
	}
	return errs;
}

/**
 * Values ready for the API: lists trimmed, empty ints as null (= default), secrets as entered
 * (SECRET_MASK keeps the stored secret, "" clears it).
 */
export function schemaPayload(fields: SchemaField[], values: SchemaValues): SchemaValues {
	const out: SchemaValues = {};
	for (const f of fields) {
		const v = values[f.key];
		switch (f.type) {
			case 'int':
				out[f.key] = v === '' || v === null || v === undefined || !isFinite(Number(v)) ? null : Number(v);
				break;
			case 'string-list':
			case 'subnet-list':
				out[f.key] = ((Array.isArray(v) ? v : []) as string[]).map((s) => s.trim()).filter(Boolean);
				break;
			case 'credential-ref':
				out[f.key] = f.multi ? ((Array.isArray(v) ? v : []) as number[]).map(Number) : Number(v) || 0;
				break;
			default:
				out[f.key] = v === undefined ? null : v;
		}
	}
	return out;
}

/** Fields grouped by `group` (order of first appearance, ungrouped first). */
export function groupFields(fields: SchemaField[]): { name: string; fields: SchemaField[] }[] {
	const groups: { name: string; fields: SchemaField[] }[] = [];
	for (const f of fields) {
		const name = f.group ?? '';
		let g = groups.find((x) => x.name === name);
		if (!g) {
			g = { name, fields: [] };
			if (name === '') groups.unshift(g);
			else groups.push(g);
		}
		g.fields.push(f);
	}
	return groups;
}
