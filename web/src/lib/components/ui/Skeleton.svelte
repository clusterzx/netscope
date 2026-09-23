<!--
	Loading placeholder.
	<Skeleton class="h-4 w-40" />            one bar
	<Skeleton lines={5} />                   text block
	<Skeleton rows={8} />                    table rows
-->
<script lang="ts">
	interface Props {
		lines?: number;
		rows?: number;
		class?: string;
	}
	let { lines = 0, rows = 0, class: klass = '' }: Props = $props();

	const bar = 'ns-skeleton';
</script>

<div role="status" aria-label="Lädt …" class="w-full">
	{#if rows > 0}
		<div class="flex flex-col gap-2">
			{#each Array(rows) as _, i (i)}
				<div class="flex items-center gap-3">
					<div class="{bar} h-4 w-4"></div>
					<div class="{bar} h-4" style="width: {30 + ((i * 37) % 40)}%"></div>
					<div class="{bar} h-4 w-24"></div>
					<div class="{bar} ml-auto h-4 w-16"></div>
				</div>
			{/each}
		</div>
	{:else if lines > 0}
		<div class="flex flex-col gap-2">
			{#each Array(lines) as _, i (i)}
				<div class="{bar} h-3.5" style="width: {i === lines - 1 ? 60 : 92 - ((i * 13) % 20)}%"></div>
			{/each}
		</div>
	{:else}
		<div class="{bar} {klass || 'h-4 w-full'}"></div>
	{/if}
</div>
