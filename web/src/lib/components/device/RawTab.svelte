<!--
	"Rohdaten" tab: latest observation per plugin (GET /devices/{id}/observations); pick a
	plugin to browse its recent observations as JSON tree and raw output.
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { ObservationView } from '$lib/api/types';
	import CodeBlock from '$lib/components/ui/CodeBlock.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import JsonView from '$lib/components/ui/JsonView.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import { formatBytes, formatDateTime } from '$lib/utils/format';
	import { LazyData, sourceName } from './util';

	interface Props {
		deviceId: number;
		version: number;
		active: boolean;
	}

	let { deviceId, version, active }: Props = $props();

	const latest = new LazyData<ObservationView[]>();
	const history = new LazyData<ObservationView[]>();
	let plugin = $state('');
	let selectedId = $state<number | null>(null);

	$effect(() => {
		if (!active) return;
		latest.ensure(
			String(version),
			async (signal) =>
				((await api.get('/api/v1/devices/{id}/observations', { path: { id: deviceId }, signal })) ??
					[]) as ObservationView[]
		);
	});
	$effect(() => {
		if (!active || !plugin) return;
		const p = plugin;
		history.ensure(
			`${version}|${p}`,
			async (signal) =>
				((await api.get('/api/v1/devices/{id}/observations', {
					path: { id: deviceId },
					query: { plugin: p, limit: 20 },
					signal
				})) ?? []) as ObservationView[]
		);
	});
	$effect(() => () => {
		latest.abort();
		history.abort();
	});

	const plugins = $derived([...(latest.data ?? [])].sort((a, b) => a.pluginId.localeCompare(b.pluginId)));
	// default: the most recent plugin
	$effect(() => {
		if (!plugin && latest.data?.length) plugin = latest.data[0].pluginId;
	});

	const list = $derived(
		history.data && history.data[0]?.pluginId === plugin
			? history.data
			: (latest.data ?? []).filter((o) => o.pluginId === plugin)
	);
	const selected = $derived(list.find((o) => o.id === selectedId) ?? list[0] ?? null);

	function choose(p: string) {
		plugin = p;
		selectedId = null;
	}

	let view = $state<'tree' | 'raw'>('tree');
</script>

<div class="flex flex-col gap-3">
	<h2 class="text-sm font-semibold">Rohdaten je Plugin</h2>
	{#if latest.error && !latest.data}
		<ErrorState error={latest.error} onretry={() => latest.reload()} />
	{:else if !latest.data}
		<Skeleton rows={5} />
	{:else if !plugins.length}
		<div class="rounded-lg border border-border bg-surface">
			<EmptyState
				icon="file"
				title="Keine Beobachtungen"
				description="Rohdaten werden gemäß Aufbewahrung (Cleanup-Plugin) gespeichert und danach gelöscht."
			/>
		</div>
	{:else}
		<div class="grid grid-cols-1 gap-3 lg:grid-cols-[16rem_1fr]">
			<nav aria-label="Plugins" class="flex flex-col gap-3">
				<ul
					class="relative flex gap-1 overflow-x-auto rounded-lg border border-border bg-surface p-1 lg:flex-col"
				>
					{#each plugins as o (o.pluginId)}
						<li class="shrink-0">
							<button
								type="button"
								class="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-sm {plugin ===
								o.pluginId
									? 'bg-accent-soft font-medium text-accent'
									: 'text-fg-muted hover:bg-surface-2 hover:text-fg'}"
								aria-current={plugin === o.pluginId ? 'true' : undefined}
								onclick={() => choose(o.pluginId)}
							>
								<span class="min-w-0 flex-1 truncate">{sourceName(o.pluginId)}</span>
								<RelativeTime value={o.ts} class="text-xs text-fg-subtle" />
							</button>
						</li>
					{/each}
				</ul>
				{#if list.length > 1 || history.loading}
					<div class="rounded-lg border border-border bg-surface p-1">
						<p class="px-2.5 pt-1 pb-1.5 text-[0.7rem] font-semibold tracking-wider text-fg-subtle uppercase">
							Letzte Beobachtungen
						</p>
						<ul class="relative flex max-h-72 flex-col overflow-auto">
							{#each list as o (o.id)}
								<li>
									<button
										type="button"
										class="flex w-full items-center gap-2 rounded-md px-2.5 py-1 text-left text-xs {selected?.id ===
										o.id
											? 'bg-surface-3 text-fg'
											: 'text-fg-muted hover:bg-surface-2'}"
										aria-current={selected?.id === o.id ? 'true' : undefined}
										onclick={() => (selectedId = o.id)}
									>
										<span class="flex-1 tabular">{formatDateTime(o.ts, true)}</span>
										{#if o.runId}<span class="mono text-fg-subtle">#{o.runId}</span>{/if}
									</button>
								</li>
							{/each}
						</ul>
					</div>
				{/if}
			</nav>

			<section class="min-w-0 rounded-lg border border-border bg-surface shadow-sm" aria-label="Beobachtung">
				{#if selected}
					<header
						class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border px-4 py-2 text-sm"
					>
						<span class="font-semibold">{sourceName(selected.pluginId)}</span>
						<span class="text-fg-muted">{formatDateTime(selected.ts, true)}</span>
						{#if selected.runId}<span class="mono text-xs text-fg-subtle">Lauf #{selected.runId}</span>{/if}
						<span class="mono min-w-0 truncate text-xs text-fg-subtle">Ziel {selected.target}</span>
						<span class="flex-1"></span>
						<div class="flex rounded-md border border-border p-0.5" role="group" aria-label="Darstellung">
							<button
								type="button"
								class="rounded px-2 py-0.5 text-xs {view === 'tree'
									? 'bg-accent-soft font-medium text-accent'
									: 'text-fg-muted hover:text-fg'}"
								aria-pressed={view === 'tree'}
								onclick={() => (view = 'tree')}>Daten (JSON)</button
							>
							<button
								type="button"
								class="rounded px-2 py-0.5 text-xs {view === 'raw'
									? 'bg-accent-soft font-medium text-accent'
									: 'text-fg-muted hover:text-fg'}"
								aria-pressed={view === 'raw'}
								disabled={!selected.raw}
								title={selected.raw ? undefined : 'Keine Rohausgabe gespeichert'}
								onclick={() => (view = 'raw')}
								>Rohausgabe{selected.raw ? ` (${formatBytes(selected.raw.length)})` : ''}</button
							>
						</div>
					</header>
					<div class="p-3">
						{#if view === 'raw' && selected.raw}
							<CodeBlock code={selected.raw} maxHeight="36rem" label="Rohausgabe" />
						{:else}
							<JsonView value={selected.data} openDepth={2} maxHeight="36rem" />
						{/if}
					</div>
				{:else}
					<p class="px-4 py-6 text-sm text-fg-subtle">Keine Beobachtung ausgewählt.</p>
				{/if}
			</section>
		</div>
	{/if}
</div>
