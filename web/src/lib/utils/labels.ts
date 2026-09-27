// Labels (in the UI language) and colour tones for enum values used across the UI.
import type { Severity } from '$lib/api/types';
import { t } from '$lib/i18n';

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
	low: t('Niedrig'),
	medium: t('Mittel'),
	high: t('Hoch'),
	critical: t('Kritisch')
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
	known: t('Bekannt'),
	unknown: t('Unbekannt'),
	ignored: t('Ignoriert')
};

export function stateTone(s: string | undefined | null): Tone {
	return s === 'known' ? 'ok' : s === 'unknown' ? 'unknown' : 'neutral';
}

export const criticalityLabel: Record<string, string> = {
	low: t('Niedrig'),
	normal: 'Normal',
	high: t('Hoch'),
	critical: t('Kritisch')
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
	queued: t('Wartend'),
	running: t('Läuft'),
	success: t('Erfolgreich'),
	failed: t('Fehlgeschlagen'),
	timeout: t('Zeitüberschreitung'),
	cancelled: t('Abgebrochen')
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
	schedule: t('Zeitplan'),
	manual: t('Manuell'),
	device: t('Gerät'),
	retry: t('Wiederholung'),
	action: t('Aktion'),
	hook: t('Folgelauf')
};

export const healthStateLabel: Record<string, string> = {
	up: 'Up',
	down: 'Down',
	degraded: t('Beeinträchtigt'),
	unknown: t('Unbekannt')
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
	phone: t('Telefon'),
	tablet: 'Tablet',
	tv: 'TV',
	'media-player': t('Mediaplayer'),
	speaker: t('Lautsprecher'),
	printer: t('Drucker'),
	camera: t('Kamera'),
	'smart-home': 'Smart Home',
	iot: 'IoT',
	'game-console': t('Spielkonsole'),
	ups: t('USV'),
	other: t('Sonstiges')
};

export function deviceTypeName(t: string | undefined | null): string {
	if (!t) return '–';
	return deviceTypeLabel[t] ?? t;
}

export const relationKindLabel: Record<string, string> = {
	switch_port: 'Switch-Port',
	lldp: t('LLDP-Nachbar'),
	l3: t('Routing (L3)'),
	runs_on: t('Läuft auf'),
	manual: t('Manuell'),
	container: 'Container',
	wireless: t('WLAN')
};

export const diffKindLabel: Record<string, string> = {
	device: t('Gerät'),
	ip: t('IP-Adresse'),
	mac: t('MAC-Adresse'),
	port: 'Port',
	cert: t('Zertifikat'),
	http: 'HTTP',
	package: t('Paket'),
	container: 'Container',
	hostname: 'Hostname',
	os: t('Betriebssystem'),
	vendor: t('Hersteller'),
	type: t('Typ'),
	model: t('Modell')
};

export const diffChangeLabel: Record<string, string> = {
	added: t('Neu'),
	removed: t('Entfernt'),
	changed: t('Geändert')
};

export const eventCategoryLabel: Record<string, string> = {
	device: t('Geräte'),
	port: t('Ports & Dienste'),
	cert: t('Zertifikate'),
	container: 'Container',
	software: 'Software',
	vulnerability: t('Schwachstellen'),
	health: 'Health',
	network: t('Netzwerk'),
	system: 'System'
};

export const factKindLabel: Record<string, string> = {
	hostname: 'Hostname',
	vendor: t('Hersteller'),
	model: t('Modell'),
	type: t('Typ'),
	os: t('Betriebssystem'),
	os_version: t('OS-Version'),
	name: 'Name',
	serial: t('Seriennummer'),
	firmware: 'Firmware',
	description: t('Beschreibung')
};

export const customFieldTypeLabel: Record<string, string> = {
	text: 'Text',
	number: t('Zahl'),
	date: t('Datum'),
	url: 'URL',
	bool: t('Ja/Nein')
};

/** Label lookup with fallback to the raw value. */
export function label(map: Record<string, string>, v: string | undefined | null, fallback = '–'): string {
	if (v === undefined || v === null || v === '') return fallback;
	return map[v] ?? v;
}
