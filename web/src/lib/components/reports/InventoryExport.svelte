<!--
	Inventory export (CSV/JSON/PDF) with an optional device filter.
	<InventoryExport />
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, errorMessage } from '$lib/api';
	import QueryInput from '$lib/components/QueryInput.svelte';
	import { Button, Card, Icon, Spinner } from '$lib/components/ui';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatNumber } from '$lib/utils/format';

	interface Props {
		class?: string;
	}
	let { class: klass = '' }: Props = $props();

	let q = $state('');
	let applied = $state('');
	let queryError = $state<string | null>(null);
	let count = $state<number | null>(null);
	let counting = $state(false);
	let busy = $state<string | null>(null);

	async function apply(text: string) {
		applied = text.trim();
		queryError = null;
		counting = true;
		try {
			const res = await api.get('/api/v1/devices', { query: { q: applied || null, limit: 1 } });
			count = res.total;
		} catch (e) {
			count = null;
			if (e instanceof ApiError && e.status === 400) queryError = e.message;
			else toast.error(e);
		} finally {
			counting = false;
		}
	}

	onMount(() => {
		apply('');
	});

	async function download(format: 'csv' | 'json' | 'pdf') {
		if (q.trim() !== applied) await apply(q);
		if (queryError) return;
		busy = format;
		try {
			await api.download(
				'/api/v1/reports/inventory',
				{ format, q: applied || null },
				`netscope-inventar.${format}`
			);
		} catch (e) {
			if (e instanceof ApiError && e.status === 400) queryError = e.message;
			else toast.error(errorMessage(e), { title: 'Export fehlgeschlagen' });
		} finally {
			busy = null;
		}
	}

	const formats = [
		{ id: 'csv', label: 'CSV', hint: 'Tabellenkalkulation, Re-Import über den CSV-Importer' },
		{ id: 'json', label: 'JSON', hint: 'Vollständige Gerätedaten für Skripte' },
		{ id: 'pdf', label: 'PDF', hint: 'Druckfertige Inventarliste' }
	] as const;
</script>

<Card
	title="Inventar-Export"
	description="Alle Geräte oder eine gefilterte Auswahl herunterladen"
	icon="download"
	class={klass}
>
	<div class="flex flex-col gap-3">
		<div>
			<QueryInput
				bind:value={q}
				onsubmit={apply}
				error={queryError}
				label="Filter für den Export"
				showLabel
			/>
			<p class="mt-1.5 flex items-center gap-1.5 text-xs text-fg-subtle" aria-live="polite">
				{#if counting}
					<Spinner size={12} /> Zähle Geräte …
				{:else if count !== null && !queryError}
					{applied
						? `${formatNumber(count)} Geräte passen zum Filter „${applied}“`
						: `${formatNumber(count)} Geräte (ohne Filter)`}
				{:else}
					Leer lassen für das komplette Inventar. Enter übernimmt den Filter.
				{/if}
			</p>
		</div>
		<div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
			{#each formats as f (f.id)}
				<button
					type="button"
					onclick={() => download(f.id)}
					disabled={busy !== null}
					class="group flex items-start gap-2.5 rounded-md border border-border bg-surface px-3 py-2.5 text-left transition-colors hover:border-border-strong hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-60"
				>
					<span class="mt-0.5 text-fg-subtle group-hover:text-accent">
						{#if busy === f.id}<Spinner size={16} />{:else}<Icon name="download" size={16} />{/if}
					</span>
					<span class="min-w-0">
						<span class="block text-sm font-medium text-fg">{f.label} herunterladen</span>
						<span class="block text-xs text-fg-subtle">{f.hint}</span>
					</span>
				</button>
			{/each}
		</div>
	</div>
	{#snippet footer()}
		<div class="flex flex-wrap items-center justify-between gap-2 text-xs text-fg-subtle">
			<span>Die Filtersprache entspricht der Geräteliste.</span>
			<Button
				size="xs"
				variant="ghost"
				href={applied ? `/devices?q=${encodeURIComponent(applied)}` : '/devices'}
				iconRight="arrow-right">In der Geräteliste ansehen</Button
			>
		</div>
	{/snippet}
</Card>
