// Sidebar navigation. `match` decides the active entry (prefix match on the path).
import type { IconName } from '$lib/components/ui/icons';

export interface NavItem {
	href: string;
	label: string;
	icon: IconName;
	/** key for live counters shown as badge */
	badge?: 'events';
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
			{ href: '/topology', label: 'Topologie', icon: 'topology' }
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
			{ href: '/credentials', label: 'Credentials', icon: 'key' },
			{ href: '/reports', label: 'Reports', icon: 'reports' },
			{ href: '/system', label: 'System', icon: 'system' }
		]
	}
];

export function isActive(href: string, path: string): boolean {
	return href === '/' ? path === '/' : path === href || path.startsWith(href + '/');
}
