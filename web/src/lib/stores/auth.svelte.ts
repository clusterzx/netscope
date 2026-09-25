// Current user/session.
//
//   auth.me                  // { user, principal } or null
//   auth.can('devices.edit') // permission of the role (administrators hold every one)
//   auth.canWrite            // false for read-only tokens (the UI session always has write scope)
//   auth.restricted          // 'password' | 'mfa' | '' – the session first has to finish its setup
//   const step = await auth.login(username, password)   // { mfa } when a second factor is needed
//   await auth.loginTotp(challenge, code) / loginRecovery(...) / loginPasskey(challenge)
//   await auth.logout()
import { goto } from '$app/navigation';
import { api, ApiError } from '$lib/api/client';
import type { Me } from '$lib/api/types';
import { getPasskey } from '$lib/utils/webauthn';
import { live } from './live.svelte';

/** Permission keys (see internal/auth/perms.go). */
export type Permission =
	| 'devices.edit'
	| 'devices.delete'
	| 'devices.scan'
	| 'devices.actions'
	| 'inventory.config'
	| 'events.ack'
	| 'health.manage'
	| 'vulns.manage'
	| 'rules.manage'
	| 'reports.send'
	| 'plugins.manage'
	| 'credentials.view'
	| 'credentials.manage'
	| 'network.manage'
	| 'sites.manage'
	| 'system.manage'
	| 'backups.manage'
	| 'audit.view'
	| 'tokens.create'
	| 'users.manage';

/** Second step of a login. */
export type MfaStep = { challenge: string; methods: string[] };

class Auth {
	me = $state<Me | null>(null);
	checked = $state(false);
	canWrite = $derived(this.me?.principal?.scope !== 'read');
	#perms = $derived(new Set(this.me?.principal?.permissions ?? []));
	restricted = $derived<'password' | 'mfa' | ''>(
		this.me?.principal?.kind !== 'session'
			? ''
			: this.me.principal.passwordChange
				? 'password'
				: this.me.principal.mfaSetup
					? 'mfa'
					: ''
	);
	#pending: Promise<Me | null> | null = null;

	/** Whether the signed-in user holds a permission. */
	can(perm: Permission): boolean {
		const p = this.me?.principal;
		if (!p || !this.canWrite) return false;
		return p.admin || this.#perms.has(perm);
	}

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

	#signedIn(res: { user?: Me['user']; principal?: Me['principal'] }): Me {
		const me = { user: res.user!, principal: res.principal! } as Me;
		this.me = me;
		this.checked = true;
		return me;
	}

	/** Password step: the signed-in user, or the second step to answer. */
	async login(username: string, password: string): Promise<{ me?: Me; mfa?: MfaStep }> {
		const res = await api.post('/api/v1/auth/login', { body: { username, password }, auth: false });
		if (res.mfa) return { mfa: { challenge: res.mfa.challenge, methods: res.mfa.methods ?? [] } };
		return { me: this.#signedIn(res) };
	}

	async loginTotp(challenge: string, code: string): Promise<Me> {
		return this.#signedIn(
			await api.post('/api/v1/auth/login/totp', { body: { challenge, code }, auth: false })
		);
	}

	async loginRecovery(challenge: string, code: string): Promise<Me> {
		return this.#signedIn(
			await api.post('/api/v1/auth/login/recovery', { body: { challenge, code }, auth: false })
		);
	}

	async loginPasskey(challenge: string): Promise<Me> {
		const options = await api.post('/api/v1/auth/login/passkey/options', {
			body: { challenge },
			auth: false
		});
		const credential = await getPasskey(options);
		return this.#signedIn(
			await api.post('/api/v1/auth/login/passkey', {
				body: { challenge, credential },
				auth: false
			})
		);
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
