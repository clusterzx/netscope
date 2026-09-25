<!--
	Bulk actions for selected devices: tags, groups, state, criticality, plugin device actions
	(e.g. WOL), merge and delete (POST /api/v1/devices/bulk, /api/v1/devices/merge).
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import type { BulkRequest, DeviceAction, DeviceRow, Group } from '$lib/api';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Menu, { type MenuItem } from '$lib/components/ui/Menu.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import TagInput from '$lib/components/ui/TagInput.svelte';
	import StatusDot from '$lib/components/ui/StatusDot.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { groups, meta, tags } from '$lib/stores/catalog.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { CRITICALITIES, criticalityLabel, stateLabel } from '$lib/utils/labels';
	import { formatNumber } from '$lib/utils/format';
	import ActionParamsDialog from './ActionParamsDialog.svelte';
	import { outcomeToast } from './actions';

	interface Props {
		ids: number[];
		/** rows of the selected devices that are loaded (for names in the merge dialog) */
		rows: DeviceRow[];
		onclear: () => void;
		ondone: () => void;
	}

	let { ids, rows, onclear, ondone }: Props = $props();

	onMount(() => {
		meta.load().catch(() => {});
		tags.load().catch(() => {});
		groups.load().catch(() => {});
	});

	const canEdit = $derived(auth.can('devices.edit'));
	const canDelete = $derived(auth.can('devices.delete'));
	const canActions = $derived(auth.can('devices.actions'));
	const canCreateGroup = $derived(auth.can('inventory.config'));

	const n = $derived(ids.length);
	const target = $derived(n === 1 ? '1 Gerät' : `${formatNumber(n)} Geräte`);
	let busy = $state(false);

	async function bulk(body: Omit<BulkRequest, 'ids'>, success: string) {
		busy = true;
		try {
			const res = await api.post('/api/v1/devices/bulk', { body: { ...body, ids } as BulkRequest });
			toast.success(`${success} (${res.affected} ${res.affected === 1 ? 'Gerät' : 'Geräte'})`);
			ondone();
			return res;
		} catch (e) {
			toast.error(e);
			throw e;
		} finally {
			busy = false;
		}
	}

	// ---------------------------------------------------------------- tags
	let tagOpen = $state(false);
	let tagMode = $state<'add_tags' | 'remove_tags'>('add_tags');
	let tagList = $state<string[]>([]);
	let tagError = $state('');
	const tagSuggestions = $derived((tags.value ?? []).map((t) => t.tag));

	function openTags(mode: 'add_tags' | 'remove_tags') {
		tagMode = mode;
		tagList = [];
		tagError = '';
		tagOpen = true;
	}
	async function submitTags() {
		if (!tagList.length) {
			tagError = 'Mindestens ein Tag angeben';
			return;
		}
		try {
			await bulk(
				{ action: tagMode, tags: tagList },
				tagMode === 'add_tags' ? 'Tags hinzugefügt' : 'Tags entfernt'
			);
			tagOpen = false;
			tags.refresh().catch(() => {});
		} catch (e) {
			tagError = errorMessage(e);
		}
	}

	// ---------------------------------------------------------------- groups
	let groupOpen = $state(false);
	let groupMode = $state<'add_group' | 'remove_group'>('add_group');
	let groupId = $state('');
	let newGroup = $state('');
	let groupError = $state('');
	const manualGroups = $derived((groups.value ?? []).filter((g) => g.kind === 'manual'));

	function openGroups(mode: 'add_group' | 'remove_group') {
		groupMode = mode;
		groupId = manualGroups[0] ? String(manualGroups[0].id) : '';
		newGroup = '';
		groupError = '';
		groupOpen = true;
	}
	async function submitGroup() {
		groupError = '';
		let gid = Number(groupId);
		try {
			if (groupMode === 'add_group' && newGroup.trim()) {
				// id/counters/timestamps are set by the server
				const body = {
					name: newGroup.trim(),
					kind: 'manual',
					description: '',
					query: '',
					color: ''
				} as Group;
				const g = await api.post('/api/v1/groups', { body });
				gid = g.id;
				groups.refresh().catch(() => {});
			}
			if (!gid) {
				groupError = 'Gruppe wählen oder neu anlegen';
				return;
			}
			await bulk(
				{ action: groupMode, group: gid },
				groupMode === 'add_group' ? 'Zur Gruppe hinzugefügt' : 'Aus Gruppe entfernt'
			);
			groupOpen = false;
			groups.refresh().catch(() => {});
		} catch (e) {
			groupError = errorMessage(e);
		}
	}

	// ---------------------------------------------------------------- merge
	let mergeOpen = $state(false);
	let mergeTarget = $state('');
	let mergeError = $state('');
	const mergeRows = $derived(rows.filter((r) => ids.includes(r.id)));

	function openMerge() {
		mergeTarget = String(mergeRows[0]?.id ?? ids[0]);
		mergeError = '';
		mergeOpen = true;
	}
	async function submitMerge() {
		const t = Number(mergeTarget);
		busy = true;
		try {
			await api.post('/api/v1/devices/merge', { body: { target: t, sources: ids.filter((i) => i !== t) } });
			toast.success(`${n - 1} ${n - 1 === 1 ? 'Gerät' : 'Geräte'} zusammengeführt`);
			mergeOpen = false;
			onclear();
			ondone();
		} catch (e) {
			mergeError = errorMessage(e);
		} finally {
			busy = false;
		}
	}

	// ---------------------------------------------------------------- plugin actions
	let paramsOpen = $state(false);
	let paramsAction = $state<DeviceAction | null>(null);

	async function runAction(a: DeviceAction, params: Record<string, unknown> = {}) {
		busy = true;
		try {
			const res = await api.post('/api/v1/devices/bulk', {
				body: { action: 'plugin_action', ids, plugin: a.plugin, name: a.name, params } as BulkRequest
			});
			outcomeToast(a, res.outcome);
		} finally {
			busy = false;
		}
	}

	async function startAction(a: DeviceAction) {
		if (a.params?.length) {
			paramsAction = a;
			paramsOpen = true;
			return;
		}
		if (
			a.confirm &&
			!(await confirm({ title: a.label, message: `${a.confirm}\n\n${target}`, confirmLabel: 'Ausführen' }))
		)
			return;
		try {
			await runAction(a);
		} catch (e) {
			toast.error(e, { title: a.label });
		}
	}

	async function remove() {
		const ok = await confirm({
			title: `${target} löschen?`,
			message:
				'Alle Daten der Geräte (Ports, Zertifikate, Historie …) werden gelöscht. Events bleiben erhalten. Beim nächsten Scan werden aktive Geräte neu angelegt.',
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		await bulk({ action: 'delete' }, 'Geräte gelöscht').catch(() => {});
		onclear();
	}

	const actionItems = $derived<MenuItem[]>([
		...(canActions && (meta.value?.deviceActions ?? []).length
			? [
					{ separator: true as const, label: 'Plugin-Aktionen' },
					...(meta.value?.deviceActions ?? []).map((a) => ({
						label: a.label,
						icon: a.plugin === 'wol' ? ('zap' as const) : ('play' as const),
						hint: a.pluginName,
						onclick: () => startAction(a)
					}))
				]
			: []),
		...(canDelete
			? [
					{ separator: true as const, label: 'Inventar' },
					{ label: 'Zusammenführen …', icon: 'merge' as const, disabled: n < 2, onclick: openMerge },
					{ label: 'Löschen …', icon: 'trash' as const, danger: true, onclick: remove }
				]
			: [])
	]);
</script>

<div
	class="flex flex-wrap items-center gap-2 rounded-lg border border-accent/30 bg-accent-soft px-3 py-2"
	role="region"
	aria-label="Massenaktionen"
>
	<span class="text-sm font-medium text-fg">{target} ausgewählt</span>
	<Button size="sm" variant="ghost" onclick={onclear}>Auswahl aufheben</Button>
	<span class="mx-1 hidden h-5 w-px bg-border sm:block"></span>
	{#if canEdit}
		<Menu
			text="Tags"
			label="Tags"
			size="sm"
			variant="secondary"
			placement="bottom-start"
			disabled={busy}
			items={[
				{ label: 'Tags hinzufügen …', icon: 'plus', onclick: () => openTags('add_tags') },
				{ label: 'Tags entfernen …', icon: 'minus', onclick: () => openTags('remove_tags') }
			]}
		/>
		<Menu
			text="Gruppe"
			label="Gruppe"
			size="sm"
			variant="secondary"
			placement="bottom-start"
			disabled={busy}
			items={[
				{ label: 'Zu Gruppe hinzufügen …', icon: 'plus', onclick: () => openGroups('add_group') },
				{ label: 'Aus Gruppe entfernen …', icon: 'minus', onclick: () => openGroups('remove_group') }
			]}
		/>
		<Menu
			text="Zustand"
			label="Zustand setzen"
			size="sm"
			variant="secondary"
			placement="bottom-start"
			disabled={busy}
			items={(['known', 'unknown', 'ignored'] as const).map((s) => ({
				label: `Als „${stateLabel[s]}“ markieren`,
				onclick: () =>
					bulk({ action: 'set_state', value: s }, `Zustand „${stateLabel[s]}“ gesetzt`).catch(() => {})
			}))}
		/>
		<Menu
			text="Kritikalität"
			label="Kritikalität setzen"
			size="sm"
			variant="secondary"
			placement="bottom-start"
			disabled={busy}
			items={CRITICALITIES.map((c) => ({
				label: criticalityLabel[c],
				onclick: () =>
					bulk(
						{ action: 'set_criticality', value: c },
						`Kritikalität „${criticalityLabel[c]}“ gesetzt`
					).catch(() => {})
			}))}
		/>
	{/if}
	{#if actionItems.length}
		<Menu
			text="Weitere"
			label="Weitere Aktionen"
			size="sm"
			variant="secondary"
			placement="bottom-start"
			disabled={busy}
			items={actionItems}
		/>
	{/if}
</div>

<Modal
	bind:open={tagOpen}
	title={tagMode === 'add_tags' ? 'Tags hinzufügen' : 'Tags entfernen'}
	description={target}
	size="sm"
	as="form"
	onsubmit={submitTags}
	{busy}
>
	<TagInput
		label="Tags"
		bind:value={tagList}
		suggestions={tagSuggestions}
		error={tagError}
		normalize={(s) => s.trim().toLowerCase()}
		hint="Enter oder Komma trennt Tags"
	/>
	{#snippet footer()}
		<Button onclick={() => (tagOpen = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" loading={busy}
			>{tagMode === 'add_tags' ? 'Hinzufügen' : 'Entfernen'}</Button
		>
	{/snippet}
</Modal>

<Modal
	bind:open={groupOpen}
	title={groupMode === 'add_group' ? 'Zu Gruppe hinzufügen' : 'Aus Gruppe entfernen'}
	description={target}
	size="sm"
	as="form"
	onsubmit={submitGroup}
	{busy}
>
	<div class="flex flex-col gap-3">
		{#if groupError}<Alert tone="danger">{groupError}</Alert>{/if}
		{#if manualGroups.length}
			<Select
				label="Gruppe"
				bind:value={groupId}
				options={manualGroups.map((g) => ({ value: String(g.id), label: `${g.name} (${g.memberCount})` }))}
				disabled={groupMode === 'add_group' && !!newGroup.trim()}
			/>
		{:else}
			<p class="text-sm text-fg-muted">
				Es gibt noch keine manuellen Gruppen. Regelbasierte Gruppen ergeben sich aus ihrem Filter.
			</p>
		{/if}
		{#if groupMode === 'add_group' && canCreateGroup}
			<Input
				label="… oder neue manuelle Gruppe anlegen"
				bind:value={newGroup}
				placeholder="Name der neuen Gruppe"
			/>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (groupOpen = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" loading={busy}
			>{groupMode === 'add_group' ? 'Hinzufügen' : 'Entfernen'}</Button
		>
	{/snippet}
</Modal>

<Modal
	bind:open={mergeOpen}
	title="Geräte zusammenführen"
	description={target}
	as="form"
	onsubmit={submitMerge}
	{busy}
>
	<div class="flex flex-col gap-3">
		<p class="text-sm text-fg-muted">
			Alle MAC- und IP-Adressen, Beobachtungen und manuellen Daten der anderen Geräte werden in das Zielgerät
			übernommen; die übrigen Geräte werden danach gelöscht.
		</p>
		{#if mergeError}<Alert tone="danger">{mergeError}</Alert>{/if}
		<fieldset class="flex flex-col gap-1.5">
			<legend class="mb-1 text-[0.8125rem] font-medium">Zielgerät (bleibt erhalten)</legend>
			{#each mergeRows as r (r.id)}
				<label
					class="flex cursor-pointer items-center gap-2.5 rounded-md border border-border px-3 py-2 text-sm hover:bg-surface-2 has-checked:border-accent has-checked:bg-accent-soft"
				>
					<input
						type="radio"
						name="merge-target"
						value={String(r.id)}
						bind:group={mergeTarget}
						class="accent-(--accent)"
					/>
					<StatusDot status={r.online ? 'online' : 'offline'} />
					<span class="min-w-0 flex-1 truncate font-medium">{r.name || r.ip || r.mac}</span>
					<span class="mono text-xs text-fg-subtle">{r.ip} {r.mac}</span>
				</label>
			{/each}
			{#if mergeRows.length < n}
				<p class="text-xs text-fg-subtle">
					{n - mergeRows.length} ausgewählte Geräte sind auf anderen Seiten.
				</p>
			{/if}
		</fieldset>
	</div>
	{#snippet footer()}
		<Button onclick={() => (mergeOpen = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="merge" loading={busy}>Zusammenführen</Button>
	{/snippet}
</Modal>

<ActionParamsDialog
	bind:open={paramsOpen}
	action={paramsAction}
	{target}
	onrun={(p) => (paramsAction ? runAction(paramsAction, p) : Promise.resolve())}
/>
