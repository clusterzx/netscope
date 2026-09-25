<!--
	Floating panel anchored to an element (fixed positioning, so it is never clipped by
	scroll containers). Closes on outside click and Escape.
	<button bind:this={btn} onclick={() => (open = !open)} aria-expanded={open}>…</button>
	<Popover bind:open anchor={btn} placement="bottom-end">…</Popover>
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	type Placement = 'bottom-start' | 'bottom-end' | 'top-start' | 'top-end';

	interface Props {
		open?: boolean;
		anchor: HTMLElement | null | undefined;
		placement?: Placement;
		/** panel at least as wide as the anchor */
		matchWidth?: boolean;
		class?: string;
		/** role of the panel, e.g. dialog | menu | listbox */
		role?: string;
		label?: string;
		onclose?: () => void;
		panel?: HTMLDivElement | null;
		children: Snippet;
	}

	let {
		open = $bindable(false),
		anchor,
		placement = 'bottom-start',
		matchWidth = false,
		class: klass = '',
		role = 'dialog',
		label,
		onclose,
		panel = $bindable(null),
		children
	}: Props = $props();

	let style = $state('');

	function position() {
		if (!anchor || !panel) return;
		const r = anchor.getBoundingClientRect();
		const pw = panel.offsetWidth;
		const ph = panel.offsetHeight;
		const vw = window.innerWidth;
		const vh = window.innerHeight;
		let top = placement.startsWith('top') ? r.top - ph - 6 : r.bottom + 6;
		if (!placement.startsWith('top') && top + ph > vh - 8 && r.top - ph - 6 > 8) top = r.top - ph - 6;
		if (placement.startsWith('top') && top < 8) top = r.bottom + 6;
		let left = placement.endsWith('end') ? r.right - pw : r.left;
		left = Math.max(8, Math.min(left, vw - pw - 8));
		top = Math.max(8, top);
		const maxH = Math.max(160, vh - top - 12);
		// an ancestor with transform, filter or backdrop-filter (e.g. the blurred top bar)
		// becomes the containing block of the fixed panel: subtract its origin
		const cur = panel.getBoundingClientRect();
		const ox = cur.left - (parseFloat(panel.style.left) || 0);
		const oy = cur.top - (parseFloat(panel.style.top) || 0);
		style = `top:${top - oy}px;left:${left - ox}px;max-height:${maxH}px;${matchWidth ? `min-width:${r.width}px;` : ''}`;
	}

	function close() {
		if (!open) return;
		open = false;
		onclose?.();
	}

	$effect(() => {
		if (!open) return;
		// position after the panel rendered
		const raf = requestAnimationFrame(position);
		const onDown = (e: PointerEvent) => {
			const t = e.target as Node;
			if (panel?.contains(t) || anchor?.contains(t)) return;
			close();
		};
		const onKey = (e: KeyboardEvent) => {
			if (e.key === 'Escape') {
				e.stopPropagation();
				close();
				const sel = 'button, a, input, [tabindex]';
				const target = anchor?.matches(sel) ? anchor : anchor?.querySelector<HTMLElement>(sel);
				target?.focus();
			}
		};
		const onMove = () => position();
		document.addEventListener('pointerdown', onDown, true);
		document.addEventListener('keydown', onKey);
		window.addEventListener('resize', onMove);
		window.addEventListener('scroll', onMove, true);
		return () => {
			cancelAnimationFrame(raf);
			document.removeEventListener('pointerdown', onDown, true);
			document.removeEventListener('keydown', onKey);
			window.removeEventListener('resize', onMove);
			window.removeEventListener('scroll', onMove, true);
		};
	});
</script>

{#if open}
	<div
		bind:this={panel}
		{role}
		aria-label={label}
		class="fixed z-50 overflow-auto rounded-lg border border-border bg-surface shadow-lg {klass}"
		style={style || 'top:-9999px;left:-9999px'}
	>
		{@render children()}
	</div>
{/if}
