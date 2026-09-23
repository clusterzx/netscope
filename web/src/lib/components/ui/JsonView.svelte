<!--
	Collapsible JSON tree with a "raw" toggle and copy.
	<JsonView value={obs.data} openDepth={2} />
-->
<script lang="ts">
	import CodeBlock from './CodeBlock.svelte';
	import CopyButton from './CopyButton.svelte';
	import JsonNode from './JsonNode.svelte';

	interface Props {
		value: unknown;
		openDepth?: number;
		maxHeight?: string;
		class?: string;
	}

	let { value, openDepth = 2, maxHeight = '32rem', class: klass = '' }: Props = $props();
	let raw = $state(false);
	const text = $derived(JSON.stringify(value, null, 2) ?? 'undefined');
</script>

<div class="min-w-0 {klass}">
	<div class="mb-1.5 flex items-center justify-end gap-1">
		<button
			type="button"
			class="rounded px-2 py-0.5 text-xs {raw ? 'text-fg-muted hover:text-fg' : 'bg-surface-3 text-fg'}"
			aria-pressed={!raw}
			onclick={() => (raw = false)}>Baum</button
		>
		<button
			type="button"
			class="rounded px-2 py-0.5 text-xs {raw ? 'bg-surface-3 text-fg' : 'text-fg-muted hover:text-fg'}"
			aria-pressed={raw}
			onclick={() => (raw = true)}>JSON</button
		>
		{#if !raw}<CopyButton {text} label="JSON kopieren" />{/if}
	</div>
	{#if raw}
		<CodeBlock code={text} {maxHeight} />
	{:else}
		<div
			class="mono overflow-auto rounded-md border border-border bg-surface-2 px-3 py-2 text-[0.8rem]"
			style="max-height:{maxHeight}"
		>
			<JsonNode {value} depth={0} {openDepth} />
		</div>
	{/if}
</div>
