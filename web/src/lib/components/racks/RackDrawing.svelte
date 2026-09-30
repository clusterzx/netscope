<!--
	One face of a rack, drawn to scale: height units with their numbers, the mounted elements
	(full, half or third width) and their ports. Free units open the mount dialog; elements
	are selected by click and moved by dragging (snaps to units and columns).
-->
<script lang="ts">
	import type { RackItem, RackPort, RackView } from '$lib/api';
	import { t } from '$lib/i18n';
	import Faceplate from './Faceplate.svelte';
	import { COLUMNS, fits, itemName, onFace, unitLabel, unitsText, type Face, type Slot } from './rack';

	interface Props {
		rack: RackView;
		face: Face;
		canEdit?: boolean;
		selectedItem?: number | null;
		selectedPort?: { item: number; port: string } | null;
		connecting?: boolean;
		onselect?: (item: RackItem) => void;
		onport?: (item: RackItem, port: RackPort) => void;
		onslot?: (slot: Slot) => void;
		onmove?: (item: RackItem, place: { position: number; col: number }) => void;
		/** available width in px (0 = natural size): narrow screens get a narrower rack */
		fit?: number;
	}

	let {
		rack,
		face,
		canEdit = false,
		selectedItem = null,
		selectedPort = null,
		connecting = false,
		onselect,
		onport,
		onslot,
		onmove,
		fit = 0
	}: Props = $props();

	const UNIT = 34;
	const RAIL = 26;
	const natural = $derived(rack.width === '10' ? 280 : 470);
	// rails, frame and border around the mounting space
	const width = $derived(fit > 0 ? Math.max(200, Math.min(natural, fit - RAIL * 2 - 16 - 4)) : natural);
	const units = $derived(Array.from({ length: rack.height }, (_, i) => rack.height - i)); // top → bottom (positions)
	const items = $derived(rack.items.filter((i) => onFace(i, face)));

	function top(position: number, height: number): number {
		return (rack.height - (position + height - 1)) * UNIT;
	}

	// ------------------------------------------------------------ free units
	function slotAt(position: number, x: number): Slot | null {
		const frac = Math.min(0.999, Math.max(0, x / width));
		const tries: Slot[] = [
			{ position, face, col: 0, cols: 6 },
			{ position, face, col: frac < 0.5 ? 0 : 3, cols: 3 },
			{ position, face, col: Math.floor(frac * 3) * 2, cols: 2 }
		];
		for (const s of tries) if (fits(rack, { ...s, height: 1, fullDepth: false })) return s;
		return null;
	}
	function freeUnit(position: number): boolean {
		return [0, 2, 4].some((col) => fits(rack, { position, height: 1, face, fullDepth: false, col, cols: 2 }));
	}

	// ------------------------------------------------------------ drag and drop
	let drag = $state<{
		item: RackItem;
		x0: number;
		y0: number;
		position: number;
		col: number;
		moved: boolean;
	} | null>(null);
	const ghostOk = $derived(
		drag
			? fits(
					rack,
					{
						position: drag.position,
						height: drag.item.height,
						face: drag.item.face,
						fullDepth: drag.item.fullDepth,
						col: drag.col,
						cols: drag.item.cols
					},
					drag.item.id
				)
			: false
	);

	function down(e: PointerEvent, item: RackItem) {
		if (!canEdit || e.button !== 0 || item.face !== face) {
			return;
		}
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		drag = { item, x0: e.clientX, y0: e.clientY, position: item.position, col: item.col, moved: false };
	}
	function move(e: PointerEvent) {
		if (!drag) return;
		const dy = e.clientY - drag.y0;
		const dx = e.clientX - drag.x0;
		if (!drag.moved && Math.abs(dy) < 4 && Math.abs(dx) < 4) return;
		const it = drag.item;
		const position = Math.min(rack.height - it.height + 1, Math.max(1, it.position - Math.round(dy / UNIT)));
		let col = it.col;
		if (it.cols < COLUMNS) {
			const step = (width / COLUMNS) * it.cols;
			col = Math.min(COLUMNS - it.cols, Math.max(0, it.col + Math.round(dx / step) * it.cols));
		}
		drag = { ...drag, position, col, moved: true };
	}
	function up() {
		const d = drag;
		drag = null;
		if (!d) return;
		if (!d.moved) {
			onselect?.(d.item);
			return;
		}
		const ok = fits(
			rack,
			{
				position: d.position,
				height: d.item.height,
				face: d.item.face,
				fullDepth: d.item.fullDepth,
				col: d.col,
				cols: d.item.cols
			},
			d.item.id
		);
		if (ok && (d.position !== d.item.position || d.col !== d.item.col))
			onmove?.(d.item, { position: d.position, col: d.col });
	}
	function key(e: KeyboardEvent, item: RackItem) {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			onselect?.(item);
		}
	}
</script>

<figure class="inline-flex flex-col items-center gap-2">
	<figcaption class="text-xs font-medium tracking-wide text-fg-muted uppercase">
		{face === 'front' ? t('Vorderseite') : t('Rückseite')}
	</figcaption>
	<div
		class="rounded-md border-2 border-border-strong bg-surface-3 p-1.5 shadow-sm"
		style="width:{width + RAIL * 2 + 16}px"
	>
		<div class="relative flex" style="height:{rack.height * UNIT}px">
			<!-- rails with unit numbers -->
			<div class="flex flex-col" style="width:{RAIL}px" aria-hidden="true">
				{#each units as u (u)}
					<div
						class="flex items-center justify-center text-[10px] text-fg-subtle tabular-nums"
						style="height:{UNIT}px"
					>
						{unitLabel(rack, u)}
					</div>
				{/each}
			</div>
			<div
				class="relative rounded-sm bg-bg"
				style="width:{width}px;height:{rack.height * UNIT}px"
				role="presentation"
				onpointermove={move}
				onpointerup={up}
				onpointercancel={() => (drag = null)}
			>
				<!-- unit rows: free ones mount an element -->
				{#each units as u (u)}
					{@const free = canEdit && !connecting && freeUnit(u)}
					<button
						type="button"
						class="group absolute inset-x-0 border-b border-dashed border-border/60 text-left {free
							? 'cursor-pointer hover:bg-accent-soft'
							: 'cursor-default'}"
						style="top:{top(u, 1)}px;height:{UNIT}px"
						tabindex={free ? 0 : -1}
						disabled={!free}
						aria-label={t('HE {unit} frei – einbauen', { unit: unitLabel(rack, u) })}
						onclick={(e) => {
							const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
							const s = slotAt(u, e.clientX - r.left);
							if (s) onslot?.(s);
						}}
					>
						{#if free}
							<span class="hidden pl-2 text-[11px] text-accent group-hover:inline group-focus-visible:inline">
								+ {t('Einbauen')}
							</span>
						{/if}
					</button>
				{/each}

				{#each items as it (it.id)}
					{@const back = it.face !== face}
					{@const dragging = drag?.item.id === it.id && drag.moved}
					<div
						class="absolute overflow-hidden rounded-[3px] border shadow-sm select-none
						{back ? 'border-dashed border-border bg-surface-2/70' : 'border-border-strong bg-surface'}
						{selectedItem === it.id ? 'ring-2 ring-accent' : ''}
						{it.kind === 'device' && it.device && !it.device.online ? 'border-l-4 border-l-offline' : ''}
						{canEdit && !back ? 'cursor-grab active:cursor-grabbing' : 'cursor-pointer'}
						{dragging ? 'opacity-40' : ''}"
						style="top:{top(it.position, it.height) + 1}px;left:{(it.col / COLUMNS) * width +
							1}px;width:{(it.cols / COLUMNS) * width - 2}px;height:{it.height * UNIT -
							2}px;touch-action:none"
						role="button"
						tabindex="0"
						aria-label="{itemName(it)}, {t('HE {units}', { units: unitsText(rack, it) })}"
						aria-pressed={selectedItem === it.id}
						onpointerdown={(e) => down(e, it)}
						onclick={() => {
							if (!canEdit || back) onselect?.(it);
						}}
						onkeydown={(e) => key(e, it)}
					>
						<Faceplate
							item={it}
							width={(it.cols / COLUMNS) * width - 2}
							unit={UNIT}
							{back}
							{connecting}
							selectedPort={selectedPort?.item === it.id ? selectedPort.port : null}
							onport={(p) => onport?.(it, p)}
						/>
					</div>
				{/each}

				{#if drag?.moved}
					<div
						class="pointer-events-none absolute rounded-[3px] border-2 {ghostOk
							? 'border-accent bg-accent-soft/60'
							: 'border-danger bg-danger-soft/60'}"
						style="top:{top(drag.position, drag.item.height) + 1}px;left:{(drag.col / COLUMNS) * width +
							1}px;width:{(drag.item.cols / COLUMNS) * width - 2}px;height:{drag.item.height * UNIT - 2}px"
						aria-hidden="true"
					></div>
				{/if}
			</div>
			<div style="width:{RAIL}px" aria-hidden="true"></div>
		</div>
	</div>
</figure>
