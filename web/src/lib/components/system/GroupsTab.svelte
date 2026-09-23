<!-- Device groups: CRUD /api/v1/groups (manual or rule based via filter query). -->
<script lang="ts">
	import { api, errorMessage } from '$lib/api';
	import type { Group } from '$lib/api';
	import QueryInput from '$lib/components/QueryInput.svelte';
	import {
		Alert,
		Badge,
		Button,
		Card,
		EmptyState,
		ErrorState,
		Input,
		Menu,
		Modal,
		Table
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { groups as groupCatalog } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatNumber } from '$lib/utils/format';
	import { apiErrors, queryValue } from './system';

	const list = new AsyncData<Group[]>();
	$effect(() => {
		list.run(async (signal) => (await api.get('/api/v1/groups', { signal })) ?? []);
	});

	const COLORS = ['#0673b0', '#15803d', '#b45309', '#c2410c', '#be123c', '#7c3aed', '#0f766e', '#475569'];

	interface Form {
		name: string;
		kind: 'manual' | 'query';
		query: string;
		color: string;
		description: string;
	}

	let open = $state(false);
	let editing = $state<Group | null>(null);
	let form = $state<Form>({ name: '', kind: 'manual', query: '', color: '', description: '' });
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	let preview = $state<number | null>(null);

	function openForm(g: Group | null) {
		editing = g;
		form = g
			? {
					name: g.name,
					kind: g.kind === 'query' ? 'query' : 'manual',
					query: g.query,
					color: g.color,
					description: g.description
				}
			: { name: '', kind: 'manual', query: '', color: COLORS[0], description: '' };
		errors = {};
		general = null;
		preview = null;
		open = true;
	}

	const devicesHref = (g: { name: string }) =>
		`/devices?q=${encodeURIComponent('group:' + queryValue(g.name))}`;

	async function checkQuery(q: string) {
		form.query = q;
		delete errors.query;
		preview = null;
		if (!q.trim()) return;
		try {
			const res = await api.get('/api/v1/devices', { query: { q, limit: 1 } });
			preview = res.total;
		} catch (e) {
			errors = { ...errors, query: errorMessage(e) };
		}
	}

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		if (!form.name.trim()) e.name = 'Name erforderlich';
		if (form.kind === 'query' && !form.query.trim()) e.query = 'Regelbasierte Gruppen brauchen einen Filter';
		if (form.kind === 'query' && form.query.toLowerCase().includes('group:' + form.name.trim().toLowerCase()))
			e.query = 'Eine Gruppe kann sich nicht selbst referenzieren';
		if (form.color && !/^#[0-9a-fA-F]{6}$/.test(form.color)) e.color = 'Farbe als #rrggbb';
		return e;
	}

	async function save() {
		general = null;
		errors = validate();
		if (Object.keys(errors).length) return;
		saving = true;
		const body = {
			name: form.name.trim(),
			kind: form.kind,
			query: form.kind === 'query' ? form.query.trim() : '',
			color: form.color,
			description: form.description.trim()
		} as Group;
		try {
			const saved = editing
				? await api.put('/api/v1/groups/{id}', { path: { id: editing.id }, body })
				: await api.post('/api/v1/groups', { body });
			toast.success(editing ? `Gruppe „${saved.name}“ gespeichert` : `Gruppe „${saved.name}“ angelegt`);
			open = false;
			list.reload();
			groupCatalog.refresh().catch(() => {});
		} catch (e) {
			({ errors, general } = apiErrors(e, ['name', 'kind', 'color', 'query']));
		} finally {
			saving = false;
		}
	}

	async function remove(g: Group) {
		const ok = await confirm({
			title: `Gruppe „${g.name}“ löschen?`,
			message:
				'Die Geräte bleiben erhalten. Plugin-Scopes und Regeln, die diese Gruppe verwenden, greifen danach nicht mehr.',
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/groups/{id}', { path: { id: g.id } });
			toast.success(`Gruppe „${g.name}“ gelöscht`);
			list.reload();
			groupCatalog.refresh().catch(() => {});
		} catch (e) {
			toast.error(e);
		}
	}

	const columns: Column<Group>[] = [
		{ key: 'name', label: 'Gruppe' },
		{ key: 'kind', label: 'Art', width: '9rem', hideBelow: 'sm' },
		{ key: 'query', label: 'Filter / Beschreibung', hideBelow: 'md' },
		{ key: 'members', label: 'Mitglieder', align: 'right', width: '7rem' },
		{ key: 'actions', label: '', align: 'right', width: '3rem' }
	];
</script>

<Card
	title="Gruppen"
	description="Regelbasierte Gruppen folgen einem Filter; manuelle Mitglieder werden in der Geräteliste zugewiesen"
	icon="layers"
	padding="none"
>
	{#snippet actions()}
		<Button size="sm" variant="primary" icon="plus" onclick={() => openForm(null)}>Gruppe anlegen</Button>
	{/snippet}
	{#if list.error && !list.data}
		<ErrorState error={list.error} onretry={() => list.reload()} />
	{:else}
		<Table
			{columns}
			rows={list.data ?? []}
			key={(g) => g.id}
			loading={list.loading && !list.data}
			class="rounded-none border-0"
			caption="Gruppen"
		>
			{#snippet cell(g, col)}
				{#if col.key === 'name'}
					<span class="flex items-center gap-2">
						<span
							class="h-3 w-3 shrink-0 rounded-full border border-border"
							style={g.color ? `background:${g.color}` : undefined}
							aria-hidden="true"
						></span>
						<a href={devicesHref(g)} class="link font-medium">{g.name}</a>
					</span>
				{:else if col.key === 'kind'}
					<Badge tone={g.kind === 'query' ? 'accent' : 'neutral'}
						>{g.kind === 'query' ? 'Regelbasiert' : 'Manuell'}</Badge
					>
				{:else if col.key === 'query'}
					{#if g.kind === 'query'}<code class="mono block max-w-md truncate text-xs" title={g.query}
							>{g.query}</code
						>{/if}
					{#if g.description}<span class="block max-w-md truncate text-xs text-fg-muted" title={g.description}
							>{g.description}</span
						>{/if}
				{:else if col.key === 'members'}
					<a href={devicesHref(g)} class="link tabular">{formatNumber(g.memberCount)}</a>
				{:else if col.key === 'actions'}
					<Menu
						label="Aktionen für {g.name}"
						items={[
							{ label: 'Bearbeiten', icon: 'edit', onclick: () => openForm(g) },
							{ label: 'Geräte anzeigen', icon: 'devices', href: devicesHref(g) },
							{ separator: true },
							{ label: 'Löschen', icon: 'trash', danger: true, onclick: () => remove(g) }
						]}
					/>
				{/if}
			{/snippet}
			{#snippet empty()}
				<EmptyState
					compact
					icon="layers"
					title="Keine Gruppen"
					description="Gruppen fassen Geräte zusammen – etwa für Plugin-Scopes, Regeln und Filter (group:name)."
				/>
			{/snippet}
		</Table>
	{/if}
</Card>

<Modal
	bind:open
	title={editing ? `Gruppe „${editing.name}“ bearbeiten` : 'Gruppe anlegen'}
	size="lg"
	as="form"
	onsubmit={save}
	busy={saving}
>
	<div class="flex flex-col gap-4">
		{#if general}<Alert tone="danger" title="Speichern fehlgeschlagen">{general}</Alert>{/if}
		<Input
			label="Name"
			bind:value={form.name}
			required
			maxlength={80}
			error={errors.name}
			oninput={() => delete errors.name}
		/>
		<fieldset>
			<legend class="mb-1.5 text-[0.8125rem] font-medium text-fg">Art</legend>
			<div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
				{#each [['manual', 'Manuell', 'Mitglieder werden in der Geräteliste zugewiesen'], ['query', 'Regelbasiert', 'Alle Geräte, die zum Filter passen']] as [v, l, d] (v)}
					<label
						class="flex cursor-pointer items-start gap-2 rounded-md border px-3 py-2 text-sm has-focus-visible:ring-2 has-focus-visible:ring-focus
							{form.kind === v ? 'border-accent bg-accent-soft' : 'border-border hover:bg-surface-2'}"
					>
						<input
							type="radio"
							name="group-kind"
							value={v}
							bind:group={form.kind}
							class="mt-0.5 accent-(--accent)"
						/>
						<span>
							<span class="block font-medium">{l}</span>
							<span class="block text-xs text-fg-subtle">{d}</span>
						</span>
					</label>
				{/each}
			</div>
			{#if errors.kind}<p class="mt-1 text-xs text-danger" role="alert">{errors.kind}</p>{/if}
		</fieldset>
		{#if form.kind === 'query'}
			<div class="flex flex-col gap-1">
				<QueryInput
					bind:value={form.query}
					onsubmit={checkQuery}
					error={errors.query}
					label="Filter"
					showLabel
					placeholder="z. B. tag:iot type:camera"
				/>
				<p class="text-xs text-fg-subtle" aria-live="polite">
					{#if preview !== null && !errors.query}Aktuell {formatNumber(preview)} passende Geräte.{:else}Enter
						prüft den Filter und zeigt die Anzahl passender Geräte.{/if}
				</p>
			</div>
		{:else if editing?.kind === 'query'}
			<Alert tone="info">Beim Wechsel auf „Manuell“ startet die Gruppe ohne Mitglieder.</Alert>
		{/if}
		<div class="flex flex-col gap-1.5">
			<span class="text-[0.8125rem] font-medium text-fg" id="grp-color-l">Farbe</span>
			<div class="flex flex-wrap items-center gap-2" role="group" aria-labelledby="grp-color-l">
				{#each COLORS as c (c)}
					<button
						type="button"
						class="h-7 w-7 rounded-full border-2 transition-transform hover:scale-110 {form.color.toLowerCase() ===
						c
							? 'border-fg'
							: 'border-transparent'}"
						style="background:{c}"
						aria-label="Farbe {c}"
						aria-pressed={form.color.toLowerCase() === c}
						onclick={() => {
							form.color = c;
							delete errors.color;
						}}
					></button>
				{/each}
				<input
					type="color"
					value={form.color || '#888888'}
					oninput={(e) => {
						form.color = (e.currentTarget as HTMLInputElement).value;
						delete errors.color;
					}}
					aria-label="Eigene Farbe wählen"
					class="h-7 w-9 cursor-pointer rounded border border-border bg-surface"
				/>
				<Input
					bind:value={form.color}
					mono
					size="sm"
					class="w-28"
					aria-label="Farbe als Hex"
					placeholder="#rrggbb"
					error={errors.color}
				/>
				<Button size="xs" variant="ghost" onclick={() => (form.color = '')} disabled={!form.color}
					>Keine</Button
				>
			</div>
		</div>
		<Input label="Beschreibung" bind:value={form.description} maxlength={300} placeholder="optional" />
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={saving}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="save" loading={saving}
			>{editing ? 'Speichern' : 'Anlegen'}</Button
		>
	{/snippet}
</Modal>
