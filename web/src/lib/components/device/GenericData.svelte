<!--
	Readable rendering of arbitrary inventory JSON: scalars as key/value list, scalar
	arrays as chips, object arrays as tables, nested objects as sub-sections (deeper → JSON).
-->
<script lang="ts">
	import JsonView from '$lib/components/ui/JsonView.svelte';
	import GenericData from './GenericData.svelte';
	import { fieldLabel, formatField, isScalar } from './inventory';

	interface Props {
		value: unknown;
		depth?: number;
	}

	let { value, depth = 0 }: Props = $props();

	const MAX_ROWS = 50;
	let showAll = $state<Record<string, boolean>>({});

	type Entry = [string, unknown];
	const obj = $derived(
		value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : null
	);
	const scalars = $derived<Entry[]>(
		obj ? Object.entries(obj).filter(([, v]) => isScalar(v) && v !== '' && v !== null) : []
	);
	const lists = $derived<Entry[]>(
		obj ? Object.entries(obj).filter(([, v]) => Array.isArray(v) && v.length > 0) : []
	);
	const nested = $derived<Entry[]>(
		obj
			? Object.entries(obj).filter(
					([, v]) => v && typeof v === 'object' && !Array.isArray(v) && Object.keys(v).length > 0
				)
			: []
	);

	function columnsOf(rows: Record<string, unknown>[]): string[] {
		const cols: string[] = [];
		for (const r of rows.slice(0, 100))
			for (const [k, v] of Object.entries(r))
				if (!cols.includes(k) && (isScalar(v) || Array.isArray(v))) cols.push(k);
		return cols.slice(0, 8);
	}

	function cell(key: string, v: unknown): string {
		if (Array.isArray(v))
			return v.map((x) => (isScalar(x) ? formatField(key, x) : JSON.stringify(x))).join(', ');
		return formatField(key, v);
	}
</script>

{#if Array.isArray(value)}
	<GenericData value={{ Einträge: value }} {depth} />
{:else if !obj}
	<p class="text-sm">{formatField('', value)}</p>
{:else}
	<div class="flex flex-col gap-3">
		{#if scalars.length}
			<dl class="grid grid-cols-1 gap-x-6 gap-y-2 sm:grid-cols-2 {depth === 0 ? 'xl:grid-cols-3' : ''}">
				{#each scalars as [k, v] (k)}
					<div class="min-w-0">
						<dt class="text-xs text-fg-subtle">{fieldLabel(k)}</dt>
						<dd class="mt-0.5 text-sm break-words">{formatField(k, v)}</dd>
					</div>
				{/each}
			</dl>
		{/if}

		{#each lists as [k, v] (k)}
			{@const arr = v as unknown[]}
			<div class="min-w-0">
				<h4 class="mb-1 text-xs font-semibold text-fg-muted">{fieldLabel(k)} ({arr.length})</h4>
				{#if arr.every(isScalar)}
					<div class="flex flex-wrap gap-1">
						{#each arr.slice(0, showAll[k] ? arr.length : 40) as x, i (i)}
							<span class="mono rounded bg-surface-3 px-1.5 py-0.5 text-xs break-all"
								>{formatField(k, x)}</span
							>
						{/each}
						{#if arr.length > 40 && !showAll[k]}
							<button
								type="button"
								class="text-xs text-accent hover:underline"
								onclick={() => (showAll[k] = true)}
							>
								+ {arr.length - 40} weitere
							</button>
						{/if}
					</div>
				{:else if arr.every((x) => x && typeof x === 'object' && !Array.isArray(x))}
					{@const rows = arr as Record<string, unknown>[]}
					{@const cols = columnsOf(rows)}
					<div class="relative overflow-x-auto rounded-md border border-border">
						<table class="w-full border-separate border-spacing-0 text-xs">
							<thead>
								<tr class="bg-surface-2 text-left text-fg-muted">
									{#each cols as c (c)}
										<th
											scope="col"
											class="border-b border-border px-2.5 py-1.5 font-semibold whitespace-nowrap"
											>{fieldLabel(c)}</th
										>
									{/each}
								</tr>
							</thead>
							<tbody>
								{#each rows.slice(0, showAll[k] ? rows.length : MAX_ROWS) as r, i (i)}
									<tr>
										{#each cols as c (c)}
											<td class="border-b border-border px-2.5 py-1 align-top break-words">{cell(c, r[c])}</td
											>
										{/each}
									</tr>
								{/each}
							</tbody>
						</table>
						{#if rows.length > MAX_ROWS && !showAll[k]}
							<button
								type="button"
								class="w-full py-1.5 text-xs text-accent hover:underline"
								onclick={() => (showAll[k] = true)}
							>
								Alle {rows.length} anzeigen
							</button>
						{/if}
					</div>
				{:else}
					<JsonView value={arr} openDepth={1} maxHeight="20rem" />
				{/if}
			</div>
		{/each}

		{#each nested as [k, v] (k)}
			<div class="min-w-0 border-l-2 border-border pl-3">
				<h4 class="mb-1.5 text-xs font-semibold text-fg-muted">{fieldLabel(k)}</h4>
				{#if depth < 2}
					<GenericData value={v} depth={depth + 1} />
				{:else}
					<JsonView value={v} openDepth={1} maxHeight="20rem" />
				{/if}
			</div>
		{/each}
	</div>
{/if}
