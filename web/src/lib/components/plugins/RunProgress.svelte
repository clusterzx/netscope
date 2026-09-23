<!--
	Live progress of a queued/running run (progress bar + done/total or elapsed time).
	<RunProgress run={active} />
-->
<script lang="ts">
	import type { RunView } from '$lib/api';
	import ProgressBar from '$lib/components/ui/ProgressBar.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import { formatNumber } from '$lib/utils/format';

	interface Props {
		run: RunView;
		class?: string;
	}

	let { run, class: klass = '' }: Props = $props();

	const total = $derived(run.progress?.total ?? 0);
	const done = $derived(run.progress?.done ?? 0);
</script>

<div class="flex min-w-0 items-center gap-2 text-xs text-fg-subtle {klass}">
	{#if run.status === 'queued'}
		<ProgressBar label="Wartet auf Ausführung" class="flex-1" />
		<span class="shrink-0">wartet</span>
	{:else}
		<ProgressBar {done} {total} tone="live" label="Fortschritt {run.pluginName}" class="flex-1" />
		<span class="shrink-0 tabular">
			{#if total > 0}
				{formatNumber(done)} / {formatNumber(total)}
			{:else}
				gestartet <RelativeTime value={run.startedAt ?? run.createdAt} />
			{/if}
		</span>
	{/if}
</div>
