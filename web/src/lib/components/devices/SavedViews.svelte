<!-- Saved device views (GET/POST/PUT/DELETE /api/v1/views): name, query, columns, sort. -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { SavedView } from '$lib/api';
	import Button from '$lib/components/ui/Button.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import Popover from '$lib/components/ui/Popover.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { toast } from '$lib/stores/toast.svelte';

	interface Props {
		activeId: number | null;
		query: string;
		columns: string[];
		sort: string;
		onapply: (v: SavedView | null) => void;
	}

	let { activeId, query, columns, sort, onapply }: Props = $props();

	let views = $state<SavedView[]>([]);
	let open = $state(false);
	let anchor: HTMLSpanElement | null = $state(null);
	let saveOpen = $state(false);
	let name = $state('');
	let nameError = $state('');
	let busy = $state(false);

	const active = $derived(views.find((v) => v.id === activeId) ?? null);
	const dirty = $derived(
		!!active &&
			(active.query !== query || active.sort !== sort || (active.columns ?? []).join() !== columns.join())
	);

	async function load() {
		try {
			views = ((await api.get('/api/v1/views')) ?? []).sort((a, b) => a.name.localeCompare(b.name, 'de'));
		} catch (e) {
			toast.error(e, { title: 'Ansichten konnten nicht geladen werden' });
		}
	}
	onMount(load);

	function openSave() {
		open = false;
		name = '';
		nameError = '';
		saveOpen = true;
	}

	async function saveNew() {
		if (!name.trim()) {
			nameError = 'Name erforderlich';
			return;
		}
		busy = true;
		try {
			// id/timestamps are set by the server (the generated type marks them required)
			const body = { name: name.trim(), query, columns, sort } as SavedView;
			const v = await api.post('/api/v1/views', { body });
			toast.success(`Ansicht „${v.name}“ gespeichert`);
			saveOpen = false;
			await load();
			onapply(v);
		} catch (e) {
			nameError = fieldErrors(e).name ?? errorMessage(e);
		} finally {
			busy = false;
		}
	}

	async function overwrite() {
		if (!active) return;
		open = false;
		try {
			await api.put('/api/v1/views/{id}', {
				path: { id: active.id },
				body: { ...active, query, columns, sort }
			});
			toast.success(`Ansicht „${active.name}“ aktualisiert`);
			await load();
		} catch (e) {
			toast.error(e);
		}
	}

	async function remove() {
		if (!active) return;
		open = false;
		const ok = await confirm({
			title: 'Ansicht löschen?',
			message: `Die gespeicherte Ansicht „${active.name}“ wird gelöscht.`,
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/views/{id}', { path: { id: active.id } });
			toast.success('Ansicht gelöscht');
			onapply(null);
			await load();
		} catch (e) {
			toast.error(e);
		}
	}
</script>

<span bind:this={anchor} class="inline-flex">
	<Button
		icon="bookmark"
		iconRight="chevron-down"
		label={active ? `Ansicht: ${active.name}${dirty ? ' (geändert)' : ''}` : 'Gespeicherte Ansichten'}
		aria-haspopup="dialog"
		aria-expanded={open}
		onclick={() => (open = !open)}
	>
		<span class="hidden max-w-40 truncate sm:inline"
			>{active ? active.name : 'Ansichten'}{dirty ? ' *' : ''}</span
		>
	</Button>
</span>

<Popover bind:open {anchor} placement="bottom-end" label="Gespeicherte Ansichten" class="w-72">
	<div class="py-1">
		<p class="px-3 pt-1.5 pb-1 text-[0.7rem] font-semibold tracking-wider text-fg-subtle uppercase">
			Gespeicherte Ansichten
		</p>
		<button
			type="button"
			class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2"
			onclick={() => {
				open = false;
				onapply(null);
			}}
		>
			<span class="w-4"
				>{#if !activeId}<Icon name="check" size={14} class="text-accent" />{/if}</span
			>
			Alle Geräte (Standard)
		</button>
		{#each views as v (v.id)}
			<button
				type="button"
				class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2"
				onclick={() => {
					open = false;
					onapply(v);
				}}
			>
				<span class="w-4"
					>{#if v.id === activeId}<Icon name="check" size={14} class="text-accent" />{/if}</span
				>
				<span class="min-w-0 flex-1">
					<span class="block truncate">{v.name}</span>
					{#if v.query}<span class="mono block truncate text-xs text-fg-subtle">{v.query}</span>{/if}
				</span>
			</button>
		{:else}
			<p class="px-3 py-1.5 text-sm text-fg-subtle">Noch keine Ansichten gespeichert.</p>
		{/each}
	</div>
	<div class="border-t border-border py-1">
		<button
			type="button"
			class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2"
			onclick={openSave}
		>
			<Icon name="plus" size={14} class="text-fg-subtle" /> Als neue Ansicht speichern …
		</button>
		{#if active}
			<button
				type="button"
				class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2 disabled:opacity-50"
				disabled={!dirty}
				onclick={overwrite}
			>
				<Icon name="save" size={14} class="text-fg-subtle" /> „{active.name}“ aktualisieren
			</button>
			<button
				type="button"
				class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-danger hover:bg-surface-2"
				onclick={remove}
			>
				<Icon name="trash" size={14} /> „{active.name}“ löschen
			</button>
		{/if}
	</div>
</Popover>

<Modal bind:open={saveOpen} title="Ansicht speichern" size="sm" as="form" onsubmit={saveNew} {busy}>
	<div class="flex flex-col gap-3">
		<Input label="Name" bind:value={name} error={nameError} required maxlength={80} />
		<div class="text-xs text-fg-muted">
			Gespeichert werden Filter <span class="mono">{query || '(keiner)'}</span>, Sortierung und {columns.length}
			Spalten.
		</div>
	</div>
	{#snippet footer()}
		<Button onclick={() => (saveOpen = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" loading={busy}>Speichern</Button>
	{/snippet}
</Modal>
