<!-- Internal: one node of JsonView (recursive). -->
<script lang="ts">
	import Self from './JsonNode.svelte';

	interface Props {
		value: unknown;
		name?: string | number;
		depth: number;
		openDepth: number;
		last?: boolean;
	}

	let { value, name, depth, openDepth, last = true }: Props = $props();

	const isArr = $derived(Array.isArray(value));
	const isObj = $derived(value !== null && typeof value === 'object');
	const entries = $derived(
		isObj
			? isArr
				? (value as unknown[]).map((v, i) => [i, v] as const)
				: Object.entries(value as object)
			: []
	);
	let open = $state<boolean | null>(null);
	const expanded = $derived(open ?? depth < openDepth);

	function fmt(v: unknown): { text: string; cls: string } {
		if (v === null) return { text: 'null', cls: 'text-fg-subtle' };
		switch (typeof v) {
			case 'string':
				return { text: JSON.stringify(v), cls: 'text-ok' };
			case 'number':
				return { text: String(v), cls: 'text-sev-low' };
			case 'boolean':
				return { text: String(v), cls: 'text-unknown' };
			default:
				return { text: String(v), cls: 'text-fg' };
		}
	}
</script>

<div class="leading-relaxed" style="padding-left:{depth ? 1 : 0}rem">
	{#if isObj}
		<button
			type="button"
			class="inline-flex items-center gap-1 rounded text-left hover:bg-surface-3"
			aria-expanded={expanded}
			onclick={() => (open = !expanded)}
		>
			<span class="inline-block w-3 text-fg-subtle">{expanded ? '▾' : '▸'}</span>
			{#if name !== undefined}<span class="text-accent"
					>{typeof name === 'number' ? name : JSON.stringify(name)}</span
				><span class="text-fg-subtle">:</span>{/if}
			<span class="text-fg-subtle"
				>{isArr ? '[' : '{'}{#if !expanded}
					<span class="text-fg-subtle italic">{entries.length} {isArr ? 'Einträge' : 'Felder'}</span>
					{isArr ? ']' : '}'}{last ? '' : ','}{/if}</span
			>
		</button>
		{#if expanded}
			{#each entries as [k, v], i (k)}
				<Self value={v} name={k} depth={depth + 1} {openDepth} last={i === entries.length - 1} />
			{/each}
			<div class="text-fg-subtle" style="padding-left:1rem">{isArr ? ']' : '}'}{last ? '' : ','}</div>
		{/if}
	{:else}
		{@const f = fmt(value)}
		<span class="pl-4">
			{#if name !== undefined}<span class="text-accent"
					>{typeof name === 'number' ? name : JSON.stringify(name)}</span
				><span class="text-fg-subtle">: </span>{/if}<span class="break-all {f.cls}">{f.text}</span><span
				class="text-fg-subtle">{last ? '' : ','}</span
			>
		</span>
	{/if}
</div>
