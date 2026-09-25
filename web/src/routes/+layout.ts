// Single page application: rendered in the browser, served by the Go binary.
// The root load resolves the session once; unauthenticated users go to /login?next=…, sessions
// that first have to change the start password or set up a second factor to /setup.
import { redirect } from '@sveltejs/kit';
import { auth } from '$lib/stores/auth.svelte';
import type { LayoutLoad } from './$types';

export const ssr = false;
export const prerender = false;

export const load: LayoutLoad = async ({ url, untrack, fetch }) => {
	const path = untrack(() => url.pathname);
	if (path === '/login') return {};
	const me = await auth.check(false, fetch);
	if (!me) {
		const next = untrack(() => url.pathname + url.search);
		redirect(307, '/login?next=' + encodeURIComponent(next));
	}
	// a start password or a missing second factor (required by the role) comes first
	if (auth.restricted && path !== '/setup') redirect(307, '/setup');
	return {};
};
