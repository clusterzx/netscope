// Column catalogue of the device list (all DeviceRow fields + custom fields).
import type { CustomField, DeviceRow } from '$lib/api/types';
import type { Column } from '$lib/components/ui/Table.svelte';
import { criticalityLabel, deviceTypeName, stateLabel } from '$lib/utils/labels';

export interface DeviceColumnDef {
	key: string;
	label: string;
	group: 'Allgemein' | 'Netzwerk' | 'Status' | 'Sicherheit' | 'Zeit' | 'Custom Fields';
	sort?: string;
	sortDesc?: boolean;
	align?: 'left' | 'right' | 'center';
	width?: string;
	hideBelow?: 'sm' | 'md' | 'lg' | 'xl';
	/** plain text value (CSV-like, used for tooltips and custom fields) */
	text?: (d: DeviceRow) => string;
	/** type of a custom field column (text | number | date | url | bool) */
	cfType?: string;
}

export const DEVICE_COLUMNS: DeviceColumnDef[] = [
	{ key: 'status', label: 'Online', group: 'Status', sort: 'online', width: '4rem' },
	{ key: 'name', label: 'Name', group: 'Allgemein', sort: 'name' },
	{ key: 'ip', label: 'IP', group: 'Netzwerk', sort: 'ip' },
	{ key: 'ips', label: 'Alle IPs', group: 'Netzwerk', text: (d) => (d.ips ?? []).join(', ') },
	{ key: 'mac', label: 'MAC', group: 'Netzwerk', sort: 'mac', hideBelow: 'md' },
	{ key: 'macs', label: 'Alle MACs', group: 'Netzwerk', text: (d) => (d.macs ?? []).join(', ') },
	{ key: 'hostname', label: 'Hostname', group: 'Netzwerk', text: (d) => d.hostname },
	{ key: 'hostnameSource', label: 'Hostname-Quelle', group: 'Netzwerk', text: (d) => d.hostnameSource },
	{ key: 'vendor', label: 'Hersteller', group: 'Allgemein', sort: 'vendor', hideBelow: 'lg' },
	{ key: 'model', label: 'Modell', group: 'Allgemein', sort: 'model', text: (d) => d.model },
	{ key: 'type', label: 'Typ', group: 'Allgemein', sort: 'type', text: (d) => deviceTypeName(d.type) },
	{ key: 'os', label: 'Betriebssystem', group: 'Allgemein', sort: 'os', hideBelow: 'lg' },
	{ key: 'osSource', label: 'OS-Quelle', group: 'Allgemein', text: (d) => d.osSource },
	{ key: 'location', label: 'Aufstellort', group: 'Allgemein', sort: 'location', text: (d) => d.location },
	{ key: 'site', label: 'Standort (Verbund)', group: 'Allgemein', text: (d) => d.site ?? '' },
	{ key: 'owner', label: 'Besitzer', group: 'Allgemein', sort: 'owner', text: (d) => d.owner },
	{ key: 'parent', label: 'Eltern-Gerät', group: 'Allgemein', text: (d) => d.parentName ?? '' },
	{ key: 'ports', label: 'Ports', group: 'Netzwerk', sort: 'ports', sortDesc: true, hideBelow: 'md' },
	{ key: 'cve', label: 'CVE', group: 'Sicherheit', sort: 'cve', sortDesc: true, hideBelow: 'md' },
	{ key: 'certExpiry', label: 'Zertifikat', group: 'Sicherheit', sort: 'certExpiry' },
	{ key: 'healthState', label: 'Health', group: 'Status' },
	{
		key: 'state',
		label: 'Zustand',
		group: 'Status',
		sort: 'state',
		text: (d) => stateLabel[d.state] ?? d.state
	},
	{
		key: 'criticality',
		label: 'Kritikalität',
		group: 'Status',
		sort: 'criticality',
		text: (d) => criticalityLabel[d.criticality] ?? d.criticality
	},
	{ key: 'tags', label: 'Tags', group: 'Allgemein', hideBelow: 'lg' },
	{ key: 'groups', label: 'Gruppen', group: 'Allgemein' },
	{ key: 'notes', label: 'Notiz', group: 'Allgemein', width: '4rem' },
	{ key: 'firstSeen', label: 'Erstsichtung', group: 'Zeit', sort: 'firstSeen', sortDesc: true },
	{
		key: 'lastSeen',
		label: 'Zuletzt gesehen',
		group: 'Zeit',
		sort: 'lastSeen',
		sortDesc: true,
		hideBelow: 'sm'
	},
	{ key: 'onlineChangedAt', label: 'Status seit', group: 'Zeit' },
	{ key: 'createdSource', label: 'Entdeckt durch', group: 'Allgemein', text: (d) => d.createdSource },
	{ key: 'id', label: 'ID', group: 'Allgemein', sort: 'id', align: 'right', text: (d) => String(d.id) }
];

export const DEFAULT_COLUMNS = [
	'status',
	'name',
	'ip',
	'mac',
	'vendor',
	'type',
	'os',
	'ports',
	'cve',
	'tags',
	'lastSeen',
	'state'
];

/** Column catalogue including custom fields (keys "cf.<key>"). */
export function allColumns(custom: CustomField[]): DeviceColumnDef[] {
	return [
		...DEVICE_COLUMNS,
		...custom.map((c): DeviceColumnDef => ({
			key: `cf.${c.key}`,
			label: c.label,
			group: 'Custom Fields',
			cfType: c.type,
			text: (d) => formatCustom(d.custom?.[c.key], c.type)
		}))
	];
}

export function formatCustom(v: unknown, type: string): string {
	if (v === null || v === undefined || v === '') return '';
	if (type === 'bool') return v === true || v === 'true' ? 'Ja' : 'Nein';
	if (type === 'date' && typeof v === 'string') {
		const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(v);
		if (m) return `${m[3]}.${m[2]}.${m[1]}`;
	}
	return String(v);
}

/** Table columns for the chosen keys (unknown keys are dropped, order of `keys`). */
export function tableColumns(keys: string[], catalogue: DeviceColumnDef[]): Column<DeviceRow>[] {
	const out: Column<DeviceRow>[] = [];
	for (const k of keys) {
		const c = catalogue.find((x) => x.key === k);
		if (!c) continue;
		out.push({
			key: c.key,
			label: c.label,
			sortable: c.sort ?? false,
			sortDesc: c.sortDesc,
			align: c.align,
			width: c.width,
			hideBelow: k === 'name' || k === 'status' ? undefined : c.hideBelow
		});
	}
	return out;
}
