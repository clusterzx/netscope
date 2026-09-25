<!-- Roles: named permission sets and whether they require a second factor. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { PermissionInfo, Role } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		Card,
		Checkbox,
		ErrorState,
		Input,
		Modal,
		Skeleton,
		Textarea,
		Toggle
	} from '$lib/components/ui';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { apiErrors } from './system';

	const roles = new AsyncData<Role[]>();
	const catalog = new AsyncData<PermissionInfo[]>();
	$effect(() => {
		roles.run(async (signal) => (await api.get('/api/v1/roles', { signal })) ?? []);
		catalog.run(async (signal) => (await api.get('/api/v1/permissions', { signal })) ?? []);
	});
	const groups = $derived.by(() => {
		const out: { name: string; perms: PermissionInfo[] }[] = [];
		for (const p of catalog.data ?? []) {
			const g = out.find((x) => x.name === p.group);
			if (g) g.perms.push(p);
			else out.push({ name: p.group, perms: [p] });
		}
		return out;
	});
	const label = (key: string) => catalog.data?.find((p) => p.key === key)?.label ?? key;

	// ---------------------------------------------------------------- editor
	let open = $state(false);
	let editing = $state<Role | null>(null);
	let name = $state('');
	let description = $state('');
	let require2fa = $state(false);
	let perms = $state<Record<string, boolean>>({});
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	const admin = $derived(!!editing?.admin);

	function openEditor(r: Role | null, copyOf?: Role) {
		const src = r ?? copyOf ?? null;
		editing = r;
		name = r?.name ?? (copyOf ? `${copyOf.name} (Kopie)` : '');
		description = src?.description ?? '';
		require2fa = src?.require2fa ?? false;
		// every key present: bind:checked does not accept undefined
		perms = Object.fromEntries((catalog.data ?? []).map((p) => [p.key, !!src?.permissions.includes(p.key)]));
		errors = {};
		general = null;
		open = true;
	}

	function setGroup(g: { perms: PermissionInfo[] }, on: boolean) {
		for (const p of g.perms) perms[p.key] = on;
	}

	async function save() {
		if (!name.trim()) {
			errors = { name: 'Name erforderlich' };
			return;
		}
		saving = true;
		general = null;
		errors = {};
		const body = {
			name: name.trim(),
			description,
			require2fa,
			permissions: Object.keys(perms).filter((k) => perms[k])
		};
		try {
			if (editing) await api.put('/api/v1/roles/{id}', { path: { id: editing.id }, body });
			else await api.post('/api/v1/roles', { body });
			toast.success(`Rolle „${body.name}“ gespeichert`);
			open = false;
			roles.reload();
		} catch (e) {
			({ errors, general } = apiErrors(e, ['name', 'description', 'permissions']));
		} finally {
			saving = false;
		}
	}

	async function remove(r: Role) {
		const ok = await confirm({
			title: `Rolle „${r.name}“ löschen?`,
			message: 'Die Rolle wird entfernt. Das geht nur, solange kein Benutzer sie hat.',
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/roles/{id}', { path: { id: r.id } });
			toast.success(`Rolle „${r.name}“ gelöscht`);
			roles.reload();
		} catch (e) {
			toast.error(e);
		}
	}
</script>

<Card
	title="Rollen"
	description="Lesen dürfen alle Benutzer; eine Rolle legt fest, was jemand ändern darf"
	icon="shield"
>
	{#snippet actions()}
		<Button size="sm" variant="primary" icon="plus" onclick={() => openEditor(null)} disabled={!catalog.data}
			>Rolle anlegen</Button
		>
	{/snippet}
	{#if (roles.error && !roles.data) || (catalog.error && !catalog.data)}
		<ErrorState
			error={roles.error ?? catalog.error}
			onretry={() => {
				roles.reload();
				catalog.reload();
			}}
		/>
	{:else if !roles.data || !catalog.data}
		<Skeleton rows={4} />
	{:else}
		<ul class="flex flex-col divide-y divide-border">
			{#each roles.data as r (r.id)}
				<li class="flex flex-wrap items-start gap-3 py-3 first:pt-0 last:pb-0">
					<div class="min-w-0 flex-1">
						<div class="flex flex-wrap items-center gap-2">
							<span class="font-medium text-fg">{r.name}</span>
							{#if r.admin}<Badge tone="accent">alle Rechte</Badge>{/if}
							{#if r.require2fa}<Badge tone="info">2FA Pflicht</Badge>{/if}
							<span class="text-xs text-fg-subtle">{r.users} Benutzer</span>
						</div>
						{#if r.description}<p class="mt-0.5 text-sm text-fg-muted">{r.description}</p>{/if}
						{#if !r.admin}
							<p class="mt-1 text-xs text-fg-subtle">
								{r.permissions.length ? r.permissions.map(label).join(' · ') : 'Nur lesen – keine Änderungen'}
							</p>
						{/if}
					</div>
					<div class="flex gap-1">
						<Button size="sm" variant="ghost" icon="edit" onclick={() => openEditor(r)}>Bearbeiten</Button>
						{#if !r.admin}
							<Button
								size="sm"
								variant="ghost"
								icon="copy"
								label="„{r.name}“ kopieren"
								onclick={() => openEditor(null, r)}
							/>
							<Button
								size="sm"
								variant="ghost"
								icon="trash"
								label="„{r.name}“ löschen"
								disabled={r.users > 0}
								title={r.users > 0 ? 'Die Rolle ist noch Benutzern zugewiesen' : undefined}
								onclick={() => remove(r)}
							/>
						{/if}
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</Card>

<Modal
	bind:open
	title={editing ? `Rolle „${editing.name}“` : 'Rolle anlegen'}
	size="lg"
	as="form"
	onsubmit={save}
	busy={saving}
>
	<div class="flex flex-col gap-4">
		{#if general}<Alert tone="danger">{general}</Alert>{/if}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-[16rem_1fr]">
			<Input label="Name" bind:value={name} required maxlength={64} disabled={admin} error={errors.name} />
			<Textarea
				label="Beschreibung"
				bind:value={description}
				rows={2}
				maxlength={300}
				error={errors.description}
			/>
		</div>
		<Toggle
			bind:checked={require2fa}
			label="Zwei-Faktor-Anmeldung verlangen"
			description="Benutzer mit dieser Rolle müssen beim nächsten Login TOTP oder einen Passkey einrichten"
		/>
		{#if admin}
			<Alert tone="info">Die Rolle Administrator hat immer alle Rechte – auch künftige.</Alert>
		{:else}
			<fieldset class="flex flex-col gap-4">
				<legend class="mb-1 text-[0.8125rem] font-medium text-fg">Rechte</legend>
				{#if errors.permissions}<p class="text-xs text-danger" role="alert">{errors.permissions}</p>{/if}
				{#each groups as g (g.name)}
					{@const all = g.perms.every((p) => perms[p.key])}
					<div class="rounded-md border border-border">
						<div class="flex items-center justify-between border-b border-border bg-surface-2 px-3 py-1.5">
							<span class="text-xs font-semibold tracking-wide text-fg-muted uppercase">{g.name}</span>
							<button
								type="button"
								class="text-xs text-accent hover:underline"
								onclick={() => setGroup(g, !all)}>{all ? 'keine' : 'alle'}</button
							>
						</div>
						<div class="flex flex-col gap-2 px-3 py-2">
							{#each g.perms as p (p.key)}
								<div class="flex items-start gap-2">
									<Checkbox bind:checked={perms[p.key]} label={p.label} description={p.hint} />
									{#if p.critical}<Badge tone="warn" title="Reicht an Geheimnisse oder das ganze System heran"
											>kritisch</Badge
										>{/if}
								</div>
							{/each}
						</div>
					</div>
				{/each}
			</fieldset>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={saving}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="save" loading={saving}>Speichern</Button>
	{/snippet}
</Modal>
