<!--
	Small drawing of a rack's front for the rack list: cabinet, rails and the mounted elements
	in their colours (switches dark with port dots, servers silver, patch panels with keystones)
	and a status light per device.
-->
<script lang="ts">
	import type { RackSummary } from '$lib/api';

	let { rack, height = 150 }: { rack: RackSummary; height?: number } = $props();

	const u = $derived(Math.max(2.5, Math.min(9, (height - 16) / rack.height)));
	const W = $derived(rack.width === '10' ? 46 : 64);
	const rail = 6;
	const inner = $derived(W - 2 * rail - 4);
	const H = $derived(rack.height * u + 16);
	const items = $derived(rack.layout.filter((p) => p.face === 'front' || p.fullDepth));

	function fill(kind: string, type: string | undefined): string {
		if (kind === 'patch_panel') return '#17191d';
		if (kind === 'shelf') return 'none';
		if (kind === 'blank' || kind === 'cable_manager') return '#24272c';
		if (kind === 'pdu') return '#1f2226';
		if (type === 'server' || type === 'hypervisor' || type === 'nas') return '#5b626b';
		if (type === 'router') return '#2b3a52';
		if (type === 'firewall') return '#4a2a2d';
		return '#2e333a';
	}
	function dots(kind: string, type: string | undefined): string | null {
		if (kind === 'patch_panel') return '#d9dde3';
		if (kind === 'device' && (type === 'switch' || type === 'router' || type === 'firewall'))
			return '#0b0c0e';
		return null;
	}
</script>

<svg width={W} height={H} viewBox="0 0 {W} {H}" aria-hidden="true" class="shrink-0">
	<defs>
		<linearGradient id="thumb-rail" x1="0" x2="1">
			<stop offset="0" stop-color="#4b5159" />
			<stop offset="0.5" stop-color="#6b7280" />
			<stop offset="1" stop-color="#4b5159" />
		</linearGradient>
	</defs>
	<rect x="0" y="0" width={W} height={H} rx="4" fill="#1d2025" stroke="#0a0b0d" />
	<rect x="2" y="2" width={W - 4} height="6" rx="2" fill="#2f343b" />
	<rect x="2" y={H - 8} width={W - 4} height="6" rx="2" fill="#2f343b" />
	<rect x="2" y="8" width={rail} height={rack.height * u} fill="url(#thumb-rail)" />
	<rect x={W - 2 - rail} y="8" width={rail} height={rack.height * u} fill="url(#thumb-rail)" />
	<rect x={2 + rail} y="8" width={inner} height={rack.height * u} fill="#0d0f12" />
	{#each items as p, i (i)}
		{@const x = 2 + rail + (p.col / 6) * inner + 0.5}
		{@const y = 8 + (rack.height - (p.position + p.height - 1)) * u + 0.5}
		{@const w = (p.cols / 6) * inner - 1}
		{@const h = p.height * u - 1}
		{@const f = fill(p.kind, p.type)}
		{#if p.kind === 'shelf'}
			<rect {x} y={y + h - 1.5} width={w} height="1.5" fill="#9ca3af" />
		{:else}
			<rect {x} {y} width={w} height={h} rx="0.8" fill={f} stroke="#0a0b0d" stroke-width="0.5" />
			{@const d = dots(p.kind, p.type)}
			{#if d && u >= 4}
				{#each Array.from({ length: Math.max(0, Math.floor((w * 0.62) / 2.6)) }) as _, k (k)}
					<rect x={x + w - 2 - (k + 1) * 2.6} y={y + h / 2 - 0.9} width="1.8" height="1.8" fill={d} />
				{/each}
			{/if}
			{#if p.kind === 'device'}
				<circle cx={x + 2.5} cy={y + Math.min(h / 2, 2.5)} r="1.1" fill={p.online ? '#4ade80' : '#f87171'} />
			{/if}
		{/if}
	{/each}
</svg>
