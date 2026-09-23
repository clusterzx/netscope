<!-- Live status of the scans started from the device page (runs store + final status). -->
<script lang="ts">
	import Badge from '$lib/components/ui/Badge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import ProgressBar from '$lib/components/ui/ProgressBar.svelte';
	import StatusDot from '$lib/components/ui/StatusDot.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { formatDuration } from '$lib/utils/format';
	import { runStatusLabel, runStatusTone } from '$lib/utils/labels';
	import type { StartedScan } from './ScanModal.svelte';

	interface Props {
		scans: StartedScan[];
		class?: string;
	}

	let { scans = $bindable([]), class: klass = '' }: Props = $props();

	const FINAL = new Set(['success', 'failed', 'timeout', 'cancelled']);

	// live state from the runs store (fed by the RunView in the SSE messages); the final
	// status/duration is copied into the scan entry when the run finished
	const rows = $derived(
		scans.map((s) => {
			const run = runs.get(s.runId);
			const status = s.status ?? run?.status ?? 'queued';
			return {
				...s,
				status,
				run,
				error: s.error ?? run?.error,
				durationMs: s.durationMs ?? run?.durationMs
			};
		})
	);
	const done = $derived(rows.every((r) => FINAL.has(r.status)));
</script>

<section
	class="flex flex-col gap-2 rounded-lg border px-3 py-2.5 {done
		? 'border-border bg-surface'
		: 'border-accent/30 bg-accent-soft'} {klass}"
	aria-label="Gestartete Scans"
	aria-live="polite"
>
	<div class="flex items-center gap-2">
		<h2 class="flex-1 text-sm font-medium">
			{done ? 'Scan abgeschlossen' : 'Scan läuft …'}
		</h2>
		{#if done}
			<Button size="xs" variant="ghost" icon="x" label="Ausblenden" onclick={() => (scans = [])} />
		{/if}
	</div>
	<ul class="flex flex-col gap-1.5">
		{#each rows as r (r.runId)}
			<li class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
				<span class="flex min-w-40 items-center gap-2">
					<span aria-hidden="true" class="inline-flex">
						<StatusDot
							status={r.status === 'running' ? 'running' : FINAL.has(r.status) ? r.status : 'queued'}
						/>
					</span>
					<span class="font-medium">{r.name}</span>
				</span>
				<Badge tone={runStatusTone(r.status)}>{runStatusLabel[r.status] ?? r.status}</Badge>
				{#if r.status === 'running'}
					<ProgressBar
						done={r.run?.progress?.done}
						total={r.run?.progress?.total}
						tone="live"
						class="w-40 max-w-full"
						label="Fortschritt {r.name}"
					/>
				{:else if FINAL.has(r.status) && r.durationMs}
					<span class="text-xs text-fg-subtle tabular">{formatDuration(r.durationMs)}</span>
				{/if}
				<a
					href="/plugins/{encodeURIComponent(r.plugin)}/runs/{r.runId}"
					class="mono text-xs text-fg-subtle hover:text-accent hover:underline"
					title="Lauf mit Log anzeigen">Lauf #{r.runId}</a
				>
				{#if r.error}<span class="w-full text-xs text-danger">{r.error}</span>{/if}
			</li>
		{/each}
	</ul>
</section>
