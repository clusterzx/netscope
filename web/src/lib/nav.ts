// Sidebar navigation. `match` decides the active entry (prefix match on the path).
import type { IconName } from '$lib/components/ui/icons';
import type { Permission } from '$lib/stores/auth.svelte';
import { t } from '$lib/i18n';

export interface NavItem {
	href: string;
	label: string;
	icon: IconName;
	/** key for live counters shown as badge */
	badge?: 'events';
	/** only shown on a central instance */
	central?: boolean;
	/** only shown with this permission */
	perm?: Permission;
}

export interface NavSection {
	label: string;
	items: NavItem[];
}

export const nav: NavSection[] = [
	{
		label: t('Inventar'),
		items: [
			{ href: '/', label: 'Dashboard', icon: 'dashboard' },
			{ href: '/devices', label: t('Geräte'), icon: 'devices' },
			{ href: '/topology', label: t('Topologie'), icon: 'topology' },
			{ href: '/sites', label: t('Standorte'), icon: 'globe', central: true }
		]
	},
	{
		label: t('Überwachung'),
		items: [
			{ href: '/events', label: 'Events', icon: 'events', badge: 'events' },
			{ href: '/diff', label: 'Diff', icon: 'diff' },
			{ href: '/health', label: 'Health', icon: 'health' },
			{ href: '/vulnerabilities', label: t('Schwachstellen'), icon: 'shield' }
		]
	},
	{
		label: t('Konfiguration'),
		items: [
			{ href: '/plugins', label: 'Plugins', icon: 'plugins' },
			{ href: '/agents', label: 'Agents', icon: 'cpu' },
			{ href: '/rules', label: t('Regeln'), icon: 'rules' },
			{ href: '/credentials', label: 'Credentials', icon: 'key', perm: 'credentials.view' },
			{ href: '/reports', label: 'Reports', icon: 'reports' },
			{ href: '/system', label: 'System', icon: 'system' }
		]
	}
];

export function isActive(href: string, path: string): boolean {
	return href === '/' ? path === '/' : path === href || path.startsWith(href + '/');
}
