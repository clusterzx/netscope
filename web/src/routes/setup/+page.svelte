<!-- First-login setup: change the start password an administrator handed out, then set up the
     second factor the role requires. Every other page redirects here until both are done. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import PasswordForm from '$lib/components/account/PasswordForm.svelte';
	import RecoveryCodes from '$lib/components/account/RecoveryCodes.svelte';
	import TotpSetup from '$lib/components/account/TotpSetup.svelte';
	import { Alert, Button } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { createPasskey, passkeysSupported } from '$lib/utils/webauthn';

	let codes = $state<string[] | null>(null);
	let passkeysAvailable = $state(false);
	let busy = $state(false);
	let error = $state('');

	$effect(() => {
		if (auth.restricted !== 'mfa' || !passkeysSupported()) return;
		api
			.get('/api/v1/auth/2fa')
			.then((st) => (passkeysAvailable = st.passkeysAvailable))
			.catch(() => {});
	});

	async function finish() {
		await auth.check(true);
		if (!auth.restricted) await goto('/', { replaceState: true });
	}

	async function afterFactor(list: string[]) {
		if (list.length) codes = list;
		else await finish();
	}

	async function addPasskey() {
		busy = true;
		error = '';
		try {
			const credential = await createPasskey(await api.post('/api/v1/auth/passkeys/options'));
			const res = await api.post('/api/v1/auth/passkeys', { body: { name: 'Passkey', credential } });
			toast.success('Passkey registriert');
			await afterFactor(res.recoveryCodes ?? []);
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>Konto einrichten · NetScope</title>
</svelte:head>

<div class="flex min-h-dvh items-start justify-center bg-bg px-4 py-10 sm:items-center">
	<div class="w-full max-w-lg">
		<div class="mb-5 flex items-center gap-3">
			<img src="/favicon.svg" alt="" width="36" height="36" class="rounded-lg" />
			<div class="min-w-0 flex-1">
				<h1 class="text-lg font-semibold text-fg">Konto einrichten</h1>
				<p class="truncate text-sm text-fg-muted">
					{auth.me?.user?.displayName || auth.me?.user?.username} · {auth.me?.principal?.roleName}
				</p>
			</div>
			<Button size="sm" variant="ghost" icon="logout" onclick={() => auth.logout()}>Abmelden</Button>
		</div>

		<div class="rounded-xl border border-border bg-surface p-6 shadow-md">
			{#if codes}
				<h2 class="mb-3 text-base font-semibold text-fg">Wiederherstellungscodes sichern</h2>
				<RecoveryCodes {codes} />
				<div class="mt-4">
					<Button variant="primary" onclick={finish}>Gespeichert – weiter zu NetScope</Button>
				</div>
			{:else if auth.restricted === 'password'}
				<h2 class="text-base font-semibold text-fg">Eigenes Passwort festlegen</h2>
				<p class="mt-1 mb-4 text-sm text-fg-muted">
					Das Konto wurde von einem Administrator angelegt oder sein Passwort zurückgesetzt. Bitte jetzt ein
					eigenes Passwort wählen – das Start-Passwort gilt danach nicht mehr.
				</p>
				<PasswordForm start onchanged={finish} />
			{:else if auth.restricted === 'mfa'}
				<h2 class="text-base font-semibold text-fg">Zweiten Faktor einrichten</h2>
				<p class="mt-1 mb-4 text-sm text-fg-muted">
					Die Rolle „{auth.me?.principal?.roleName}“ verlangt neben dem Passwort einen zweiten Faktor.
				</p>
				{#if error}<div class="mb-3"><Alert tone="danger">{error}</Alert></div>{/if}
				{#if passkeysAvailable}
					<div class="mb-5 flex flex-col gap-2 rounded-lg border border-border p-4">
						<h3 class="text-sm font-medium text-fg">Passkey</h3>
						<p class="text-sm text-fg-muted">
							Fingerabdruck, Gesichtserkennung, Geräte-PIN oder Sicherheitsschlüssel.
						</p>
						<div><Button icon="key" loading={busy} onclick={addPasskey}>Passkey einrichten</Button></div>
					</div>
					<h3 class="mb-2 text-sm font-medium text-fg">Oder: Authenticator-App</h3>
				{/if}
				<TotpSetup ondone={afterFactor} />
			{:else}
				<p class="text-sm text-fg-muted">Das Konto ist eingerichtet.</p>
				<div class="mt-4"><Button variant="primary" href="/">Weiter zu NetScope</Button></div>
			{/if}
		</div>
	</div>
</div>
