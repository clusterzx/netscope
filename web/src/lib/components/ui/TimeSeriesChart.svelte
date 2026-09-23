<!--
	Time series line chart (hand-written SVG): average line with min–max band, hairline
	grid, crosshair tooltip (mouse, touch and ←/→ keys), gaps for missing data and a table view.

	<TimeSeriesChart points={res.points} label="Ping-Latenz" format={formatMs} />
	points: { t: RFC3339, min, avg, max, count? }[] (the API's SeriesPoint)
	Props: height (px, default 180), band (default true), yMin (default 0; null = auto),
	       color (CSS colour, default var(--chart-1)), from/to (ms, x domain; default data range),
	       emptyText.
-->
<script lang="ts">
	import type { SeriesPoint } from '$lib/api/types';
	import { niceTicks, splitGaps, timeTickLabel, timeTicks } from '$lib/utils/chart';
	import { formatDateTime, formatNumber } from '$lib/utils/format';

	interface Props {
		points: SeriesPoint[];
		label: string;
		format?: (v: number) => string;
		height?: number;
		band?: boolean;
		yMin?: number | null;
		yMax?: number | null;
		color?: string;
		from?: number;
		to?: number;
		emptyText?: string;
		class?: string;
	}

	let {
		points,
		label,
		format = (v: number) => formatNumber(v, 2),
		height = 180,
		band = true,
		yMin = 0,
		yMax = null,
		color = 'var(--chart-1)',
		from,
		to,
		emptyText = 'Keine Messwerte im Zeitraum',
		class: klass = ''
	}: Props = $props();

	let width = $state(0);
	let hover = $state<number | null>(null);
	let showTable = $state(false);
	const uid = $props.id();

	const m = { top: 10, right: 12, bottom: 22, left: 48 };
	const pts = $derived(
		(points ?? [])
			.map((p) => ({ ...p, ms: new Date(p.t).getTime() }))
			.filter((p) => isFinite(p.ms))
			.sort((a, b) => a.ms - b.ms)
	);
	const x0 = $derived(from ?? (pts.length ? pts[0].ms : 0));
	const x1 = $derived(to ?? (pts.length ? pts[pts.length - 1].ms : 1));
	const yTicks = $derived.by(() => {
		if (!pts.length) return [0, 1];
		let lo = Math.min(...pts.map((p) => (band ? p.min : p.avg)));
		let hi = Math.max(...pts.map((p) => (band ? p.max : p.avg)));
		if (yMin !== null && yMin !== undefined) lo = Math.min(yMin, lo);
		if (yMax !== null && yMax !== undefined) hi = Math.max(yMax, hi);
		return niceTicks(lo, hi, height < 120 ? 2 : 4);
	});
	const y0 = $derived(yTicks[0]);
	const y1 = $derived(yTicks[yTicks.length - 1]);
	const iw = $derived(Math.max(10, width - m.left - m.right));
	const ih = $derived(Math.max(10, height - m.top - m.bottom));
	const sx = (t: number) => m.left + (x1 === x0 ? iw / 2 : ((t - x0) / (x1 - x0)) * iw);
	const sy = (v: number) => m.top + ih - (y1 === y0 ? ih / 2 : ((v - y0) / (y1 - y0)) * ih);

	const runs = $derived(splitGaps(pts, (p) => p.ms));
	const avgPaths = $derived(
		runs.map((r) =>
			r.map((p, i) => `${i ? 'L' : 'M'}${sx(p.ms).toFixed(1)},${sy(p.avg).toFixed(1)}`).join('')
		)
	);
	const bandPaths = $derived(
		band
			? runs.map((r) => {
					const top = r
						.map((p, i) => `${i ? 'L' : 'M'}${sx(p.ms).toFixed(1)},${sy(p.max).toFixed(1)}`)
						.join('');
					const bottom = [...r]
						.reverse()
						.map((p) => `L${sx(p.ms).toFixed(1)},${sy(p.min).toFixed(1)}`)
						.join('');
					return top + bottom + 'Z';
				})
			: []
	);
	const xTicks = $derived(timeTicks(x0, x1, iw));
	const hp = $derived(hover !== null ? pts[hover] : null);

	function nearest(clientX: number, rect: DOMRect) {
		if (!pts.length) return;
		const t = x0 + ((clientX - rect.left - m.left) / iw) * (x1 - x0);
		let best = 0;
		let bd = Infinity;
		for (let i = 0; i < pts.length; i++) {
			const d = Math.abs(pts[i].ms - t);
			if (d < bd) {
				bd = d;
				best = i;
			}
		}
		hover = best;
	}

	function onMove(e: PointerEvent) {
		nearest(e.clientX, (e.currentTarget as SVGElement).getBoundingClientRect());
	}

	function onKey(e: KeyboardEvent) {
		if (!pts.length) return;
		if (e.key === 'ArrowRight') hover = Math.min(pts.length - 1, (hover ?? -1) + 1);
		else if (e.key === 'ArrowLeft') hover = Math.max(0, (hover ?? pts.length) - 1);
		else if (e.key === 'Escape') hover = null;
		else return;
		e.preventDefault();
	}

	const tipLeft = $derived(hp ? Math.min(Math.max(sx(hp.ms) + 10, 4), Math.max(4, width - 170)) : 0);
	const hasBand = $derived(band && pts.some((p) => p.min !== p.max));
</script>

<div class="relative min-w-0 {klass}" bind:clientWidth={width}>
	{#if pts.length === 0}
		<div
			class="flex items-center justify-center rounded-md border border-dashed border-border text-sm text-fg-subtle"
			style="height:{height}px"
		>
			{emptyText}
		</div>
	{:else}
		<!-- keyboard exploration (←/→) of the data points; the same data is available as a table -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
		<svg
			{width}
			{height}
			role="img"
			aria-label="{label}: {pts.length} Messwerte von {formatDateTime(x0)} bis {formatDateTime(x1)}"
			tabindex="0"
			class="block touch-pan-y focus-visible:outline-2 focus-visible:outline-focus"
			onpointermove={onMove}
			onpointerdown={onMove}
			onpointerleave={() => (hover = null)}
			onkeydown={onKey}
			onblur={() => (hover = null)}
		>
			<!-- grid + y axis -->
			{#each yTicks as v (v)}
				<line
					x1={m.left}
					x2={m.left + iw}
					y1={sy(v)}
					y2={sy(v)}
					stroke="var(--chart-grid)"
					stroke-width="1"
				/>
				<text
					x={m.left - 6}
					y={sy(v)}
					dy="0.32em"
					text-anchor="end"
					class="fill-fg-subtle text-[10px] tabular"
				>
					{format(v)}
				</text>
			{/each}
			<!-- x axis -->
			{#each xTicks as t (t)}
				<text x={sx(t)} y={height - 6} text-anchor="middle" class="fill-fg-subtle text-[10px] tabular">
					{timeTickLabel(t, x1 - x0)}
				</text>
			{/each}
			<!-- data -->
			{#each bandPaths as d, i (i)}
				<path {d} fill={color} fill-opacity="0.14" stroke="none" />
			{/each}
			{#each avgPaths as d, i (i)}
				<path
					{d}
					fill="none"
					stroke={color}
					stroke-width="2"
					stroke-linejoin="round"
					stroke-linecap="round"
				/>
			{/each}
			{#each runs as r, i (i)}
				{#if r.length === 1}
					<circle cx={sx(r[0].ms)} cy={sy(r[0].avg)} r="3" fill={color} />
				{/if}
			{/each}
			<!-- crosshair -->
			{#if hp}
				<line
					x1={sx(hp.ms)}
					x2={sx(hp.ms)}
					y1={m.top}
					y2={m.top + ih}
					stroke="var(--fg-subtle)"
					stroke-width="1"
				/>
				<circle cx={sx(hp.ms)} cy={sy(hp.avg)} r="4" fill={color} stroke="var(--surface)" stroke-width="2" />
			{/if}
		</svg>
		{#if hp}
			<div
				class="pointer-events-none absolute top-1 z-10 w-40 rounded-md border border-border bg-surface px-2.5 py-1.5 text-xs shadow-md"
				style="left:{tipLeft}px"
				role="status"
			>
				<div class="text-fg-subtle">{formatDateTime(hp.ms)}</div>
				<div class="mt-0.5 flex items-center gap-1.5">
					<span class="inline-block h-0.5 w-3 rounded" style="background:{color}"></span>
					<span class="font-semibold text-fg tabular">{format(hp.avg)}</span>
					<span class="text-fg-muted">Ø</span>
				</div>
				{#if hasBand}
					<div class="text-fg-muted tabular">{format(hp.min)} – {format(hp.max)}</div>
				{/if}
			</div>
		{/if}
		<div class="mt-1 flex items-center justify-between gap-2 text-[11px] text-fg-subtle">
			<div class="flex items-center gap-3">
				<span class="flex items-center gap-1"
					><span class="inline-block h-0.5 w-3 rounded" style="background:{color}"></span>Durchschnitt</span
				>
				{#if hasBand}
					<span class="flex items-center gap-1"
						><span class="inline-block h-2.5 w-3 rounded-sm" style="background:{color};opacity:0.22"
						></span>Min–Max</span
					>
				{/if}
			</div>
			<button
				type="button"
				class="hover:text-fg"
				aria-expanded={showTable}
				aria-controls="{uid}-table"
				onclick={() => (showTable = !showTable)}>{showTable ? 'Tabelle ausblenden' : 'Als Tabelle'}</button
			>
		</div>
		{#if showTable}
			<div id="{uid}-table" class="mt-2 max-h-56 overflow-auto rounded border border-border">
				<table class="w-full text-xs">
					<caption class="sr-only">{label}</caption>
					<thead class="sticky top-0 bg-surface-2 text-fg-muted">
						<tr>
							<th scope="col" class="px-2 py-1 text-left font-medium">Zeit</th>
							<th scope="col" class="px-2 py-1 text-right font-medium">Min</th>
							<th scope="col" class="px-2 py-1 text-right font-medium">Ø</th>
							<th scope="col" class="px-2 py-1 text-right font-medium">Max</th>
						</tr>
					</thead>
					<tbody>
						{#each [...pts].reverse() as p (p.ms)}
							<tr class="border-t border-border">
								<td class="px-2 py-0.5">{formatDateTime(p.ms)}</td>
								<td class="px-2 py-0.5 text-right tabular">{format(p.min)}</td>
								<td class="px-2 py-0.5 text-right tabular">{format(p.avg)}</td>
								<td class="px-2 py-0.5 text-right tabular">{format(p.max)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	{/if}
</div>
