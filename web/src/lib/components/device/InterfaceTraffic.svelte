<!--
	Tab "Traffic" of an SNMP device: current rate, utilisation and errors per interface
	(snmp_traffic series if.in_bps, if.out_bps, if.util_pct, if.errors, if.discards with their
	newest sample) joined with the SNMP inventory (alias, speed, status), plus the charts of the
	selected interface with a 24 h / 7 d / 30 d range picker.
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { SeriesResponse } from '$lib/api/types';
	import { Badge, Card, EmptyState, ErrorState, Input, RelativeTime, Skeleton } from '$lib/components/ui';
	import Table, { type Column } from '$lib/components/ui/Table.svelte';
	import TimeSeriesChart from '$lib/components/ui/TimeSeriesChart.svelte';
	import { formatBps, formatNumber, formatPercent } from '$lib/utils/format';
	import type { Tone } from '$lib/utils/labels';
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
	let range = $state<RangeId>(loadPref<RangeId>('device.trafficRange', '24h'));
	const span = $derived(RANGES.find((r) => r.id === range)?.ms ?? RANGES[0].ms);

	/** a newest sample older than this belongs to an interface that is no longer measured */
	const STALE_MS = 3600_000;

	type SnmpIf = {
		index: number;
		name?: string;
		alias?: string;
		type?: number;
		speedMbps?: number;
		adminStatus?: string;
		operStatus?: string;
	};
	type Row = {
		key: string;
		name: string;
		alias: string;
		speedMbps: number | null;
		admin: string;
		oper: string;
		in: number | null;
		out: number | null;
		util: number | null;
		errors: number | null;
		discards: number | null;
		at: string | null;
	};

	/** series key per ifIndex, the same rule as ifKeys in internal/plugins/snmp/traffic.go */
	function ifKeys(ifs: SnmpIf[]): Map<string, SnmpIf> {
		const seen = new Map<string, number>();
		for (const i of ifs) seen.set(i.name ?? '', (seen.get(i.name ?? '') ?? 0) + 1);
		const out = new Map<string, SnmpIf>();
		for (const i of ifs) {
			const n = i.name ?? '';
			out.set(n === '' || (seen.get(n) ?? 0) > 1 ? `${n}#${i.index}` : n, i);
		}
		return out;
	}

	const data = new LazyData<{ rows: Row[]; at: string | null }>();
	$effect(() => {
		if (!active) return;
		data.ensure(String(version), async (signal) => {
			const [list, inv] = await Promise.all([
				api.get('/api/v1/devices/{id}/timeseries', { path: { id: deviceId }, signal }),
				api.get('/api/v1/devices/{id}/inventory', { path: { id: deviceId }, signal })
			]);
			const snmp = (inv as Record<string, { data?: { interfaces?: SnmpIf[] } }> | null)?.snmp;
			const ifs = ifKeys(snmp?.data?.interfaces ?? []);
			const now = Date.now();
			const last = new Map<string, { v: number; t: string }>();
			for (const s of list.series ?? []) {
				if (!s.metric.startsWith('if.') || !s.last) continue;
				if (now - new Date(s.last.t).getTime() > STALE_MS) continue;
				last.set(`${s.metric}|${s.key ?? ''}`, { v: s.last.avg, t: s.last.t });
			}
			const val = (metric: string, key: string) => last.get(`${metric}|${key}`)?.v ?? null;
			const rows = (list.series ?? [])
				.filter((s) => s.metric === 'if.in_bps')
				.map((s): Row => {
					const key = s.key ?? '';
					const i = ifs.get(key);
					return {
						key,
						name: i?.name || key,
						alias: i?.alias ?? '',
						speedMbps: i?.speedMbps || null,
						admin: i?.adminStatus ?? '',
						oper: i?.operStatus ?? '',
						in: val('if.in_bps', key),
						out: val('if.out_bps', key),
						util: val('if.util_pct', key),
						errors: val('if.errors', key),
						discards: val('if.discards', key),
						at: last.get(`if.in_bps|${key}`)?.t ?? null
					};
				});
			const at =
				rows
					.map((r) => r.at ?? '')
					.sort()
					.at(-1) || null;
			return { rows, at };
		});
	});

	let q = $state('');
	let sort = $state(loadPref('device.trafficSort', 'name'));
	const collator = new Intl.Collator('de', { numeric: true, sensitivity: 'base' });
	const rows = $derived.by(() => {
		const needle = q.trim().toLowerCase();
		const list = (data.data?.rows ?? []).filter(
			(r) => !needle || r.name.toLowerCase().includes(needle) || r.alias.toLowerCase().includes(needle)
		);
		const desc = sort.startsWith('-');
		const field = sort.replace(/^-/, '') as keyof Row;
		return list.sort((a, b) => {
			const c =
				field === 'name'
					? collator.compare(a.name, b.name)
					: ((a[field] as number | null) ?? -1) - ((b[field] as number | null) ?? -1) ||
						collator.compare(a.name, b.name);
			return desc ? -c : c;
		});
	});
	function setSort(s: string) {
		sort = s;
		savePref('device.trafficSort', s);
	}

	// the selected interface: the busiest one until the user picks another
	let picked = $state<string | null>(null);
	const selected = $derived.by(() => {
		const all = data.data?.rows ?? [];
		return (
			all.find((r) => r.key === picked) ??
			[...all].sort((a, b) => (b.util ?? -1) - (a.util ?? -1) || (b.in ?? 0) - (a.in ?? 0))[0] ??
			null
		);
	});

	type Points = SeriesResponse['points'];
	type Charts = { in: Points; out: Points; util: Points; errors: Points; discards: Points };
	const charts = new LazyData<Charts>();
	let win = $state({ from: 0, to: 0 });
	$effect(() => {
		if (!active || !selected) return;
		const key = selected.key;
		const ms = span;
		charts.ensure(`${version}|${range}|${key}`, async (signal) => {
			const to = Date.now();
			const from = to - ms;
			const get = async (metric: string): Promise<Points> => {
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
			const [i, o, u, e, d] = await Promise.all(
				['if.in_bps', 'if.out_bps', 'if.util_pct', 'if.errors', 'if.discards'].map(get)
			);
			win = { from, to };
			return { in: i, out: o, util: u, errors: e, discards: d };
		});
	});
	$effect(() => () => {
		data.abort();
		charts.abort();
	});

	function setRange(r: RangeId) {
		range = r;
		savePref('device.trafficRange', r);
	}

	function speedText(mbps: number | null): string {
		if (!mbps) return '–';
		return mbps >= 1000 ? `${formatNumber(mbps / 1000, mbps % 1000 ? 1 : 0)} Gbit/s` : `${mbps} Mbit/s`;
	}
	function utilTone(v: number | null): string {
		if (v === null) return 'bg-surface-3';
		return v >= 90 ? 'bg-danger' : v >= 70 ? 'bg-warn' : 'bg-accent';
	}
	function statusOf(r: Row): { label: string; tone: Tone } {
		if (r.admin === 'down') return { label: 'abgeschaltet', tone: 'neutral' };
		if (r.oper === 'up') return { label: 'verbunden', tone: 'ok' };
		if (r.oper === 'down' || r.oper === 'lowerLayerDown') return { label: 'kein Link', tone: 'warn' };
		return { label: r.oper || 'unbekannt', tone: 'neutral' };
	}
	const hasErrors = (p: Points) => p.some((x) => x.max > 0);

	const columns = $derived<Column<Row>[]>([
		{ key: 'name', label: 'Interface', sortable: true, cell: nameCell },
		{ key: 'status', label: 'Status', hideBelow: 'sm', cell: statusCell },
		{
			key: 'speedMbps',
			label: 'Speed',
			align: 'right',
			hideBelow: 'lg',
			sortable: true,
			sortDesc: true,
			value: (r) => speedText(r.speedMbps)
		},
		{
			key: 'in',
			label: 'Eingehend',
			align: 'right',
			sortable: true,
			sortDesc: true,
			value: (r) => formatBps(r.in)
		},
		{
			key: 'out',
			label: 'Ausgehend',
			align: 'right',
			sortable: true,
			sortDesc: true,
			value: (r) => formatBps(r.out)
		},
		{ key: 'util', label: 'Auslastung', sortable: true, sortDesc: true, width: '9rem', cell: utilCell },
		{
			key: 'errors',
			label: 'Fehler/min',
			align: 'right',
			hideBelow: 'md',
			sortable: true,
			sortDesc: true,
			title: 'Fehler und verworfene Pakete je Minute (Summe beider Richtungen)',
			cell: errorsCell
		}
	]);
</script>

{#snippet nameCell(r: Row)}
	<button
		type="button"
		class="mono rounded-sm text-left text-xs font-medium hover:text-accent focus-visible:outline-2 focus-visible:outline-accent"
		aria-pressed={r.key === selected?.key}
		title="Verlauf anzeigen"
		onclick={() => (picked = r.key)}>{r.name}</button
	>
	{#if r.alias}<span class="block text-xs text-fg-subtle">{r.alias}</span>{/if}
{/snippet}
{#snippet statusCell(r: Row)}
	{@const s = statusOf(r)}
	<Badge tone={s.tone} dot>{s.label}</Badge>
{/snippet}
{#snippet utilCell(r: Row)}
	<div class="flex items-center gap-2">
		<div class="h-1.5 w-16 overflow-hidden rounded-full bg-surface-3" aria-hidden="true">
			<div class="h-full rounded-full {utilTone(r.util)}" style="width:{Math.min(100, r.util ?? 0)}%"></div>
		</div>
		<span class="tabular text-xs {r.util !== null && r.util >= 90 ? 'font-medium text-danger' : ''}"
			>{r.util === null ? '–' : formatPercent(r.util, r.util < 10 ? 1 : 0)}</span
		>
	</div>
{/snippet}
{#snippet errorsCell(r: Row)}
	<span class="tabular {r.errors ? 'text-warn' : 'text-fg-subtle'}">{formatNumber(r.errors, 1)}</span>
	{#if r.discards}<span class="block text-xs text-fg-subtle">{formatNumber(r.discards, 1)} verworfen</span
		>{/if}
{/snippet}

<div class="flex flex-col gap-4">
	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton rows={6} />
	{:else if !data.data.rows.length}
		<div class="rounded-lg border border-border bg-surface">
			<EmptyState
				icon="network"
				title="Noch keine Interface-Messwerte"
				description="Das Plugin „SNMP-Traffic“ misst die Interfaces alle 5 Minuten; Raten entstehen ab der zweiten Abfrage."
			/>
		</div>
	{:else}
		<Card padding="none">
			{#snippet header()}
				<div class="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-2">
					<h3 class="text-sm font-semibold">Interfaces</h3>
					<span class="text-xs text-fg-subtle">
						{data.data?.rows.length} gemessen
						{#if data.data?.at}· Stand <RelativeTime value={data.data.at} />{/if}
					</span>
					<Input
						icon="search"
						placeholder="Interface oder Beschreibung …"
						bind:value={q}
						size="sm"
						class="ml-auto w-full sm:w-64"
					/>
				</div>
			{/snippet}
			<Table
				{columns}
				{rows}
				key={(r) => r.key}
				{sort}
				onsort={setSort}
				dense
				maxHeight="60vh"
				class="rounded-none border-0"
				caption="Interfaces"
				onrowclick={(r) => (picked = r.key)}
				rowClass={(r) =>
					`${r.key === selected?.key ? 'bg-accent-soft/60' : ''} ${r.in === null ? 'opacity-60' : ''}`}
			>
				{#snippet empty()}<EmptyState compact title="Kein Interface passt zur Suche" />{/snippet}
			</Table>
		</Card>

		{#if selected}
			<Card padding="md">
				{#snippet header()}
					<div class="flex min-w-0 flex-1 flex-wrap items-center gap-x-2">
						<h3 class="text-sm font-semibold">
							Verlauf <span class="mono">{selected.name}</span>
						</h3>
						{#if selected.alias}<span class="text-xs text-fg-subtle">{selected.alias}</span>{/if}
					</div>
				{/snippet}
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
				{#if charts.error && !charts.data}
					<ErrorState error={charts.error} compact onretry={() => charts.reload()} />
				{:else if !charts.data}
					<Skeleton class="h-40 w-full" />
				{:else}
					{@const c = charts.data}
					<div
						class="grid grid-cols-1 gap-4 lg:grid-cols-2 {charts.loading
							? 'opacity-60 transition-opacity'
							: ''}"
					>
						<div>
							<h4 class="mb-1 text-xs font-medium text-fg-muted">Eingehend</h4>
							<TimeSeriesChart
								points={c.in}
								label="{selected.name} eingehend"
								format={formatBps}
								height={150}
								color="var(--chart-4)"
								from={win.from}
								to={win.to}
							/>
						</div>
						<div>
							<h4 class="mb-1 text-xs font-medium text-fg-muted">Ausgehend</h4>
							<TimeSeriesChart
								points={c.out}
								label="{selected.name} ausgehend"
								format={formatBps}
								height={150}
								color="var(--chart-1)"
								from={win.from}
								to={win.to}
							/>
						</div>
						<div>
							<h4 class="mb-1 text-xs font-medium text-fg-muted">
								Auslastung <span class="font-normal text-fg-subtle">(stärkere Richtung)</span>
							</h4>
							<TimeSeriesChart
								points={c.util}
								label="{selected.name} Auslastung"
								format={(v) => formatPercent(v, 0)}
								height={120}
								yMax={100}
								color="var(--chart-2)"
								emptyText={selected.speedMbps ? undefined : 'Keine Portgeschwindigkeit bekannt'}
								from={win.from}
								to={win.to}
							/>
						</div>
						<div>
							<h4 class="mb-1 text-xs font-medium text-fg-muted">Fehler je Minute</h4>
							<TimeSeriesChart
								points={c.errors}
								label="{selected.name} Fehler je Minute"
								format={(v) => formatNumber(v, 1)}
								height={120}
								color="var(--chart-3)"
								from={win.from}
								to={win.to}
							/>
						</div>
						{#if hasErrors(c.discards)}
							<div>
								<h4 class="mb-1 text-xs font-medium text-fg-muted">Verworfene Pakete je Minute</h4>
								<TimeSeriesChart
									points={c.discards}
									label="{selected.name} verworfen je Minute"
									format={(v) => formatNumber(v, 1)}
									height={120}
									color="var(--chart-3)"
									from={win.from}
									to={win.to}
								/>
							</div>
						{/if}
					</div>
				{/if}
			</Card>
		{/if}
	{/if}
</div>
