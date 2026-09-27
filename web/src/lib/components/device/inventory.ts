// Formatting helpers for structured inventory data (GET /devices/{id}/inventory).
import { t } from '$lib/i18n';
import { formatBytes, formatDateTime, formatNumber, formatSeconds } from '$lib/utils/format';

const labels: Record<string, string> = {
	apiVersion: t('API-Version'),
	architecture: t('Architektur'),
	arch: t('Architektur'),
	bootTime: t('Boot-Zeitpunkt'),
	lastBoot: t('Letzter Boot'),
	composeProjects: t('Compose-Projekte'),
	containers: 'Container',
	containersRunning: t('Container (laufend)'),
	containersPaused: t('Container (pausiert)'),
	containersStopped: t('Container (gestoppt)'),
	cpus: 'CPUs',
	cpu: 'CPU',
	description: t('Beschreibung'),
	descr: t('Beschreibung'),
	distance: t('Netzwerk-Distanz (Hops)'),
	endpoint: t('Endpunkt'),
	engineVersion: t('Engine-Version'),
	images: 'Images',
	interfaces: 'Interfaces',
	kernel: 'Kernel',
	location: t('Aufstellort'),
	contact: t('Kontakt'),
	memory: t('Arbeitsspeicher'),
	model: t('Modell'),
	name: 'Name',
	names: t('Namen'),
	os: t('Betriebssystem'),
	osType: t('OS-Typ'),
	osMatches: t('OS-Kandidaten'),
	accuracy: t('Genauigkeit (%)'),
	platform: t('Plattform'),
	rootDir: t('Datenverzeichnis'),
	storageDriver: t('Storage-Treiber'),
	swarm: 'Swarm',
	services: t('Dienste'),
	serial: t('Seriennummer'),
	status: 'Status',
	state: t('Zustand'),
	type: t('Typ'),
	uptimeSeconds: 'Uptime',
	vendor: t('Hersteller'),
	version: 'Version',
	macs: t('MAC-Adressen'),
	mac: 'MAC',
	ip: 'IP',
	source: t('Quelle'),
	hostname: 'Hostname',
	friendlyName: t('Anzeigename'),
	manufacturer: t('Hersteller'),
	modelName: t('Modellname'),
	modelNumber: t('Modellnummer'),
	serialNumber: t('Seriennummer'),
	deviceType: t('Gerätetyp'),
	presentationUrl: t('Web-Oberfläche'),
	leases: t('DHCP-Leases'),
	expires: t('Läuft ab'),
	node: 'Node',
	vmid: 'VMID',
	cores: t('Kerne'),
	maxmem: 'RAM (max.)',
	maxdisk: 'Disk (max.)',
	speedMbps: 'Speed (Mbit/s)',
	adminStatus: t('Admin-Status'),
	operStatus: t('Betriebsstatus'),
	alias: 'Alias',
	objectId: 'sysObjectID',
	enterprise: t('Enterprise-Nr.'),
	walked: t('Abgefragte Bereiche'),
	errors: t('Fehler'),
	unavailable: t('Nicht verfügbar'),
	// GenericData shows a top-level array under this key
	entries: t('Einträge')
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
	if (typeof v === 'boolean') return v ? t('Ja') : t('Nein');
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
