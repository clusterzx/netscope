<!--
	Key/value list (<dl>). Use with <DescItem>.
	<DescList cols={2}>
		<DescItem label="Hersteller">{dev.vendor}</DescItem>
		<DescItem label="MAC" mono>{dev.mac}</DescItem>
	</DescList>
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		cols?: 1 | 2 | 3;
		class?: string;
		children: Snippet;
	}

	let { cols = 1, class: klass = '', children }: Props = $props();
	const grid = { 1: '', 2: 'sm:grid-cols-2', 3: 'sm:grid-cols-2 lg:grid-cols-3' };
</script>

<dl class="grid grid-cols-1 gap-x-6 gap-y-2.5 {grid[cols]} {klass}">
	{@render children()}
</dl>
