// Sidebar navigation. `match` decides the active entry (prefix match on the path).
import type { IconName } from '$lib/components/ui/icons';
import type { Permission } from '$lib/stores/auth.svelte';

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
		label: 'Inventar',
		items: [
			{ href: '/', label: 'Dashboard', icon: 'dashboard' },
			{ href: '/devices', label: 'Geräte', icon: 'devices' },
			{ href: '/topology', label: 'Topologie', icon: 'topology' },
			{ href: '/sites', label: 'Standorte', icon: 'globe', central: true }
		]
	},
	{
		label: 'Überwachung',
		items: [
			{ href: '/events', label: 'Events', icon: 'events', badge: 'events' },
			{ href: '/diff', label: 'Diff', icon: 'diff' },
			{ href: '/health', label: 'Health', icon: 'health' },
			{ href: '/vulnerabilities', label: 'Schwachstellen', icon: 'shield' }
		]
	},
	{
		label: 'Konfiguration',
		items: [
			{ href: '/plugins', label: 'Plugins', icon: 'plugins' },
			{ href: '/rules', label: 'Regeln', icon: 'rules' },
			{ href: '/credentials', label: 'Credentials', icon: 'key', perm: 'credentials.view' },
			{ href: '/reports', label: 'Reports', icon: 'reports' },
			{ href: '/system', label: 'System', icon: 'system' }
		]
	}
];

export function isActive(href: string, path: string): boolean {
	return href === '/' ? path === '/' : path === href || path.startsWith(href + '/');
}
