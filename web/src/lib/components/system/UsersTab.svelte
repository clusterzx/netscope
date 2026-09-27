<!-- Users: create, edit (role, language, disabled), new start password, reset the second factor, delete. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { Role, User } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		Card,
		CopyButton,
		EmptyState,
		ErrorState,
		Input,
		Menu,
		Modal,
		RelativeTime,
		Select,
		Table,
		Toggle
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { LOCALES, applyPreference, t, tn } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { apiErrors } from './system';

	const users = new AsyncData<User[]>();
	const roles = new AsyncData<Role[]>();
	$effect(() => {
		users.run(async (signal) => (await api.get('/api/v1/users', { signal })) ?? []);
		roles.run(async (signal) => (await api.get('/api/v1/roles', { signal })) ?? []);
	});
	const me = $derived(auth.me?.user?.id);
	const roleOptions = $derived((roles.data ?? []).map((r) => ({ value: String(r.id), label: r.name })));

	// language preference: '' follows the browser; the language names stay in their own language
	const localeOptions = [{ value: '', label: t('Wie im Browser') }, ...LOCALES];
	const localeName = (l: string) => LOCALES.find((x) => x.value === l)?.label;

	// ---------------------------------------------------------------- create / edit
	let open = $state(false);
	let editing = $state<User | null>(null);
	let username = $state('');
	let displayName = $state('');
	let email = $state('');
	let roleId = $state('');
	let locale = $state('');
	let disabled = $state(false);
	let password = $state('');
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);

	function openEditor(u: User | null) {
		editing = u;
		username = u?.username ?? '';
		displayName = u?.displayName ?? '';
		email = u?.email ?? '';
		const fallback = roles.data?.find((r) => r.name === 'Betrachter') ?? roles.data?.[roles.data.length - 1]; // i18n-ignore: built-in role name
		roleId = String(u?.roleId ?? fallback?.id ?? '');
		locale = u?.locale ?? '';
		disabled = u?.disabled ?? false;
		password = '';
		errors = {};
		general = null;
		open = true;
	}

	async function save() {
		const e: Record<string, string> = {};
		if (!username.trim()) e.username = t('Benutzername erforderlich');
		if (!roleId) e.roleId = t('Rolle wählen');
		if (!editing && password && password.length < 10)
			e.password = t('Mindestens 10 Zeichen – oder leer lassen');
		errors = e;
		if (Object.keys(e).length) return;
		saving = true;
		general = null;
		const body = { username: username.trim(), displayName, email, roleId: Number(roleId), locale, disabled };
		try {
			if (editing) {
				const saved = await api.put('/api/v1/users/{id}', { path: { id: editing.id }, body });
				toast.success(t('„{name}“ gespeichert', { name: body.username }));
				// the own language changed here: show the page in it
				if (editing.id === me && applyPreference(saved.locale)) window.location.reload();
			} else {
				const res = await api.post('/api/v1/users', { body: { ...body, password: password || undefined } });
				showPassword(res.user?.username ?? body.username, res.password || password, true);
			}
			open = false;
			users.reload();
			roles.reload();
		} catch (err) {
			({ errors, general } = apiErrors(err, [
				'username',
				'displayName',
				'email',
				'roleId',
				'locale',
				'disabled',
				'password'
			]));
		} finally {
			saving = false;
		}
	}

	// ---------------------------------------------------------------- start password shown once
	let pwShown = $state<{ user: string; password: string; created: boolean } | null>(null);
	function showPassword(user: string, pw: string, created: boolean) {
		pwShown = { user, password: pw, created };
	}

	async function resetPassword(u: User) {
		const ok = await confirm({
			title: t('Neues Start-Passwort für „{name}“?', { name: u.username }),
			message: t(
				'Das bisherige Passwort gilt sofort nicht mehr, laufende Sitzungen enden. Beim nächsten Login muss ein eigenes Passwort gewählt werden.'
			),
			confirmLabel: t('Neues Passwort erzeugen')
		});
		if (!ok) return;
		try {
			const res = await api.post('/api/v1/users/{id}/password', { path: { id: u.id } });
			showPassword(u.username, res.password, false);
			users.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	async function resetMfa(u: User) {
		const ok = await confirm({
			title: t('Zweiten Faktor von „{name}“ zurücksetzen?', { name: u.username }),
			message: t(
				'Entfernt TOTP, alle Passkeys und die Wiederherstellungscodes, z. B. nach Verlust des Handys. Laufende Sitzungen enden. Verlangt die Rolle 2FA, wird sie beim nächsten Login neu eingerichtet.'
			),
			confirmLabel: t('Zurücksetzen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.post('/api/v1/users/{id}/2fa/reset', { path: { id: u.id } });
			toast.success(t('Zweiter Faktor zurückgesetzt'));
			users.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	async function remove(u: User) {
		const ok = await confirm({
			title: t('„{name}“ löschen?', { name: u.username }),
			message: t(
				'Das Konto, seine Sitzungen und API-Tokens werden gelöscht. Das Audit-Log behält die Einträge. Alternativ lässt sich das Konto deaktivieren.'
			),
			confirmLabel: t('Löschen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/users/{id}', { path: { id: u.id } });
			toast.success(t('„{name}“ gelöscht', { name: u.username }));
			users.reload();
			roles.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	const columns: Column<User>[] = [
		{ key: 'user', label: t('Benutzer') },
		{ key: 'role', label: t('Rolle') },
		{ key: 'mfa', label: '2FA' },
		{ key: 'status', label: 'Status', hideBelow: 'md' },
		{ key: 'locale', label: t('Sprache'), hideBelow: 'xl' },
		{ key: 'lastLoginAt', label: t('Letzte Anmeldung'), hideBelow: 'lg' },
		{ key: 'actions', label: '', align: 'right', width: '3rem' }
	];
</script>

<Card
	title={t('Benutzer@@Mehrzahl')}
	description={t('Jeder Benutzer hat genau eine Rolle')}
	icon="user"
	padding="none"
>
	{#snippet actions()}
		<Button size="sm" variant="primary" icon="plus" onclick={() => openEditor(null)} disabled={!roles.data}
			>{t('Benutzer anlegen')}</Button
		>
	{/snippet}
	{#if users.error && !users.data}
		<ErrorState error={users.error} onretry={() => users.reload()} />
	{:else}
		<Table
			{columns}
			rows={users.data ?? []}
			key={(u) => u.id}
			loading={users.loading && !users.data}
			class="rounded-none border-0"
			caption={t('Benutzer@@Mehrzahl')}
		>
			{#snippet cell(u, col)}
				{#if col.key === 'user'}
					<span class="font-medium {u.disabled ? 'text-fg-subtle line-through' : ''}">{u.username}</span>
					{#if u.id === me}<span class="text-xs text-fg-subtle"> {t('(du)')}</span>{/if}
					{#if u.authSource === 'ldap'}<Badge
							tone="info"
							title={t('Meldet sich mit dem Konto aus dem Verzeichnis an')}>LDAP</Badge
						>{:else if u.authSource === 'oidc'}<Badge
							tone="info"
							title={t('Meldet sich über den Identity Provider an')}>SSO</Badge
						>{/if}
					{#if u.displayName || u.email}
						<span class="block text-xs text-fg-subtle"
							>{[u.displayName, u.email].filter(Boolean).join(' · ')}</span
						>
					{/if}
				{:else if col.key === 'role'}
					{u.roleName}
				{:else if col.key === 'mfa'}
					<span class="inline-flex flex-wrap gap-1">
						{#if u.totp}<Badge tone="ok">App</Badge>{/if}
						{#if u.passkeys}<Badge tone="ok">{tn(u.passkeys, '{n} Passkey', '{n} Passkeys')}</Badge>{/if}
						{#if !u.totp && !u.passkeys}
							<Badge
								tone={u.mfaRequired ? 'warn' : 'neutral'}
								title={u.mfaRequired ? t('Die Rolle verlangt 2FA') : undefined}
								>{u.mfaRequired ? t('noch nicht eingerichtet') : t('aus')}</Badge
							>
						{/if}
					</span>
				{:else if col.key === 'status'}
					{#if u.disabled}
						<Badge tone="neutral">{t('deaktiviert')}</Badge>
					{:else if u.mustChangePassword}
						<Badge tone="warn" title={t('Muss beim nächsten Login ein eigenes Passwort wählen')}
							>{t('Start-Passwort')}</Badge
						>
					{:else}
						<Badge tone="ok">{t('aktiv')}</Badge>
					{/if}
				{:else if col.key === 'locale'}
					{#if localeName(u.locale)}{localeName(u.locale)}{:else}<span class="text-fg-subtle"
							>{t('Wie im Browser')}</span
						>{/if}
				{:else if col.key === 'lastLoginAt'}
					{#if u.lastLoginAt}<RelativeTime value={u.lastLoginAt} class="text-fg-muted" />{:else}<span
							class="text-fg-subtle">{t('noch nie')}</span
						>{/if}
				{:else if col.key === 'actions'}
					<Menu
						label={t('Aktionen für {name}', { name: u.username })}
						size="sm"
						items={[
							{ label: t('Bearbeiten'), icon: 'edit', onclick: () => openEditor(u) },
							{
								label: t('Neues Start-Passwort'),
								icon: 'key',
								disabled: u.authSource !== 'local',
								hint: u.authSource !== 'local' ? t('Passwort liegt im Verzeichnis bzw. beim IdP') : undefined,
								onclick: () => resetPassword(u)
							},
							{
								label: t('Zweiten Faktor zurücksetzen'),
								icon: 'shield',
								disabled: !u.totp && !u.passkeys,
								onclick: () => resetMfa(u)
							},
							{ separator: true },
							{
								label: t('Löschen'),
								icon: 'trash',
								danger: true,
								disabled: u.id === me,
								onclick: () => remove(u)
							}
						]}
					/>
				{/if}
			{/snippet}
			{#snippet empty()}
				<EmptyState compact icon="user" title={t('Keine Benutzer')} />
			{/snippet}
		</Table>
	{/if}
</Card>

<Modal
	bind:open
	title={editing ? t('„{name}“ bearbeiten', { name: editing.username }) : t('Benutzer anlegen')}
	size="md"
	as="form"
	onsubmit={save}
	busy={saving}
>
	<div class="flex flex-col gap-4">
		{#if general}<Alert tone="danger">{general}</Alert>{/if}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<Input
				label={t('Benutzername')}
				bind:value={username}
				autocomplete="off"
				autocapitalize="none"
				spellcheck={false}
				required
				error={errors.username}
			/>
			<Select label={t('Rolle')} bind:value={roleId} options={roleOptions} required error={errors.roleId} />
			<Input label={t('Anzeigename')} bind:value={displayName} error={errors.displayName} />
			<Input label={t('E-Mail')} type="email" bind:value={email} error={errors.email} />
			<Select label={t('Sprache')} bind:value={locale} options={localeOptions} error={errors.locale} />
		</div>
		{#if !editing}
			<Input
				label={t('Start-Passwort')}
				type="text"
				bind:value={password}
				autocomplete="off"
				hint={t('Leer lassen, um eins zu erzeugen. Beim ersten Login wählt der Benutzer ein eigenes.')}
				error={errors.password}
			/>
		{:else if editing.id !== me}
			<Toggle
				bind:checked={disabled}
				label={t('Deaktiviert')}
				description={t('Kann sich nicht anmelden; Sitzungen und API-Tokens funktionieren nicht mehr')}
			/>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={saving}>{t('Abbrechen')}</Button>
		<Button type="submit" variant="primary" icon="save" loading={saving}
			>{editing ? t('Speichern') : t('Anlegen')}</Button
		>
	{/snippet}
</Modal>

<Modal
	open={pwShown !== null}
	onclose={() => (pwShown = null)}
	title={pwShown?.created ? t('Benutzer angelegt') : t('Neues Start-Passwort')}
	size="md"
>
	{#if pwShown}
		<div class="flex flex-col gap-3 text-sm">
			<Alert tone="warn" title={t('Nur jetzt sichtbar')}>
				{t(
					'Das Start-Passwort für „{name}“ jetzt weitergeben. Beim ersten Login muss ein eigenes gewählt werden.',
					{ name: pwShown.user }
				)}
			</Alert>
			<div class="flex items-center gap-2 rounded-md border border-border bg-surface-2 py-1.5 pr-1.5 pl-3">
				<code class="mono min-w-0 flex-1 text-[0.85rem] break-all select-all">{pwShown.password}</code>
				<CopyButton text={pwShown.password} label={t('Passwort kopieren')} size="sm" />
			</div>
		</div>
	{/if}
	{#snippet footer()}
		<Button variant="primary" onclick={() => (pwShown = null)}>{t('Fertig')}</Button>
	{/snippet}
</Modal>
