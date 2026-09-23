<!--
  Canvas view of the topology graph (engine: graph.ts). Handles size/devicePixelRatio, theme
  colours and the hover tooltip; interaction results are reported via callbacks.

  <TopologyCanvas bind:this={view} {nodes} {edges} {pins} selected={id} matches={set}
    onnodeclick={(n, e) => …} onedgeclick={(e) => …} onemptyclick={() => …}
    onnodedblclick={(n) => …} onpinschange={(pins) => …} />
  view.zoomIn() / zoomOut() / fit() / center(id) / unpin(id) / unpinAll() / relayout()
-->
<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import type { GraphEdge, GraphNode } from '$lib/api';
	import { deviceTypeName, relationKindLabel, stateLabel, label as lbl } from '$lib/utils/labels';
	import { TopologyEngine, edgeLabel, readPalette, type HoverTarget, type Pins } from './graph';

	interface Props {
		nodes: GraphNode[];
		edges: GraphEdge[];
		/** initial pinned positions */
		pins: Pins;
		selected?: string | null;
		selectedEdge?: string | null;
		matches?: Set<string> | null;
		connectMode?: boolean;
		connectFrom?: string | null;
		/** accessible description of the graph */
		label: string;
		class?: string;
		onnodeclick?: (node: GraphNode, e: MouseEvent) => void;
		onedgeclick?: (edge: GraphEdge, e: MouseEvent) => void;
		onemptyclick?: () => void;
		onnodedblclick?: (node: GraphNode) => void;
		onpinschange?: (pins: Pins) => void;
	}

	let {
		nodes,
		edges,
		pins,
		selected = null,
		selectedEdge = null,
		matches = null,
		connectMode = false,
		connectFrom = null,
		label,
		class: klass = '',
		onnodeclick,
		onedgeclick,
		onemptyclick,
		onnodedblclick,
		onpinschange
	}: Props = $props();

	let wrap: HTMLDivElement | null = $state(null);
	let canvas: HTMLCanvasElement | null = $state(null);
	let engine: TopologyEngine | null = $state(null);
	let hover = $state<{ target: HoverTarget; x: number; y: number } | null>(null);
	let size = $state({ w: 0, h: 0 });

	const nodeLabel = (id: string) => nodes.find((n) => n.id === id)?.label ?? id;

	onMount(() => {
		if (!canvas || !wrap) return;
		const eng = new TopologyEngine(canvas, readPalette(), {
			onHover: (target, x, y) => {
				hover = target ? { target, x, y } : null;
			},
			onClickNode: (n, e) => onnodeclick?.(n.data, e),
			onClickEdge: (l, e) => onedgeclick?.(l.data, e),
			onClickEmpty: () => onemptyclick?.(),
			onDoubleClickNode: (n) => onnodedblclick?.(n.data),
			onPinsChange: (p) => onpinschange?.(p)
		});
		engine = eng;

		const ro = new ResizeObserver((entries) => {
			const r = entries[0]?.contentRect;
			if (!r) return;
			size = { w: r.width, h: r.height };
			eng.resize(r.width, r.height);
		});
		ro.observe(wrap);

		// theme switches (class on <html>) → re-read the CSS variables
		const mo = new MutationObserver(() => eng.setPalette(readPalette()));
		mo.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style'] });

		// devicePixelRatio changes (moving the window to another screen, browser zoom)
		let mq: MediaQueryList | null = null;
		const onDpr = () => {
			const r = wrap?.getBoundingClientRect();
			if (r) eng.resize(r.width, r.height);
			watchDpr();
		};
		const watchDpr = () => {
			mq?.removeEventListener('change', onDpr);
			mq = window.matchMedia(`(resolution: ${window.devicePixelRatio || 1}dppx)`);
			mq.addEventListener('change', onDpr);
		};
		watchDpr();

		return () => {
			ro.disconnect();
			mo.disconnect();
			mq?.removeEventListener('change', onDpr);
			eng.destroy();
			engine = null;
		};
	});

	$effect(() => {
		const n = nodes;
		const e = edges;
		if (!engine) return;
		untrack(() => engine?.setData(n, e, pins));
	});

	$effect(() => {
		engine?.setState({ selected, selectedEdge, matches, connectMode, connectFrom });
	});

	export function zoomIn() {
		engine?.zoomBy(1.5);
	}
	export function zoomOut() {
		engine?.zoomBy(1 / 1.5);
	}
	export function fit() {
		engine?.fit(true);
	}
	export function center(id: string) {
		engine?.center(id);
	}
	export function unpin(id: string) {
		engine?.unpin(id);
	}
	export function unpinAll() {
		engine?.unpinAll();
	}
	export function relayout() {
		engine?.relayout();
	}
	export function isPinned(id: string): boolean {
		return engine?.isPinned(id) ?? false;
	}

	function onKey(e: KeyboardEvent) {
		if (!engine || e.altKey || e.ctrlKey || e.metaKey) return;
		const step = e.shiftKey ? 160 : 60;
		switch (e.key) {
			case 'ArrowLeft':
				engine.panBy(step, 0);
				break;
			case 'ArrowRight':
				engine.panBy(-step, 0);
				break;
			case 'ArrowUp':
				engine.panBy(0, step);
				break;
			case 'ArrowDown':
				engine.panBy(0, -step);
				break;
			case '+':
			case '=':
				engine.zoomBy(1.4);
				break;
			case '-':
				engine.zoomBy(1 / 1.4);
				break;
			case '0':
				engine.fit(true);
				break;
			default:
				return;
		}
		e.preventDefault();
	}

	// tooltip placement: next to the pointer, flipped at the right/bottom edge
	const tipStyle = $derived.by(() => {
		if (!hover) return '';
		const flipX = hover.x > size.w - 260;
		const flipY = hover.y > size.h - 150;
		const x = flipX ? `right:${Math.max(4, size.w - hover.x + 14)}px` : `left:${hover.x + 14}px`;
		const y = flipY ? `bottom:${Math.max(4, size.h - hover.y + 14)}px` : `top:${hover.y + 14}px`;
		return `${x};${y}`;
	});
</script>

<div bind:this={wrap} class="relative h-full w-full overflow-hidden {klass}">
	<canvas
		bind:this={canvas}
		tabindex="0"
		aria-label={label}
		class="absolute inset-0 block h-full w-full touch-none select-none focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-focus"
		onkeydown={onKey}
	></canvas>

	{#if hover}
		{@const t = hover.target}
		<div
			class="pointer-events-none absolute z-10 max-w-64 rounded-md border border-border bg-surface px-3 py-2 text-xs shadow-md"
			style={tipStyle}
		>
			{#if t.node}
				{@const n = t.node.data}
				<div class="truncate text-sm font-semibold text-fg">{n.label}</div>
				{#if n.ip}<div class="mono text-fg-muted">{n.ip}</div>{/if}
				<dl class="mt-1.5 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5">
					{#if n.kind === 'container'}
						<dt class="text-fg-subtle">Typ</dt>
						<dd class="text-fg">Container</dd>
						{#if n.image}
							<dt class="text-fg-subtle">Image</dt>
							<dd class="mono truncate text-fg">{n.image}</dd>
						{/if}
					{:else}
						<dt class="text-fg-subtle">Typ</dt>
						<dd class="text-fg">{deviceTypeName(n.type)}</dd>
						<dt class="text-fg-subtle">Hersteller</dt>
						<dd class="truncate text-fg">{n.vendor || '–'}</dd>
					{/if}
					<dt class="text-fg-subtle">Status</dt>
					<dd class="text-fg">
						<span class={n.online ? 'text-online' : 'text-fg-muted'}>
							{n.kind === 'container' ? (n.online ? 'läuft' : 'gestoppt') : n.online ? 'Online' : 'Offline'}
						</span>{#if n.state && n.kind !== 'container'}{' · '}{lbl(stateLabel, n.state)}{/if}
					</dd>
				</dl>
				<div class="mt-1.5 text-fg-subtle">
					{#if connectMode}
						Klicken zum Verbinden
					{:else if n.id === selected && n.deviceId}
						Erneut klicken: Gerät öffnen
					{:else}
						Klicken für Details
					{/if}
				</div>
			{:else if t.link}
				{@const e = t.link.data}
				<div class="font-semibold text-fg">{lbl(relationKindLabel, e.kind)}</div>
				<div class="mt-0.5 text-fg-muted">
					{nodeLabel(e.source)}{#if e.parentPort}{' '}<span class="mono">({e.parentPort})</span>{/if}
					→ {nodeLabel(e.target)}{#if e.childPort}{' '}<span class="mono">({e.childPort})</span>{/if}
				</div>
				{#if edgeLabel(e)}<div class="mt-0.5 text-fg-muted">„{edgeLabel(e)}“</div>{/if}
				<div class="mt-1 text-fg-subtle">
					{e.origin === 'manual' ? 'Manuell angelegt' : `Quelle: ${e.origin}`} · Klicken für Details
				</div>
			{/if}
		</div>
	{/if}
</div>
