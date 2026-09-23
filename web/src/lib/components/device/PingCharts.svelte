<!--
	Ping latency and packet loss of a device (GET /devices/{id}/timeseries, metrics
	icmp.rtt_ms and icmp.loss_pct) with a 24h / 7d / 30d range picker.
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { SeriesResponse } from '$lib/api/types';
	import Card from '$lib/components/ui/Card.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import TimeSeriesChart from '$lib/components/ui/TimeSeriesChart.svelte';
	import { formatMs, formatPercent } from '$lib/utils/format';
	import { loadPref, savePref } from '$lib/utils/url';
	import { LazyData } from './util';

	interface Props {
		deviceId: number;
		version: number;
		active: boolean;
	}

	let { deviceId, version, active }: Props = $props();

	const RANGES = [
		{ id: '24h', label: '24 h', ms: 24 * 3600_000 },
		{ id: '7d', label: '7 Tage', ms: 7 * 86400_000 },
		{ id: '30d', label: '30 Tage', ms: 30 * 86400_000 }
	] as const;
	type RangeId = (typeof RANGES)[number]['id'];

	let range = $state<RangeId>(loadPref<RangeId>('device.pingRange', '24h'));
	const span = $derived(RANGES.find((r) => r.id === range)?.ms ?? RANGES[0].ms);
	let win = $state({ from: 0, to: 0 });

	type Pair = { rtt: SeriesResponse; loss: SeriesResponse };
	const data = new LazyData<Pair>();

	$effect(() => {
		if (!active) return;
		const key = `${version}|${range}`;
		const ms = span;
		data.ensure(key, async (signal) => {
			const to = Date.now();
			const from = to - ms;
			const query = { from: new Date(from).toISOString(), to: new Date(to).toISOString(), points: 300 };
			const [rtt, loss] = await Promise.all([
				api.get('/api/v1/devices/{id}/timeseries', {
					path: { id: deviceId },
					query: { ...query, metric: 'icmp.rtt_ms' },
					signal
				}),
				api.get('/api/v1/devices/{id}/timeseries', {
					path: { id: deviceId },
					query: { ...query, metric: 'icmp.loss_pct' },
					signal
				})
			]);
			win = { from, to };
			return { rtt, loss };
		});
	});
	$effect(() => () => data.abort());

	function setRange(r: RangeId) {
		range = r;
		savePref('device.pingRange', r);
	}

	const rttPts = $derived(data.data?.rtt.points ?? []);
	const lossPts = $derived(data.data?.loss.points ?? []);
	const hasSeries = $derived(
		!!data.data && (data.data.rtt.series ?? []).some((s) => s.metric === 'icmp.rtt_ms')
	);
	const stats = $derived.by(() => {
		if (!rttPts.length) return null;
		const n = rttPts.reduce((a, p) => a + (p.count || 1), 0);
		const avg = rttPts.reduce((a, p) => a + p.avg * (p.count || 1), 0) / n;
		const max = Math.max(...rttPts.map((p) => p.max));
		const ln = lossPts.reduce((a, p) => a + (p.count || 1), 0);
		const loss = ln ? lossPts.reduce((a, p) => a + p.avg * (p.count || 1), 0) / ln : null;
		return { avg, max, loss, last: rttPts[rttPts.length - 1].avg };
	});
</script>

<Card title="Erreichbarkeit (Ping)" icon="activity" padding="md">
	{#snippet actions()}
		<div class="flex rounded-md border border-border p-0.5" role="group" aria-label="Zeitraum">
			{#each RANGES as r (r.id)}
				<button
					type="button"
					class="rounded px-2 py-0.5 text-xs {range === r.id
						? 'bg-accent-soft font-medium text-accent'
						: 'text-fg-muted hover:text-fg'}"
					aria-pressed={range === r.id}
					onclick={() => setRange(r.id)}>{r.label}</button
				>
			{/each}
		</div>
	{/snippet}
	{#if data.error && !data.data}
		<ErrorState error={data.error} compact onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton class="h-40 w-full" />
	{:else if !hasSeries}
		<EmptyState
			compact
			icon="activity"
			title="Keine Ping-Messwerte"
			description="Der ICMP-Scanner hat dieses Gerät noch nicht gemessen."
		/>
	{:else}
		{#if stats}
			<dl class="mb-3 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
				<div>
					<dt class="text-xs text-fg-subtle">Zuletzt</dt>
					<dd class="font-medium tabular">{formatMs(stats.last)}</dd>
				</div>
				<div>
					<dt class="text-xs text-fg-subtle">Ø Latenz</dt>
					<dd class="font-medium tabular">{formatMs(stats.avg)}</dd>
				</div>
				<div>
					<dt class="text-xs text-fg-subtle">Maximum</dt>
					<dd class="font-medium tabular">{formatMs(stats.max)}</dd>
				</div>
				<div>
					<dt class="text-xs text-fg-subtle">Ø Paketverlust</dt>
					<dd class="font-medium tabular {stats.loss && stats.loss > 0 ? 'text-warn' : ''}">
						{formatPercent(stats.loss, 1)}
					</dd>
				</div>
			</dl>
		{/if}
		<div class="flex flex-col gap-4 {data.loading ? 'opacity-60 transition-opacity' : ''}">
			<div>
				<h3 class="mb-1 text-xs font-medium text-fg-muted">Latenz (Min / Ø / Max)</h3>
				<TimeSeriesChart
					points={rttPts}
					label="Ping-Latenz"
					format={formatMs}
					height={170}
					from={win.from}
					to={win.to}
				/>
			</div>
			<div>
				<h3 class="mb-1 text-xs font-medium text-fg-muted">Paketverlust</h3>
				<TimeSeriesChart
					points={lossPts}
					label="Paketverlust"
					format={(v) => formatPercent(v, 0)}
					height={110}
					band={false}
					yMax={100}
					color="var(--chart-2)"
					from={win.from}
					to={win.to}
				/>
			</div>
		</div>
	{/if}
</Card>
