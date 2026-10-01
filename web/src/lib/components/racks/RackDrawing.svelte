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

	const UNIT = 36;
	const RAIL = 28;
	const FRAME = 6;
	const natural = $derived(rack.width === '10' ? 280 : 470);
	// rails, frame and border around the mounting space
	const width = $derived(
		fit > 0 ? Math.max(200, Math.min(natural, fit - RAIL * 2 - FRAME * 2 - 4)) : natural
	);
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
	<div class="cabinet" style="width:{width + RAIL * 2 + FRAME * 2}px;--unit:{UNIT}px;--frame:{FRAME}px">
		<div class="cap top" aria-hidden="true"><span class="nameplate">{rack.name}</span></div>
		<div class="relative flex" style="height:{rack.height * UNIT}px">
			<!-- rails with cage nut holes and unit numbers -->
			<div class="rail left" style="width:{RAIL}px" aria-hidden="true">
				{#each units as u (u)}
					<div class="u">
						<span class="unum">{unitLabel(rack, u)}</span>
						<span class="holes"><i></i><i></i><i></i></span>
					</div>
				{/each}
			</div>
			<div
				class="mount"
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
						class="slot group {free ? 'free' : ''}"
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
							<span class="hint">+ {t('Einbauen')}</span>
						{/if}
					</button>
				{/each}

				{#each items as it (it.id)}
					{@const back = it.face !== face}
					{@const dragging = drag?.item.id === it.id && drag.moved}
					<div
						class="item {selectedItem === it.id ? 'selected' : ''} {back ? 'from-back' : ''} {canEdit && !back
							? 'cursor-grab active:cursor-grabbing'
							: 'cursor-pointer'} {dragging ? 'dragging' : ''}"
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
						class="ghost {ghostOk ? 'ok' : 'bad'}"
						style="top:{top(drag.position, drag.item.height) + 1}px;left:{(drag.col / COLUMNS) * width +
							1}px;width:{(drag.item.cols / COLUMNS) * width - 2}px;height:{drag.item.height * UNIT - 2}px"
						aria-hidden="true"
					></div>
				{/if}
			</div>
			<div class="rail right" style="width:{RAIL}px" aria-hidden="true">
				{#each units as u (u)}
					<div class="u"><span class="holes"><i></i><i></i><i></i></span></div>
				{/each}
			</div>
		</div>
		<div class="cap bottom" aria-hidden="true"></div>
		<div class="feet" aria-hidden="true"><span></span><span></span></div>
	</div>
</figure>

<style>
	.cabinet {
		position: relative;
		padding: 0 var(--frame);
		border-radius: 8px;
		background: linear-gradient(90deg, #15171b 0%, #262a30 6%, #1d2025 50%, #262a30 94%, #15171b 100%);
		box-shadow:
			0 0 0 1px #0a0b0d,
			inset 0 1px 0 rgb(255 255 255 / 0.08),
			0 12px 28px -8px rgb(0 0 0 / 0.55),
			0 2px 6px rgb(0 0 0 / 0.35);
	}
	.cap {
		position: relative;
		margin: 0 calc(-1 * var(--frame));
		background: linear-gradient(180deg, #2f343b 0%, #1c1f24 100%);
	}
	.cap.top {
		height: 26px;
		border-radius: 8px 8px 0 0;
		display: flex;
		align-items: center;
		justify-content: center;
		background:
			repeating-linear-gradient(90deg, transparent 0 18px, rgb(0 0 0 / 0.5) 18px 46px, transparent 46px 52px)
				center / 70% 5px no-repeat,
			linear-gradient(180deg, #333840 0%, #1c1f24 100%);
		border-bottom: 1px solid #0a0b0d;
	}
	.nameplate {
		position: absolute;
		left: calc(var(--frame) + 8px);
		top: 50%;
		transform: translateY(-50%);
		max-width: 40%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		padding: 1px 6px;
		border-radius: 2px;
		background: linear-gradient(180deg, #d1d5db, #9ca3af);
		color: #111827;
		font:
			700 8px/1.4 system-ui,
			sans-serif;
		letter-spacing: 0.1em;
		text-transform: uppercase;
		box-shadow: 0 0.5px 1px rgb(0 0 0 / 0.8);
	}
	.cap.bottom {
		height: 14px;
		border-top: 1px solid #0a0b0d;
		border-radius: 0 0 8px 8px;
	}
	.feet {
		position: absolute;
		left: 10%;
		right: 10%;
		bottom: -6px;
		display: flex;
		justify-content: space-between;
	}
	.feet span {
		width: 22px;
		height: 6px;
		border-radius: 0 0 3px 3px;
		background: linear-gradient(180deg, #1f2227, #0a0b0d);
	}

	/* rails: punched steel with square cage nut holes, three per unit */
	.rail {
		position: relative;
		display: flex;
		flex-direction: column;
		background: linear-gradient(90deg, #4b5159 0%, #6b7280 40%, #4b5159 100%);
		box-shadow:
			inset 0 0 0 1px rgb(0 0 0 / 0.5),
			inset 0 1px 0 rgb(255 255 255 / 0.1);
	}
	.rail .u {
		position: relative;
		height: var(--unit);
		display: flex;
		align-items: center;
		border-bottom: 1px solid rgb(0 0 0 / 0.18);
	}
	.rail.left .u {
		justify-content: space-between;
		padding: 0 4px 0 2px;
	}
	.rail.right .u {
		justify-content: flex-start;
		padding-left: 4px;
	}
	.unum {
		color: #f3f4f6;
		font:
			700 9px/1 ui-monospace,
			monospace;
		text-shadow: 0 1px 0 rgb(0 0 0 / 0.6);
		font-variant-numeric: tabular-nums;
	}
	.holes {
		display: flex;
		flex-direction: column;
		justify-content: space-between;
		height: calc(var(--unit) - 10px);
	}
	.holes i {
		display: block;
		width: 6px;
		height: 6px;
		border-radius: 1px;
		background: #0b0c0e;
		box-shadow:
			inset 0 1px 1px rgb(0 0 0 / 0.9),
			0 0.5px 0 rgb(255 255 255 / 0.25);
	}

	/* mounting space: deep inside of the cabinet with faint unit lines */
	.mount {
		position: relative;
		background:
			repeating-linear-gradient(
				180deg,
				transparent 0 calc(var(--unit) - 1px),
				rgb(255 255 255 / 0.04) calc(var(--unit) - 1px) var(--unit)
			),
			radial-gradient(ellipse at 50% 30%, #16191d 0%, #0b0c0f 100%);
		box-shadow: inset 0 0 18px rgb(0 0 0 / 0.9);
	}
	.slot {
		position: absolute;
		left: 0;
		right: 0;
		text-align: left;
		cursor: default;
		border-radius: 2px;
	}
	.slot.free {
		cursor: pointer;
	}
	.slot.free:hover,
	.slot.free:focus-visible {
		outline: 1px dashed rgb(96 165 250 / 0.7);
		outline-offset: -2px;
		background: rgb(59 130 246 / 0.14);
	}
	.hint {
		display: none;
		padding-left: 10px;
		color: #93c5fd;
		font:
			600 11px/1 system-ui,
			sans-serif;
	}
	.slot.free:hover .hint,
	.slot.free:focus-visible .hint {
		display: inline;
	}

	.item {
		position: absolute;
		user-select: none;
		border-radius: 3px;
		filter: drop-shadow(0 2px 2px rgb(0 0 0 / 0.65));
		transition:
			box-shadow 120ms,
			opacity 120ms;
	}
	.item:focus-visible {
		outline: none;
		box-shadow:
			0 0 0 2px #60a5fa,
			0 0 14px rgb(96 165 250 / 0.6);
	}
	.item.selected {
		box-shadow:
			0 0 0 2px #60a5fa,
			0 0 16px rgb(96 165 250 / 0.75);
		z-index: 2;
	}
	.item.from-back {
		opacity: 0.7;
	}
	.item.dragging {
		opacity: 0.35;
	}
	.ghost {
		position: absolute;
		pointer-events: none;
		border-radius: 3px;
		border: 2px dashed;
	}
	.ghost.ok {
		border-color: #60a5fa;
		background: rgb(59 130 246 / 0.2);
		box-shadow: 0 0 14px rgb(96 165 250 / 0.5);
	}
	.ghost.bad {
		border-color: #f87171;
		background: rgb(239 68 68 / 0.2);
	}
</style>
