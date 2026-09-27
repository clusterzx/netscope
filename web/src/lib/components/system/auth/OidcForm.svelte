<!-- Sign-in with an OpenID Connect provider (Authentik, Keycloak, Entra ID, …). -->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { AuthOIDCConfig, AuthOIDCTest } from '$lib/api/generated';
	import { Alert, Button, Card, CopyButton, Input, Textarea, Toggle } from '$lib/components/ui';
	import { toast } from '$lib/stores/toast.svelte';
	import { apiErrors } from '../system';
	import Provisioning from './Provisioning.svelte';
	import TestSteps from './TestSteps.svelte';

	interface Props {
		config: AuthOIDCConfig;
		redirectUrl: string;
		roles: { id: number; name: string }[];
		onsaved: (c: AuthOIDCConfig) => void;
	}

	let { config, redirectUrl, roles, onsaved }: Props = $props();

	const FIELDS = ['issuer', 'clientId', 'redirectUrl', 'ca', 'defaultRoleId', 'name'];

	// the form starts from the loaded configuration; later changes come from saving
	let form = $state<AuthOIDCConfig>(
		untrack(() => structuredClone($state.snapshot(config)) as AuthOIDCConfig)
	);
	let scopes = $state(untrack(() => config.scopes.join(' ')));
	let secret = $state('');
	let removeSecret = $state(false);
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	let testing = $state(false);
	let test = $state<AuthOIDCTest | null>(null);

	const callback = $derived(form.redirectUrl?.trim() || redirectUrl);

	function body(): AuthOIDCConfig {
		return { ...$state.snapshot(form), scopes: scopes.split(/[\s,]+/).filter(Boolean) } as AuthOIDCConfig;
	}

	async function save() {
		saving = true;
		errors = {};
		general = null;
		try {
			const clientSecret = removeSecret ? '' : secret ? secret : undefined;
			const saved = await api.put('/api/v1/system/auth/oidc', { body: { config: body(), clientSecret } });
			secret = '';
			removeSecret = false;
			form = structuredClone(saved);
			scopes = saved.scopes.join(' ');
			onsaved(saved);
			toast.success('OIDC-Anmeldung gespeichert');
		} catch (e) {
			({ errors, general } = apiErrors(e, [
				...FIELDS,
				...form.mappings.flatMap((_, i) => [`mappings.${i}.group`, `mappings.${i}.roleId`])
			]));
		} finally {
			saving = false;
		}
	}

	async function check() {
		testing = true;
		test = null;
		try {
			test = await api.post('/api/v1/system/auth/oidc/test', { body: { config: body() } });
		} catch (e) {
			toast.error(e);
		} finally {
			testing = false;
		}
	}
</script>

<form
	novalidate
	onsubmit={(e) => {
		e.preventDefault();
		save();
	}}
>
	<Card
		title="OpenID Connect (SSO)"
		description="Anmeldung über einen Identity Provider wie Authentik, Keycloak, Authelia oder Entra ID"
		icon="key"
	>
		<div class="flex flex-col gap-5">
			{#if general}<Alert tone="danger" title="Speichern fehlgeschlagen">{general}</Alert>{/if}
			<Toggle
				bind:checked={form.enabled}
				label="OIDC-Anmeldung aktiv"
				description="Die Login-Seite zeigt dann „Anmelden mit {form.name || 'Single Sign-on'}“."
			/>

			<div class="flex flex-col gap-1.5 rounded-md border border-border bg-surface-2 p-3">
				<p class="text-xs font-medium text-fg-muted">
					Redirect-URI – beim Identity Provider für diese Anwendung eintragen
				</p>
				<div class="flex items-center gap-2">
					<code class="mono min-w-0 flex-1 text-sm break-all text-fg">{callback}</code>
					<CopyButton text={callback} label="Redirect-URI kopieren" size="sm" />
				</div>
				{#if callback.startsWith('http://') && !callback.includes('localhost')}
					<p class="text-xs text-warn">
						Ohne HTTPS verlangen viele Identity Provider eine Ausnahme; für den Betrieb NetScope hinter einem
						HTTPS-Proxy betreiben.
					</p>
				{/if}
			</div>

			<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
				<Input
					label="Issuer-URL"
					type="url"
					bind:value={form.issuer}
					placeholder="https://auth.example.org/application/o/netscope/"
					hint="Authentik: …/application/o/<slug>/ · Keycloak: …/realms/<realm> · Entra ID: https://login.microsoftonline.com/<tenant>/v2.0"
					error={errors.issuer}
					class="md:col-span-2"
					required
				/>
				<Input label="Client-ID" bind:value={form.clientId} error={errors.clientId} mono required />
				<div class="flex flex-col gap-1.5">
					<Input
						label="Client-Secret"
						type="password"
						bind:value={secret}
						autocomplete="new-password"
						placeholder={form.hasSecret
							? 'gespeichert – leer lassen zum Beibehalten'
							: 'leer bei öffentlichem Client'}
						disabled={removeSecret}
					/>
					{#if form.hasSecret}
						<label class="flex items-center gap-2 text-xs text-fg-muted">
							<input type="checkbox" bind:checked={removeSecret} /> Gespeichertes Secret entfernen
						</label>
					{/if}
				</div>
				<Input
					label="Name auf der Login-Seite"
					bind:value={form.name}
					placeholder="Authentik"
					error={errors.name}
				/>
				<Input
					label="Scopes"
					bind:value={scopes}
					hint="Zusätzlich zu openid, durch Leerzeichen getrennt"
					mono
				/>
				<Input
					label="Claim für den Benutzernamen"
					bind:value={form.usernameClaim}
					placeholder="preferred_username"
					hint="Fällt auf email und sub zurück"
					mono
				/>
				<Input
					label="Claim für Gruppen"
					bind:value={form.groupsClaim}
					placeholder="groups"
					hint="Auch als Pfad, z. B. realm_access.roles (Keycloak); fehlt er im ID-Token, fragt NetScope Userinfo"
					mono
				/>
			</div>

			<Provisioning
				bind:mappings={form.mappings}
				bind:defaultRoleId={form.defaultRoleId}
				bind:syncRole={form.syncRole}
				{roles}
				groupHint="Verglichen wird der Wert im Gruppen-Claim (Groß-/Kleinschreibung egal); Entra ID liefert dort Objekt-IDs."
				groupPlaceholder="netscope-admins"
				{errors}
			/>

			<details class="group rounded-md border border-border">
				<summary class="cursor-pointer px-3 py-2 text-sm font-medium text-fg-muted hover:text-fg">
					Erweitert: Redirect-URI, eigene Zertifizierungsstelle
				</summary>
				<div class="flex flex-col gap-4 border-t border-border p-3">
					<Input
						label="Redirect-URI festlegen"
						type="url"
						bind:value={form.redirectUrl}
						placeholder={redirectUrl}
						hint="Nur nötig, wenn NetScope hinter einem Proxy unter einer anderen Adresse erreichbar ist"
						error={errors.redirectUrl}
					/>
					<Textarea
						label="CA-Zertifikat (PEM)"
						bind:value={form.ca}
						rows={4}
						mono
						placeholder="-----BEGIN CERTIFICATE-----"
						hint="Für Identity Provider mit Zertifikat einer eigenen Zertifizierungsstelle"
						error={errors.ca}
					/>
					<Toggle
						bind:checked={form.insecureSkipVerify}
						label="Zertifikat nicht prüfen"
						description="Nur zum Testen – ohne Prüfung kann sich jeder im Netz als Identity Provider ausgeben."
					/>
				</div>
			</details>

			{#if test}
				<div class="flex flex-col gap-2 rounded-md border border-border p-3">
					<p class="text-sm font-medium {test.ok ? 'text-ok' : 'text-danger'}">
						{test.ok ? 'Identity Provider erreichbar' : 'Prüfung fehlgeschlagen'}
					</p>
					<TestSteps steps={test.steps} />
					{#if test.ok}
						<p class="text-xs text-fg-muted">
							Signaturschlüssel: {test.keys} · Algorithmen: {(test.signingAlgorithms ?? []).join(', ') || '–'}
						</p>
					{/if}
				</div>
			{/if}
		</div>
		{#snippet footer()}
			<div class="flex flex-wrap items-center justify-end gap-2">
				<Button icon="zap" loading={testing} disabled={!form.issuer.trim()} onclick={check}
					>Verbindung prüfen</Button
				>
				<Button type="submit" variant="primary" icon="save" loading={saving}>Speichern</Button>
			</div>
		{/snippet}
	</Card>
</form>
