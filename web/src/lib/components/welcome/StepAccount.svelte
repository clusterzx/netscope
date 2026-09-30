<!--
	Step 2: the administrator account. "Weiter" creates it with the setup code and signs in;
	from then on the wizard continues with this session. TOTP can be set up right away.
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import RecoveryCodes from '$lib/components/account/RecoveryCodes.svelte';
	import TotpSetup from '$lib/components/account/TotpSetup.svelte';
	import { Alert, Badge, Button, Input } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { wizard } from './wizard.svelte';

	const MIN = 10;
	let password = $state('');
	let repeat = $state('');
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	let totpOpen = $state(false);
	let codes = $state<string[] | null>(null);

	const account = $derived(wizard.options?.account ?? null);

	/** Creates the account (once); false keeps the wizard on this step. */
	export async function submit(): Promise<boolean> {
		if (account) return true;
		const d = wizard.data;
		const e: Record<string, string> = {};
		if (!/^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$/.test(d.username.trim()))
			e.username = t('1–64 Zeichen: Buchstaben, Ziffern, . _ @ -');
		if (password.length < MIN) e.password = t('Mindestens {n} Zeichen', { n: MIN });
		else if (password !== repeat) e.repeat = t('Die Passwörter stimmen nicht überein');
		errors = e;
		formError = '';
		if (Object.keys(e).length) return false;
		try {
			const res = await api.post('/api/v1/setup/account', {
				body: {
					username: d.username.trim(),
					displayName: d.displayName.trim(),
					password,
					locale: d.language
				},
				auth: false
			});
			password = repeat = '';
			await auth.check(true);
			if (wizard.options) wizard.options.account = res.user;
			return true;
		} catch (err) {
			errors = fieldErrors(err);
			formError = Object.keys(errors).length ? '' : errorMessage(err);
			return false;
		}
	}

	async function totpDone(list: string[]) {
		totpOpen = false;
		if (list.length) codes = list;
		try {
			const me = await auth.check(true);
			if (me?.user && wizard.options) wizard.options.account = me.user;
		} catch {
			// the badge updates with the next visit
		}
	}
</script>

{#if account}
	<div class="flex flex-col gap-4">
		<Alert tone="ok" title={t('Konto angelegt')}>
			{t('Angemeldet als {name}. Der Assistent arbeitet ab jetzt mit diesem Konto weiter.', {
				name: account.displayName || account.username
			})}
		</Alert>
		<div class="flex flex-col gap-2 rounded-lg border border-border p-4">
			<div class="flex flex-wrap items-center gap-2">
				<span class="font-medium text-fg">{t('Zweiter Faktor (TOTP)')}</span>
				{#if account.totp}
					<Badge tone="ok" dot>{t('eingerichtet')}</Badge>
				{:else}
					<Badge>optional</Badge>
				{/if}
			</div>
			<p class="text-sm text-fg-muted">
				{t(
					'Ein Code aus einer Authenticator-App schützt das Konto zusätzlich. Passkeys lassen sich später unter System → Konto hinzufügen.'
				)}
			</p>
			{#if codes}
				<RecoveryCodes {codes} />
			{:else if totpOpen}
				<TotpSetup ondone={totpDone} oncancel={() => (totpOpen = false)} />
			{:else if !account.totp}
				<div><Button icon="lock" onclick={() => (totpOpen = true)}>{t('TOTP jetzt einrichten')}</Button></div>
			{/if}
		</div>
	</div>
{:else}
	<div class="flex flex-col gap-4">
		{#if formError}<Alert tone="danger">{formError}</Alert>{/if}
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			<Input
				id="welcome-username"
				label={t('Benutzername')}
				bind:value={wizard.data.username}
				autocomplete="username"
				error={errors.username}
				required
			/>
			<Input
				id="welcome-displayname"
				label={t('Anzeigename')}
				bind:value={wizard.data.displayName}
				autocomplete="name"
				placeholder={t('z. B. Vor- und Nachname')}
				error={errors.displayName}
			/>
			<Input
				id="welcome-password"
				type="password"
				label={t('Passwort')}
				bind:value={password}
				autocomplete="new-password"
				hint={t('Mindestens {n} Zeichen', { n: MIN })}
				error={errors.password}
				required
			/>
			<Input
				id="welcome-repeat"
				type="password"
				label={t('Passwort wiederholen')}
				bind:value={repeat}
				autocomplete="new-password"
				error={errors.repeat}
				required
			/>
		</div>
		<p class="text-xs text-fg-subtle">
			{t(
				'Das Konto erhält die Rolle Administrator. „Konto anlegen“ legt es an und meldet dich an; danach kannst du TOTP einrichten.'
			)}
		</p>
	</div>
{/if}
