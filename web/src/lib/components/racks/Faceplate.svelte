<!--
	Front of a mounted element: icon, name, status and its ports as small sockets. A socket
	shows what is plugged in (cable in its colour, device set by hand, device detected, link)
	and selects the port on click.
-->
<script lang="ts">
	import type { RackItem, RackPort } from '$lib/api';
	import { Icon, StatusDot } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { cableCss, itemIcon, itemName, portState, portTitle } from './rack';

	interface Props {
		item: RackItem;
		/** width of the element in px (decides how many ports fit) */
		width: number;
		/** height of one unit in px */
		unit: number;
		/** shown from behind (full-depth element seen from the other face): no ports */
		back?: boolean;
		selectedPort?: string | null;
		/** a cable is being drawn: every port is a target */
		connecting?: boolean;
		onport?: (port: RackPort) => void;
	}

	let { item, width, unit, back = false, selectedPort = null, connecting = false, onport }: Props = $props();

	const SOCKET = 12;
	const GAP = 2;
	const ports = $derived(back ? [] : item.ports);
	const portArea = $derived(Math.max(0, width - Math.min(150, width * 0.38) - 12));
	const maxCols = $derived(Math.max(1, Math.floor((portArea + GAP) / (SOCKET + GAP))));
	const rows = $derived.by(() => {
		const n = ports.length;
		const perUnit = Math.max(1, Math.floor((unit - 4 + GAP) / (SOCKET + GAP)));
		const max = perUnit * item.height;
		if (item.height === 1) return Math.min(max, n > 8 || n > maxCols ? 2 : 1);
		return Math.max(1, Math.min(max, Math.ceil(n / maxCols)));
	});
	const shown = $derived(Math.min(ports.length, maxCols * rows));
	const offline = $derived(item.kind === 'device' && item.device && !item.device.online);
	const missing = $derived(item.kind === 'device' && !item.device);

	function socketStyle(p: RackPort): string {
		const s = portState(p);
		if (s === 'cable')
			return `background:${cableCss(p.cables[0].color)};border-color:${cableCss(p.cables[0].color)}`;
		return '';
	}
	function socketClass(p: RackPort): string {
		switch (portState(p)) {
			case 'device':
				return 'bg-accent border-accent';
			case 'detected':
				return 'border-dashed border-ok bg-ok-soft';
			case 'up':
				return 'border-ok bg-surface';
			case 'disabled':
				return 'border-border bg-surface-3 opacity-50';
			case 'cable':
				return '';
		}
		return 'border-border-strong bg-surface-2';
	}
</script>

<div class="flex h-full min-w-0 items-center gap-2 px-1.5">
	<div class="flex min-w-0 flex-1 items-center gap-1.5 {item.height > 1 ? 'self-start pt-1' : ''}">
		<Icon name={itemIcon(item)} size={13} class="shrink-0 text-fg-muted" />
		<span
			class="truncate text-[11px] leading-tight font-medium text-fg {missing ? 'italic text-fg-subtle' : ''}"
		>
			{itemName(item)}
		</span>
		{#if item.kind === 'device' && item.device}
			<StatusDot status={item.device.online ? 'online' : 'offline'} pulse={false} size={6} />
		{/if}
		{#if offline}<span class="sr-only">{t('offline')}</span>{/if}
	</div>
	{#if shown > 0}
		<div
			class="grid shrink-0 gap-[2px]"
			style="grid-template-rows:repeat({rows},{SOCKET}px);grid-auto-flow:column;grid-auto-columns:{SOCKET}px"
		>
			{#each ports.slice(0, shown) as p (p.name)}
				<button
					type="button"
					class="rounded-[2px] border transition-shadow {socketClass(p)} {p.media === 'sfp'
						? 'rounded-none'
						: ''} {selectedPort === p.name
						? 'ring-2 ring-fg ring-offset-1 ring-offset-surface'
						: ''} {connecting
						? 'hover:ring-2 hover:ring-accent'
						: 'hover:ring-1 hover:ring-fg-muted'} focus-visible:ring-2 focus-visible:ring-focus focus-visible:outline-none"
					style={socketStyle(p)}
					title={portTitle(p)}
					aria-label={portTitle(p)}
					aria-pressed={selectedPort === p.name}
					onpointerdown={(e) => e.stopPropagation()}
					onclick={(e) => {
						e.stopPropagation();
						onport?.(p);
					}}
				></button>
			{/each}
		</div>
		{#if shown < ports.length}
			<span class="shrink-0 text-[10px] text-fg-subtle">+{ports.length - shown}</span>
		{/if}
	{:else if back}
		<span class="shrink-0 text-[10px] text-fg-subtle">{t('Rückseite')}</span>
	{/if}
</div>
