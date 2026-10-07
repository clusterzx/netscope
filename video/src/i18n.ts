import type { IconName } from './components/Icon';
import type { Lang } from './timeline';
import type { Tone } from './theme';

// Every text shown on screen, per language. The spoken text is in voiceover.json.
const de = {
	hook: {
		lines: ['Wie viele Geräte hängen', 'gerade in deinem Netzwerk?'],
		highlight: 'Geräte',
		sub: 'Und was läuft eigentlich auf ihnen?',
		scanning: 'Unbekannte Geräte'
	},
	questions: [
		['info', 'Was ist es?'],
		['pin', 'Wo hängt es?'],
		['box', 'Was läuft darauf?'],
		['clock', 'Seit wann?'],
		['diff', 'Was hat sich geändert?'],
		['activity', 'Ist es erreichbar?'],
		['shield', 'Ist es verwundbar?']
	] as [IconName, string][],
	reveal: {
		tagline: 'Netzwerk-Inventar & Asset-Monitoring',
		claims: ['Ein Go-Binary', '41 Plugins', 'Live-Updates', 'CVE-Abgleich', 'Selbst gehostet']
	},
	discovery: {
		eyebrow: 'Discovery',
		title: '15 Scanner',
		sub: 'finden jedes Gerät – automatisch.',
		scanners: [
			'ARP',
			'ICMP',
			'OUI',
			'DNS',
			'mDNS',
			'NetBIOS',
			'UPnP',
			'nmap',
			'nmap UDP',
			'HTTP',
			'TLS',
			'SNMP',
			'SNMP-Traffic',
			'SSH',
			'Wake-on-LAN'
		],
		os: 'Betriebssystem'
	},
	sources: {
		eyebrow: 'Importer & Agents',
		title: 'Deine ganze Infrastruktur, angebunden.',
		more: '+ OpenWrt, Sophos, NetAlertX, CSV',
		terminal: 'Agent installieren',
		output: [
			['ok', 'Agent installiert, läuft ohne Admin-Rechte'],
			['ok', 'Verbunden mit netscope.lan – nur ausgehend'],
			['ok', 'Inventar gesendet: 214 Pakete, 6 Dienste, 3 Container'],
			['accent', 'Automatische Updates aktiv']
		] as [Tone, string][],
		badges: ['Linux & Windows', 'Hinter NAT', 'Aktualisiert sich selbst']
	},
	inventory: {
		eyebrow: 'Inventar & Historie',
		title: 'Ein Inventar. Jede Änderung.',
		headers: ['Gerät', 'IP-Adresse', 'Hersteller', 'Typ', 'Status'],
		types: {
			server: 'Server',
			nas: 'NAS',
			switch: 'Switch',
			ap: 'Access Point',
			printer: 'Drucker',
			camera: 'Kamera',
			hypervisor: 'Hypervisor',
			tv: 'Smart TV',
			firewall: 'Firewall'
		},
		newBadge: 'Neu',
		history: 'Änderungshistorie',
		events: [
			['package', 'violet', 'Paket aktualisiert', 'web-01 · openssl 3.0.13 › 3.0.14'],
			['zap', 'accent', 'Port 8443/tcp geöffnet', 'nas-01 · vor 2 Min.'],
			['lock', 'ok', 'Zertifikat erneuert', 'pve-01 · gültig bis 04.01.2027'],
			['plus', 'amber', 'Neues Gerät', 'esp32-cam · 192.168.1.87'],
			['x-circle', 'muted', 'Offline', 'printer-og · seit 08:12']
		] as [IconName, Tone, string, string][],
		toast: ['Neues Gerät entdeckt', 'esp32-cam · Espressif · 192.168.1.87']
	},
	vulns: {
		eyebrow: 'Schwachstellen',
		title: 'Wissen, was zuerst dran ist.',
		sub: 'CVE-Abgleich gegen die lokale NVD · priorisiert nach CISA KEV und EPSS',
		headers: ['CVE', 'Betroffen', 'Gerät', 'CVSS', 'EPSS', 'KEV'],
		sortedBy: 'Sortiert nach',
		cvss: 'CVSS',
		priority: 'Priorität',
		kevYes: 'Aktiv ausgenutzt'
	},
	topology: {
		eyebrow: 'Topologie · Health-Checks · Racks',
		title: 'Sehen, wie alles zusammenhängt.',
		rack: 'Rack A · Technikraum',
		cable: ['Port 7', 'Dose 2.14 · Büro OG'],
		down: 'Ausfall',
		check: 'HTTPS · 14 ms'
	},
	alerts: {
		eyebrow: 'Regeln & Benachrichtigungen',
		title: 'Du erfährst es zuerst.',
		ruleName: 'Kritisch & neu',
		rule: [
			['Wenn', 'Neues Gerät  oder  CVE in KEV'],
			['Netz', '192.168.1.0/24'],
			['Bündeln', '5 Minuten'],
			['Ruhezeit', '22:00 – 07:00'],
			['Eskalation', 'nach 30 Min. ohne Quittung']
		],
		to: 'Zustellen über',
		notifications: [
			['Telegram', 'jetzt', 'Kritische CVE auf unifi-ctrl', 'CVE-2021-44228 · in CISA KEV'],
			['ntfy', 'jetzt', 'Neues Gerät im Netz', 'esp32-cam · 192.168.1.87'],
			['E-Mail', '1 Min.', 'Health-Check fehlgeschlagen', 'printer-og · ICMP seit 3 Min.'],
			['n8n', '2 Min.', 'Workflow gestartet', 'Ticket #4711 angelegt']
		],
		lock: 'Dienstag, 6. Oktober'
	},
	selfhosted: {
		eyebrow: 'Selbst gehostet',
		title: 'Deine Daten bleiben bei dir.',
		stats: [
			['1', 'Go-Binary'],
			['41', 'Plugins'],
			['0', 'Cloud-Abhängigkeiten']
		],
		pills: [
			'Zwei-Faktor & Passkeys',
			'OIDC & LDAP',
			'Rollen',
			'Deutsch & English',
			'JSON-API & OpenAPI',
			'Prometheus'
		]
	},
	outro: {
		tagline: 'Kenne dein Netzwerk.',
		sub: 'Netzwerk-Inventar & Asset-Monitoring · selbst gehostet'
	}
};

export type Strings = typeof de;

const en: Strings = {
	hook: {
		lines: ['How many devices are on', 'your network right now?'],
		highlight: 'devices',
		sub: 'And what is actually running on them?',
		scanning: 'Unknown devices'
	},
	questions: [
		['info', 'What is it?'],
		['pin', 'Where is it?'],
		['box', 'What runs on it?'],
		['clock', 'Since when?'],
		['diff', 'What changed?'],
		['activity', 'Is it reachable?'],
		['shield', 'Is it vulnerable?']
	],
	reveal: {
		tagline: 'Network inventory & asset monitoring',
		claims: ['Single Go binary', '41 plugins', 'Live updates', 'CVE matching', 'Self-hosted']
	},
	discovery: {
		eyebrow: 'Discovery',
		title: '15 scanners',
		sub: 'find every device – automatically.',
		scanners: [
			'ARP',
			'ICMP',
			'OUI',
			'DNS',
			'mDNS',
			'NetBIOS',
			'UPnP',
			'nmap',
			'nmap UDP',
			'HTTP',
			'TLS',
			'SNMP',
			'SNMP traffic',
			'SSH',
			'Wake-on-LAN'
		],
		os: 'Operating system'
	},
	sources: {
		eyebrow: 'Importers & agents',
		title: 'Your whole stack, connected.',
		more: '+ OpenWrt, Sophos, NetAlertX, CSV',
		terminal: 'Install agent',
		output: [
			['ok', 'Agent installed, runs without admin rights'],
			['ok', 'Connected to netscope.lan – outbound only'],
			['ok', 'Inventory sent: 214 packages, 6 services, 3 containers'],
			['accent', 'Automatic updates enabled']
		],
		badges: ['Linux & Windows', 'Behind NAT', 'Updates itself']
	},
	inventory: {
		eyebrow: 'Inventory & history',
		title: 'One inventory. Every change.',
		headers: ['Device', 'IP address', 'Vendor', 'Type', 'Status'],
		types: {
			server: 'Server',
			nas: 'NAS',
			switch: 'Switch',
			ap: 'Access point',
			printer: 'Printer',
			camera: 'Camera',
			hypervisor: 'Hypervisor',
			tv: 'Smart TV',
			firewall: 'Firewall'
		},
		newBadge: 'New',
		history: 'Change history',
		events: [
			['package', 'violet', 'Package updated', 'web-01 · openssl 3.0.13 › 3.0.14'],
			['zap', 'accent', 'Port 8443/tcp opened', 'nas-01 · 2 min ago'],
			['lock', 'ok', 'Certificate renewed', 'pve-01 · valid until 2027-01-04'],
			['plus', 'amber', 'New device', 'esp32-cam · 192.168.1.87'],
			['x-circle', 'muted', 'Offline', 'printer-og · since 08:12']
		],
		toast: ['New device discovered', 'esp32-cam · Espressif · 192.168.1.87']
	},
	vulns: {
		eyebrow: 'Vulnerabilities',
		title: 'Know what to fix first.',
		sub: 'CVE matching against a local NVD mirror · prioritised by CISA KEV and EPSS',
		headers: ['CVE', 'Affected', 'Device', 'CVSS', 'EPSS', 'KEV'],
		sortedBy: 'Sorted by',
		cvss: 'CVSS',
		priority: 'Priority',
		kevYes: 'Known exploited'
	},
	topology: {
		eyebrow: 'Topology · health checks · racks',
		title: 'See how it all connects.',
		rack: 'Rack A · server room',
		cable: ['Port 7', 'Socket 2.14 · office 1F'],
		down: 'Down',
		check: 'HTTPS · 14 ms'
	},
	alerts: {
		eyebrow: 'Rules & notifications',
		title: 'You hear it first.',
		ruleName: 'Critical & new',
		rule: [
			['When', 'New device  or  CVE in KEV'],
			['Network', '192.168.1.0/24'],
			['Bundle', '5 minutes'],
			['Quiet hours', '22:00 – 07:00'],
			['Escalate', 'after 30 min without ack']
		],
		to: 'Deliver via',
		notifications: [
			['Telegram', 'now', 'Critical CVE on unifi-ctrl', 'CVE-2021-44228 · in CISA KEV'],
			['ntfy', 'now', 'New device on the network', 'esp32-cam · 192.168.1.87'],
			['E-mail', '1 min', 'Health check failed', 'printer-og · ICMP for 3 min'],
			['n8n', '2 min', 'Workflow started', 'Ticket #4711 created']
		],
		lock: 'Tuesday, October 6'
	},
	selfhosted: {
		eyebrow: 'Self-hosted',
		title: 'Your data stays with you.',
		stats: [
			['1', 'Go binary'],
			['41', 'plugins'],
			['0', 'cloud dependencies']
		],
		pills: [
			'Two-factor & passkeys',
			'OIDC & LDAP',
			'Roles',
			'English & German',
			'JSON API & OpenAPI',
			'Prometheus'
		]
	},
	outro: {
		tagline: 'Know your network.',
		sub: 'Network inventory & asset monitoring · self-hosted'
	}
};

export const strings: Record<Lang, Strings> = { de, en };
