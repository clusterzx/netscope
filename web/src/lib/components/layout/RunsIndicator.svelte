<!-- Running/queued plugin runs with live progress (popover with details and cancel). -->
<script lang="ts">
	import { api, errorMessage } from '$lib/api/client';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Popover from '$lib/components/ui/Popover.svelte';
	import ProgressBar from '$lib/components/ui/ProgressBar.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Spinner from '$lib/components/ui/Spinner.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDuration } from '$lib/utils/format';
	import { runStatusLabel, runStatusTone } from '$lib/utils/labels';

	let open = $state(false);
	let btn: HTMLButtonElement | null = $state(null);
	const running = $derived(runs.active.filter((r) => r.status === 'running'));
	const count = $derived(runs.active.length);
	const overall = $derived.by(() => {
		const withTotal = running.filter((r) => r.progress && r.progress.total > 0);
		if (!withTotal.length) return null;
		const done = withTotal.reduce((a, r) => a + (r.progress?.done ?? 0), 0);
		const total = withTotal.reduce((a, r) => a + (r.progress?.total ?? 0), 0);
		return { done, total };
	});

	async function cancel(id: number) {
		try {
			await api.post('/api/v1/runs/{id}/cancel', { path: { id } });
			toast.info('Lauf wird abgebrochen');
		} catch (e) {
			toast.error(errorMessage(e));
		}
	}
</script>

<button
	bind:this={btn}
	type="button"
	onclick={() => (open = !open)}
	aria-haspopup="dialog"
	aria-expanded={open}
	aria-label={count ? `${count} laufende oder wartende Läufe` : 'Läufe – zurzeit keine aktiv'}
	class="inline-flex h-8 items-center gap-1.5 rounded-md px-2 text-xs font-medium transition-colors hover:bg-surface-3
		{count ? 'text-live' : 'text-fg-subtle'}"
	title={count ? `${count} Läufe aktiv` : 'Keine laufenden Scans'}
>
	{#if running.length}
		<Spinner size={14} />
	{:else}
		<Icon name="radar" size={16} />
	{/if}
	<span class="tabular" aria-hidden="true">{count}</span>
	{#if overall}
		<span class="hidden w-14 md:block" aria-hidden="true">
			<span class="block h-1.5 w-full overflow-hidden rounded-full bg-surface-3">
				<span
					class="block h-full rounded-full bg-live transition-[width] duration-300"
					style="width:{Math.round((overall.done / overall.total) * 100)}%"
				></span>
			</span>
		</span>
	{/if}
</button>

<Popover
	bind:open
	anchor={btn}
	placement="bottom-end"
	label="Laufende Scans"
	class="w-[min(24rem,calc(100vw-1rem))]"
>
	<div class="flex items-center justify-between border-b border-border px-3.5 py-2.5">
		<h2 class="text-sm font-semibold">Läufe</h2>
		<a href="/plugins" class="text-xs text-accent hover:underline" onclick={() => (open = false)}>Plugins</a>
	</div>
	{#if runs.active.length === 0}
		<p class="px-3.5 py-4 text-sm text-fg-subtle">Zurzeit läuft kein Scan.</p>
	{:else}
		<ul class="divide-y divide-border">
			{#each runs.active as r (r.id)}
				<li class="px-3.5 py-2.5">
					<div class="flex items-center gap-2">
						<span class="min-w-0 flex-1 truncate text-sm font-medium">{r.pluginName}</span>
						<Badge tone={runStatusTone(r.status)} dot>{runStatusLabel[r.status] ?? r.status}</Badge>
						{#if r.status === 'running' || r.status === 'queued'}
							<button
								type="button"
								class="rounded p-1 text-fg-subtle hover:bg-surface-3 hover:text-danger"
								aria-label="Lauf {r.id} abbrechen"
								title="Abbrechen"
								onclick={() => cancel(r.id)}
							>
								<Icon name="stop" size={13} />
							</button>
						{/if}
					</div>
					<div class="mt-1.5 flex items-center gap-2 text-xs text-fg-subtle">
						{#if r.status === 'running'}
							<ProgressBar done={r.progress?.done} total={r.progress?.total} tone="live" class="flex-1" />
							<span class="tabular"
								>{#if r.progress?.total}{r.progress.done}/{r.progress.total}{:else}{formatDuration(
										r.durationMs
									)}{/if}</span
							>
						{:else}
							<span>Lauf #{r.id} · eingereiht <RelativeTime value={r.createdAt} /></span>
						{/if}
					</div>
				</li>
			{/each}
		</ul>
	{/if}
	{#if runs.recent.length}
		<div
			class="border-t border-border px-3.5 pt-2 pb-1 text-[0.68rem] font-semibold tracking-wider text-fg-subtle uppercase"
		>
			Zuletzt beendet
		</div>
		<ul class="pb-2">
			{#each runs.recent.slice(0, 5) as r (r.id)}
				<li class="flex items-center gap-2 px-3.5 py-1 text-xs">
					<span class="min-w-0 flex-1 truncate text-fg-muted">{r.pluginName}</span>
					<Badge tone={runStatusTone(r.status)}>{runStatusLabel[r.status] ?? r.status}</Badge>
					<span class="w-16 text-right text-fg-subtle tabular">{formatDuration(r.durationMs)}</span>
				</li>
			{/each}
		</ul>
	{/if}
</Popover>
