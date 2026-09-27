<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { ApiError, errorMessage } from '$lib/api/client';
	import Button from '$lib/components/ui/Button.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Alert from '$lib/components/ui/Alert.svelte';
	import { auth, type MfaStep } from '$lib/stores/auth.svelte';
	import { applyPreference, t } from '$lib/i18n';
	import { theme } from '$lib/stores/theme.svelte';
	import { passkeysSupported } from '$lib/utils/webauthn';

	let username = $state('admin');
	let password = $state('');
	// errors of an OIDC sign-in come back as ?error=
	let error = $state(page.url.searchParams.get('error') ?? '');
	let providers = $state<{ oidc: boolean; oidcName?: string; ldap: boolean } | null>(null);
	const ssoHref = $derived(
		'/api/v1/auth/oidc/start' + (target() !== '/' ? `?next=${encodeURIComponent(target())}` : '')
	);
	let busy = $state(false);
	let pwInput: HTMLInputElement | null = $state(null);
	let userInput: HTMLInputElement | null = $state(null);

	// second step: code from the authenticator app, passkey or recovery code
	let mfa = $state<MfaStep | null>(null);
	let method = $state<'totp' | 'passkey' | 'recovery'>('totp');
	let code = $state('');
	let codeInput: HTMLInputElement | null = $state(null);
	const canPasskey = passkeysSupported();
	const methods = $derived(
		(mfa?.methods ?? []).filter((m) => m !== 'passkey' || canPasskey) as ('totp' | 'passkey' | 'recovery')[]
	);
	const methodLabel = {
		totp: t('Code aus der App'),
		passkey: 'Passkey',
		recovery: t('Wiederherstellungscode')
	};

	/** only local paths are accepted as redirect target */
	function target(): string {
		const next = page.url.searchParams.get('next') ?? '/';
		return next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/login') ? next : '/';
	}

	onMount(async () => {
		try {
			if (await auth.check(true)) {
				await proceed();
				return;
			}
		} catch {
			// backend unreachable – the form shows the error on submit
		}
		try {
			providers = await api.get('/api/v1/auth/providers');
			// directory accounts sign in with their own name
			if (providers?.ldap && username === 'admin') username = '';
		} catch {
			// without the list only the password form is offered
		}
		(username ? pwInput : userInput)?.focus();
	});

	/** after the login: finish a pending setup first */
	async function proceed() {
		const next = auth.restricted ? '/setup' : target();
		// the user's language preference may differ from the browser language of this page
		if (applyPreference(auth.me?.user?.locale)) window.location.assign(next);
		else await goto(next, { replaceState: true });
	}

	function useMethod(m: 'totp' | 'passkey' | 'recovery') {
		method = m;
		code = '';
		error = '';
		if (m !== 'passkey') queueMicrotask(() => codeInput?.focus());
	}

	function restart(msg = '') {
		mfa = null;
		password = '';
		code = '';
		error = msg;
		queueMicrotask(() => pwInput?.focus());
	}

	async function second(e?: SubmitEvent) {
		e?.preventDefault();
		if (!mfa) return;
		if (method !== 'passkey' && !code.trim()) {
			error =
				method === 'totp'
					? t('Den 6-stelligen Code aus der App eingeben')
					: t('Einen Wiederherstellungscode eingeben');
			return;
		}
		busy = true;
		error = '';
		try {
			if (method === 'totp') await auth.loginTotp(mfa.challenge, code);
			else if (method === 'recovery') await auth.loginRecovery(mfa.challenge, code);
			else await auth.loginPasskey(mfa.challenge);
			await proceed();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'challenge_expired') restart(err.message);
			else {
				error = errorMessage(err);
				code = '';
				codeInput?.focus();
			}
		} finally {
			busy = false;
		}
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!username.trim() || !password) {
			error = t('Benutzername und Passwort eingeben');
			return;
		}
		busy = true;
		error = '';
		try {
			const res = await auth.login(username.trim(), password);
			if (res.mfa) {
				mfa = res.mfa;
				const avail = res.mfa.methods.filter((m) => m !== 'passkey' || canPasskey);
				useMethod(avail.includes('passkey') ? 'passkey' : avail.includes('totp') ? 'totp' : 'recovery');
				return;
			}
			await proceed();
		} catch (err) {
			error =
				err instanceof ApiError && err.status === 401
					? t('Benutzername oder Passwort falsch')
					: errorMessage(err);
			password = '';
			pwInput?.focus();
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>{t('Anmelden')} · NetScope</title>
</svelte:head>

<div class="relative flex min-h-dvh items-center justify-center overflow-hidden bg-bg px-4 py-10">
	<!-- subtle radar rings as background texture -->
	<svg class="pointer-events-none absolute inset-0 h-full w-full text-border" aria-hidden="true">
		<defs>
			<pattern id="grid" width="32" height="32" patternUnits="userSpaceOnUse">
				<path d="M32 0H0V32" fill="none" stroke="currentColor" stroke-width="0.6" opacity="0.6" />
			</pattern>
			<radialGradient id="fade" cx="50%" cy="45%" r="60%">
				<stop offset="0%" stop-color="white" stop-opacity="1" />
				<stop offset="100%" stop-color="white" stop-opacity="0" />
			</radialGradient>
			<mask id="m"><rect width="100%" height="100%" fill="url(#fade)" /></mask>
		</defs>
		<rect width="100%" height="100%" fill="url(#grid)" mask="url(#m)" />
	</svg>

	<div class="relative w-full max-w-sm">
		<div class="mb-6 flex flex-col items-center gap-3 text-center">
			<img src="/favicon.svg" alt="" width="52" height="52" class="rounded-xl shadow-md" />
			<div>
				<h1 class="text-2xl font-semibold tracking-tight text-fg">NetScope</h1>
				<p class="mt-1 text-sm text-fg-muted">{t('Netzwerk-Inventar & Asset-Monitoring')}</p>
			</div>
		</div>

		{#if mfa}
			<form onsubmit={second} class="rounded-xl border border-border bg-surface p-6 shadow-md" novalidate>
				<div class="flex flex-col gap-4">
					<div>
						<h2 class="text-base font-semibold text-fg">{t('Zweiter Faktor')}</h2>
						<p class="mt-0.5 text-sm text-fg-muted">
							{method === 'totp'
								? t('Den aktuellen Code aus deiner Authenticator-App eingeben.')
								: method === 'passkey'
									? t(
											'Mit deinem Passkey bestätigen (Fingerabdruck, Gesicht, PIN oder Sicherheitsschlüssel).'
										)
									: t('Einen deiner Wiederherstellungscodes eingeben – jeder gilt nur einmal.')}
						</p>
					</div>
					{#if error}
						<Alert tone="danger">{error}</Alert>
					{/if}
					{#if method === 'passkey'}
						<Button type="submit" variant="primary" icon="key" full loading={busy}
							>{t('Mit Passkey bestätigen')}</Button
						>
					{:else}
						<Input
							label={methodLabel[method]}
							bind:value={code}
							bind:ref={codeInput}
							inputmode={method === 'totp' ? 'numeric' : 'text'}
							autocomplete="one-time-code"
							autocapitalize="none"
							spellcheck={false}
							placeholder={method === 'totp' ? '123456' : 'xxxxx-xxxxx'}
							mono
							required
						/>
						<Button type="submit" variant="primary" full loading={busy}>{t('Bestätigen')}</Button>
					{/if}
					{#if methods.length > 1}
						<div class="flex flex-wrap gap-x-3 gap-y-1 text-xs">
							{#each methods.filter((m) => m !== method) as m (m)}
								<button type="button" class="text-accent hover:underline" onclick={() => useMethod(m)}>
									{m === 'recovery'
										? t('Wiederherstellungscode verwenden')
										: t('Stattdessen: {method}', { method: methodLabel[m] })}
								</button>
							{/each}
						</div>
					{/if}
					<button
						type="button"
						class="self-start text-xs text-fg-subtle hover:text-fg hover:underline"
						onclick={() => restart()}
					>
						{t('Zurück zur Anmeldung')}
					</button>
				</div>
			</form>
		{:else}
			<form onsubmit={submit} class="rounded-xl border border-border bg-surface p-6 shadow-md" novalidate>
				<div class="flex flex-col gap-4">
					{#if error}
						<Alert tone="danger">{error}</Alert>
					{/if}
					<Input
						label={t('Benutzername')}
						bind:value={username}
						bind:ref={userInput}
						autocomplete="username"
						autocapitalize="none"
						spellcheck={false}
						required
					/>
					<Input
						label={t('Passwort')}
						type="password"
						bind:value={password}
						bind:ref={pwInput}
						autocomplete="current-password"
						required
					/>
					<Button type="submit" variant="primary" full loading={busy}>{t('Anmelden')}</Button>
					{#if providers?.oidc}
						<div class="flex items-center gap-3 text-xs text-fg-subtle" aria-hidden="true">
							<span class="h-px flex-1 bg-border"></span>{t('oder')}<span class="h-px flex-1 bg-border"
							></span>
						</div>
						<!-- a full page load: the server redirects to the identity provider -->
						<a
							href={ssoHref}
							data-sveltekit-reload
							class="inline-flex h-9 w-full items-center justify-center gap-2 rounded-md border border-border bg-surface px-4 text-sm font-medium text-fg shadow-sm transition-colors hover:border-border-strong hover:bg-surface-2"
						>
							<Icon name="key" size={15} />{t('Anmelden mit {provider}', {
								provider: providers.oidcName || 'Single Sign-on'
							})}
						</a>
					{/if}
					{#if providers?.ldap}
						<p class="text-center text-xs text-fg-subtle">
							{t('Auch mit dem Konto aus dem Verzeichnis (LDAP / Active Directory).')}
						</p>
					{/if}
				</div>
			</form>
		{/if}
		<p class="mt-5 text-center text-xs text-fg-subtle">
			{t('Darstellung:')}
			<button
				type="button"
				class="underline-offset-2 hover:text-fg hover:underline"
				onclick={() => theme.cycle()}
			>
				{theme.mode === 'system' ? 'System' : theme.mode === 'dark' ? t('Dunkel') : t('Hell')}
			</button>
		</p>
	</div>
</div>
