<!--
	Tab "Auslastung" of a host with NetScope agent: agent status and the time series host.cpu,
	host.mem, host.load1, host.disk (per mount) and host.net_rx / host.net_tx (per interface)
	with a 24 h / 7 d / 30 d range picker.
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { Agent, SeriesResponse } from '$lib/api/types';
	import {
		Badge,
		Button,
		Card,
		EmptyState,
		ErrorState,
		RelativeTime,
		Skeleton,
		StatusDot
	} from '$lib/components/ui';
	import TimeSeriesChart from '$lib/components/ui/TimeSeriesChart.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatBytes, formatNumber, formatPercent } from '$lib/utils/format';
	import { loadPref, savePref } from '$lib/utils/url';
	import { LazyData } from './util';

	interface Props {
		deviceId: number;
		agent: Agent;
		version: number;
		active: boolean;
	}

	let { deviceId, agent, version, active }: Props = $props();

	const RANGES = [
		{ id: '24h', label: '24 h', ms: 24 * 3600_000 },
		{ id: '7d', label: '7 Tage', ms: 7 * 86400_000 },
		{ id: '30d', label: '30 Tage', ms: 30 * 86400_000 }
	] as const;
	type RangeId = (typeof RANGES)[number]['id'];
	let range = $state<RangeId>(loadPref<RangeId>('device.usageRange', '24h'));
	const span = $derived(RANGES.find((r) => r.id === range)?.ms ?? RANGES[0].ms);
	let win = $state({ from: 0, to: 0 });

	type Points = SeriesResponse['points'];
	type Usage = {
		cpu: Points;
		mem: Points;
		load: Points;
		disks: [string, Points][];
		net: [string, Points, Points][];
	};
	const data = new LazyData<Usage>();

	$effect(() => {
		if (!active) return;
		const ms = span;
		data.ensure(`${version}|${range}`, async (signal) => {
			const to = Date.now();
			const from = to - ms;
			const list = await api.get('/api/v1/devices/{id}/timeseries', { path: { id: deviceId }, signal });
			const series = (list.series ?? []).filter((s) => s.metric.startsWith('host.'));
			const get = async (metric: string, key = ''): Promise<Points> => {
				if (!series.some((s) => s.metric === metric && (s.key ?? '') === key)) return [];
				const r = await api.get('/api/v1/devices/{id}/timeseries', {
					path: { id: deviceId },
					query: {
						metric,
						key,
						from: new Date(from).toISOString(),
						to: new Date(to).toISOString(),
						points: 300
					},
					signal
				});
				return r.points ?? [];
			};
			const keys = (metric: string) =>
				series
					.filter((s) => s.metric === metric)
					.map((s) => s.key ?? '')
					.sort();
			const [cpu, mem, load, disks, net] = await Promise.all([
				get('host.cpu'),
				get('host.mem'),
				get('host.load1'),
				Promise.all(keys('host.disk').map(async (k) => [k, await get('host.disk', k)] as [string, Points])),
				Promise.all(
					keys('host.net_rx').map(
						async (k) =>
							[k, await get('host.net_rx', k), await get('host.net_tx', k)] as [string, Points, Points]
					)
				)
			]);
			win = { from, to };
			// interfaces without traffic in the range only add noise
			const busy = net.filter(([, rx, tx]) => [...rx, ...tx].some((p) => p.max > 0)).slice(0, 4);
			return { cpu, mem, load, disks: disks.filter(([, p]) => p.length), net: busy };
		});
	});
	$effect(() => () => data.abort());

	function setRange(r: RangeId) {
		range = r;
		savePref('device.usageRange', r);
	}

	const last = (p: Points) => (p.length ? p[p.length - 1].avg : null);
	const empty = $derived(
		!!data.data && !data.data.cpu.length && !data.data.mem.length && !data.data.disks.length
	);
	const rate = (v: number) => formatBytes(v) + '/s';

	let asking = $state(false);
	async function refresh() {
		asking = true;
		try {
			await api.post('/api/v1/agents/{id}/refresh', { path: { id: agent.id } });
			toast.success('Inventar angefordert – kommt in wenigen Sekunden');
		} catch (e) {
			toast.error(e);
		} finally {
			asking = false;
		}
	}
</script>

<div class="flex flex-col gap-4">
	<Card title="NetScope-Agent" icon="cpu" padding="md">
		{#snippet actions()}
			{#if auth.can('devices.scan')}
				<Button size="sm" variant="ghost" icon="refresh" loading={asking} onclick={refresh}
					>Inventar jetzt anfordern</Button
				>
			{/if}
		{/snippet}
		<dl class="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
			<div>
				<dt class="text-xs text-fg-subtle">Status</dt>
				<dd class="flex items-center gap-1.5 font-medium">
					<StatusDot status={agent.online ? 'online' : 'offline'} pulse={false} />
					{agent.online ? 'meldet sich' : 'keine Meldung'}
				</dd>
			</div>
			<div>
				<dt class="text-xs text-fg-subtle">Letzter Kontakt</dt>
				<dd>
					{#if agent.lastSeenAt}<RelativeTime value={agent.lastSeenAt} />{:else}–{/if}
				</dd>
			</div>
			<div>
				<dt class="text-xs text-fg-subtle">Letztes Inventar</dt>
				<dd>
					{#if agent.lastInventoryAt}<RelativeTime value={agent.lastInventoryAt} />{:else}–{/if}
				</dd>
			</div>
			<div>
				<dt class="text-xs text-fg-subtle">Version</dt>
				<dd class="mono text-xs">
					{agent.version || '–'}
					{#if agent.outdated}<Badge tone="info">Update folgt</Badge>{/if}
				</dd>
			</div>
		</dl>
		{#if agent.lastError}<p class="mt-2 text-xs text-warn">{agent.lastError}</p>{/if}
	</Card>

	<Card title="Auslastung" icon="activity" padding="md">
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
		{:else if empty}
			<EmptyState
				compact
				icon="activity"
				title="Noch keine Messwerte"
				description="Der Agent sendet seine Messungen gesammelt, standardmäßig alle 5 Minuten."
			/>
		{:else}
			{@const u = data.data}
			<dl class="mb-4 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
				<div>
					<dt class="text-xs text-fg-subtle">CPU</dt>
					<dd class="font-medium tabular">{formatPercent(last(u.cpu), 0)}</dd>
				</div>
				<div>
					<dt class="text-xs text-fg-subtle">Arbeitsspeicher</dt>
					<dd class="font-medium tabular">{formatPercent(last(u.mem), 0)}</dd>
				</div>
				{#if u.load.length}
					<!-- Windows has no load average -->
					<div>
						<dt class="text-xs text-fg-subtle">Last (1 min)</dt>
						<dd class="font-medium tabular">
							{last(u.load) === null ? '–' : formatNumber(last(u.load) ?? 0, 2)}
						</dd>
					</div>
				{/if}
				<div>
					<dt class="text-xs text-fg-subtle">Vollstes Dateisystem</dt>
					<dd class="font-medium tabular">
						{#if u.disks.length}
							{@const top = u.disks
								.map(([k, p]) => [k, last(p) ?? 0] as const)
								.sort((a, b) => b[1] - a[1])[0]}
							<span class={top[1] >= 90 ? 'text-danger' : top[1] >= 80 ? 'text-warn' : ''}
								>{formatPercent(top[1], 0)}</span
							>
							<span class="mono text-xs text-fg-subtle">{top[0]}</span>
						{:else}–{/if}
					</dd>
				</div>
			</dl>
			<div
				class="grid grid-cols-1 gap-4 lg:grid-cols-2 {data.loading ? 'opacity-60 transition-opacity' : ''}"
			>
				<div>
					<h3 class="mb-1 text-xs font-medium text-fg-muted">CPU</h3>
					<TimeSeriesChart
						points={u.cpu}
						label="CPU"
						format={(v) => formatPercent(v, 0)}
						height={150}
						yMax={100}
						from={win.from}
						to={win.to}
					/>
				</div>
				<div>
					<h3 class="mb-1 text-xs font-medium text-fg-muted">Arbeitsspeicher</h3>
					<TimeSeriesChart
						points={u.mem}
						label="Arbeitsspeicher"
						format={(v) => formatPercent(v, 0)}
						height={150}
						yMax={100}
						color="var(--chart-2)"
						from={win.from}
						to={win.to}
					/>
				</div>
				{#each u.disks as [mount, pts] (mount)}
					<div>
						<h3 class="mb-1 text-xs font-medium text-fg-muted">
							Dateisystem <span class="mono">{mount}</span>
						</h3>
						<TimeSeriesChart
							points={pts}
							label="Belegung {mount}"
							format={(v) => formatPercent(v, 0)}
							height={120}
							yMax={100}
							band={false}
							color="var(--chart-3)"
							from={win.from}
							to={win.to}
						/>
					</div>
				{/each}
				{#each u.net as [iface, rx, tx] (iface)}
					<div>
						<h3 class="mb-1 text-xs font-medium text-fg-muted">
							Netz <span class="mono">{iface}</span> – empfangen
						</h3>
						<TimeSeriesChart
							points={rx}
							label="{iface} empfangen"
							format={rate}
							height={120}
							color="var(--chart-4)"
							from={win.from}
							to={win.to}
						/>
					</div>
					<div>
						<h3 class="mb-1 text-xs font-medium text-fg-muted">
							Netz <span class="mono">{iface}</span> – gesendet
						</h3>
						<TimeSeriesChart
							points={tx}
							label="{iface} gesendet"
							format={rate}
							height={120}
							color="var(--chart-1)"
							from={win.from}
							to={win.to}
						/>
					</div>
				{/each}
				{#if u.load.length}
					<div>
						<h3 class="mb-1 text-xs font-medium text-fg-muted">Systemlast (1 min)</h3>
						<TimeSeriesChart
							points={u.load}
							label="Systemlast"
							format={(v) => formatNumber(v, 2)}
							height={120}
							band={false}
							from={win.from}
							to={win.to}
						/>
					</div>
				{/if}
			</div>
		{/if}
	</Card>
</div>
