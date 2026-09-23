// Shared, cached catalogs used by many pages. Call `.load()` (cached) or `.refresh()`
// after changing the underlying data (e.g. after creating a group call groups.refresh()).
//
//   import { meta, groups } from '$lib/stores/catalog.svelte';
//   onMount(() => { meta.load(); });
//   meta.value?.scanners
import { api } from '$lib/api/client';
import { Resource } from './resource.svelte';
import { live } from './live.svelte';

/** /api/v1/meta – event types, credential types, device types, query fields, scanners, device actions … */
export const meta = new Resource(() => api.get('/api/v1/meta'));
/** /api/v1/groups */
export const groups = new Resource(async () => (await api.get('/api/v1/groups')) ?? []);
/** /api/v1/tags (tag + count) */
export const tags = new Resource(async () => (await api.get('/api/v1/tags')) ?? []);
/** /api/v1/subnets */
export const subnets = new Resource(async () => (await api.get('/api/v1/subnets')) ?? []);
/** /api/v1/custom-fields */
export const customFields = new Resource(async () => (await api.get('/api/v1/custom-fields')) ?? []);
/** /api/v1/credentials (metadata only, never secrets) */
export const credentials = new Resource(async () => (await api.get('/api/v1/credentials')) ?? []);
/** /api/v1/events/types – the event type catalog */
export const eventTypes = new Resource(async () => (await api.get('/api/v1/events/types')) ?? []);

/** Label of an event type from the catalog (falls back to the type). */
export function eventTypeLabel(type: string): string {
	return meta.value?.eventTypes.find((e) => e.type === type)?.label ?? type;
}

let wired = false;
/** Keeps catalogs fresh on relevant live messages. Called once by the app shell. */
export function wireCatalogRefresh() {
	if (wired) return;
	wired = true;
	live.on('plugin', () => {
		if (meta.value) meta.refresh().catch(() => {});
	});
}
