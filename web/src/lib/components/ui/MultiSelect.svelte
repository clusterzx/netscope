<!--
	Multi selection from a list (dropdown with search and checkboxes).
	<MultiSelect label="Event-Typen" bind:value={types} options={[{ value: 'port.opened', label: 'Port neu', group: 'Ports' }]} />
	value: string[]; options: { value, label, description?, group? } (strings allowed).
-->
<script lang="ts" module>
	export type MultiOption = string | { value: string; label: string; description?: string; group?: string };
</script>

<script lang="ts">
	import FormField from './FormField.svelte';
	import Icon from './Icon.svelte';
	import Popover from './Popover.svelte';

	interface Props {
		value?: string[];
		options: MultiOption[];
		label?: string;
		hint?: string;
		error?: string | null;
		placeholder?: string;
		required?: boolean;
		disabled?: boolean;
		size?: 'sm' | 'md';
		/** show a search box (default: more than 8 options) */
		searchable?: boolean;
		id?: string;
		class?: string;
		onchange?: (value: string[]) => void;
	}

	let {
		value = $bindable([]),
		options,
		label,
		hint,
		error,
		placeholder = 'Auswählen …',
		required = false,
		disabled = false,
		size = 'md',
		searchable,
		id,
		class: klass = '',
		onchange
	}: Props = $props();

	let open = $state(false);
	let search = $state('');
	let trigger: HTMLButtonElement | null = $state(null);

	const norm = $derived(
		options.map((o) => (typeof o === 'string' ? { value: o, label: o, description: '', group: '' } : o))
	);
	const selected = $derived(new Set(value ?? []));
	const filtered = $derived.by(() => {
		const q = search.trim().toLowerCase();
		if (!q) return norm;
		return norm.filter((o) => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q));
	});
	const showSearch = $derived(searchable ?? norm.length > 8);
	const summary = $derived.by(() => {
		const sel = norm.filter((o) => selected.has(o.value));
		if (sel.length === 0) return '';
		if (sel.length <= 2) return sel.map((o) => o.label).join(', ');
		return `${sel.length} ausgewählt`;
	});

	function toggle(v: string) {
		const next = selected.has(v) ? (value ?? []).filter((x) => x !== v) : [...(value ?? []), v];
		value = next;
		onchange?.(next);
	}

	function clear() {
		value = [];
		onchange?.([]);
	}

	$effect(() => {
		if (!open) search = '';
	});
</script>

<FormField {label} {hint} {error} {required} {id} class={klass}>
	{#snippet children(fid, describedby)}
		<button
			bind:this={trigger}
			type="button"
			id={fid}
			{disabled}
			aria-haspopup="listbox"
			aria-expanded={open}
			aria-describedby={describedby}
			onclick={() => (open = !open)}
			class="flex w-full min-w-0 items-center gap-2 rounded-md border bg-surface text-left shadow-sm transition-colors
				focus:border-accent focus:outline-none focus:ring-2 focus:ring-focus disabled:cursor-not-allowed disabled:opacity-60
				{error ? 'border-danger' : 'border-border hover:border-border-strong'}
				{size === 'sm' ? 'h-7 px-2 text-[0.8125rem]' : 'h-8.5 px-2.5 text-sm'}"
		>
			<span class="min-w-0 flex-1 truncate {summary ? 'text-fg' : 'text-fg-subtle'}"
				>{summary || placeholder}</span
			>
			{#if (value?.length ?? 0) > 0}
				<span class="rounded bg-accent-soft px-1.5 text-xs font-medium text-accent tabular"
					>{value?.length}</span
				>
			{/if}
			<Icon name="chevron-down" size={14} class="text-fg-subtle" />
		</button>
	{/snippet}
</FormField>

<Popover
	bind:open
	anchor={trigger}
	matchWidth
	role="listbox"
	label={label ?? placeholder}
	class="w-72 max-w-[90vw]"
>
	{#if showSearch}
		<div class="sticky top-0 border-b border-border bg-surface p-2">
			<!-- svelte-ignore a11y_autofocus -->
			<input
				type="search"
				bind:value={search}
				autofocus
				placeholder="Filtern …"
				aria-label="Optionen filtern"
				class="h-7 w-full rounded border border-border bg-surface-2 px-2 text-[0.8125rem] focus:border-accent focus:outline-none"
			/>
		</div>
	{/if}
	<div class="py-1">
		{#each filtered as o, i (o.value)}
			{#if o.group && (i === 0 || filtered[i - 1].group !== o.group)}
				<div class="px-3 pt-2 pb-1 text-[0.7rem] font-semibold tracking-wide text-fg-subtle uppercase">
					{o.group}
				</div>
			{/if}
			<label class="flex cursor-pointer items-start gap-2 px-3 py-1.5 text-sm hover:bg-surface-2">
				<input
					type="checkbox"
					checked={selected.has(o.value)}
					onchange={() => toggle(o.value)}
					class="mt-0.5 h-4 w-4 shrink-0 accent-(--accent)"
				/>
				<span class="min-w-0">
					<span class="block text-fg">{o.label}</span>
					{#if o.description}<span class="block text-xs text-fg-subtle">{o.description}</span>{/if}
				</span>
			</label>
		{:else}
			<p class="px-3 py-2 text-sm text-fg-subtle">Keine Treffer</p>
		{/each}
	</div>
	{#if (value?.length ?? 0) > 0}
		<div class="sticky bottom-0 flex justify-between border-t border-border bg-surface px-3 py-1.5">
			<span class="text-xs text-fg-subtle">{value?.length} ausgewählt</span>
			<button type="button" class="text-xs text-accent hover:underline" onclick={clear}>Auswahl leeren</button
			>
		</div>
	{/if}
</Popover>
