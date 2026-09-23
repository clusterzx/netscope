<!-- One entry of <DescList>. Empty content renders "–". <DescItem label="OS" hint="Quelle: nmap">{os}</DescItem> -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		hint?: string;
		mono?: boolean;
		/** text value; alternatively use children */
		value?: string | number | null;
		class?: string;
		children?: Snippet;
	}

	let { label, hint, mono = false, value, class: klass = '', children }: Props = $props();
</script>

<div class="min-w-0 {klass}">
	<dt class="text-xs text-fg-subtle">{label}</dt>
	<dd class="mt-0.5 min-w-0 text-sm break-words text-fg {mono ? 'mono' : ''}">
		{#if children}
			{@render children()}
		{:else if value !== undefined && value !== null && value !== ''}
			{value}
		{:else}
			<span class="text-fg-subtle">–</span>
		{/if}
		{#if hint}<span class="ml-1 text-xs text-fg-subtle">{hint}</span>{/if}
	</dd>
</div>
