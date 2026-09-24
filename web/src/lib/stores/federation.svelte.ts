// Federation state of this instance (GET /api/v1/federation) and the site selector of a
// central instance.
//
//   import { federation, siteFilter } from '$lib/stores/federation.svelte';
//   federation.load();                 // cached
//   federation.isCentral               // central instance with at least one site
//   siteFilter.value                   // '' = all, 'local' = this instance, otherwise the slug
//   api.get('/api/v1/devices', { query: { q, site: siteFilter.value } })
import { api } from '$lib/api/client';
import type { FederationView, SiteRef } from '$lib/api';
import { Resource } from './resource.svelte';
import { live } from './live.svelte';

const STORAGE_KEY = 'ns.site';

class FederationStore {
	#res = new Resource<FederationView>(() => api.get('/api/v1/federation'));
	#wired = false;

	get value(): FederationView | undefined {
		return this.#res.value;
	}
	get role(): string {
		return this.#res.value?.settings.role ?? 'standalone';
	}
	/** Central instance with at least one site: site labels and the selector are shown. */
	get isCentral(): boolean {
		return this.role === 'central' && (this.#res.value?.sites.length ?? 0) > 0;
	}
	get sites(): SiteRef[] {
		return this.#res.value?.sites ?? [];
	}
	/** Name of this instance among the sites. */
	get localName(): string {
		return this.#res.value?.localName || 'Zentrale';
	}
	site(id: number | undefined): SiteRef | undefined {
		return id ? this.sites.find((s) => s.id === id) : undefined;
	}

	load() {
		this.#wire();
		return this.#res.load();
	}
	refresh() {
		this.#wire();
		return this.#res.refresh();
	}

	#wire() {
		if (this.#wired || typeof window === 'undefined') return;
		this.#wired = true;
		live.on('system', (m) => {
			if (m.type === 'sites' || m.type === 'federation') this.#res.refresh().catch(() => {});
		});
	}
}

export const federation = new FederationStore();

function readStored(): string {
	try {
		return localStorage.getItem(STORAGE_KEY) ?? '';
	} catch {
		return '';
	}
}

class SiteFilter {
	#value = $state(typeof window === 'undefined' ? '' : readStored());

	/** '' = all sites, 'local' = this instance, otherwise the slug of a site. */
	get value(): string {
		// a stored site that no longer exists (or no central role) means "all"
		if (!federation.isCentral) return '';
		const v = this.#value;
		if (v === '' || v === 'local' || federation.sites.some((s) => s.slug === v)) return v;
		return '';
	}
	set value(v: string) {
		this.#value = v;
		try {
			if (v) localStorage.setItem(STORAGE_KEY, v);
			else localStorage.removeItem(STORAGE_KEY);
		} catch {
			// private mode: selection lasts for this page only
		}
	}
	/** Label of the current selection. */
	get label(): string {
		const v = this.value;
		if (v === '') return 'Alle Standorte';
		if (v === 'local') return federation.localName;
		return federation.sites.find((s) => s.slug === v)?.name ?? v;
	}
}

export const siteFilter = new SiteFilter();
