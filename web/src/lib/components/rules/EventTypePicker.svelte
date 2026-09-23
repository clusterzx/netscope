<!--
	Multi selection of event types (catalog grouped by category) incl. wildcard patterns
	like "port.*" and "*".
	<EventTypePicker bind:value={rule.conditions.eventTypes} />
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { Icon, MultiSelect } from '$lib/components/ui';
	import type { MultiOption } from '$lib/components/ui';
	import { eventTypes } from '$lib/stores/catalog.svelte';
	import { eventCategoryLabel } from '$lib/utils/labels';
	import { eventTypeName, typePatterns } from './rule';

	interface Props {
		value: string[];
		label?: string;
		hint?: string;
		error?: string | null;
		id?: string;
	}

	let { value = $bindable([]), label = 'Event-Typen', hint, error, id }: Props = $props();

	onMount(() => {
		eventTypes.load().catch(() => {});
	});

	const catalog = $derived(eventTypes.value ?? []);

	const options = $derived.by((): MultiOption[] => {
		const out: MultiOption[] = [
			{ value: '*', label: 'Alle Events', description: '*', group: 'Muster' },
			...typePatterns(catalog).map((p) => ({
				value: p,
				label: eventTypeName(p, catalog),
				description: p,
				group: 'Muster'
			}))
		];
		const cats = [...new Set(catalog.map((e) => e.category))];
		for (const c of cats)
			for (const e of catalog.filter((x) => x.category === c))
				out.push({ value: e.type, label: e.label, description: e.type, group: eventCategoryLabel[c] ?? c });
		// keep unknown stored values visible
		for (const v of value ?? [])
			if (!out.some((o) => typeof o !== 'string' && o.value === v))
				out.push({ value: v, label: v, description: 'unbekannt', group: 'Sonstige' });
		return out;
	});
</script>

<div class="flex flex-col gap-1.5">
	<MultiSelect {id} {label} {hint} {error} bind:value {options} searchable placeholder="Alle Event-Typen" />
	{#if (value ?? []).length > 2}
		<ul class="flex flex-wrap gap-1" aria-label="Ausgewählte Event-Typen">
			{#each value as t (t)}
				<li
					class="inline-flex items-center gap-0.5 rounded bg-surface-3 py-0.5 pr-0.5 pl-1.5 text-[0.8125rem] text-fg"
				>
					{eventTypeName(t, catalog)}
					<button
						type="button"
						class="rounded p-0.5 text-fg-subtle hover:bg-border hover:text-fg"
						aria-label="{eventTypeName(t, catalog)} entfernen"
						onclick={() => (value = value.filter((x) => x !== t))}
					>
						<Icon name="x" size={12} />
					</button>
				</li>
			{/each}
		</ul>
	{/if}
</div>
