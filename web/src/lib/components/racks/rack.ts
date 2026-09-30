// Helpers of the rack view: labels, geometry (height units, sixths of the width), the state
// of a port and client-side collision checks for drag and drop.
import type { RackItem, RackPort, RackView } from '$lib/api';
import type { IconName } from '$lib/components/ui';
import { typeIcon } from '$lib/components/topology/graph';
import { t } from '$lib/i18n';

export type Face = 'front' | 'rear';
export const COLUMNS = 6;

/** A free place in a rack (preset of the mount dialog). */
export interface Slot {
	position: number;
	face: Face;
	col: number;
	cols: number;
}

export const KINDS = ['device', 'patch_panel', 'shelf', 'blank', 'cable_manager', 'pdu', 'other'] as const;
export type Kind = (typeof KINDS)[number];

export function kindLabel(kind: string): string {
	switch (kind) {
		case 'device':
			return t('Gerät aus dem Inventar');
		case 'patch_panel':
			return t('Patchfeld');
		case 'shelf':
			return t('Fachboden');
		case 'blank':
			return t('Blende');
		case 'cable_manager':
			return t('Kabelführung');
		case 'pdu':
			return t('Steckdosenleiste');
	}
	return t('Sonstiges');
}

export function kindIcon(kind: string): IconName {
	switch (kind) {
		case 'patch_panel':
			return 'network';
		case 'shelf':
			return 'layers';
		case 'cable_manager':
			return 'menu';
		case 'pdu':
			return 'zap';
		case 'blank':
			return 'minus';
	}
	return 'box';
}

/** Kinds that have ports by default. */
export function hasPorts(kind: string): boolean {
	return kind === 'device' || kind === 'patch_panel' || kind === 'pdu';
}

export const WIDTHS = [
	{ cols: 6, label: () => t('Volle Breite') },
	{ cols: 3, label: () => t('Halbe Breite') },
	{ cols: 2, label: () => t('Drittel Breite') }
];

export const CABLE_COLORS: { value: string; label: () => string; css: string }[] = [
	{ value: '', label: () => t('Ohne Farbe'), css: 'var(--fg-muted)' },
	{ value: 'blue', label: () => t('Blau'), css: '#3b82f6' },
	{ value: 'green', label: () => t('Grün'), css: '#22c55e' },
	{ value: 'yellow', label: () => t('Gelb'), css: '#eab308' },
	{ value: 'red', label: () => t('Rot'), css: '#ef4444' },
	{ value: 'orange', label: () => t('Orange'), css: '#f97316' },
	{ value: 'white', label: () => t('Weiß'), css: '#f8fafc' },
	{ value: 'grey', label: () => t('Grau'), css: '#94a3b8' },
	{ value: 'black', label: () => t('Schwarz'), css: '#1e293b' },
	{ value: 'purple', label: () => t('Lila'), css: '#a855f7' },
	{ value: 'pink', label: () => t('Rosa'), css: '#ec4899' }
];

export function cableCss(color: string | undefined): string {
	return CABLE_COLORS.find((c) => c.value === (color ?? ''))?.css ?? 'var(--fg-muted)';
}

/** Height unit as labelled in the rack. */
export function unitLabel(rack: { height: number; numbering: string }, position: number): number {
	return rack.numbering === 'top' ? rack.height - position + 1 : position;
}

/** Units an item occupies, as labelled ("12" or "10–12"). */
export function unitsText(
	rack: { height: number; numbering: string },
	item: { position: number; height: number }
): string {
	const a = unitLabel(rack, item.position);
	const b = unitLabel(rack, item.position + item.height - 1);
	return item.height === 1 ? String(a) : `${Math.min(a, b)}–${Math.max(a, b)}`;
}

/** Whether an item shows on a face (full-depth items show on both). */
export function onFace(item: RackItem, face: Face): boolean {
	return item.face === face || item.fullDepth;
}

export function itemName(item: RackItem): string {
	return item.label || item.device?.name || item.deviceName || kindLabel(item.kind);
}

export function itemIcon(item: RackItem): IconName {
	if (item.kind === 'device') return typeIcon(item.device?.type) ?? 'server';
	return kindIcon(item.kind);
}

/** Whether a place is free (for drag and drop and the mount dialog). */
export function fits(
	rack: RackView,
	place: { position: number; height: number; face: string; fullDepth: boolean; col: number; cols: number },
	self = 0
): boolean {
	if (place.position < 1 || place.position + place.height - 1 > rack.height) return false;
	if (place.col < 0 || place.col + place.cols > COLUMNS || place.col % place.cols !== 0) return false;
	return !rack.items.some(
		(i) =>
			i.id !== self &&
			(i.face === place.face || i.fullDepth || place.fullDepth) &&
			i.position < place.position + place.height &&
			place.position < i.position + i.height &&
			i.col < place.col + place.cols &&
			place.col < i.col + i.cols
	);
}

export type PortState = 'cable' | 'device' | 'detected' | 'up' | 'free' | 'disabled';

/** What a port shows: a cable, a device set by hand, a detected device, a link, or nothing. */
export function portState(p: RackPort): PortState {
	if (p.cables.length) return 'cable';
	if (p.device) return 'device';
	if (p.detected.length) return 'detected';
	if (p.status === 'up') return 'up';
	if (p.status === 'disabled') return 'disabled';
	return 'free';
}

/** The device a port leads to (set by hand, through cables, or detected). */
export function portDevice(p: RackPort) {
	if (p.device) return p.device;
	for (const c of p.cables) {
		const end = c.end ?? c.peer;
		if (end.device) return end.device;
	}
	return p.detected.length === 1 ? p.detected[0].device : undefined;
}

/** Name of a port for text: "Port 5" for bare numbers, names like "ether5" or "Port 5" as they are. */
export function portLabel(name: string): string {
	return /^\d/.test(name) ? t('Port {name}', { name }) : name;
}

export function portTitle(p: RackPort): string {
	const parts = [portLabel(p.name)];
	if (p.label) parts.push(p.label);
	const d = portDevice(p);
	if (d) parts.push(d.name);
	else if (p.detected.length > 1)
		parts.push(t('{n} Geräte erkannt', { n: p.detected.length + (p.detectedMore ?? 0) }));
	if (p.cables.length && !d) {
		const c = p.cables[0];
		parts.push(`→ ${c.peer.itemName} ${c.peer.port}`);
	}
	if (p.status === 'up') parts.push(t('Link aktiv'));
	return parts.join(' · ');
}

export function speedText(mbps: number | undefined): string {
	if (!mbps) return '';
	return mbps >= 1000 ? `${mbps / 1000} Gbit/s` : `${mbps} Mbit/s`;
}

export function sourceLabel(source: string): string {
	switch (source) {
		case 'topology':
			return t('Topologie (SNMP)');
		case 'manual':
			return t('manuelle Kante');
		case 'unifi':
			return 'UniFi';
		case 'mikrotik':
			return 'MikroTik';
		case 'snmp':
			return 'SNMP';
	}
	return source;
}
