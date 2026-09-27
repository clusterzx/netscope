<!--
	Live progress of a queued/running run (progress bar + done/total or elapsed time).
	<RunProgress run={active} />
-->
<script lang="ts">
	import type { RunView } from '$lib/api';
	import { t } from '$lib/i18n';
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
		<ProgressBar label={t('Wartet auf Ausführung')} class="flex-1" />
		<span class="shrink-0">{t('wartet')}</span>
	{:else}
		<ProgressBar
			{done}
			{total}
			tone="live"
			label={t('Fortschritt {name}', { name: run.pluginName })}
			class="flex-1"
		/>
		<span class="shrink-0 tabular">
			{#if total > 0}
				{formatNumber(done)} / {formatNumber(total)}
			{:else}
				{t('gestartet')} <RelativeTime value={run.startedAt ?? run.createdAt} />
			{/if}
		</span>
	{/if}
</div>
