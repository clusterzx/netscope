// Column catalogue of the device list (all DeviceRow fields + custom fields).
import type { CustomField, DeviceRow } from '$lib/api/types';
import type { Column } from '$lib/components/ui/Table.svelte';
import { t } from '$lib/i18n';
import { formatDate } from '$lib/utils/format';
import { criticalityLabel, deviceTypeName, stateLabel } from '$lib/utils/labels';

export interface DeviceColumnDef {
	key: string;
	label: string;
	/** heading in the column chooser (UI language) */
	group: string;
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

const G = {
	general: t('Allgemein'),
	network: t('Netzwerk'),
	status: 'Status',
	security: t('Sicherheit'),
	time: t('Zeit')
};

export const DEVICE_COLUMNS: DeviceColumnDef[] = [
	{ key: 'status', label: 'Online', group: G.status, sort: 'online', width: '4rem' },
	{ key: 'name', label: 'Name', group: G.general, sort: 'name' },
	{ key: 'ip', label: 'IP', group: G.network, sort: 'ip' },
	{ key: 'ips', label: t('Alle IPs'), group: G.network, text: (d) => (d.ips ?? []).join(', ') },
	{ key: 'mac', label: 'MAC', group: G.network, sort: 'mac', hideBelow: 'md' },
	{ key: 'macs', label: t('Alle MACs'), group: G.network, text: (d) => (d.macs ?? []).join(', ') },
	{ key: 'hostname', label: 'Hostname', group: G.network, text: (d) => d.hostname },
	{ key: 'hostnameSource', label: t('Hostname-Quelle'), group: G.network, text: (d) => d.hostnameSource },
	{ key: 'vendor', label: t('Hersteller'), group: G.general, sort: 'vendor', hideBelow: 'lg' },
	{ key: 'model', label: t('Modell'), group: G.general, sort: 'model', text: (d) => d.model },
	{ key: 'type', label: t('Typ'), group: G.general, sort: 'type', text: (d) => deviceTypeName(d.type) },
	{ key: 'os', label: t('Betriebssystem'), group: G.general, sort: 'os', hideBelow: 'lg' },
	{ key: 'osSource', label: t('OS-Quelle'), group: G.general, text: (d) => d.osSource },
	{ key: 'location', label: t('Aufstellort'), group: G.general, sort: 'location', text: (d) => d.location },
	{ key: 'site', label: t('Standort (Verbund)'), group: G.general, text: (d) => d.site ?? '' },
	{ key: 'owner', label: t('Besitzer'), group: G.general, sort: 'owner', text: (d) => d.owner },
	{ key: 'parent', label: t('Eltern-Gerät'), group: G.general, text: (d) => d.parentName ?? '' },
	{ key: 'ports', label: 'Ports', group: G.network, sort: 'ports', sortDesc: true, hideBelow: 'md' },
	{ key: 'cve', label: 'CVE', group: G.security, sort: 'cve', sortDesc: true, hideBelow: 'md' },
	{ key: 'certExpiry', label: t('Zertifikat'), group: G.security, sort: 'certExpiry' },
	{ key: 'healthState', label: 'Health', group: G.status },
	{
		key: 'state',
		label: t('Zustand'),
		group: G.status,
		sort: 'state',
		text: (d) => stateLabel[d.state] ?? d.state
	},
	{
		key: 'criticality',
		label: t('Kritikalität'),
		group: G.status,
		sort: 'criticality',
		text: (d) => criticalityLabel[d.criticality] ?? d.criticality
	},
	{ key: 'tags', label: 'Tags', group: G.general, hideBelow: 'lg' },
	{ key: 'groups', label: t('Gruppen'), group: G.general },
	{ key: 'notes', label: t('Notiz'), group: G.general, width: '4rem' },
	{ key: 'firstSeen', label: t('Erstsichtung'), group: G.time, sort: 'firstSeen', sortDesc: true },
	{
		key: 'lastSeen',
		label: t('Zuletzt gesehen'),
		group: G.time,
		sort: 'lastSeen',
		sortDesc: true,
		hideBelow: 'sm'
	},
	{ key: 'onlineChangedAt', label: t('Status seit'), group: G.time },
	{ key: 'createdSource', label: t('Entdeckt durch'), group: G.general, text: (d) => d.createdSource },
	{ key: 'id', label: 'ID', group: G.general, sort: 'id', align: 'right', text: (d) => String(d.id) }
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
	if (type === 'bool') return v === true || v === 'true' ? t('Ja') : t('Nein');
	if (type === 'date' && typeof v === 'string') {
		const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(v);
		if (m) return formatDate(new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
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
