<!-- Custom field definitions: CRUD /api/v1/custom-fields (key immutable after creation). -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { CustomField } from '$lib/api';
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
		Select,
		Table
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { customFields as cfCatalog } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { customFieldTypeLabel } from '$lib/utils/labels';
	import { apiErrors } from './system';

	const TYPES = ['text', 'number', 'date', 'url', 'bool'];
	const KEY_RE = /^[a-z][a-z0-9_]{0,39}$/;

	const canManage = $derived(auth.can('inventory.config'));

	const list = new AsyncData<CustomField[]>();
	$effect(() => {
		list.run(async (signal) => (await api.get('/api/v1/custom-fields', { signal })) ?? []);
	});

	interface Form {
		key: string;
		label: string;
		type: string;
		description: string;
		sortOrder: number | null;
	}

	let open = $state(false);
	let editing = $state<CustomField | null>(null);
	let form = $state<Form>({ key: '', label: '', type: 'text', description: '', sortOrder: 0 });
	let keyTouched = $state(false);
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);

	function slug(s: string): string {
		const map: Record<string, string> = { ä: 'ae', ö: 'oe', ü: 'ue', ß: 'ss' };
		let k = s
			.toLowerCase()
			.replace(/[äöüß]/g, (c) => map[c])
			.replace(/[^a-z0-9]+/g, '_')
			.replace(/^_+|_+$/g, '');
		if (k && !/^[a-z]/.test(k)) k = 'f_' + k;
		return k.slice(0, 40);
	}

	function openForm(c: CustomField | null) {
		editing = c;
		const nextOrder = Math.max(0, ...(list.data ?? []).map((x) => x.sortOrder)) + 10;
		form = c
			? { key: c.key, label: c.label, type: c.type, description: c.description, sortOrder: c.sortOrder }
			: {
					key: '',
					label: '',
					type: 'text',
					description: '',
					sortOrder: (list.data ?? []).length ? nextOrder : 0
				};
		keyTouched = !!c;
		errors = {};
		general = null;
		open = true;
	}

	function onLabel() {
		delete errors.label;
		if (!editing && !keyTouched) form.key = slug(form.label);
	}

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		if (!form.label.trim()) e.label = 'Bezeichnung erforderlich';
		if (!editing && !KEY_RE.test(form.key))
			e.key = 'Kleinbuchstaben, Ziffern und _, beginnt mit Buchstabe (max. 40)';
		if (!editing && (list.data ?? []).some((c) => c.key === form.key)) e.key = 'Schlüssel bereits vergeben';
		if (form.sortOrder !== null && !Number.isInteger(form.sortOrder)) e.sortOrder = 'Ganze Zahl';
		return e;
	}

	async function save() {
		general = null;
		errors = validate();
		if (Object.keys(errors).length) return;
		saving = true;
		const body = {
			key: editing ? editing.key : form.key,
			label: form.label.trim(),
			type: form.type,
			description: form.description.trim(),
			sortOrder: form.sortOrder ?? 0
		} as CustomField;
		try {
			const saved = editing
				? await api.put('/api/v1/custom-fields/{id}', { path: { id: editing.id }, body })
				: await api.post('/api/v1/custom-fields', { body });
			toast.success(editing ? `Feld „${saved.label}“ gespeichert` : `Feld „${saved.label}“ angelegt`);
			open = false;
			list.reload();
			cfCatalog.refresh().catch(() => {});
		} catch (e) {
			({ errors, general } = apiErrors(e, ['label', 'key', 'type', 'sortOrder']));
		} finally {
			saving = false;
		}
	}

	async function remove(c: CustomField) {
		const ok = await confirm({
			title: `Custom Field „${c.label}“ löschen?`,
			message: `Die Definition und alle Werte von cf.${c.key} bei allen Geräten werden gelöscht. Filter und Ansichten mit cf.${c.key} funktionieren danach nicht mehr.`,
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/custom-fields/{id}', { path: { id: c.id } });
			toast.success(`Custom Field „${c.label}“ gelöscht`);
			list.reload();
			cfCatalog.refresh().catch(() => {});
		} catch (e) {
			toast.error(e);
		}
	}

	const columns: Column<CustomField>[] = [
		{ key: 'sortOrder', label: '#', width: '3.5rem', align: 'right' },
		{ key: 'label', label: 'Feld' },
		{ key: 'type', label: 'Typ', width: '7rem' },
		{ key: 'description', label: 'Beschreibung', hideBelow: 'md' },
		{ key: 'actions', label: '', align: 'right', width: '3rem' }
	];
	const shownColumns = $derived(canManage ? columns : columns.filter((c) => c.key !== 'actions'));
</script>

<Card
	title="Custom Fields"
	description="Eigene Attribute für alle Geräte – filterbar als cf.schlüssel, z. B. cf.standort:keller"
	icon="tag"
	padding="none"
>
	{#snippet actions()}
		{#if canManage}
			<Button size="sm" variant="primary" icon="plus" onclick={() => openForm(null)}>Feld anlegen</Button>
		{/if}
	{/snippet}
	{#if list.error && !list.data}
		<ErrorState error={list.error} onretry={() => list.reload()} />
	{:else}
		<Table
			columns={shownColumns}
			rows={list.data ?? []}
			key={(c) => c.id}
			loading={list.loading && !list.data}
			class="rounded-none border-0"
			caption="Custom Fields"
		>
			{#snippet cell(c, col)}
				{#if col.key === 'sortOrder'}
					<span class="text-fg-subtle tabular">{c.sortOrder}</span>
				{:else if col.key === 'label'}
					<span class="font-medium">{c.label}</span>
					<code class="mono block text-xs text-fg-subtle">cf.{c.key}</code>
				{:else if col.key === 'type'}
					<Badge>{customFieldTypeLabel[c.type] ?? c.type}</Badge>
				{:else if col.key === 'description'}
					<span class="block max-w-md truncate text-fg-muted" title={c.description}
						>{c.description || '–'}</span
					>
				{:else if col.key === 'actions'}
					<Menu
						label="Aktionen für {c.label}"
						items={[
							{ label: 'Bearbeiten', icon: 'edit', onclick: () => openForm(c) },
							{ separator: true },
							{ label: 'Löschen', icon: 'trash', danger: true, onclick: () => remove(c) }
						]}
					/>
				{/if}
			{/snippet}
			{#snippet empty()}
				<EmptyState
					compact
					icon="tag"
					title="Keine Custom Fields"
					description="Z. B. Standort, Inventarnummer, Garantie bis (Datum) oder Handbuch (URL)."
				/>
			{/snippet}
		</Table>
	{/if}
</Card>

<Modal
	bind:open
	title={editing ? `Feld „${editing.label}“ bearbeiten` : 'Custom Field anlegen'}
	size="md"
	as="form"
	onsubmit={save}
	busy={saving}
>
	<div class="flex flex-col gap-4">
		{#if general}<Alert tone="danger" title="Speichern fehlgeschlagen">{general}</Alert>{/if}
		<Input
			label="Bezeichnung"
			bind:value={form.label}
			required
			maxlength={80}
			placeholder="z. B. Inventarnummer"
			error={errors.label}
			oninput={onLabel}
		/>
		<Input
			label="Schlüssel"
			bind:value={form.key}
			mono
			required={!editing}
			disabled={!!editing}
			maxlength={40}
			placeholder="inventarnummer"
			hint={editing
				? 'Der Schlüssel kann nach dem Anlegen nicht mehr geändert werden.'
				: `Im Filter als cf.${form.key || 'schlüssel'}`}
			error={errors.key}
			oninput={() => {
				keyTouched = true;
				delete errors.key;
			}}
		/>
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			<Select
				label="Typ"
				bind:value={form.type}
				options={TYPES.map((t) => ({ value: t, label: customFieldTypeLabel[t] ?? t }))}
				error={errors.type}
			/>
			<Input
				label="Reihenfolge"
				type="number"
				step={1}
				bind:value={form.sortOrder}
				hint="Kleinere Werte zuerst"
				error={errors.sortOrder}
			/>
		</div>
		{#if editing && form.type !== editing.type}
			<Alert tone="warn"
				>Vorhandene Werte werden nicht umgewandelt und passen eventuell nicht zum neuen Typ.</Alert
			>
		{/if}
		<Input
			label="Beschreibung"
			bind:value={form.description}
			maxlength={300}
			placeholder="optional, erscheint als Hinweis am Feld"
		/>
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={saving}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="save" loading={saving}
			>{editing ? 'Speichern' : 'Anlegen'}</Button
		>
	{/snippet}
</Modal>
