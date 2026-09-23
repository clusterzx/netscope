<!--
	Preformatted text with copy button and optional max height.
	<CodeBlock code={raw} />   <CodeBlock code={JSON.stringify(x, null, 2)} maxHeight="24rem" wrap />
-->
<script lang="ts">
	import CopyButton from './CopyButton.svelte';

	interface Props {
		code: string;
		label?: string;
		maxHeight?: string;
		wrap?: boolean;
		class?: string;
	}

	let { code, label, maxHeight = '28rem', wrap = false, class: klass = '' }: Props = $props();
</script>

<div class="group relative min-w-0 rounded-md border border-border bg-surface-2 {klass}">
	{#if label}
		<div class="border-b border-border px-3 py-1.5 text-xs font-medium text-fg-muted">{label}</div>
	{/if}
	<div class="absolute top-1 right-1 opacity-70 group-hover:opacity-100 focus-within:opacity-100">
		<CopyButton text={code} />
	</div>
	<pre
		class="mono overflow-auto px-3 py-2.5 pr-10 text-[0.8rem] leading-relaxed text-fg {wrap
			? 'break-all whitespace-pre-wrap'
			: ''}"
		style="max-height:{maxHeight}">{code}</pre>
</div>
