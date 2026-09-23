// Current user/session.
//
//   auth.me          // { user, principal } or null
//   auth.canWrite    // false for read-only tokens (the UI session always has write scope)
//   await auth.login(username, password) / await auth.logout()
import { goto } from '$app/navigation';
import { api, ApiError } from '$lib/api/client';
import type { Me } from '$lib/api/types';
import { live } from './live.svelte';

class Auth {
	me = $state<Me | null>(null);
	checked = $state(false);
	canWrite = $derived(this.me?.principal?.scope !== 'read');
	#pending: Promise<Me | null> | null = null;

	/** Resolves the session once (cached). 401 → null, other errors are thrown. */
	check(force = false, fetchFn?: typeof fetch): Promise<Me | null> {
		if (this.checked && !force) return Promise.resolve(this.me);
		if (this.#pending) return this.#pending;
		this.#pending = api
			.get('/api/v1/auth/me', { auth: false, fetch: fetchFn })
			.then((me) => {
				this.me = me;
				return me;
			})
			.catch((e) => {
				if (e instanceof ApiError && e.status === 401) {
					this.me = null;
					return null;
				}
				throw e;
			})
			.finally(() => {
				this.checked = true;
				this.#pending = null;
			});
		return this.#pending;
	}

	async login(username: string, password: string): Promise<Me> {
		const me = await api.post('/api/v1/auth/login', { body: { username, password }, auth: false });
		this.me = me;
		this.checked = true;
		return me;
	}

	async logout() {
		try {
			await api.post('/api/v1/auth/logout', { auth: false });
		} finally {
			live.disconnect();
			this.me = null;
			this.checked = true;
			await goto('/login', { replaceState: true });
		}
	}
}

export const auth = new Auth();
