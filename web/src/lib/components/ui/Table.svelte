<!--
	Data table with sortable headers, row selection, sticky header and responsive columns.

	<Table columns={cols} rows={items} key={(r) => r.id} sort={sort} onsort={(s) => (sort = s)}
	       selectable bind:selected onrowclick={(r) => goto(`/devices/${r.id}`)}>
		{#snippet cell(row, col)}
			{#if col.key === 'name'}<a href="/devices/{row.id}" class="link">{row.name}</a>
			{:else}{col.value?.(row) ?? ''}{/if}
		{/snippet}
		{#snippet empty()}<EmptyState title="Keine Geräte" />{/snippet}
	</Table>

	Column: { key, label, sortable?: boolean | string (sort field, default key), align?, width?,
	          class?, headerClass?, hideBelow?: 'sm' | 'md' | 'lg' | 'xl', value?: (row) => string | number,
	          cell?: Snippet<[row]>, title?: string }
	Cell precedence: column.cell › table `cell` snippet › column.value › row[key].
	sort: "field" (ascending) or "-field" (descending); onsort receives the next value.
	selected: array of row keys (bindable).
-->
<script lang="ts" module>
	import type { Snippet } from 'svelte';
	export interface Column<T> {
		key: string;
		label: string;
		sortable?: boolean | string;
		/** first click sorts descending (dates, counts) */
		sortDesc?: boolean;
		align?: 'left' | 'right' | 'center';
		width?: string;
		class?: string;
		headerClass?: string;
		hideBelow?: 'sm' | 'md' | 'lg' | 'xl';
		value?: (row: T) => string | number | null | undefined;
		cell?: Snippet<[T]>;
		/** tooltip of the header */
		title?: string;
	}
	export type RowKey = string | number;
</script>

<script lang="ts" generics="T">
	import Checkbox from './Checkbox.svelte';
	import Icon from './Icon.svelte';
	import Skeleton from './Skeleton.svelte';

	interface Props {
		columns: Column<T>[];
		rows: T[];
		key: (row: T) => RowKey;
		sort?: string;
		onsort?: (sort: string) => void;
		selectable?: boolean;
		selected?: RowKey[];
		onrowclick?: (row: T, e: MouseEvent) => void;
		rowClass?: (row: T) => string;
		loading?: boolean;
		dense?: boolean;
		/** CSS max-height of the scroll container (enables the sticky header), e.g. "70vh" */
		maxHeight?: string;
		caption?: string;
		class?: string;
		cell?: Snippet<[T, Column<T>]>;
		empty?: Snippet;
		/** extra content rendered below a row (e.g. expanded details) */
		expanded?: Snippet<[T]>;
	}

	let {
		columns,
		rows,
		key,
		sort = '',
		onsort,
		selectable = false,
		selected = $bindable([]),
		onrowclick,
		rowClass,
		loading = false,
		dense = false,
		maxHeight,
		caption,
		class: klass = '',
		cell,
		empty,
		expanded
	}: Props = $props();

	const hide = {
		sm: 'hidden sm:table-cell',
		md: 'hidden md:table-cell',
		lg: 'hidden lg:table-cell',
		xl: 'hidden xl:table-cell'
	};
	const alignCls = { left: 'text-left', right: 'text-right', center: 'text-center' };

	const sortField = $derived(sort.startsWith('-') ? sort.slice(1) : sort);
	const sortDir = $derived(sort.startsWith('-') ? 'desc' : 'asc');
	const selSet = $derived(new Set(selected ?? []));
	const allSelected = $derived(rows.length > 0 && rows.every((r) => selSet.has(key(r))));
	const someSelected = $derived(!allSelected && rows.some((r) => selSet.has(key(r))));

	function fieldOf(c: Column<T>): string {
		return typeof c.sortable === 'string' ? c.sortable : c.key;
	}

	function toggleSort(c: Column<T>) {
		const f = fieldOf(c);
		let next: string;
		if (sortField === f) next = sortDir === 'asc' ? '-' + f : f;
		else next = c.sortDesc ? '-' + f : f;
		onsort?.(next);
	}

	function toggleAll() {
		if (allSelected) {
			const visible = new Set(rows.map(key));
			selected = (selected ?? []).filter((k) => !visible.has(k));
		} else {
			const next = new Set(selected ?? []);
			for (const r of rows) next.add(key(r));
			selected = [...next];
		}
	}

	function toggleRow(k: RowKey) {
		selected = selSet.has(k) ? (selected ?? []).filter((x) => x !== k) : [...(selected ?? []), k];
	}

	function rowClick(row: T, e: MouseEvent) {
		if (!onrowclick) return;
		const t = e.target as HTMLElement;
		if (t.closest('a,button,input,label,select,textarea,[role="button"]')) return;
		if (window.getSelection()?.toString()) return;
		onrowclick(row, e);
	}

	function raw(row: T, c: Column<T>): string {
		if (c.value) return String(c.value(row) ?? '');
		const v = (row as Record<string, unknown>)[c.key];
		return v === null || v === undefined ? '' : String(v);
	}
</script>

<div
	class="relative min-w-0 overflow-auto rounded-lg border border-border bg-surface {klass}"
	style={maxHeight ? `max-height:${maxHeight}` : undefined}
>
	<table class="w-full border-separate border-spacing-0 text-sm" aria-busy={loading || undefined}>
		{#if caption}<caption class="sr-only">{caption}</caption>{/if}
		<thead class="sticky top-0 z-10">
			<tr>
				{#if selectable}
					<th scope="col" class="w-9 border-b border-border bg-surface-2 px-3 {dense ? 'py-1.5' : 'py-2'}">
						<Checkbox
							checked={allSelected}
							indeterminate={someSelected}
							onchange={toggleAll}
							label="Alle sichtbaren auswählen"
							hideLabel
						/>
					</th>
				{/if}
				{#each columns as c (c.key)}
					<th
						scope="col"
						class="border-b border-border bg-surface-2 px-3 text-xs font-semibold whitespace-nowrap text-fg-muted
							{dense ? 'py-1.5' : 'py-2'} {alignCls[c.align ?? 'left']} {c.hideBelow
							? hide[c.hideBelow]
							: ''} {c.headerClass ?? ''}"
						style={c.width ? `width:${c.width}` : undefined}
						title={c.title}
						aria-sort={c.sortable && sortField === fieldOf(c)
							? sortDir === 'asc'
								? 'ascending'
								: 'descending'
							: undefined}
					>
						{#if c.sortable && onsort}
							<button
								type="button"
								class="inline-flex items-center gap-1 rounded hover:text-fg {sortField === fieldOf(c)
									? 'text-fg'
									: ''}"
								onclick={() => toggleSort(c)}
							>
								{c.label}
								{#if sortField === fieldOf(c)}
									<Icon name={sortDir === 'asc' ? 'arrow-up' : 'arrow-down'} size={12} />
								{:else}
									<Icon name="sort" size={12} class="opacity-30" />
								{/if}
							</button>
						{:else}
							{c.label}
						{/if}
					</th>
				{/each}
			</tr>
		</thead>
		<tbody class={loading && rows.length ? 'opacity-60 transition-opacity' : ''}>
			{#each rows as row (key(row))}
				{@const k = key(row)}
				{@const isSel = selSet.has(k)}
				<!-- rows contain a real link/button for keyboard users; the row click is a mouse shortcut -->
				<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
				<tr
					class="group {onrowclick ? 'cursor-pointer' : ''} {isSel
						? 'bg-accent-soft'
						: 'hover:bg-surface-2'} {rowClass?.(row) ?? ''}"
					onclick={(e) => rowClick(row, e)}
				>
					{#if selectable}
						<td class="border-b border-border px-3 {dense ? 'py-1' : 'py-2'}">
							<Checkbox checked={isSel} onchange={() => toggleRow(k)} label="Zeile auswählen" hideLabel />
						</td>
					{/if}
					{#each columns as c (c.key)}
						<td
							class="border-b border-border px-3 align-middle {dense ? 'py-1' : 'py-2'} {alignCls[
								c.align ?? 'left'
							]}
								{c.hideBelow ? hide[c.hideBelow] : ''} {c.class ?? ''}"
						>
							{#if c.cell}
								{@render c.cell(row)}
							{:else if cell}
								{@render cell(row, c)}
							{:else}
								{raw(row, c)}
							{/if}
						</td>
					{/each}
				</tr>
				{#if expanded}
					{@render expanded(row)}
				{/if}
			{/each}
		</tbody>
	</table>
	{#if rows.length === 0}
		{#if loading}
			<div class="p-4"><Skeleton rows={6} /></div>
		{:else if empty}
			{@render empty()}
		{:else}
			<p class="px-4 py-8 text-center text-sm text-fg-subtle">Keine Einträge</p>
		{/if}
	{/if}
</div>
