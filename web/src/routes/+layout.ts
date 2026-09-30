// Single page application: rendered in the browser, served by the Go binary.
// The root load resolves the session once; unauthenticated users go to /login?next=…, a new
// installation to the setup wizard (/welcome), sessions that first have to change the start
// password or set up a second factor to /setup. A signed-in user whose language preference
// differs from the language the page started in gets a reload.
import { redirect } from '@sveltejs/kit';
import { applyPreference } from '$lib/i18n';
import { auth } from '$lib/stores/auth.svelte';
import { setupState } from '$lib/stores/setup.svelte';
import type { LayoutLoad } from './$types';

export const ssr = false;
export const prerender = false;

export const load: LayoutLoad = async ({ url, untrack, fetch }) => {
	const path = untrack(() => url.pathname);
	// a new installation is set up in the wizard first; once its account exists, the
	// login page leads back into it
	const setup = await setupState.check(fetch);
	if (path === '/welcome') {
		if (setup && !setup.pending) redirect(307, '/');
		return {};
	}
	if (setup?.pending && !(path === '/login' && setup.account)) redirect(307, '/welcome');
	if (path === '/login') return {};
	const me = await auth.check(false, fetch);
	if (!me) {
		const next = untrack(() => url.pathname + url.search);
		redirect(307, '/login?next=' + encodeURIComponent(next));
	}
	if (applyPreference(me.user?.locale)) {
		window.location.reload();
		// keep the old page from rendering while the browser reloads
		await new Promise(() => {});
	}
	// a start password or a missing second factor (required by the role) comes first
	if (auth.restricted && path !== '/setup') redirect(307, '/setup');
	return {};
};
