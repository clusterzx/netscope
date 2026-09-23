<!--
	Offset/limit pagination with page size selection.
	<Pagination total={list.total} bind:offset bind:limit onchange={load} sizes={[50, 100, 250, 500]} />
-->
<script lang="ts">
	import Button from './Button.svelte';
	import { formatNumber } from '$lib/utils/format';

	interface Props {
		total: number;
		offset?: number;
		limit?: number;
		sizes?: number[];
		class?: string;
		/** called after offset/limit changed */
		onchange?: (offset: number, limit: number) => void;
	}

	let {
		total,
		offset = $bindable(0),
		limit = $bindable(50),
		sizes = [25, 50, 100, 250],
		class: klass = '',
		onchange
	}: Props = $props();

	const page = $derived(Math.floor(offset / limit) + 1);
	const pages = $derived(Math.max(1, Math.ceil(total / limit)));
	const from = $derived(total === 0 ? 0 : offset + 1);
	const to = $derived(Math.min(total, offset + limit));
	const uid = $props.id();

	function go(p: number) {
		const np = Math.min(Math.max(1, p), pages);
		offset = (np - 1) * limit;
		onchange?.(offset, limit);
	}

	function setLimit(e: Event) {
		limit = Number((e.target as HTMLSelectElement).value);
		offset = 0;
		onchange?.(offset, limit);
	}
</script>

<nav
	class="flex flex-wrap items-center justify-between gap-2 text-sm text-fg-muted {klass}"
	aria-label="Seitennavigation"
>
	<div class="tabular">
		{#if total > 0}
			{formatNumber(from)}–{formatNumber(to)} von {formatNumber(total)}
		{:else}
			0 Einträge
		{/if}
	</div>
	<div class="flex items-center gap-2">
		<label for="{uid}-size" class="hidden sm:inline">Pro Seite</label>
		<select
			id="{uid}-size"
			value={String(limit)}
			onchange={setLimit}
			class="h-7 rounded-md border border-border bg-surface px-1.5 text-[0.8125rem] text-fg"
		>
			{#each sizes as s (s)}<option value={String(s)}>{s}</option>{/each}
		</select>
		<div class="flex items-center gap-0.5">
			<Button
				variant="ghost"
				size="sm"
				icon="chevrons-left"
				label="Erste Seite"
				disabled={page <= 1}
				onclick={() => go(1)}
			/>
			<Button
				variant="ghost"
				size="sm"
				icon="chevron-left"
				label="Vorherige Seite"
				disabled={page <= 1}
				onclick={() => go(page - 1)}
			/>
			<span class="px-1.5 tabular">{page} / {pages}</span>
			<Button
				variant="ghost"
				size="sm"
				icon="chevron-right"
				label="Nächste Seite"
				disabled={page >= pages}
				onclick={() => go(page + 1)}
			/>
			<Button
				variant="ghost"
				size="sm"
				icon="chevrons-right"
				label="Letzte Seite"
				disabled={page >= pages}
				onclick={() => go(pages)}
			/>
		</div>
	</div>
</nav>
