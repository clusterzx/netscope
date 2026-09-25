// Topology graph engine: d3-force simulation + Canvas 2D renderer with zoom/pan, node dragging
// (pins), hover/click hit testing and pinch zoom. Framework independent – the Svelte wrapper
// (TopologyCanvas.svelte) feeds data and view state and renders tooltips and panels.
//
// Performance notes (500 nodes / 1000 edges): the simulation is ticked from our own rAF loop
// (d3's internal timer is stopped) and the loop only runs while something moves (simulation,
// drag, transform animation) – an idle graph costs nothing. Edges are stroked in one path per
// kind, nodes/edges outside the viewport are culled, labels are only drawn when zoomed in.
import {
	forceCollide,
	forceLink,
	forceManyBody,
	forceSimulation,
	forceX,
	forceY,
	type ForceLink,
	type Simulation,
	type SimulationLinkDatum,
	type SimulationNodeDatum
} from 'd3-force';
import type { GraphEdge, GraphNode } from '$lib/api';
import { icons, type IconName } from '$lib/components/ui/icons';
import { radialLayout } from './radial';

/** force: free d3-force layout (nodes can be pinned); radial: children on rings around their parent */
export type LayoutMode = 'force' | 'radial';

// ---------------------------------------------------------------- visual encoding

export interface EdgeStyle {
	/** CSS variable of the stroke colour */
	color: string;
	/** dash pattern in screen px ([] = solid) */
	dash: number[];
	/** stroke width in screen px at zoom 1 */
	width: number;
}

export const EDGE_STYLES: Record<string, EdgeStyle> = {
	switch_port: { color: '--chart-1', dash: [], width: 1.8 },
	lldp: { color: '--chart-3', dash: [7, 4], width: 1.6 },
	wireless: { color: '--chart-1', dash: [1.5, 3.5], width: 1.7 },
	l3: { color: '--fg-subtle', dash: [4, 4], width: 1.1 },
	runs_on: { color: '--chart-2', dash: [10, 3, 2, 3], width: 1.6 },
	manual: { color: '--chart-4', dash: [], width: 2.2 },
	container: { color: '--chart-2', dash: [2, 2.5], width: 1.2 }
};
/** legend / drawing order */
export const EDGE_KINDS = ['switch_port', 'lldp', 'wireless', 'l3', 'runs_on', 'manual', 'container'];
const FALLBACK_EDGE: EdgeStyle = { color: '--fg-muted', dash: [], width: 1.2 };

export function edgeStyle(kind: string): EdgeStyle {
	return EDGE_STYLES[kind] ?? FALLBACK_EDGE;
}

/** The edge's own label ('' when the API only filled it with the parent port as a fallback). */
export function edgeLabel(e: GraphEdge): string {
	const l = e.label ?? '';
	return l && l !== e.parentPort ? l : '';
}

const TYPE_ICON: Record<string, IconName> = {
	router: 'router',
	switch: 'network',
	'access-point': 'wifi',
	firewall: 'shield',
	server: 'server',
	hypervisor: 'layers',
	vm: 'box',
	container: 'package',
	nas: 'disk',
	desktop: 'monitor',
	laptop: 'laptop',
	phone: 'phone',
	tablet: 'tablet',
	tv: 'tv',
	'media-player': 'play',
	speaker: 'speaker',
	printer: 'printer',
	camera: 'camera',
	'smart-home': 'home',
	iot: 'cpu',
	'game-console': 'gamepad',
	ups: 'battery',
	other: 'help'
};

/** Glyph of a device type (null: untyped → plain dot). */
export function typeIcon(type: string | undefined | null): IconName | null {
	return type ? (TYPE_ICON[type] ?? null) : null;
}

const INFRA = new Set(['router', 'switch', 'firewall', 'access-point', 'hypervisor']);

const PALETTE_VARS = [
	'--surface',
	'--fg',
	'--fg-muted',
	'--fg-subtle',
	'--accent',
	'--online',
	'--offline',
	'--unknown',
	'--chart-1',
	'--chart-2',
	'--chart-3',
	'--chart-4'
];

/** Resolved theme colours (CSS variable → value) plus the UI font. */
export type Palette = Record<string, string>;

/** Reads the current theme colours from the CSS variables on <html>. */
export function readPalette(): Palette {
	const cs = getComputedStyle(document.documentElement);
	const p: Palette = {};
	for (const v of PALETTE_VARS) p[v] = cs.getPropertyValue(v).trim() || '#888888';
	p.font = getComputedStyle(document.body).fontFamily || 'system-ui, sans-serif';
	return p;
}

// ---------------------------------------------------------------- model

export interface TNode extends SimulationNodeDatum {
	id: string;
	data: GraphNode;
	r: number;
	degree: number;
	icon: IconName | null;
	container: boolean;
	infra: boolean;
}

export interface TLink extends SimulationLinkDatum<TNode> {
	id: string;
	data: GraphEdge;
	source: TNode;
	target: TNode;
}

export type HoverTarget = { node: TNode; link?: undefined } | { link: TLink; node?: undefined };

/** Pinned node positions (graph coordinates) by node id. */
export type Pins = Record<string, [number, number]>;

export interface ViewState {
	selected: string | null;
	selectedEdge: string | null;
	/** search matches (null = no search active) */
	matches: Set<string> | null;
	connectMode: boolean;
	connectFrom: string | null;
}

export interface EngineCallbacks {
	/** hover changed or moved; x/y relative to the canvas (CSS px) */
	onHover: (target: HoverTarget | null, x: number, y: number) => void;
	onClickNode: (node: TNode, e: MouseEvent) => void;
	onClickEdge: (link: TLink, e: MouseEvent) => void;
	onClickEmpty: (e: MouseEvent) => void;
	onDoubleClickNode: (node: TNode) => void;
	/** a node was dragged (and is now pinned) */
	onPinsChange: (pins: Pins) => void;
}

type Transform = { k: number; x: number; y: number };

const MIN_K = 0.08;
const MAX_K = 4;

function radius(n: TNode): number {
	if (n.container) return 6.5;
	const t = n.data.type ?? '';
	const base = INFRA.has(t) ? 13 : t === 'server' || t === 'nas' || t === 'vm' ? 10.5 : 9;
	return base + Math.min(6, Math.sqrt(n.degree) * 0.9);
}

function linkDistance(l: TLink): number {
	if (l.data.kind === 'container') return 26 + l.source.r;
	const base = l.data.kind === 'runs_on' ? 55 : l.data.kind === 'l3' ? 85 : 70;
	// hubs (a router with dozens of l3 edges) need a larger ring so their leaves do not pile up
	const hub = Math.max(l.source.degree, l.target.degree);
	return base + Math.min(260, Math.max(0, hub - 5) * 4.2);
}

/** centering force per node: isolated nodes are pulled in harder so they do not drift off */
const gravity = (n: TNode) => (n.degree === 0 ? 0.14 : 0.04);

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));
const ease = (t: number) => (t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2);

function segDist2(px: number, py: number, ax: number, ay: number, bx: number, by: number): number {
	const dx = bx - ax;
	const dy = by - ay;
	const len = dx * dx + dy * dy;
	let t = len ? ((px - ax) * dx + (py - ay) * dy) / len : 0;
	t = clamp(t, 0, 1);
	const cx = ax + t * dx - px;
	const cy = ay + t * dy - py;
	return cx * cx + cy * cy;
}

// ---------------------------------------------------------------- engine

export class TopologyEngine {
	nodes: TNode[] = [];
	links: TLink[] = [];

	#canvas: HTMLCanvasElement;
	#ctx: CanvasRenderingContext2D;
	#cb: EngineCallbacks;
	#palette: Palette;
	#state: ViewState = {
		selected: null,
		selectedEdge: null,
		matches: null,
		connectMode: false,
		connectFrom: null
	};

	#sim: Simulation<TNode, TLink>;
	#linkForce: ForceLink<TNode, TLink>;
	#byId = new Map<string, TNode>();
	#adj = new Map<string, TLink[]>();
	#byKind = new Map<string, TLink[]>();
	#signature = '';

	#w = 0;
	#h = 0;
	#dpr = 1;
	#t: Transform = { k: 1, x: 0, y: 0 };
	#anim: { from: Transform; to: Transform; start: number; dur: number } | null = null;
	/** fit the view when the layout settles until the user pans/zooms */
	#autoFit = true;

	#frame = 0;
	#simRunning = false;
	#hover: HoverTarget | null = null;
	#pointer: { x: number; y: number } | null = null;
	#gesture: {
		id: number;
		sx: number;
		sy: number;
		tx: number;
		ty: number;
		node: TNode | null;
		moved: boolean;
	} | null = null;
	#pointers = new Map<number, { x: number; y: number }>();
	#pinch: { dist: number; k: number; gx: number; gy: number } | null = null;
	#suppressClick = false;
	#iconPaths = new Map<IconName, Path2D>();
	#labelOrder: TNode[] = [];
	#labels = new Map<string, { src: string; text: string; w: number }>();
	#occ = new Uint8Array(0);
	#layout: LayoutMode = 'force';
	/** children in the radial layout tree (dragging moves them along) */
	#tree = new Map<string, TNode[]>();
	/** animated move of all nodes to new positions (radial layout) */
	#tween: {
		from: Map<TNode, [number, number]>;
		to: Map<TNode, [number, number]>;
		start: number;
		dur: number;
	} | null = null;
	#reduceMotion = false;
	#destroyed = false;

	constructor(canvas: HTMLCanvasElement, palette: Palette, cb: EngineCallbacks) {
		this.#canvas = canvas;
		const ctx = canvas.getContext('2d');
		if (!ctx) throw new Error('Canvas 2D nicht verfügbar');
		this.#ctx = ctx;
		this.#palette = palette;
		this.#cb = cb;
		this.#reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

		this.#linkForce = forceLink<TNode, TLink>([])
			.id((d) => d.id)
			.distance(linkDistance);
		this.#sim = forceSimulation<TNode, TLink>([])
			.force('link', this.#linkForce)
			.force(
				'charge',
				forceManyBody<TNode>()
					.strength((d) => (d.container ? -45 : -190))
					.distanceMax(520)
					.theta(0.9)
			)
			.force('collide', forceCollide<TNode>((d) => d.r + 5).iterations(1))
			.force('x', forceX<TNode>(0).strength(gravity))
			.force('y', forceY<TNode>(0).strength(gravity))
			.stop();

		canvas.addEventListener('pointerdown', this.#onPointerDown);
		canvas.addEventListener('pointermove', this.#onPointerMove);
		canvas.addEventListener('pointerup', this.#onPointerUp);
		canvas.addEventListener('pointercancel', this.#onPointerUp);
		canvas.addEventListener('pointerleave', this.#onPointerLeave);
		canvas.addEventListener('wheel', this.#onWheel, { passive: false });
		canvas.addEventListener('click', this.#onClick);
		canvas.addEventListener('dblclick', this.#onDblClick);
	}

	destroy() {
		this.#destroyed = true;
		if (this.#frame) cancelAnimationFrame(this.#frame);
		this.#frame = 0;
		this.#sim.stop();
		const c = this.#canvas;
		c.removeEventListener('pointerdown', this.#onPointerDown);
		c.removeEventListener('pointermove', this.#onPointerMove);
		c.removeEventListener('pointerup', this.#onPointerUp);
		c.removeEventListener('pointercancel', this.#onPointerUp);
		c.removeEventListener('pointerleave', this.#onPointerLeave);
		c.removeEventListener('wheel', this.#onWheel);
		c.removeEventListener('click', this.#onClick);
		c.removeEventListener('dblclick', this.#onDblClick);
	}

	// ------------------------------------------------------------ data & state

	/**
	 * Replaces the graph. Known nodes keep their position (live refreshes do not reshuffle
	 * the layout); the simulation is only reheated when nodes or edges were added/removed.
	 */
	setData(nodes: GraphNode[], edges: GraphEdge[], pins: Pins) {
		const prev = this.#byId;
		const next = new Map<string, TNode>();
		const list: TNode[] = [];
		for (const d of nodes) {
			if (next.has(d.id)) continue;
			let n = prev.get(d.id);
			if (!n) {
				n = { id: d.id, data: d, r: 9, degree: 0, icon: null, container: false, infra: false };
				const pin = pins[d.id];
				if (pin) {
					n.x = n.fx = pin[0];
					n.y = n.fy = pin[1];
				}
			}
			n.data = d;
			n.degree = 0;
			n.icon = typeIcon(d.type);
			n.container = d.kind === 'container';
			n.infra = INFRA.has(d.type ?? '');
			next.set(d.id, n);
			list.push(n);
		}
		const links: TLink[] = [];
		const seen = new Set<string>();
		for (const e of edges) {
			const s = next.get(e.source);
			const t = next.get(e.target);
			if (!s || !t || s === t || seen.has(e.id)) continue;
			seen.add(e.id);
			s.degree++;
			t.degree++;
			links.push({ id: e.id, data: e, source: s, target: t });
		}
		for (const n of list) n.r = radius(n);

		const adj = new Map<string, TLink[]>();
		const byKind = new Map<string, TLink[]>();
		for (const l of links) {
			for (const id of [l.source.id, l.target.id]) {
				const a = adj.get(id);
				if (a) a.push(l);
				else adj.set(id, [l]);
			}
			const k = byKind.get(l.data.kind);
			if (k) k.push(l);
			else byKind.set(l.data.kind, [l]);
		}

		// place new nodes next to an already placed neighbour
		const hadLayout = prev.size > 0;
		for (const n of list) {
			if (n.x !== undefined && n.y !== undefined) continue;
			const nb = (adj.get(n.id) ?? [])
				.map((l) => (l.source === n ? l.target : l.source))
				.find((m) => m.x !== undefined && m.y !== undefined);
			if (nb) {
				const a = Math.random() * Math.PI * 2;
				n.x = nb.x! + Math.cos(a) * (nb.r + 30);
				n.y = nb.y! + Math.sin(a) * (nb.r + 30);
			} else if (hadLayout) {
				n.x = (Math.random() - 0.5) * 200;
				n.y = (Math.random() - 0.5) * 200;
			}
		}

		this.nodes = list;
		this.links = links;
		// label priority: infrastructure and well connected nodes first
		this.#labelOrder = [...list].sort(
			(a, b) =>
				Number(b.infra) - Number(a.infra) || b.degree - a.degree || a.data.label.localeCompare(b.data.label)
		);
		this.#byId = next;
		this.#adj = adj;
		this.#byKind = byKind;

		const sig = list.map((n) => n.id).join(',') + '|' + links.map((l) => l.id).join(',');
		if (sig !== this.#signature) {
			const added = list.filter((n) => !prev.has(n.id)).length;
			this.#signature = sig;
			this.#sim.nodes(list);
			this.#linkForce.links(links);
			if (this.#layout === 'radial') {
				this.#applyRadial(hadLayout && added <= list.length / 2);
			} else if (!hadLayout || added > list.length / 2) {
				// fresh layout: settle most of it synchronously, then animate the rest
				this.#sim.alpha(1);
				const start = performance.now();
				while (this.#sim.alpha() > 0.12 && performance.now() - start < 180) this.#sim.tick();
				this.#autoFit = true;
				this.fit(false);
			} else {
				this.#sim.alpha(Math.max(this.#sim.alpha(), 0.3));
			}
			this.#simRunning = this.#layout === 'force';
		}
		if (this.#hover?.node && !next.has(this.#hover.node.id)) this.#setHover(null, 0, 0);
		this.#requestDraw();
	}

	setState(s: Partial<ViewState>) {
		Object.assign(this.#state, s);
		if (!this.#state.connectMode) this.#state.connectFrom = null;
		this.#updateCursor();
		this.#requestDraw();
	}

	setPalette(p: Palette) {
		if (p.font !== this.#palette.font) this.#labels.clear();
		this.#palette = p;
		this.#requestDraw();
	}

	node(id: string): TNode | undefined {
		return this.#byId.get(id);
	}

	/** Edges touching a node. */
	edgesOf(id: string): TLink[] {
		return this.#adj.get(id) ?? [];
	}

	isPinned(id: string): boolean {
		const n = this.#byId.get(id);
		return !!n && n.fx !== null && n.fx !== undefined;
	}

	pins(): Pins {
		const out: Pins = {};
		for (const n of this.nodes) {
			if (n.fx !== null && n.fx !== undefined && n.fy !== null && n.fy !== undefined)
				out[n.id] = [Math.round(n.fx * 10) / 10, Math.round(n.fy * 10) / 10];
		}
		return out;
	}

	unpin(id: string) {
		const n = this.#byId.get(id);
		if (!n) return;
		n.fx = n.fy = null;
		this.#kick(0.2);
		this.#cb.onPinsChange(this.pins());
	}

	unpinAll() {
		for (const n of this.nodes) n.fx = n.fy = null;
		this.#kick(0.5);
		this.#cb.onPinsChange(this.pins());
	}

	/** Re-runs the layout (pinned nodes stay) and fits the view afterwards. */
	relayout() {
		this.#autoFit = true;
		if (this.#layout === 'radial') this.#applyRadial(true);
		else this.#kick(1);
	}

	get layout(): LayoutMode {
		return this.#layout;
	}

	/** Switches between the free force layout and the rings around each parent. */
	setLayout(mode: LayoutMode) {
		if (mode === this.#layout) return;
		this.#layout = mode;
		this.#autoFit = true;
		if (mode === 'radial') {
			this.#simRunning = false;
			if (this.nodes.length) this.#applyRadial(true);
		} else {
			this.#tween = null;
			this.#tree = new Map();
			this.#kick(0.6);
		}
		this.#requestDraw();
	}

	/** Computes the radial layout and moves the nodes there (animated unless reduced motion). */
	#applyRadial(animate: boolean) {
		const { pos, kids } = radialLayout(this.nodes, this.links);
		this.#tree = kids;
		const from = new Map<TNode, [number, number]>();
		const to = new Map<TNode, [number, number]>();
		for (const n of this.nodes) {
			const p = pos.get(n.id);
			if (!p) continue;
			if (!animate || this.#reduceMotion || n.x === undefined || n.y === undefined) {
				n.x = p[0];
				n.y = p[1];
			} else {
				from.set(n, [n.x, n.y]);
				to.set(n, p);
			}
			n.vx = n.vy = 0;
		}
		this.#tween = to.size ? { from, to, start: performance.now(), dur: 650 } : null;
		if (!this.#tween && this.#autoFit) this.fit(false);
		this.#requestDraw();
	}

	// ------------------------------------------------------------ view

	/** Sets the canvas size (CSS px); handles devicePixelRatio. */
	resize(w: number, h: number) {
		const dpr = window.devicePixelRatio || 1;
		if (w <= 0 || h <= 0) return;
		if (this.#w && this.#h) {
			this.#t.x += (w - this.#w) / 2;
			this.#t.y += (h - this.#h) / 2;
		} else {
			this.#t.x = w / 2;
			this.#t.y = h / 2;
		}
		this.#w = w;
		this.#h = h;
		this.#dpr = dpr;
		this.#canvas.width = Math.max(1, Math.round(w * dpr));
		this.#canvas.height = Math.max(1, Math.round(h * dpr));
		if (this.#autoFit && this.nodes.length) this.fit(false);
		// setting width clears the canvas – redraw synchronously to avoid a blank frame
		this.#draw();
	}

	get zoom(): number {
		return this.#t.k;
	}

	zoomBy(factor: number) {
		this.#autoFit = false;
		const to = this.#zoomed(this.#w / 2, this.#h / 2, factor);
		this.#animateTo(to, 220);
	}

	panBy(dx: number, dy: number) {
		this.#autoFit = false;
		this.#anim = null;
		this.#t.x += dx;
		this.#t.y += dy;
		this.#requestDraw();
	}

	/** Fits all nodes into the view. */
	fit(animate = true) {
		if (!this.nodes.length || !this.#w) return;
		let x0 = Infinity,
			y0 = Infinity,
			x1 = -Infinity,
			y1 = -Infinity;
		for (const n of this.nodes) {
			const x = n.x ?? 0;
			const y = n.y ?? 0;
			x0 = Math.min(x0, x - n.r);
			y0 = Math.min(y0, y - n.r - 4);
			x1 = Math.max(x1, x + n.r);
			y1 = Math.max(y1, y + n.r + 16);
		}
		const pad = Math.min(48, Math.min(this.#w, this.#h) * 0.08);
		const k = clamp(
			Math.min((this.#w - 2 * pad) / Math.max(1, x1 - x0), (this.#h - 2 * pad) / Math.max(1, y1 - y0)),
			MIN_K,
			1.6
		);
		const to = { k, x: this.#w / 2 - k * ((x0 + x1) / 2), y: this.#h / 2 - k * ((y0 + y1) / 2) };
		if (animate) this.#animateTo(to, 400);
		else {
			this.#anim = null;
			this.#t = to;
			this.#requestDraw();
		}
	}

	/** Centers a node (zooming in if the view is far out). */
	center(id: string) {
		const n = this.#byId.get(id);
		if (!n || !this.#w) return;
		this.#autoFit = false;
		const k = Math.max(this.#t.k, 1.1);
		this.#animateTo({ k, x: this.#w / 2 - k * (n.x ?? 0), y: this.#h / 2 - k * (n.y ?? 0) }, 450);
	}

	/** Screen position (CSS px, relative to the canvas) of a node. */
	screenPos(id: string): { x: number; y: number } | null {
		const n = this.#byId.get(id);
		if (!n) return null;
		return { x: (n.x ?? 0) * this.#t.k + this.#t.x, y: (n.y ?? 0) * this.#t.k + this.#t.y };
	}

	#zoomed(cx: number, cy: number, factor: number): Transform {
		const t = this.#anim ? this.#anim.to : this.#t;
		const k = clamp(t.k * factor, MIN_K, MAX_K);
		return { k, x: cx - (cx - t.x) * (k / t.k), y: cy - (cy - t.y) * (k / t.k) };
	}

	#animateTo(to: Transform, dur: number) {
		if (this.#reduceMotion || dur <= 0) {
			this.#anim = null;
			this.#t = to;
		} else {
			this.#anim = { from: { ...this.#t }, to, start: performance.now(), dur };
		}
		this.#requestDraw();
	}

	// ------------------------------------------------------------ loop

	#kick(alpha?: number) {
		if (this.#layout === 'radial') {
			// positions come from the rings, not from the simulation
			this.#requestDraw();
			return;
		}
		if (alpha !== undefined) this.#sim.alpha(Math.max(this.#sim.alpha(), alpha));
		this.#simRunning = true;
		this.#requestDraw();
	}

	#requestDraw() {
		if (!this.#frame && !this.#destroyed) this.#frame = requestAnimationFrame(this.#loop);
	}

	#loop = (now: number) => {
		this.#frame = 0;
		let more = false;
		if (this.#tween) {
			const tw = this.#tween;
			const p = clamp((now - tw.start) / tw.dur, 0, 1);
			const e = ease(p);
			for (const [n, [fx, fy]] of tw.from) {
				const [tx, ty] = tw.to.get(n)!;
				n.x = fx + (tx - fx) * e;
				n.y = fy + (ty - fy) * e;
			}
			if (p >= 1) {
				this.#tween = null;
				if (this.#autoFit) this.fit(true);
			} else more = true;
		}
		if (this.#simRunning) {
			this.#sim.tick();
			if (this.#sim.alpha() < this.#sim.alphaMin()) {
				this.#simRunning = false;
				if (this.#autoFit) this.fit(true);
			} else more = true;
		}
		if (this.#anim) {
			const a = this.#anim;
			const p = clamp((now - a.start) / a.dur, 0, 1);
			const e = ease(p);
			// interpolate in "scale space" so zooming feels even
			const k = a.from.k * Math.pow(a.to.k / a.from.k, e);
			this.#t = {
				k,
				x: a.from.x + (a.to.x - a.from.x) * e,
				y: a.from.y + (a.to.y - a.from.y) * e
			};
			if (p >= 1) {
				this.#t = a.to;
				this.#anim = null;
			} else more = true;
		}
		this.#draw();
		if (more) this.#requestDraw();
	};

	// ------------------------------------------------------------ rendering

	#iconPath(name: IconName): Path2D {
		let p = this.#iconPaths.get(name);
		if (!p) {
			p = new Path2D(icons[name].join(' '));
			this.#iconPaths.set(name, p);
		}
		return p;
	}

	#focus(): { center: TNode | null; ids: Set<string>; alpha: number } | null {
		const hovered = this.#hover?.node ?? null;
		const hoveredLink = this.#hover?.link ?? null;
		if (hoveredLink) {
			return { center: null, ids: new Set([hoveredLink.source.id, hoveredLink.target.id]), alpha: 0.3 };
		}
		const center = hovered ?? (this.#state.selected ? (this.#byId.get(this.#state.selected) ?? null) : null);
		if (center) {
			const ids = new Set([center.id]);
			for (const l of this.#adj.get(center.id) ?? []) {
				ids.add(l.source.id);
				ids.add(l.target.id);
			}
			return { center, ids, alpha: hovered ? 0.22 : 0.4 };
		}
		if (this.#state.matches) return { center: null, ids: this.#state.matches, alpha: 0.16 };
		return null;
	}

	#draw() {
		const ctx = this.#ctx;
		const pal = this.#palette;
		const dpr = this.#dpr;
		const { k, x: tx, y: ty } = this.#t;
		const st = this.#state;
		ctx.setTransform(1, 0, 0, 1, 0, 0);
		ctx.globalAlpha = 1;
		ctx.clearRect(0, 0, this.#canvas.width, this.#canvas.height);
		if (!this.nodes.length) return;

		const base = () => ctx.setTransform(dpr * k, 0, 0, dpr * k, dpr * tx, dpr * ty);
		base();
		const m = 40 / k;
		const vx0 = -tx / k - m;
		const vy0 = -ty / k - m;
		const vx1 = (this.#w - tx) / k + m;
		const vy1 = (this.#h - ty) / k + m;
		const culled = (a: TNode, b: TNode) =>
			(a.x! < vx0 && b.x! < vx0) ||
			(a.x! > vx1 && b.x! > vx1) ||
			(a.y! < vy0 && b.y! < vy0) ||
			(a.y! > vy1 && b.y! > vy1);
		const focus = this.#focus();
		const sw = clamp(k, 0.6, 1.6) / k; // screen px → graph units (edges grow a little when zoomed in)
		const px = 1 / k;

		// --- edges (one path per kind)
		const center = focus?.center ?? null;
		const hoverLink = this.#hover?.link ?? null;
		ctx.lineCap = 'round';
		for (const [kind, list] of this.#byKind) {
			const es = edgeStyle(kind);
			ctx.strokeStyle = pal[es.color] ?? pal['--fg-muted'];
			ctx.lineWidth = es.width * sw;
			ctx.setLineDash(es.dash.map((d) => d * sw));
			ctx.globalAlpha = focus ? 0.12 : kind === 'l3' ? 0.7 : 0.9;
			ctx.beginPath();
			for (const l of list) {
				if (center && (l.source === center || l.target === center)) continue;
				if (culled(l.source, l.target)) continue;
				ctx.moveTo(l.source.x!, l.source.y!);
				ctx.lineTo(l.target.x!, l.target.y!);
			}
			ctx.stroke();
		}
		// focused edges (around the hovered/selected node), the selected and the hovered edge
		const strong: TLink[] = [];
		if (center) strong.push(...(this.#adj.get(center.id) ?? []));
		for (const id of [st.selectedEdge, hoverLink?.id]) {
			const l = id ? this.links.find((x) => x.id === id) : undefined;
			if (l && !strong.includes(l)) strong.push(l);
		}
		for (const l of strong) {
			const es = edgeStyle(l.data.kind);
			const active = l.id === st.selectedEdge || l === hoverLink;
			if (active) {
				ctx.setLineDash([]);
				ctx.globalAlpha = 0.28;
				ctx.strokeStyle = pal['--accent'];
				ctx.lineWidth = (es.width + 7) * sw;
				ctx.beginPath();
				ctx.moveTo(l.source.x!, l.source.y!);
				ctx.lineTo(l.target.x!, l.target.y!);
				ctx.stroke();
			}
			ctx.globalAlpha = 1;
			ctx.strokeStyle = pal[es.color] ?? pal['--fg-muted'];
			ctx.lineWidth = es.width * sw * (active ? 2 : 1.6);
			ctx.setLineDash(es.dash.map((d) => d * sw * 1.3));
			ctx.beginPath();
			ctx.moveTo(l.source.x!, l.source.y!);
			ctx.lineTo(l.target.x!, l.target.y!);
			ctx.stroke();
		}

		// --- rubber band while creating a connection
		const from = st.connectMode && st.connectFrom ? this.#byId.get(st.connectFrom) : undefined;
		if (from && this.#pointer) {
			ctx.globalAlpha = 0.9;
			ctx.strokeStyle = pal['--accent'];
			ctx.lineWidth = 2 * px;
			ctx.setLineDash([6 * px, 4 * px]);
			ctx.beginPath();
			ctx.moveTo(from.x!, from.y!);
			ctx.lineTo((this.#pointer.x - tx) / k, (this.#pointer.y - ty) / k);
			ctx.stroke();
		}
		ctx.setLineDash([]);

		// --- nodes
		const showIcons = k * 9 >= 6.5;
		const TAU = Math.PI * 2;
		const hoveredId = this.#hover?.node?.id ?? null;
		for (const n of this.nodes) {
			const x = n.x!;
			const y = n.y!;
			const r = n.r;
			if (x + r < vx0 || x - r > vx1 || y + r < vy0 || y - r > vy1) continue;
			const dim = focus && !focus.ids.has(n.id) ? focus.alpha : 1;
			const a = dim * (n.data.state === 'ignored' ? 0.5 : 1);
			const selected = n.id === st.selected;
			const match = st.matches?.has(n.id) ?? false;

			if (selected) {
				ctx.globalAlpha = 0.22;
				ctx.fillStyle = pal['--accent'];
				ctx.beginPath();
				ctx.arc(x, y, r + 9 * px, 0, TAU);
				ctx.fill();
			}
			ctx.globalAlpha = a;
			ctx.fillStyle = n.data.online ? pal['--online'] : pal['--offline'];
			ctx.beginPath();
			if (n.container) ctx.roundRect(x - r, y - r, 2 * r, 2 * r, r * 0.35);
			else ctx.arc(x, y, r, 0, TAU);
			ctx.fill();

			// ring: device not yet marked as known
			let ring = r;
			if (n.data.state === 'unknown') {
				ring = r + 2.4 * px;
				ctx.strokeStyle = pal['--unknown'];
				ctx.lineWidth = 2.2 * px;
				ctx.beginPath();
				ctx.arc(x, y, ring, 0, TAU);
				ctx.stroke();
			}
			if (selected || n.id === hoveredId || match || n.id === st.connectFrom) {
				ctx.strokeStyle = selected || match || n.id === st.connectFrom ? pal['--accent'] : pal['--fg-muted'];
				ctx.lineWidth = (selected ? 2.6 : 2) * px;
				if (n.id === st.connectFrom) ctx.setLineDash([4 * px, 3 * px]);
				ctx.beginPath();
				ctx.arc(x, y, ring + 3.2 * px, 0, TAU);
				ctx.stroke();
				ctx.setLineDash([]);
			}

			if (showIcons && n.icon) {
				const s = r * 1.15;
				const f = (dpr * k * s) / 24;
				ctx.setTransform(f, 0, 0, f, dpr * (tx + k * (x - s / 2)), dpr * (ty + k * (y - s / 2)));
				ctx.strokeStyle = pal['--surface'];
				ctx.lineWidth = 2.3;
				ctx.lineJoin = 'round';
				ctx.stroke(this.#iconPath(n.icon));
				base();
			}
			// pinned marker
			if (k * r >= 7 && this.#layout === 'force' && n.fx !== null && n.fx !== undefined) {
				const pr = 2.6 * px;
				ctx.fillStyle = pal['--fg-muted'];
				ctx.strokeStyle = pal['--surface'];
				ctx.lineWidth = 1.2 * px;
				ctx.beginPath();
				ctx.arc(x + r * 0.78, y - r * 0.78, pr, 0, TAU);
				ctx.fill();
				ctx.stroke();
			}
		}

		// --- labels (screen space, constant size; greedy placement without overlaps)
		ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
		ctx.font = `500 11px ${pal.font}`;
		ctx.textAlign = 'center';
		ctx.textBaseline = 'top';
		ctx.lineJoin = 'round';
		const all = k >= 0.95;
		const important = k >= 0.5;
		const fewMatches = !!st.matches && st.matches.size <= 60;
		const fewFocus = !!focus?.center && focus.ids.size <= 40;
		const cell = 6;
		const cols = Math.ceil(this.#w / cell) + 1;
		const rows = Math.ceil(this.#h / cell) + 1;
		if (this.#occ.length < cols * rows) this.#occ = new Uint8Array(cols * rows);
		else this.#occ.fill(0, 0, cols * rows);
		const occ = this.#occ;
		const place = (x0: number, y0: number, x1: number, y1: number, force: boolean): boolean => {
			const c0 = Math.max(0, Math.floor(x0 / cell));
			const c1 = Math.min(cols - 1, Math.floor(x1 / cell));
			const r0 = Math.max(0, Math.floor(y0 / cell));
			const r1 = Math.min(rows - 1, Math.floor(y1 / cell));
			if (c0 > c1 || r0 > r1) return false;
			if (!force) {
				for (let r = r0; r <= r1; r++) for (let c = c0; c <= c1; c++) if (occ[r * cols + c]) return false;
			}
			for (let r = r0; r <= r1; r++) occ.fill(1, r * cols + c0, r * cols + c1 + 1);
			return true;
		};
		const top: { n: TNode; text: string; sx: number; sy: number }[] = [];
		const draw = (n: TNode, text: string, sx: number, sy: number, strong: boolean) => {
			ctx.globalAlpha = focus && !focus.ids.has(n.id) ? Math.max(focus.alpha, 0.3) : 1;
			ctx.strokeStyle = pal['--surface'];
			ctx.lineWidth = 3.5;
			ctx.strokeText(text, sx, sy);
			ctx.fillStyle = strong ? pal['--fg'] : pal['--fg-muted'];
			ctx.fillText(text, sx, sy);
		};
		// tier 0: always (selected, hovered, connection start, ends of the hovered edge)
		// tier 1: placed first but without overlaps (neighbours of the focus node, search matches)
		// tier 2: everything else when zoomed in far enough (infrastructure first)
		const tier = (n: TNode): number => {
			if (
				n.id === st.selected ||
				n.id === hoveredId ||
				n.id === st.connectFrom ||
				(!!hoverLink && (hoverLink.source === n || hoverLink.target === n))
			)
				return 0;
			if ((fewMatches && st.matches!.has(n.id)) || (fewFocus && focus!.ids.has(n.id))) return 1;
			return all || (important && (n.infra || n.degree >= 5)) ? 2 : 3;
		};
		for (const pass of [0, 1, 2]) {
			for (const n of this.#labelOrder) {
				const x = n.x!;
				const y = n.y!;
				if (x < vx0 || x > vx1 || y < vy0 || y > vy1) continue;
				const t = tier(n);
				if (pass === 0 ? t !== 0 : pass === 1 ? t !== 1 : t !== 2) continue;
				const { text, w } = this.#labelText(n);
				const sx = x * k + tx;
				const sy = y * k + ty + n.r * k + (n.data.state === 'unknown' ? 5 : 3);
				if (!place(sx - w / 2 - 2, sy, sx + w / 2 + 2, sy + 12, t === 0)) continue;
				if (t === 2) draw(n, text, sx, sy, false);
				else top.push({ n, text, sx, sy });
			}
		}
		for (const t of top) draw(t.n, t.text, t.sx, t.sy, true);
		ctx.globalAlpha = 1;
	}

	#labelText(n: TNode): { text: string; w: number } {
		let c = this.#labels.get(n.id);
		if (!c || c.src !== n.data.label) {
			const src = n.data.label;
			const text = src.length > 28 ? src.slice(0, 27) + '…' : src;
			c = { src, text, w: this.#ctx.measureText(text).width };
			this.#labels.set(n.id, c);
		}
		return c;
	}

	// ------------------------------------------------------------ hit testing

	#local(e: MouseEvent): { x: number; y: number } {
		const r = this.#canvas.getBoundingClientRect();
		return { x: e.clientX - r.left, y: e.clientY - r.top };
	}

	nodeAt(sx: number, sy: number): TNode | null {
		const { k, x, y } = this.#t;
		const gx = (sx - x) / k;
		const gy = (sy - y) / k;
		const slop = 4 / k;
		for (let i = this.nodes.length - 1; i >= 0; i--) {
			const n = this.nodes[i];
			const dx = n.x! - gx;
			const dy = n.y! - gy;
			const rr = n.r + slop;
			if (dx * dx + dy * dy <= rr * rr) return n;
		}
		return null;
	}

	edgeAt(sx: number, sy: number): TLink | null {
		const { k, x, y } = this.#t;
		const gx = (sx - x) / k;
		const gy = (sy - y) / k;
		const tol = 5 / k;
		let best: TLink | null = null;
		let bestD = tol * tol;
		for (const l of this.links) {
			const d = segDist2(gx, gy, l.source.x!, l.source.y!, l.target.x!, l.target.y!);
			if (d <= bestD) {
				bestD = d;
				best = l;
			}
		}
		return best;
	}

	#setHover(t: HoverTarget | null, x: number, y: number) {
		const same =
			(t?.node && t.node === this.#hover?.node) ||
			(t?.link && t.link === this.#hover?.link) ||
			(!t && !this.#hover);
		this.#hover = t;
		this.#cb.onHover(t, x, y);
		if (!same) {
			this.#updateCursor();
			this.#requestDraw();
		}
	}

	#updateCursor() {
		const c = this.#canvas;
		if (this.#gesture?.moved) c.style.cursor = 'grabbing';
		else if (this.#hover) c.style.cursor = 'pointer';
		else c.style.cursor = this.#state.connectMode ? 'crosshair' : 'grab';
	}

	// ------------------------------------------------------------ input

	#onPointerDown = (e: PointerEvent) => {
		if (e.pointerType === 'mouse' && e.button !== 0) return;
		const p = this.#local(e);
		this.#pointers.set(e.pointerId, p);
		try {
			this.#canvas.setPointerCapture(e.pointerId);
		} catch {
			// pointer already released
		}
		this.#anim = null;
		if (this.#pointers.size === 2) {
			// second finger: switch from drag/pan to pinch zoom
			if (this.#gesture?.node && this.#gesture.moved) this.#sim.alphaTarget(0);
			this.#gesture = null;
			const [a, b] = [...this.#pointers.values()];
			const { k, x, y } = this.#t;
			const mx = (a.x + b.x) / 2;
			const my = (a.y + b.y) / 2;
			this.#pinch = { dist: Math.hypot(a.x - b.x, a.y - b.y) || 1, k, gx: (mx - x) / k, gy: (my - y) / k };
			this.#autoFit = false;
			return;
		}
		if (this.#pointers.size > 2) return;
		this.#gesture = {
			id: e.pointerId,
			sx: p.x,
			sy: p.y,
			tx: this.#t.x,
			ty: this.#t.y,
			node: this.nodeAt(p.x, p.y),
			moved: false
		};
	};

	#onPointerMove = (e: PointerEvent) => {
		const p = this.#local(e);
		this.#pointer = p;
		if (this.#pointers.has(e.pointerId)) this.#pointers.set(e.pointerId, p);
		if (this.#pinch && this.#pointers.size >= 2) {
			const [a, b] = [...this.#pointers.values()];
			const pc = this.#pinch;
			const k = clamp((pc.k * Math.hypot(a.x - b.x, a.y - b.y)) / pc.dist, MIN_K, MAX_K);
			const mx = (a.x + b.x) / 2;
			const my = (a.y + b.y) / 2;
			this.#t = { k, x: mx - pc.gx * k, y: my - pc.gy * k };
			this.#requestDraw();
			return;
		}
		const g = this.#gesture;
		if (g && g.id === e.pointerId) {
			const dx = p.x - g.sx;
			const dy = p.y - g.sy;
			if (!g.moved) {
				if (dx * dx + dy * dy < 16) return;
				g.moved = true;
				this.#setHover(null, p.x, p.y);
				if (g.node && this.#layout === 'radial') {
					this.#tween = null;
					this.#autoFit = false;
				} else if (g.node) {
					g.node.fx = g.node.x;
					g.node.fy = g.node.y;
					this.#sim.alphaTarget(0.25);
				} else this.#autoFit = false;
			}
			if (g.node && this.#layout === 'radial') {
				// the node takes its whole subtree along
				const { k, x, y } = this.#t;
				const dx = (p.x - x) / k - g.node.x!;
				const dy = (p.y - y) / k - g.node.y!;
				const stack = [g.node];
				while (stack.length) {
					const n = stack.pop()!;
					n.x! += dx;
					n.y! += dy;
					stack.push(...(this.#tree.get(n.id) ?? []));
				}
				this.#requestDraw();
			} else if (g.node) {
				const { k, x, y } = this.#t;
				g.node.fx = (p.x - x) / k;
				g.node.fy = (p.y - y) / k;
				this.#kick();
			} else {
				this.#t.x = g.tx + dx;
				this.#t.y = g.ty + dy;
				this.#requestDraw();
			}
			this.#updateCursor();
			return;
		}
		if (e.pointerType !== 'mouse' && !this.#gesture) return;
		const node = this.nodeAt(p.x, p.y);
		if (node) this.#setHover({ node }, p.x, p.y);
		else {
			const link = this.edgeAt(p.x, p.y);
			this.#setHover(link ? { link } : null, p.x, p.y);
		}
		if (this.#state.connectMode && this.#state.connectFrom) this.#requestDraw();
	};

	#onPointerUp = (e: PointerEvent) => {
		this.#pointers.delete(e.pointerId);
		if (this.#pinch) {
			if (this.#pointers.size < 2) {
				this.#pinch = null;
				this.#suppressClick = true;
			}
			return;
		}
		const g = this.#gesture;
		if (!g || g.id !== e.pointerId) return;
		this.#gesture = null;
		this.#suppressClick = g.moved;
		if (g.moved && g.node && this.#layout === 'force') {
			this.#sim.alphaTarget(0);
			this.#cb.onPinsChange(this.pins());
		}
		this.#updateCursor();
	};

	#onPointerLeave = (e: PointerEvent) => {
		if (e.pointerType === 'mouse' && !this.#gesture) {
			this.#pointer = null;
			this.#setHover(null, 0, 0);
		}
	};

	#onWheel = (e: WheelEvent) => {
		e.preventDefault();
		const p = this.#local(e);
		const unit = e.deltaMode === 1 ? 33 : e.deltaMode === 2 ? 400 : 1;
		// ctrlKey: trackpad pinch gesture → larger factor
		const factor = Math.exp(-e.deltaY * unit * (e.ctrlKey ? 0.01 : 0.0018));
		this.#autoFit = false;
		this.#anim = null;
		this.#t = this.#zoomed(p.x, p.y, factor);
		this.#requestDraw();
	};

	#onClick = (e: MouseEvent) => {
		if (this.#suppressClick) {
			this.#suppressClick = false;
			return;
		}
		const p = this.#local(e);
		const node = this.nodeAt(p.x, p.y);
		if (node) return this.#cb.onClickNode(node, e);
		const link = this.edgeAt(p.x, p.y);
		if (link) return this.#cb.onClickEdge(link, e);
		this.#cb.onClickEmpty(e);
	};

	#onDblClick = (e: MouseEvent) => {
		const p = this.#local(e);
		const node = this.nodeAt(p.x, p.y);
		if (node) {
			this.#cb.onDoubleClickNode(node);
			return;
		}
		if (this.edgeAt(p.x, p.y)) return;
		this.#autoFit = false;
		this.#animateTo(this.#zoomed(p.x, p.y, 1.8), 250);
	};
}
