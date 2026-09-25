<!-- Users: create, edit (role, disabled), new start password, reset the second factor, delete. -->
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

	// ---------------------------------------------------------------- create / edit
	let open = $state(false);
	let editing = $state<User | null>(null);
	let username = $state('');
	let displayName = $state('');
	let email = $state('');
	let roleId = $state('');
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
		const fallback = roles.data?.find((r) => r.name === 'Betrachter') ?? roles.data?.[roles.data.length - 1];
		roleId = String(u?.roleId ?? fallback?.id ?? '');
		disabled = u?.disabled ?? false;
		password = '';
		errors = {};
		general = null;
		open = true;
	}

	async function save() {
		const e: Record<string, string> = {};
		if (!username.trim()) e.username = 'Benutzername erforderlich';
		if (!roleId) e.roleId = 'Rolle wählen';
		if (!editing && password && password.length < 10) e.password = 'Mindestens 10 Zeichen – oder leer lassen';
		errors = e;
		if (Object.keys(e).length) return;
		saving = true;
		general = null;
		const body = { username: username.trim(), displayName, email, roleId: Number(roleId), disabled };
		try {
			if (editing) {
				await api.put('/api/v1/users/{id}', { path: { id: editing.id }, body });
				toast.success(`„${body.username}“ gespeichert`);
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
			title: `Neues Start-Passwort für „${u.username}“?`,
			message:
				'Das bisherige Passwort gilt sofort nicht mehr, laufende Sitzungen enden. Beim nächsten Login muss ein eigenes Passwort gewählt werden.',
			confirmLabel: 'Neues Passwort erzeugen'
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
			title: `Zweiten Faktor von „${u.username}“ zurücksetzen?`,
			message:
				'Entfernt TOTP, alle Passkeys und die Wiederherstellungscodes, z. B. nach Verlust des Handys. Laufende Sitzungen enden. Verlangt die Rolle 2FA, wird sie beim nächsten Login neu eingerichtet.',
			confirmLabel: 'Zurücksetzen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.post('/api/v1/users/{id}/2fa/reset', { path: { id: u.id } });
			toast.success('Zweiter Faktor zurückgesetzt');
			users.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	async function remove(u: User) {
		const ok = await confirm({
			title: `„${u.username}“ löschen?`,
			message:
				'Das Konto, seine Sitzungen und API-Tokens werden gelöscht. Das Audit-Log behält die Einträge. Alternativ lässt sich das Konto deaktivieren.',
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/users/{id}', { path: { id: u.id } });
			toast.success(`„${u.username}“ gelöscht`);
			users.reload();
			roles.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	const columns: Column<User>[] = [
		{ key: 'user', label: 'Benutzer' },
		{ key: 'role', label: 'Rolle' },
		{ key: 'mfa', label: '2FA' },
		{ key: 'status', label: 'Status', hideBelow: 'md' },
		{ key: 'login', label: 'Letzte Anmeldung', hideBelow: 'lg' },
		{ key: 'actions', label: '', align: 'right', width: '3rem' }
	];
</script>

<Card title="Benutzer" description="Jeder Benutzer hat genau eine Rolle" icon="user" padding="none">
	{#snippet actions()}
		<Button size="sm" variant="primary" icon="plus" onclick={() => openEditor(null)} disabled={!roles.data}
			>Benutzer anlegen</Button
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
			caption="Benutzer"
		>
			{#snippet cell(u, col)}
				{#if col.key === 'user'}
					<span class="font-medium {u.disabled ? 'text-fg-subtle line-through' : ''}">{u.username}</span>
					{#if u.id === me}<span class="text-xs text-fg-subtle"> (du)</span>{/if}
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
						{#if u.passkeys}<Badge tone="ok">{u.passkeys} Passkey{u.passkeys > 1 ? 's' : ''}</Badge>{/if}
						{#if !u.totp && !u.passkeys}
							<Badge
								tone={u.mfaRequired ? 'warn' : 'neutral'}
								title={u.mfaRequired ? 'Die Rolle verlangt 2FA' : undefined}
								>{u.mfaRequired ? 'noch nicht eingerichtet' : 'aus'}</Badge
							>
						{/if}
					</span>
				{:else if col.key === 'status'}
					{#if u.disabled}
						<Badge tone="neutral">deaktiviert</Badge>
					{:else if u.mustChangePassword}
						<Badge tone="warn" title="Muss beim nächsten Login ein eigenes Passwort wählen"
							>Start-Passwort</Badge
						>
					{:else}
						<Badge tone="ok">aktiv</Badge>
					{/if}
				{:else if col.key === 'login'}
					{#if u.lastLoginAt}<RelativeTime value={u.lastLoginAt} class="text-fg-muted" />{:else}<span
							class="text-fg-subtle">noch nie</span
						>{/if}
				{:else if col.key === 'actions'}
					<Menu
						label="Aktionen für {u.username}"
						size="sm"
						items={[
							{ label: 'Bearbeiten', icon: 'edit', onclick: () => openEditor(u) },
							{ label: 'Neues Start-Passwort', icon: 'key', onclick: () => resetPassword(u) },
							{
								label: 'Zweiten Faktor zurücksetzen',
								icon: 'shield',
								disabled: !u.totp && !u.passkeys,
								onclick: () => resetMfa(u)
							},
							{ separator: true },
							{
								label: 'Löschen',
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
				<EmptyState compact icon="user" title="Keine Benutzer" />
			{/snippet}
		</Table>
	{/if}
</Card>

<Modal
	bind:open
	title={editing ? `„${editing.username}“ bearbeiten` : 'Benutzer anlegen'}
	size="md"
	as="form"
	onsubmit={save}
	busy={saving}
>
	<div class="flex flex-col gap-4">
		{#if general}<Alert tone="danger">{general}</Alert>{/if}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<Input
				label="Benutzername"
				bind:value={username}
				autocomplete="off"
				autocapitalize="none"
				spellcheck={false}
				required
				error={errors.username}
			/>
			<Select label="Rolle" bind:value={roleId} options={roleOptions} required error={errors.roleId} />
			<Input label="Anzeigename" bind:value={displayName} error={errors.displayName} />
			<Input label="E-Mail" type="email" bind:value={email} error={errors.email} />
		</div>
		{#if !editing}
			<Input
				label="Start-Passwort"
				type="text"
				bind:value={password}
				autocomplete="off"
				hint="Leer lassen, um eins zu erzeugen. Beim ersten Login wählt der Benutzer ein eigenes."
				error={errors.password}
			/>
		{:else if editing.id !== me}
			<Toggle
				bind:checked={disabled}
				label="Deaktiviert"
				description="Kann sich nicht anmelden; Sitzungen und API-Tokens funktionieren nicht mehr"
			/>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={saving}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="save" loading={saving}
			>{editing ? 'Speichern' : 'Anlegen'}</Button
		>
	{/snippet}
</Modal>

<Modal
	open={pwShown !== null}
	onclose={() => (pwShown = null)}
	title={pwShown?.created ? 'Benutzer angelegt' : 'Neues Start-Passwort'}
	size="md"
>
	{#if pwShown}
		<div class="flex flex-col gap-3 text-sm">
			<Alert tone="warn" title="Nur jetzt sichtbar">
				Das Start-Passwort für „{pwShown.user}“ jetzt weitergeben. Beim ersten Login muss ein eigenes gewählt
				werden.
			</Alert>
			<div class="flex items-center gap-2 rounded-md border border-border bg-surface-2 py-1.5 pr-1.5 pl-3">
				<code class="mono min-w-0 flex-1 text-[0.85rem] break-all select-all">{pwShown.password}</code>
				<CopyButton text={pwShown.password} label="Passwort kopieren" size="sm" />
			</div>
		</div>
	{/if}
	{#snippet footer()}
		<Button variant="primary" onclick={() => (pwShown = null)}>Fertig</Button>
	{/snippet}
</Modal>
