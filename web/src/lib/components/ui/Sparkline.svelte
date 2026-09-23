<!--
	Tiny trend line without axes (for tables and stat tiles).
	<Sparkline values={[3, 5, 2, 8]} label="Latenz 24 h" />
	<Sparkline points={series.points} band label="Ping" />   (min–max band from SeriesPoint)
-->
<script lang="ts">
	import type { SeriesPoint } from '$lib/api/types';

	interface Props {
		values?: number[];
		points?: SeriesPoint[];
		label: string;
		width?: number;
		height?: number;
		band?: boolean;
		color?: string;
		class?: string;
	}

	let {
		values,
		points,
		label,
		width = 96,
		height = 24,
		band = false,
		color = 'var(--chart-1)',
		class: klass = ''
	}: Props = $props();

	const data = $derived(
		points
			? points.map((p) => ({ avg: p.avg, min: p.min, max: p.max }))
			: (values ?? []).map((v) => ({ avg: v, min: v, max: v }))
	);
	const lo = $derived(data.length ? Math.min(...data.map((d) => (band ? d.min : d.avg))) : 0);
	const hi = $derived(data.length ? Math.max(...data.map((d) => (band ? d.max : d.avg))) : 1);
	const pad = 2;
	const x = (i: number) => (data.length <= 1 ? width / 2 : pad + (i / (data.length - 1)) * (width - 2 * pad));
	const y = (v: number) =>
		hi === lo ? height / 2 : height - pad - ((v - lo) / (hi - lo)) * (height - 2 * pad);
	const line = $derived(
		data.map((d, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(d.avg).toFixed(1)}`).join('')
	);
	const area = $derived(
		band && data.length > 1
			? data.map((d, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(d.max).toFixed(1)}`).join('') +
					[...data]
						.map((d, i) => [d, i] as const)
						.reverse()
						.map(([d, i]) => `L${x(i).toFixed(1)},${y(d.min).toFixed(1)}`)
						.join('') +
					'Z'
			: ''
	);
</script>

<svg
	{width}
	{height}
	viewBox="0 0 {width} {height}"
	role="img"
	aria-label={label}
	class="inline-block shrink-0 {klass}"
>
	{#if data.length === 0}
		<line x1="0" x2={width} y1={height / 2} y2={height / 2} stroke="var(--border)" stroke-dasharray="2 3" />
	{:else}
		{#if area}<path d={area} fill={color} fill-opacity="0.15" />{/if}
		<path
			d={line}
			fill="none"
			stroke={color}
			stroke-width="1.5"
			stroke-linejoin="round"
			stroke-linecap="round"
		/>
		<circle cx={x(data.length - 1)} cy={y(data[data.length - 1].avg)} r="2" fill={color} />
	{/if}
</svg>
