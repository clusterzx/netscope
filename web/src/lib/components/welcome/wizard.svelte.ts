// State of the setup wizard (/welcome). The entries survive "Zurück", a reload (the
// language step reloads the page) and a new sign-in: they are kept in sessionStorage –
// except the password, which is sent once when the account is created.
import type { SetupOptions, SetupScanner } from '$lib/api';
import { browserTimezone } from '$lib/components/system/timezones';
import { locale, type Locale } from '$lib/i18n';

export type Role = 'standalone' | 'central' | 'site';

/** A subnet of the network step (detected or added). */
export type WizardSubnet = {
	cidr: string;
	name: string;
	interface: string;
	gateway: string;
	access: 'direct' | 'routed';
	selected: boolean;
	detected: boolean;
};

export type WizardData = {
	step: number;
	/** the setup code (until the account exists; needed again after a reload) */
	code: string;
	initialized: boolean;
	language: Locale;
	timezone: string;
	username: string;
	displayName: string;
	role: Role;
	localName: string;
	centralUrl: string;
	token: string;
	fingerprint: string;
	publicUrl: string;
	subnets: WizardSubnet[];
	exclusions: string[];
	dnsServer: string;
	scanners: string[];
	firstScan: boolean;
};

export const STEPS = [1, 2, 3, 4, 5, 6, 7] as const;

const KEY = 'netscope.welcome';

function empty(): WizardData {
	return {
		step: 1,
		code: '',
		initialized: false,
		language: locale,
		timezone: '',
		username: 'admin',
		displayName: '',
		role: 'standalone',
		localName: '',
		centralUrl: '',
		token: '',
		fingerprint: '',
		publicUrl: '',
		subnets: [],
		exclusions: [],
		dnsServer: '',
		scanners: [],
		firstScan: true
	};
}

function load(): WizardData {
	try {
		const raw = sessionStorage.getItem(KEY);
		if (raw) return { ...empty(), ...JSON.parse(raw) };
	} catch {
		// storage unavailable or broken: start over
	}
	return empty();
}

/** Scanner presets of the scanner step. */
export const PRESETS = {
	presence: (list: SetupScanner[]) => list.filter((s) => s.presence && s.load === 'low').map((s) => s.id),
	gentle: (list: SetupScanner[]) => list.filter((s) => s.load !== 'high').map((s) => s.id),
	full: (list: SetupScanner[]) => list.map((s) => s.id)
};

class Wizard {
	data = $state<WizardData>(load());
	options = $state<SetupOptions | null>(null);

	save() {
		try {
			sessionStorage.setItem(KEY, JSON.stringify($state.snapshot(this.data)));
		} catch {
			// storage unavailable: the entries live as long as the page
		}
	}

	/** Forgets everything (after the setup is finished). */
	clear() {
		try {
			sessionStorage.removeItem(KEY);
		} catch {
			// ignore
		}
		this.data = empty();
	}

	/** Fills the suggestions of the server on the first visit. */
	init(o: SetupOptions) {
		this.options = o;
		const d = this.data;
		if (d.initialized) return;
		d.timezone = browserTimezone() || o.timezone;
		d.publicUrl = o.publicUrl || (typeof window !== 'undefined' ? window.location.origin : '');
		if (o.federation.role === 'site' || o.federation.role === 'central') d.role = o.federation.role as Role;
		d.localName = o.federation.localName ?? '';
		d.centralUrl = o.federation.centralUrl ?? '';
		d.fingerprint = o.federation.fingerprint ?? '';
		const configured = new Set((o.configuredSubnets ?? []).map((s) => s.cidr));
		d.subnets = [
			...(o.subnets ?? []).map((s) => ({
				cidr: s.cidr,
				name: s.name,
				interface: s.interface,
				gateway: s.gateway,
				access: 'direct' as const,
				selected: true,
				detected: true
			})),
			...(o.configuredSubnets ?? [])
				.filter((s) => !(o.subnets ?? []).some((x) => x.cidr === s.cidr))
				.map((s) => ({
					cidr: s.cidr,
					name: s.name,
					interface: s.interface,
					gateway: s.gateway,
					access: (s.access === 'routed' ? 'routed' : 'direct') as 'direct' | 'routed',
					selected: configured.has(s.cidr),
					detected: false
				}))
		];
		d.scanners = PRESETS.gentle(o.scanners ?? []);
		d.initialized = true;
		this.save();
	}
}

export const wizard = new Wizard();
