// Helpers for the device detail page (/devices/[id]).
import { t, tn } from '$lib/i18n';
import { AsyncData } from '$lib/stores/resource.svelte';
import { meta } from '$lib/stores/catalog.svelte';
import type { FactView } from '$lib/api/types';
import type { Tone } from '$lib/utils/labels';

/** Tabs of the device detail page (id = ?tab= value). */
export const DEVICE_TABS = [
	'overview',
	'ports',
	'software',
	'containers',
	'certificates',
	'cves',
	'health',
	'usage',
	'traffic',
	'history',
	'relations',
	'raw'
] as const;
export type DeviceTab = (typeof DEVICE_TABS)[number];

export function isDeviceTab(v: string | null | undefined): v is DeviceTab {
	return !!v && (DEVICE_TABS as readonly string[]).includes(v);
}

/**
 * AsyncData that loads lazily: `ensure(key, fn)` only fetches when the key (e.g. the data
 * version plus filter values) differs from the last loaded one.
 */
export class LazyData<T> extends AsyncData<T> {
	#key: string | null = null;

	ensure(key: string, fn: (signal: AbortSignal) => Promise<T>) {
		if (key === this.#key) return;
		this.#key = key;
		this.run(fn);
	}

	/** Forces the next ensure() to fetch again. */
	invalidate() {
		this.#key = null;
	}
}

const staticSources: Record<string, string> = {
	manual: t('Manuell'),
	arpscan: t('ARP-Scan'),
	icmp: t('ICMP-Ping'),
	nmap: 'Nmap',
	nmap_udp: 'Nmap (UDP)',
	dns: 'DNS',
	mdns: 'mDNS',
	netbios: 'NetBIOS',
	upnp: 'UPnP',
	oui: 'OUI',
	http: 'HTTP',
	tls: 'TLS',
	snmp: 'SNMP',
	ssh: 'SSH',
	wol: 'Wake-on-LAN',
	proxmox: 'Proxmox',
	openwrt: 'OpenWrt',
	docker: 'Docker',
	netalertx: 'NetAlertX',
	csv: t('CSV-Import'),
	diff: 'Diff',
	cve: t('CVE-Abgleich'),
	healthcheck: t('Health-Check'),
	topology: t('Topologie'),
	cleanup: 'Cleanup',
	windows_dhcp: t('Windows-DHCP')
};

/** Human readable name of a data source (plugin id or "manual"). */
export function sourceName(id: string | null | undefined): string {
	if (!id) return '–';
	return staticSources[id] ?? meta.value?.scanners.find((s) => s.id === id)?.name ?? id;
}

/** Facts grouped by kind in a stable, meaningful order. */
export function groupFacts(facts: FactView[]): { kind: string; items: FactView[] }[] {
	const order = ['hostname', 'name', 'vendor', 'model', 'type', 'os', 'os_version', 'firmware', 'serial'];
	const map = new Map<string, FactView[]>();
	for (const f of facts) {
		let l = map.get(f.kind);
		if (!l) map.set(f.kind, (l = []));
		l.push(f);
	}
	const rank = (k: string) => {
		const i = order.indexOf(k);
		return i < 0 ? order.length : i;
	};
	return [...map.entries()]
		.sort((a, b) => rank(a[0]) - rank(b[0]) || a[0].localeCompare(b[0]))
		.map(([kind, items]) => ({ kind, items }));
}

/** Tone for the days until a certificate expires. */
export function daysLeftTone(days: number): Tone {
	if (days < 0) return 'critical';
	if (days <= 14) return 'high';
	if (days <= 30) return 'medium';
	return 'ok';
}

export function daysLeftText(days: number): string {
	if (days < 0) return tn(-days, 'seit {n} Tag abgelaufen', 'seit {n} Tagen abgelaufen');
	if (days === 0) return t('läuft heute ab');
	return tn(days, 'noch {n} Tag', 'noch {n} Tage');
}

export const matchTypeLabel: Record<string, string> = {
	exact: t('Exakt'),
	range: t('Versionsbereich'),
	heuristic: t('Heuristisch')
};

export function matchTypeTone(type: string): Tone {
	return type === 'exact' ? 'ok' : type === 'range' ? 'accent' : 'warn';
}

export function httpStatusTone(code: number): Tone {
	if (code >= 500) return 'danger';
	if (code >= 400) return 'warn';
	if (code >= 300) return 'info';
	if (code >= 200) return 'ok';
	return 'neutral';
}

export function containerStateTone(state: string | undefined): Tone {
	switch (state) {
		case 'running':
			return 'ok';
		case 'paused':
		case 'restarting':
			return 'warn';
		case 'dead':
			return 'danger';
		default:
			return 'neutral';
	}
}

export const containerStateLabel: Record<string, string> = {
	running: t('läuft'),
	exited: t('beendet'),
	paused: t('pausiert'),
	restarting: t('startet neu'),
	created: t('erstellt'),
	dead: t('tot'),
	removing: t('wird entfernt')
};

export const portStateLabel: Record<string, string> = {
	open: t('offen'),
	closed: t('geschlossen'),
	filtered: t('gefiltert'),
	'open|filtered': t('offen|gefiltert'),
	unfiltered: t('ungefiltert')
};

/** Query value for a group filter (quotes names with spaces). */
export function groupQuery(name: string): string {
	return /[\s"]/.test(name) ? `group:"${name.replace(/"/g, '')}"` : `group:${name}`;
}

/** Device list URL for a query. */
export function devicesHref(q: string): string {
	return '/devices?q=' + encodeURIComponent(q);
}
