<!--
	Front (or rear) of a mounted element, drawn like the hardware: switch with port pairs in
	blocks of twelve and SFP cages, patch panel with numbered keystones, server and NAS with
	drive bays, power button and vents, PDU with outlets, blank, brush strip and shelf.
	Full-width elements have rack ears with screws. The ports are buttons (PortSocket).
-->
<script lang="ts">
	import type { RackItem, RackPort } from '$lib/api';
	import { t } from '$lib/i18n';
	import PortSocket, { type SocketKind } from './PortSocket.svelte';
	import {
		COLUMNS,
		GRID_GAP,
		GRID_GROUP_GAP,
		gridColumns,
		itemName,
		kindLabel,
		portGrid,
		skinOf,
		type PortGrid
	} from './rack';

	interface Props {
		item: RackItem;
		/** width of the element in px */
		width: number;
		/** height of one unit in px */
		unit: number;
		/** seen from the other face (full-depth element): rear panel, no ports */
		back?: boolean;
		selectedPort?: string | null;
		/** a cable is being drawn: every port is a target */
		connecting?: boolean;
		onport?: (port: RackPort) => void;
	}

	let { item, width, unit, back = false, selectedPort = null, connecting = false, onport }: Props = $props();

	const EAR = 10;
	const PAD = 5;
	const skin = $derived(skinOf(item));
	const ears = $derived(item.cols === COLUMNS && skin !== 'shelf');
	const inner = $derived(Math.max(20, width - (ears ? 2 * EAR : 0) - 2 * PAD));
	const innerH = $derived(item.height * unit - 2 - 2 * PAD + 2);
	const name = $derived(itemName(item));
	const model = $derived([item.device?.vendor, item.device?.model].filter(Boolean).join(' '));
	const power = $derived(
		item.kind !== 'device' ? 'on' : !item.device ? 'off' : item.device.online ? 'on' : 'down'
	);
	const screws = $derived(Array.from({ length: item.height }, (_, i) => i));

	// ------------------------------------------------------------ port layout
	const copper = $derived(item.ports.filter((p) => p.media !== 'sfp'));
	const sfp = $derived(item.ports.filter((p) => p.media === 'sfp'));

	interface Layout {
		label: number;
		main: PortGrid;
		mainPorts: RackPort[];
		mainKind: SocketKind;
		side: PortGrid | null;
		sidePorts: RackPort[];
	}
	const empty: PortGrid = { rows: 0, cols: 0, size: 0, shown: 0, width: 0 };

	const layout = $derived.by((): Layout => {
		const twoRows = item.height === 1 ? 2 : 2 * item.height;
		switch (skin) {
			case 'switch':
			case 'router':
			case 'firewall': {
				const labelMin = inner < 200 ? 44 : 86;
				const side = sfp.length
					? portGrid(sfp.length, Math.min(90, inner * 0.22), innerH, {
							aspect: 0.6,
							maxSize: 19,
							minSize: 12,
							group: 99,
							minRows: Math.min(sfp.length, twoRows)
						})
					: null;
				const avail = inner - labelMin - (side ? side.width + 8 : 0) - 6;
				const main = portGrid(copper.length, avail, innerH, {
					minRows: copper.length > 8 ? Math.min(2, twoRows) : 1,
					maxSize: 15
				});
				const label = Math.min(140, inner - main.width - (side ? side.width + 8 : 0) - 6);
				return {
					label: Math.max(labelMin, label),
					main,
					mainPorts: copper,
					mainKind: 'rj45',
					side,
					sidePorts: sfp
				};
			}
			case 'patch': {
				const labelMin = inner < 200 ? 40 : 82;
				const main = portGrid(item.ports.length, inner - labelMin - 6, innerH, {
					minRows: item.height,
					aspect: 1.55,
					maxSize: 14,
					minSize: 9,
					group: 12
				});
				return {
					label: Math.max(labelMin, Math.min(110, inner - main.width - 6)),
					main,
					mainPorts: item.ports,
					mainKind: 'keystone',
					side: null,
					sidePorts: []
				};
			}
			case 'pdu': {
				const main = portGrid(item.ports.length, inner - 64, innerH, {
					aspect: 1,
					maxSize: 14,
					minSize: 9,
					group: 4
				});
				return { label: 58, main, mainPorts: item.ports, mainKind: 'outlet', side: null, sidePorts: [] };
			}
			default: {
				// servers, NAS and small boxes: a compact NIC block on the right
				const main = item.ports.length
					? portGrid(item.ports.length, Math.min(inner * 0.4, 110), innerH, {
							maxSize: 13,
							minSize: 9,
							group: 99
						})
					: empty;
				return {
					label: inner < 200 ? inner * 0.55 : Math.min(130, inner * 0.32),
					main,
					mainPorts: item.ports,
					mainKind: 'rj45',
					side: null,
					sidePorts: []
				};
			}
		}
	});

	// ------------------------------------------------------------ drive bays
	const bays = $derived.by(() => {
		if (skin !== 'server' && skin !== 'nas') return null;
		const area = inner - layout.label - (layout.main.width ? layout.main.width + 10 : 0) - 34;
		if (area < 24) return null;
		if (skin === 'server' && item.height === 1) {
			// 2.5" bays, upright
			const n = Math.max(1, Math.min(10, Math.floor((area + 2) / 12)));
			return { rows: 1, cols: n, w: 10, h: innerH };
		}
		const rows = Math.max(1, Math.min(4 * item.height, Math.floor((innerH + 2) / 13)));
		const cols = Math.max(1, Math.min(4, Math.floor((area + 3) / 40)));
		return {
			rows,
			cols,
			w: Math.floor((area - (cols - 1) * 3) / cols),
			h: Math.floor((innerH - (rows - 1) * 2) / rows)
		};
	});
	const vents = $derived(
		skin === 'server' || skin === 'nas'
			? Math.max(
					0,
					inner -
						layout.label -
						(bays ? bays.cols * (bays.w + 3) : 0) -
						(layout.main.width ? layout.main.width + 10 : 0) -
						12
				)
			: 0
	);
</script>

{#snippet sockets(grid: PortGrid, ports: RackPort[], kind: SocketKind, numbers = false)}
	<div class="flex shrink-0 items-center" style="gap:{GRID_GAP}px">
		{#each gridColumns(ports, grid) as col, c (c)}
			<div
				class="flex flex-col"
				style="gap:{GRID_GAP}px;margin-left:{c > 0 &&
				c % (kind === 'keystone' ? 12 : kind === 'outlet' ? 4 : kind === 'sfp' ? 99 : 6) === 0
					? GRID_GROUP_GAP
					: 0}px"
			>
				{#each col as p, r (p.name)}
					<div class="flex flex-col items-center">
						{#if numbers}<span class="num">{p.name}</span>{/if}
						<PortSocket
							port={p}
							{kind}
							width={grid.size}
							row={grid.rows > 1 && r % 2 === 1 ? 'bottom' : 'top'}
							selected={selectedPort === p.name}
							{connecting}
							onclick={() => onport?.(p)}
						/>
					</div>
				{/each}
			</div>
		{/each}
	</div>
	{#if grid.shown < ports.length}
		<span class="more">+{ports.length - grid.shown}</span>
	{/if}
{/snippet}

{#snippet labelZone(w: number, button = false)}
	<div class="flex min-w-0 flex-col justify-center gap-[3px]" style="width:{w}px">
		<div class="flex min-w-0 items-center gap-1.5">
			{#if button}<span class="pwr-btn {power}"></span>{/if}
			<span class="sticker {item.kind === 'device' && !item.device ? 'missing' : ''}" title={name}
				>{name}</span
			>
		</div>
		<div class="flex min-w-0 items-center gap-1.5">
			{#if !button}
				<span class="led-dot {power}" title={power === 'down' ? t('offline') : undefined}></span>
				<span class="led-dot {power === 'on' ? 'sys' : 'off'}"></span>
			{/if}
			{#if model && innerH > 14}<span class="engrave truncate">{model}</span>{/if}
		</div>
	</div>
{/snippet}

<div class="plate skin-{skin} {back ? 'rear' : ''}" style="--ear:{ears ? EAR : 0}px;--pad:{PAD}px">
	{#if ears}
		{#each ['left', 'right'] as side (side)}
			<span class="ear {side}" aria-hidden="true">
				{#each screws as s (s)}<span class="screw" style="top:{(s + 0.5) * unit - 4}px"></span>{/each}
			</span>
		{/each}
	{/if}

	<div class="body">
		{#if back}
			<!-- rear: name, vents and power supply -->
			<span class="sticker shrink-0" style="max-width:{Math.max(40, inner * 0.35)}px" title={name}
				>{name}</span
			>
			<div class="vents flex-1 self-stretch"></div>
			{#if skin !== 'patch' && skin !== 'blank' && skin !== 'cable' && skin !== 'shelf'}
				<div class="psu" style="height:{Math.min(innerH, unit * 1.6)}px">
					<span class="fan"></span>
					<span class="inlet"></span>
				</div>
			{/if}
		{:else if skin === 'switch' || skin === 'router' || skin === 'firewall'}
			{#if skin !== 'switch'}<span class="accent {skin}" aria-hidden="true"></span>{/if}
			{@render labelZone(layout.label)}
			<div class="flex flex-1 items-center justify-end gap-2">
				{@render sockets(layout.main, layout.mainPorts, 'rj45')}
				{#if layout.side}
					<div class="sfp-block">{@render sockets(layout.side, layout.sidePorts, 'sfp')}</div>
				{/if}
			</div>
		{:else if skin === 'patch'}
			{@render labelZone(layout.label)}
			<div class="flex flex-1 items-center justify-end">
				{@render sockets(layout.main, layout.mainPorts, 'keystone', layout.main.size >= 10)}
			</div>
		{:else if skin === 'server' || skin === 'nas'}
			{@render labelZone(layout.label, true)}
			{#if bays}
				<div
					class="grid shrink-0"
					style="grid-template-columns:repeat({bays.cols},{bays.w}px);grid-auto-rows:{bays.h}px;gap:2px 3px"
					aria-hidden="true"
				>
					{#each Array.from({ length: bays.rows * bays.cols }) as _, i (i)}
						<span class="bay {bays.w < bays.h ? 'upright' : ''} {power === 'on' ? 'lit' : ''}"></span>
					{/each}
				</div>
			{/if}
			{#if vents > 12}<div class="vents mx-1.5 self-stretch" style="width:{vents}px"></div>{/if}
			{#if layout.main.width}
				<div class="ml-auto flex items-center">{@render sockets(layout.main, layout.mainPorts, 'rj45')}</div>
			{/if}
		{:else if skin === 'pdu'}
			<div class="flex shrink-0 items-center gap-1.5" style="width:{layout.label}px">
				<span class="lcd"
					>{item.ports.length
						? `${item.ports.filter((p) => p.cables.length || p.device).length}/${item.ports.length}`
						: '230V'}</span
				>
			</div>
			<div class="flex min-w-0 flex-1 items-center gap-2">
				{#if layout.main.width}{@render sockets(layout.main, layout.mainPorts, 'outlet')}{/if}
				<span class="engrave truncate">{name}</span>
			</div>
		{:else if skin === 'blank'}
			<div class="slots flex-1 self-stretch" aria-hidden="true"></div>
			{#if item.label}<span class="engrave absolute-center">{item.label}</span>{/if}
		{:else if skin === 'cable'}
			<div class="brush flex-1" aria-hidden="true"></div>
			{#if item.label}<span class="engrave ml-2 truncate">{item.label}</span>{/if}
		{:else if skin === 'shelf'}
			<div class="tray" aria-hidden="true"></div>
			<div class="flex flex-1 items-end justify-center gap-2 self-stretch pb-[6px]">
				{#if item.label}
					<span
						class="parcel"
						style="width:{Math.max(40, Math.min(width * 0.7, 300))}px;height:{Math.max(
							14,
							item.height * unit - 14
						)}px"><span class="parcel-led"></span><span class="truncate">{item.label}</span></span
					>
				{:else}
					<span class="engrave">{kindLabel('shelf')}</span>
				{/if}
			</div>
		{:else}
			<!-- small boxes: mini PCs, access points, anything without a rack chassis -->
			{@render labelZone(layout.main.width ? Math.max(40, inner - layout.main.width - 8) : inner)}
			{#if layout.main.width}
				<div class="ml-auto flex items-center">{@render sockets(layout.main, layout.mainPorts, 'rj45')}</div>
			{/if}
		{/if}
	</div>
</div>

<style>
	.plate {
		position: relative;
		height: 100%;
		color: #e5e7eb;
	}
	.body {
		position: absolute;
		inset: 0 var(--ear);
		display: flex;
		align-items: center;
		gap: 8px;
		padding: var(--pad);
		border-radius: 2px;
		overflow: hidden;
	}

	/* ------------------------------------------------ chassis */
	.skin-switch .body,
	.skin-router .body,
	.skin-firewall .body {
		background:
			linear-gradient(180deg, rgb(255 255 255 / 0.1) 0, rgb(255 255 255 / 0) 2px),
			linear-gradient(180deg, #353a42 0%, #2a2e35 55%, #23272c 100%);
		box-shadow:
			inset 0 0 0 1px rgb(0 0 0 / 0.45),
			inset 0 -1px 0 rgb(0 0 0 / 0.6);
	}
	.skin-patch .body {
		background: linear-gradient(180deg, #23262b 0%, #17191d 100%);
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.6);
	}
	.skin-server .body,
	.skin-nas .body {
		background:
			linear-gradient(180deg, rgb(255 255 255 / 0.18) 0, rgb(255 255 255 / 0) 2px),
			linear-gradient(180deg, #6b727c 0%, #4a5058 18%, #3c4148 100%);
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.45);
	}
	.skin-box .body {
		background: linear-gradient(180deg, #3d434b 0%, #2c3036 100%);
		border-radius: 4px;
		box-shadow:
			inset 0 0 0 1px rgb(0 0 0 / 0.5),
			inset 0 1px 0 rgb(255 255 255 / 0.12);
		inset: 1px 2px;
	}
	.skin-pdu .body {
		background: linear-gradient(180deg, #2a2d32 0%, #1b1d21 100%);
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.6);
	}
	.skin-blank .body {
		background: linear-gradient(180deg, #2b2f35 0%, #1f2227 100%);
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.5);
	}
	.skin-cable .body {
		background: linear-gradient(180deg, #202328 0%, #15171a 100%);
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.6);
	}
	.skin-shelf .body {
		background: transparent;
		padding: 0;
	}
	.rear .body {
		background: linear-gradient(180deg, #24272c 0%, #1a1c20 100%);
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.6);
		opacity: 0.85;
	}

	.accent {
		position: absolute;
		left: 0;
		top: 0;
		bottom: 0;
		width: 3px;
	}
	.accent.router {
		background: linear-gradient(180deg, #60a5fa, #2563eb);
	}
	.accent.firewall {
		background: linear-gradient(180deg, #f87171, #dc2626);
	}

	/* ------------------------------------------------ ears and screws */
	.ear {
		position: absolute;
		top: 0;
		bottom: 0;
		width: var(--ear);
		background: linear-gradient(90deg, #4b5159 0%, #3a3f46 50%, #2f343a 100%);
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.45);
	}
	.skin-server .ear,
	.skin-nas .ear {
		background: linear-gradient(90deg, #7a818b 0%, #5d646d 50%, #4d535b 100%);
	}
	.ear.left {
		left: 0;
		border-radius: 2px 0 0 2px;
	}
	.ear.right {
		right: 0;
		border-radius: 0 2px 2px 0;
	}
	.screw {
		position: absolute;
		left: 50%;
		width: 7px;
		height: 7px;
		margin-left: -3.5px;
		border-radius: 50%;
		background: radial-gradient(circle at 35% 35%, #e5e7eb 0%, #9ca3af 45%, #4b5563 100%);
		box-shadow: 0 0.5px 1px rgb(0 0 0 / 0.8);
	}
	.screw::after {
		content: '';
		position: absolute;
		left: 1.5px;
		right: 1.5px;
		top: 3px;
		height: 1px;
		background: #374151;
		transform: rotate(-30deg);
	}

	/* ------------------------------------------------ labels and lights */
	.sticker {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		padding: 1px 4px;
		border-radius: 1.5px;
		background: linear-gradient(180deg, #fbfaf5, #ebe8dc);
		color: #111827;
		font:
			600 9.5px/1.25 ui-monospace,
			SFMono-Regular,
			Menlo,
			monospace;
		box-shadow: 0 0.5px 1px rgb(0 0 0 / 0.6);
	}
	.sticker.missing {
		background: #fde68a;
		font-style: italic;
	}
	.engrave {
		min-width: 0;
		color: #9ca3af;
		font:
			600 7.5px/1 system-ui,
			sans-serif;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		white-space: nowrap;
	}
	.skin-server .engrave,
	.skin-nas .engrave {
		color: #d1d5db;
	}
	.led-dot {
		width: 4px;
		height: 4px;
		flex: none;
		border-radius: 50%;
		background: #374151;
	}
	.led-dot.on {
		background: #4ade80;
		box-shadow: 0 0 5px #22c55e;
	}
	.led-dot.sys {
		background: #60a5fa;
		box-shadow: 0 0 5px #3b82f6;
	}
	.led-dot.down {
		background: #f87171;
		box-shadow: 0 0 5px #ef4444;
	}
	.pwr-btn {
		position: relative;
		width: 10px;
		height: 10px;
		flex: none;
		border-radius: 50%;
		background: radial-gradient(circle at 40% 35%, #6b7280, #1f2937);
		box-shadow: 0 0 0 1px #111827;
	}
	.pwr-btn::after {
		content: '';
		position: absolute;
		inset: 2.5px;
		border-radius: 50%;
		border: 1.5px solid #4b5563;
	}
	.pwr-btn.on::after {
		border-color: #4ade80;
		box-shadow: 0 0 4px #22c55e;
	}
	.pwr-btn.down::after {
		border-color: #f87171;
		box-shadow: 0 0 4px #ef4444;
	}
	.num {
		color: #e5e7eb;
		font:
			600 6.5px/7px system-ui,
			sans-serif;
		height: 7px;
		margin-bottom: 1px;
	}
	.more {
		color: #9ca3af;
		font:
			600 9px/1 system-ui,
			sans-serif;
		margin-left: 3px;
	}
	.sfp-block {
		padding: 2px 3px;
		border-radius: 2px;
		background: rgb(0 0 0 / 0.25);
		box-shadow: inset 0 0 0 1px rgb(255 255 255 / 0.05);
	}
	.lcd {
		padding: 1px 4px;
		border-radius: 2px;
		background: linear-gradient(180deg, #0f2a1a, #0a1f13);
		color: #4ade80;
		font:
			700 9px/1.3 ui-monospace,
			monospace;
		text-shadow: 0 0 4px #22c55e;
		box-shadow: inset 0 0 0 1px #052e16;
	}

	/* ------------------------------------------------ bays, vents, brush, shelf */
	.bay {
		position: relative;
		border-radius: 1.5px;
		background: linear-gradient(180deg, #2b2f35 0%, #1c1f23 100%);
		box-shadow:
			inset 0 0 0 1px rgb(0 0 0 / 0.6),
			inset 0 1px 0 rgb(255 255 255 / 0.08);
	}
	.bay::before {
		content: '';
		position: absolute;
		left: 4px;
		right: 10px;
		top: 50%;
		height: 1px;
		background: rgb(255 255 255 / 0.12);
	}
	.bay.upright::before {
		left: 50%;
		right: auto;
		top: 4px;
		bottom: 8px;
		width: 1px;
		height: auto;
	}
	.bay::after {
		content: '';
		position: absolute;
		right: 3px;
		top: 3px;
		width: 3px;
		height: 3px;
		border-radius: 50%;
		background: #374151;
	}
	.bay.upright::after {
		right: auto;
		left: 50%;
		top: auto;
		bottom: 3px;
		margin-left: -1.5px;
	}
	.bay.lit::after {
		background: #4ade80;
		box-shadow: 0 0 3px #22c55e;
	}
	.vents {
		border-radius: 2px;
		background-image: radial-gradient(circle, rgb(0 0 0 / 0.55) 1.1px, transparent 1.4px);
		background-size: 5px 5px;
		background-position: center;
		min-height: 8px;
	}
	.slots {
		border-radius: 1px;
		background-image: repeating-linear-gradient(
			90deg,
			transparent 0 4px,
			rgb(0 0 0 / 0.55) 4px 22px,
			transparent 22px 26px
		);
		mask-image: linear-gradient(180deg, transparent 20%, black 20% 80%, transparent 80%);
	}
	.brush {
		height: 46%;
		border-radius: 2px;
		background:
			linear-gradient(180deg, transparent 47%, #050607 47% 53%, transparent 53%),
			repeating-linear-gradient(90deg, #3a3e45 0 1px, #15171a 1px 2.5px);
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.7);
	}
	.tray {
		position: absolute;
		left: 0;
		right: 0;
		bottom: 1px;
		height: 6px;
		border-radius: 0 0 2px 2px;
		background: linear-gradient(180deg, #9ca3af 0%, #6b7280 40%, #4b5563 100%);
		box-shadow: 0 1px 2px rgb(0 0 0 / 0.6);
	}
	.parcel {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 0 8px;
		border-radius: 3px 3px 1px 1px;
		background:
			linear-gradient(180deg, rgb(255 255 255 / 0.1) 0, rgb(255 255 255 / 0) 2px),
			linear-gradient(180deg, #3b4148 0%, #262a2f 100%);
		color: #e5e7eb;
		font:
			600 9px/1.2 system-ui,
			sans-serif;
		box-shadow:
			inset 0 0 0 1px rgb(0 0 0 / 0.5),
			0 1px 2px rgb(0 0 0 / 0.6);
	}
	.parcel-led {
		width: 5px;
		height: 5px;
		flex: none;
		border-radius: 50%;
		background: #4ade80;
		box-shadow: 0 0 5px #22c55e;
	}
	.psu {
		position: relative;
		width: 44px;
		flex: none;
		border-radius: 2px;
		background: linear-gradient(180deg, #3a3f46, #2b2f35);
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.6);
	}
	.fan {
		position: absolute;
		left: 4px;
		top: 50%;
		width: 18px;
		height: 18px;
		margin-top: -9px;
		border-radius: 50%;
		background:
			radial-gradient(circle, #4b5563 0 2px, transparent 2.5px),
			repeating-conic-gradient(#111317 0 20deg, #2b2f35 20deg 45deg);
		box-shadow: 0 0 0 1px #111317;
	}
	.inlet {
		position: absolute;
		right: 4px;
		top: 50%;
		width: 12px;
		height: 9px;
		margin-top: -4.5px;
		border-radius: 1px 1px 3px 3px;
		background: #0b0c0e;
		box-shadow: 0 0 0 1px #6b7280;
	}
	.absolute-center {
		position: absolute;
		left: 50%;
		top: 50%;
		transform: translate(-50%, -50%);
		padding: 1px 4px;
		background: #1f2227;
	}
</style>
