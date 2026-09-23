// German labels and colour tones for enum values used across the UI.
import type { Severity } from '$lib/api/types';

/** Badge tones (see Badge.svelte). */
export type Tone =
	| 'neutral'
	| 'accent'
	| 'ok'
	| 'warn'
	| 'danger'
	| 'unknown'
	| 'info'
	| 'low'
	| 'medium'
	| 'high'
	| 'critical';

export const severityLabel: Record<string, string> = {
	info: 'Info',
	low: 'Niedrig',
	medium: 'Mittel',
	high: 'Hoch',
	critical: 'Kritisch'
};

export const severityRank: Record<string, number> = { info: 0, low: 1, medium: 2, high: 3, critical: 4 };

export function severityTone(s: string | undefined | null): Tone {
	return s && s in severityRank ? (s as Severity) : 'neutral';
}

/** CVSS base score → severity (same thresholds as the backend). */
export function severityFromCvss(score: number | null | undefined): Severity {
	if (!score || score <= 0) return 'info';
	if (score >= 9) return 'critical';
	if (score >= 7) return 'high';
	if (score >= 4) return 'medium';
	return 'low';
}

export const stateLabel: Record<string, string> = {
	known: 'Bekannt',
	unknown: 'Unbekannt',
	ignored: 'Ignoriert'
};

export function stateTone(s: string | undefined | null): Tone {
	return s === 'known' ? 'ok' : s === 'unknown' ? 'unknown' : 'neutral';
}

export const criticalityLabel: Record<string, string> = {
	low: 'Niedrig',
	normal: 'Normal',
	high: 'Hoch',
	critical: 'Kritisch'
};
export const CRITICALITIES = ['low', 'normal', 'high', 'critical'] as const;

export function criticalityTone(c: string | undefined | null): Tone {
	return c === 'critical' ? 'critical' : c === 'high' ? 'high' : c === 'low' ? 'info' : 'neutral';
}

export const pluginKindLabel: Record<string, string> = {
	scanner: 'Scanner',
	importer: 'Importer',
	processor: 'Processor',
	publisher: 'Publisher'
};

export const runStatusLabel: Record<string, string> = {
	queued: 'Wartend',
	running: 'Läuft',
	success: 'Erfolgreich',
	failed: 'Fehlgeschlagen',
	timeout: 'Zeitüberschreitung',
	cancelled: 'Abgebrochen'
};

export function runStatusTone(s: string | undefined | null): Tone {
	switch (s) {
		case 'success':
			return 'ok';
		case 'running':
		case 'queued':
			return 'accent';
		case 'failed':
		case 'timeout':
			return 'danger';
		default:
			return 'neutral';
	}
}

export const runTriggerLabel: Record<string, string> = {
	schedule: 'Zeitplan',
	manual: 'Manuell',
	device: 'Gerät',
	retry: 'Wiederholung',
	action: 'Aktion',
	hook: 'Folgelauf'
};

export const healthStateLabel: Record<string, string> = {
	up: 'Up',
	down: 'Down',
	degraded: 'Beeinträchtigt',
	unknown: 'Unbekannt'
};

export function healthTone(s: string | undefined | null): Tone {
	return s === 'up' ? 'ok' : s === 'down' ? 'danger' : s === 'degraded' ? 'warn' : 'neutral';
}

export const deviceTypeLabel: Record<string, string> = {
	router: 'Router',
	switch: 'Switch',
	'access-point': 'Access Point',
	firewall: 'Firewall',
	server: 'Server',
	hypervisor: 'Hypervisor',
	vm: 'VM',
	container: 'Container',
	nas: 'NAS',
	desktop: 'Desktop',
	laptop: 'Laptop',
	phone: 'Telefon',
	tablet: 'Tablet',
	tv: 'TV',
	'media-player': 'Mediaplayer',
	speaker: 'Lautsprecher',
	printer: 'Drucker',
	camera: 'Kamera',
	'smart-home': 'Smart Home',
	iot: 'IoT',
	'game-console': 'Spielkonsole',
	ups: 'USV',
	other: 'Sonstiges'
};

export function deviceTypeName(t: string | undefined | null): string {
	if (!t) return '–';
	return deviceTypeLabel[t] ?? t;
}

export const relationKindLabel: Record<string, string> = {
	switch_port: 'Switch-Port',
	lldp: 'LLDP-Nachbar',
	l3: 'Routing (L3)',
	runs_on: 'Läuft auf',
	manual: 'Manuell',
	container: 'Container',
	wireless: 'WLAN'
};

export const diffKindLabel: Record<string, string> = {
	device: 'Gerät',
	ip: 'IP-Adresse',
	mac: 'MAC-Adresse',
	port: 'Port',
	cert: 'Zertifikat',
	http: 'HTTP',
	package: 'Paket',
	container: 'Container',
	hostname: 'Hostname',
	os: 'Betriebssystem',
	vendor: 'Hersteller',
	type: 'Typ',
	model: 'Modell'
};

export const diffChangeLabel: Record<string, string> = {
	added: 'Neu',
	removed: 'Entfernt',
	changed: 'Geändert'
};

export const eventCategoryLabel: Record<string, string> = {
	device: 'Geräte',
	port: 'Ports & Dienste',
	cert: 'Zertifikate',
	container: 'Container',
	software: 'Software',
	vulnerability: 'Schwachstellen',
	health: 'Health',
	system: 'System'
};

export const factKindLabel: Record<string, string> = {
	hostname: 'Hostname',
	vendor: 'Hersteller',
	model: 'Modell',
	type: 'Typ',
	os: 'Betriebssystem',
	os_version: 'OS-Version',
	name: 'Name',
	serial: 'Seriennummer',
	firmware: 'Firmware',
	description: 'Beschreibung'
};

export const customFieldTypeLabel: Record<string, string> = {
	text: 'Text',
	number: 'Zahl',
	date: 'Datum',
	url: 'URL',
	bool: 'Ja/Nein'
};

/** Label lookup with fallback to the raw value. */
export function label(map: Record<string, string>, v: string | undefined | null, fallback = '–'): string {
	if (v === undefined || v === null || v === '') return fallback;
	return map[v] ?? v;
}
