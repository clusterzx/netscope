// Publisher list (GET /api/v1/publishers), shared by the rule pages and kept fresh when a
// plugin configuration changes (enabling a publisher).
import { api } from '$lib/api';
import { live } from '$lib/stores/live.svelte';
import { Resource } from '$lib/stores/resource.svelte';

export const publishers = new Resource(async () => (await api.get('/api/v1/publishers')) ?? []);

let wired = false;
/** Loads the list once and refreshes it on plugin updates. */
export function loadPublishers() {
	publishers.load().catch(() => {});
	if (wired) return;
	wired = true;
	live.on('plugin', () => {
		if (publishers.value) publishers.refresh().catch(() => {});
	});
}
