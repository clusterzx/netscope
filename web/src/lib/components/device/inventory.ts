// Formatting helpers for structured inventory data (GET /devices/{id}/inventory).
import { formatBytes, formatDateTime, formatNumber, formatSeconds } from '$lib/utils/format';

const labels: Record<string, string> = {
	apiVersion: 'API-Version',
	architecture: 'Architektur',
	arch: 'Architektur',
	bootTime: 'Boot-Zeitpunkt',
	lastBoot: 'Letzter Boot',
	composeProjects: 'Compose-Projekte',
	containers: 'Container',
	containersRunning: 'Container (laufend)',
	containersPaused: 'Container (pausiert)',
	containersStopped: 'Container (gestoppt)',
	cpus: 'CPUs',
	cpu: 'CPU',
	description: 'Beschreibung',
	descr: 'Beschreibung',
	distance: 'Netzwerk-Distanz (Hops)',
	endpoint: 'Endpunkt',
	engineVersion: 'Engine-Version',
	images: 'Images',
	interfaces: 'Interfaces',
	kernel: 'Kernel',
	location: 'Aufstellort',
	contact: 'Kontakt',
	memory: 'Arbeitsspeicher',
	model: 'Modell',
	name: 'Name',
	names: 'Namen',
	os: 'Betriebssystem',
	osType: 'OS-Typ',
	osMatches: 'OS-Kandidaten',
	accuracy: 'Genauigkeit (%)',
	platform: 'Plattform',
	rootDir: 'Datenverzeichnis',
	storageDriver: 'Storage-Treiber',
	swarm: 'Swarm',
	services: 'Dienste',
	serial: 'Seriennummer',
	status: 'Status',
	state: 'Zustand',
	type: 'Typ',
	uptimeSeconds: 'Uptime',
	vendor: 'Hersteller',
	version: 'Version',
	macs: 'MAC-Adressen',
	mac: 'MAC',
	ip: 'IP',
	source: 'Quelle',
	hostname: 'Hostname',
	friendlyName: 'Anzeigename',
	manufacturer: 'Hersteller',
	modelName: 'Modellname',
	modelNumber: 'Modellnummer',
	serialNumber: 'Seriennummer',
	deviceType: 'Gerätetyp',
	presentationUrl: 'Web-Oberfläche',
	leases: 'DHCP-Leases',
	expires: 'Läuft ab',
	node: 'Node',
	vmid: 'VMID',
	cores: 'Kerne',
	maxmem: 'RAM (max.)',
	maxdisk: 'Disk (max.)',
	speedMbps: 'Speed (Mbit/s)',
	adminStatus: 'Admin-Status',
	operStatus: 'Betriebsstatus',
	alias: 'Alias',
	objectId: 'sysObjectID',
	enterprise: 'Enterprise-Nr.',
	walked: 'Abgefragte Bereiche',
	errors: 'Fehler',
	unavailable: 'Nicht verfügbar'
};

/** Label for an inventory key (falls back to the key). */
export function fieldLabel(key: string): string {
	return labels[key] ?? key;
}

export function isScalar(v: unknown): boolean {
	return v === null || ['string', 'number', 'boolean'].includes(typeof v);
}

const isoRe = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/;

/** Formats a value according to its key (bytes, seconds, timestamps, booleans). */
export function formatField(key: string, v: unknown): string {
	if (v === null || v === undefined || v === '') return '–';
	if (typeof v === 'boolean') return v ? 'Ja' : 'Nein';
	if (typeof v === 'number') {
		if (/bytes$/i.test(key) || key === 'memory' || key === 'size' || key === 'maxmem' || key === 'maxdisk')
			return formatBytes(v);
		if (/seconds$/i.test(key) || key === 'uptime') return formatSeconds(v);
		return formatNumber(v, 2);
	}
	if (typeof v === 'string') {
		if (isoRe.test(v)) return formatDateTime(v);
		return v;
	}
	return JSON.stringify(v);
}
