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
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { locale, t, tn } from '$lib/i18n';

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

	// the filter is set in monospace: the sentence is split at its placeholder
	const savedText = $derived(
		tn(
			columns.length,
			'Gespeichert werden Filter {filter}, Sortierung und {n} Spalte.',
			'Gespeichert werden Filter {filter}, Sortierung und {n} Spalten.'
		).split('{filter}')
	);

	async function load() {
		try {
			views = ((await api.get('/api/v1/views')) ?? []).sort((a, b) => a.name.localeCompare(b.name, locale));
		} catch (e) {
			toast.error(e, { title: t('Ansichten konnten nicht geladen werden') });
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
			nameError = t('Name erforderlich');
			return;
		}
		busy = true;
		try {
			// id/timestamps are set by the server (the generated type marks them required)
			const body = { name: name.trim(), query, columns, sort } as SavedView;
			const v = await api.post('/api/v1/views', { body });
			toast.success(t('Ansicht „{name}“ gespeichert', { name: v.name }));
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
			toast.success(t('Ansicht „{name}“ aktualisiert', { name: active.name }));
			await load();
		} catch (e) {
			toast.error(e);
		}
	}

	async function remove() {
		if (!active) return;
		open = false;
		const ok = await confirm({
			title: t('Ansicht löschen?'),
			message: t('Die gespeicherte Ansicht „{name}“ wird gelöscht.', { name: active.name }),
			confirmLabel: t('Löschen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/views/{id}', { path: { id: active.id } });
			toast.success(t('Ansicht gelöscht'));
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
		label={active
			? dirty
				? t('Ansicht: {name} (geändert)', { name: active.name })
				: t('Ansicht: {name}', { name: active.name })
			: t('Gespeicherte Ansichten')}
		aria-haspopup="dialog"
		aria-expanded={open}
		onclick={() => (open = !open)}
	>
		<span class="hidden max-w-40 truncate sm:inline"
			>{active ? active.name : t('Ansichten')}{dirty ? ' *' : ''}</span
		>
	</Button>
</span>

<Popover bind:open {anchor} placement="bottom-end" label={t('Gespeicherte Ansichten')} class="w-72">
	<div class="py-1">
		<p class="px-3 pt-1.5 pb-1 text-[0.7rem] font-semibold tracking-wider text-fg-subtle uppercase">
			{t('Gespeicherte Ansichten')}
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
			{t('Alle Geräte (Standard)')}
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
			<p class="px-3 py-1.5 text-sm text-fg-subtle">{t('Noch keine Ansichten gespeichert.')}</p>
		{/each}
	</div>
	{#if auth.can('inventory.config')}
		<div class="border-t border-border py-1">
			<button
				type="button"
				class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2"
				onclick={openSave}
			>
				<Icon name="plus" size={14} class="text-fg-subtle" />
				{t('Als neue Ansicht speichern …')}
			</button>
			{#if active}
				<button
					type="button"
					class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2 disabled:opacity-50"
					disabled={!dirty}
					onclick={overwrite}
				>
					<Icon name="save" size={14} class="text-fg-subtle" />
					{t('„{name}“ aktualisieren', { name: active.name })}
				</button>
				<button
					type="button"
					class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-danger hover:bg-surface-2"
					onclick={remove}
				>
					<Icon name="trash" size={14} />
					{t('„{name}“ löschen', { name: active.name })}
				</button>
			{/if}
		</div>
	{/if}
</Popover>

<Modal bind:open={saveOpen} title={t('Ansicht speichern')} size="sm" as="form" onsubmit={saveNew} {busy}>
	<div class="flex flex-col gap-3">
		<Input label="Name" bind:value={name} error={nameError} required maxlength={80} />
		<div class="text-xs text-fg-muted">
			{savedText[0]}<span class="mono">{query || t('(keiner)')}</span>{savedText[1] ?? ''}
		</div>
	</div>
	{#snippet footer()}
		<Button onclick={() => (saveOpen = false)} disabled={busy}>{t('Abbrechen')}</Button>
		<Button type="submit" variant="primary" loading={busy}>{t('Speichern')}</Button>
	{/snippet}
</Modal>
