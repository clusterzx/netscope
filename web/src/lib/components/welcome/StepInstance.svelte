<!--
	Step 3: role of the instance (on its own, central instance, site of a central instance)
	and the public URL. A site tests the connection to its central instance.
-->
<script lang="ts">
	import { api, errorMessage } from '$lib/api';
	import type { FederationTestResult } from '$lib/api';
	import { Alert, Icon, Input, Button } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { wizard, type Role } from './wizard.svelte';

	let { errors = {} }: { errors?: Record<string, string> } = $props();

	const managed = $derived(!!wizard.options?.federation.managed);
	let local = $state<Record<string, string>>({});
	const err = $derived({ ...errors, ...local });
	let testing = $state(false);
	let test = $state<FederationTestResult | null>(null);
	let testError = $state('');

	const roles: { value: Role; title: string; text: string; icon: 'home' | 'layers' | 'network' }[] = [
		{
			value: 'standalone',
			title: t('Allein'),
			text: t('Diese Instanz arbeitet für sich.'),
			icon: 'home'
		},
		{
			value: 'central',
			title: t('Zentrale'),
			text: t('Standorte liefern ihre Geräte und Events hierher.'),
			icon: 'layers'
		},
		{
			value: 'site',
			title: t('Standort'),
			text: t('Diese Instanz liefert an eine Zentrale.'),
			icon: 'network'
		}
	];

	function federationBody() {
		const d = wizard.data;
		return {
			role: d.role,
			localName: d.localName.trim(),
			centralUrl: d.centralUrl.trim(),
			fingerprint: d.fingerprint.trim(),
			token: d.token.trim() || undefined
		};
	}

	async function runTest() {
		testing = true;
		test = null;
		testError = '';
		try {
			test = await api.post('/api/v1/setup/federation/test', { body: federationBody(), auth: false });
		} catch (e) {
			testError = errorMessage(e);
		} finally {
			testing = false;
		}
	}

	/** Checks the step; false keeps the wizard here. */
	export function validate(): boolean {
		const d = wizard.data;
		const e: Record<string, string> = {};
		const url = (v: string) => {
			try {
				const u = new URL(v);
				return (u.protocol === 'http:' || u.protocol === 'https:') && !!u.host;
			} catch {
				return false;
			}
		};
		if (d.publicUrl.trim() && !url(d.publicUrl.trim()))
			e.publicUrl = t('Gültige http(s)-URL erwartet, z. B. https://netscope.lan');
		if (d.role === 'site' && !managed) {
			if (!url(d.centralUrl.trim())) e['federation.centralUrl'] = t('http(s)-URL der Zentrale erwartet');
			if (!d.token.trim().startsWith('nss_')) e['federation.token'] = t('Standort-Tokens beginnen mit nss_');
		}
		local = e;
		return !Object.keys(e).length;
	}
</script>

<div class="flex flex-col gap-6">
	{#if managed}
		<Alert tone="info" title={t('Standort über Umgebungsvariablen')}>
			{t(
				'Diese Instanz liefert an {url} (NETSCOPE_CENTRAL_URL). Die Anbindung lässt sich hier nicht ändern.',
				{
					url: wizard.options?.federation.centralUrl ?? ''
				}
			)}
		</Alert>
	{:else}
		<fieldset>
			<legend class="mb-2 text-sm font-medium text-fg">{t('Rolle im Verbund')}</legend>
			<div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
				{#each roles as r (r.value)}
					<label
						class="flex cursor-pointer flex-col gap-1 rounded-lg border px-4 py-3 transition-colors
						{wizard.data.role === r.value ? 'border-accent bg-accent-soft' : 'border-border hover:border-border-strong'}"
					>
						<span class="flex items-center gap-2">
							<input
								type="radio"
								name="role"
								value={r.value}
								bind:group={wizard.data.role}
								class="accent-accent"
								onchange={() => (test = null)}
							/>
							<Icon name={r.icon} size={16} class="text-fg-muted" />
							<span class="font-medium text-fg">{r.title}</span>
						</span>
						<span class="text-sm text-fg-muted">{r.text}</span>
					</label>
				{/each}
			</div>
		</fieldset>

		{#if wizard.data.role === 'central'}
			<Input
				id="welcome-localname"
				label={t('Name dieser Instanz')}
				bind:value={wizard.data.localName}
				placeholder={t('z. B. Zuhause')}
				hint={t('So heißt diese Instanz in der Standortauswahl neben den Standorten.')}
				error={err['federation.localName']}
				maxlength={64}
			/>
		{:else if wizard.data.role === 'site'}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
				<Input
					id="welcome-central"
					label={t('URL der Zentrale')}
					type="url"
					bind:value={wizard.data.centralUrl}
					placeholder="https://netscope.example.org"
					error={err['federation.centralUrl']}
					class="sm:col-span-2"
					required
				/>
				<Input
					id="welcome-token"
					label={t('Token des Standorts')}
					type="password"
					bind:value={wizard.data.token}
					placeholder="nss_…"
					hint={t('Wird in der Zentrale beim Anlegen des Standorts angezeigt.')}
					error={err['federation.token']}
					autocomplete="off"
					mono
					required
				/>
				<Input
					id="welcome-fingerprint"
					label={t('Zertifikat-Fingerprint (optional)')}
					bind:value={wizard.data.fingerprint}
					placeholder="SHA-256"
					hint={t('Nur für selbstsignierte Zertifikate der Zentrale.')}
					error={err['federation.fingerprint']}
					mono
				/>
			</div>
			<div class="flex flex-col gap-2">
				<div>
					<Button icon="zap" onclick={() => validate() && runTest()} loading={testing}>
						{t('Verbindung testen')}
					</Button>
				</div>
				<div aria-live="polite">
					{#if testError}
						<p class="text-sm text-danger">{testError}</p>
					{:else if test?.ok}
						<p class="flex items-center gap-1.5 text-sm text-ok">
							<Icon name="check-circle" size={16} />
							{t('Verbunden – die Zentrale kennt diesen Standort als „{name}“.', { name: test.site ?? '' })}
						</p>
					{:else if test}
						<p class="flex items-start gap-1.5 text-sm text-danger">
							<Icon name="x-circle" size={16} class="mt-0.5 shrink-0" />{test.error}
						</p>
					{/if}
				</div>
			</div>
		{/if}
	{/if}

	<Input
		id="welcome-publicurl"
		label={t('Öffentliche URL')}
		type="url"
		bind:value={wizard.data.publicUrl}
		placeholder="https://netscope.example.lan"
		hint={t(
			'Unter dieser Adresse erreichen Benutzer NetScope – für Links in Benachrichtigungen, den Installationsbefehl des Agents und Passkeys. Vorschlag: die aufgerufene Adresse.'
		)}
		error={err.publicUrl}
	/>
</div>
