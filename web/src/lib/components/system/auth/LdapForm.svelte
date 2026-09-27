<!-- Sign-in against a directory (Active Directory, OpenLDAP, FreeIPA, …) via the login form. -->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { AuthLDAPConfig, AuthLDAPTest } from '$lib/api/generated';
	import { Alert, Button, Card, Input, Textarea, Toggle } from '$lib/components/ui';
	import { toast } from '$lib/stores/toast.svelte';
	import { apiErrors } from '../system';
	import Provisioning from './Provisioning.svelte';
	import TestSteps from './TestSteps.svelte';

	interface Props {
		config: AuthLDAPConfig;
		roles: { id: number; name: string }[];
		onsaved: (c: AuthLDAPConfig) => void;
	}

	let { config, roles, onsaved }: Props = $props();

	const FIELDS = ['url', 'startTls', 'baseDn', 'userFilter', 'groupFilter', 'ca', 'defaultRoleId'];

	// the form starts from the loaded configuration; later changes come from saving
	let form = $state<AuthLDAPConfig>(
		untrack(() => structuredClone($state.snapshot(config)) as AuthLDAPConfig)
	);
	let password = $state('');
	let removePassword = $state(false);
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	let testUser = $state('');
	let testPassword = $state('');
	let testing = $state(false);
	let test = $state<AuthLDAPTest | null>(null);

	const roleName = (id: number) => roles.find((r) => r.id === id)?.name ?? '–';

	/** fills filter and attributes for a directory type */
	function preset(kind: 'ad' | 'openldap') {
		if (kind === 'ad') {
			Object.assign(form, {
				userFilter: '(&(objectClass=user)(sAMAccountName={username}))',
				usernameAttr: 'sAMAccountName',
				displayNameAttr: 'displayName',
				emailAttr: 'mail',
				groupAttr: 'memberOf',
				groupBaseDn: '',
				groupFilter: ''
			});
		} else {
			Object.assign(form, {
				userFilter: '(&(objectClass=inetOrgPerson)(uid={username}))',
				usernameAttr: 'uid',
				displayNameAttr: 'cn',
				emailAttr: 'mail',
				groupAttr: '',
				groupBaseDn: form.baseDn ? `ou=groups,${form.baseDn.replace(/^ou=[^,]+,/i, '')}` : '',
				groupFilter: '(|(member={dn})(uniqueMember={dn})(memberUid={username}))'
			});
		}
	}

	async function save() {
		saving = true;
		errors = {};
		general = null;
		try {
			const bindPassword = removePassword ? '' : password ? password : undefined;
			const saved = await api.put('/api/v1/system/auth/ldap', {
				body: { config: $state.snapshot(form), bindPassword }
			});
			password = '';
			removePassword = false;
			form = structuredClone(saved);
			onsaved(saved);
			toast.success('LDAP-Anmeldung gespeichert');
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
			test = await api.post('/api/v1/system/auth/ldap/test', {
				body: {
					config: $state.snapshot(form),
					bindPassword: removePassword ? '' : password || undefined,
					username: testUser.trim() || undefined,
					password: testPassword || undefined
				}
			});
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
		title="LDAP / Active Directory"
		description="Anmeldung mit dem Konto aus dem Verzeichnis über das normale Anmeldeformular"
		icon="user"
	>
		<div class="flex flex-col gap-5">
			{#if general}<Alert tone="danger" title="Speichern fehlgeschlagen">{general}</Alert>{/if}
			<Toggle
				bind:checked={form.enabled}
				label="LDAP-Anmeldung aktiv"
				description="Lokale Konten melden sich weiter mit ihrem NetScope-Passwort an; ein lokales Konto hat bei gleichem Namen Vorrang."
			/>
			<div class="flex flex-wrap items-center gap-2 text-sm">
				<span class="text-fg-muted">Vorlage:</span>
				<Button size="sm" onclick={() => preset('ad')}>Active Directory</Button>
				<Button size="sm" onclick={() => preset('openldap')}>OpenLDAP / FreeIPA</Button>
			</div>

			<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
				<Input
					label="Server-URL"
					bind:value={form.url}
					placeholder="ldaps://dc01.example.local"
					hint="ldaps:// (Port 636) oder ldap:// (389), mit StartTLS verschlüsselt"
					error={errors.url}
					mono
					required
				/>
				<Toggle
					bind:checked={form.startTls}
					label="StartTLS"
					description="Verbindung über ldap:// verschlüsseln"
				/>
				<Input
					label="Dienstkonto (Bind-DN)"
					bind:value={form.bindDn}
					placeholder="cn=netscope,ou=services,dc=example,dc=local"
					hint="Leer: anonyme Suche. Im AD auch netscope@example.local"
					mono
				/>
				<div class="flex flex-col gap-1.5">
					<Input
						label="Passwort des Dienstkontos"
						type="password"
						bind:value={password}
						autocomplete="new-password"
						placeholder={form.hasPassword ? 'gespeichert – leer lassen zum Beibehalten' : ''}
						disabled={removePassword}
					/>
					{#if form.hasPassword}
						<label class="flex items-center gap-2 text-xs text-fg-muted">
							<input type="checkbox" bind:checked={removePassword} /> Gespeichertes Passwort entfernen
						</label>
					{/if}
				</div>
				<Input
					label="Basis-DN der Benutzer"
					bind:value={form.baseDn}
					placeholder="ou=people,dc=example,dc=local"
					error={errors.baseDn}
					class="md:col-span-2"
					mono
					required
				/>
				<Input
					label="Benutzerfilter"
					bind:value={form.userFilter}
					hint="{'{username}'} wird durch den eingegebenen Namen ersetzt (sicher maskiert)"
					error={errors.userFilter}
					class="md:col-span-2"
					mono
				/>
				<Input label="Attribut Benutzername" bind:value={form.usernameAttr} placeholder="uid" mono />
				<Input label="Attribut Anzeigename" bind:value={form.displayNameAttr} placeholder="cn" mono />
				<Input label="Attribut E-Mail" bind:value={form.emailAttr} placeholder="mail" mono />
				<Input
					label="Attribut Gruppen"
					bind:value={form.groupAttr}
					placeholder="memberOf"
					hint="Leer: Gruppen per Suche (unten)"
					mono
				/>
				{#if !form.groupAttr?.trim()}
					<Input
						label="Basis-DN der Gruppen"
						bind:value={form.groupBaseDn}
						placeholder="ou=groups,dc=example,dc=local"
						mono
					/>
					<Input
						label="Gruppenfilter"
						bind:value={form.groupFilter}
						hint="{'{dn}'} = DN des Kontos, {'{username}'} = Anmeldename. AD mit verschachtelten Gruppen: (member:1.2.840.113556.1.4.1941:={'{dn}'})"
						error={errors.groupFilter}
						mono
					/>
				{/if}
			</div>

			<Provisioning
				bind:mappings={form.mappings}
				bind:defaultRoleId={form.defaultRoleId}
				bind:syncRole={form.syncRole}
				{roles}
				groupHint="Eine Gruppe passt mit vollem DN oder ihrem Namen (CN), Groß-/Kleinschreibung egal."
				groupPlaceholder="netscope-admins oder cn=netscope-admins,ou=groups,dc=…"
				{errors}
			/>

			<details class="rounded-md border border-border">
				<summary class="cursor-pointer px-3 py-2 text-sm font-medium text-fg-muted hover:text-fg">
					Erweitert: eigene Zertifizierungsstelle
				</summary>
				<div class="flex flex-col gap-4 border-t border-border p-3">
					<Textarea
						label="CA-Zertifikat (PEM)"
						bind:value={form.ca}
						rows={4}
						mono
						placeholder="-----BEGIN CERTIFICATE-----"
						hint="Für Domänencontroller mit Zertifikat der eigenen Firmen-CA"
						error={errors.ca}
					/>
					<Toggle
						bind:checked={form.insecureSkipVerify}
						label="Zertifikat nicht prüfen"
						description="Nur zum Testen – ohne Prüfung könnte ein Angreifer im Netz Passwörter abfangen."
					/>
				</div>
			</details>

			<div class="flex flex-col gap-3 rounded-md border border-border p-3">
				<p class="text-sm font-medium text-fg">Einstellungen prüfen</p>
				<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
					<Input
						label="Testbenutzer (optional)"
						bind:value={testUser}
						autocomplete="off"
						hint="Wird gesucht; mit Passwort auch angemeldet"
					/>
					<Input
						label="Passwort (optional)"
						type="password"
						bind:value={testPassword}
						autocomplete="new-password"
					/>
				</div>
				{#if test}
					<TestSteps steps={test.steps} />
					{#if test.ok && test.dn}
						<dl class="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1 text-xs">
							<dt class="text-fg-subtle">DN</dt>
							<dd class="mono break-all">{test.dn}</dd>
							<dt class="text-fg-subtle">Benutzername</dt>
							<dd class="mono">{test.username || '–'}</dd>
							<dt class="text-fg-subtle">Name, E-Mail</dt>
							<dd>{test.displayName || '–'} · {test.email || '–'}</dd>
							<dt class="text-fg-subtle">Gruppen</dt>
							<dd class="mono break-all">{(test.groups ?? []).join(' · ') || 'keine'}</dd>
							<dt class="text-fg-subtle">Rolle</dt>
							<dd class={test.roleId ? 'font-medium text-fg' : 'font-medium text-danger'}>
								{test.roleId ? roleName(test.roleId) : 'kein Zugriff (keine passende Gruppe)'}
							</dd>
						</dl>
					{/if}
				{/if}
			</div>
		</div>
		{#snippet footer()}
			<div class="flex flex-wrap items-center justify-end gap-2">
				<Button icon="zap" loading={testing} disabled={!form.url.trim()} onclick={check}>Prüfen</Button>
				<Button type="submit" variant="primary" icon="save" loading={saving}>Speichern</Button>
			</div>
		{/snippet}
	</Card>
</form>
