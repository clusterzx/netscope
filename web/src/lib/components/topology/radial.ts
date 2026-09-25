// "Kreise" layout of the topology: every node carries its children on a ring around itself
// (router → switches → devices, host → containers), recursively – a balloon tree. The ring
// of a child leaves a wedge free towards its parent, so edges do not cut through siblings.
// Separate trees and the devices without any edge are placed as round groups around the
// largest tree. Deterministic: the same graph always gives the same picture.
import type { TLink, TNode } from './graph';

const TAU = Math.PI * 2;
/** angle kept free towards the parent (radians) */
const WEDGE = 1.1;
/** distance between neighbouring balloons */
const GAP = 12;
/** containers sit close to their host */
const CONTAINER_GAP = 4;

/** Preference of an edge kind as the parent of a node (lower wins). */
const PARENT_RANK: Record<string, number> = {
	container: 0,
	runs_on: 1,
	switch_port: 2,
	wireless: 3,
	lldp: 4,
	manual: 5,
	l3: 6
};

export interface RadialLayout {
	/** position per node id */
	pos: Map<string, [number, number]>;
	/** children in the layout tree (dragging a node moves them along) */
	kids: Map<string, TNode[]>;
}

const gap = (n: TNode) => (n.container ? CONTAINER_GAP : GAP);
/** own radius incl. room for the label below the node */
const own = (n: TNode) => n.r + (n.container ? 3 : 9);
const byLabel = (a: TNode, b: TNode) =>
	a.data.label.localeCompare(b.data.label, 'de', { numeric: true }) || a.id.localeCompare(b.id);

export function radialLayout(nodes: TNode[], links: TLink[]): RadialLayout {
	// one parent per node: the most specific edge (container > runs_on > switch port … > l3)
	const parent = new Map<string, { p: TNode; rank: number }>();
	for (const l of links) {
		const rank = PARENT_RANK[l.data.kind] ?? 7;
		const cur = parent.get(l.target.id);
		const better =
			!cur ||
			rank < cur.rank ||
			(rank === cur.rank &&
				(l.source.degree > cur.p.degree || (l.source.degree === cur.p.degree && l.source.id < cur.p.id)));
		if (better) parent.set(l.target.id, { p: l.source, rank });
	}
	const byId = new Map(nodes.map((n) => [n.id, n]));
	const candidates = new Map<string, TNode[]>();
	for (const [child, { p }] of parent) {
		const list = candidates.get(p.id);
		const c = byId.get(child);
		if (!c) continue;
		if (list) list.push(c);
		else candidates.set(p.id, [c]);
	}

	// tree from the roots; nodes on cycles get their own root (highest degree first)
	const kids = new Map<string, TNode[]>();
	const seen = new Set<string>();
	const grow = (root: TNode) => {
		seen.add(root.id);
		const queue = [root];
		while (queue.length) {
			const n = queue.shift()!;
			const ch = (candidates.get(n.id) ?? []).filter((c) => !seen.has(c.id)).sort(byLabel);
			for (const c of ch) seen.add(c.id);
			if (ch.length) kids.set(n.id, ch);
			queue.push(...ch);
		}
	};
	const roots: TNode[] = [];
	for (const n of [...nodes].sort(byLabel)) {
		if (!parent.has(n.id)) {
			roots.push(n);
			grow(n);
		}
	}
	const rest = nodes.filter((n) => !seen.has(n.id)).sort((a, b) => b.degree - a.degree || byLabel(a, b));
	for (const n of rest) {
		if (!seen.has(n.id)) {
			roots.push(n);
			grow(n);
		}
	}

	// sizes (radius of the balloon around a node) and ring radii, bottom-up
	const size = new Map<string, number>();
	const ring = new Map<string, number>();
	const measure = (n: TNode, root: boolean): number => {
		const ch = kids.get(n.id);
		if (!ch) {
			const s = own(n);
			size.set(n.id, s);
			return s;
		}
		let need = 0;
		let maxS = 0;
		for (const c of ch) {
			const s = measure(c, false);
			need += 2 * s + gap(c);
			maxS = Math.max(maxS, s);
		}
		const range = root ? TAU : TAU - WEDGE;
		const r = Math.max(own(n) + maxS + gap(ch[0]), need / range);
		ring.set(n.id, r);
		const s = r + maxS;
		size.set(n.id, s);
		return s;
	};
	for (const r of roots) measure(r, true);

	const pos = new Map<string, [number, number]>();
	const order: TNode[] = [];
	const put = (n: TNode, x: number, y: number) => {
		pos.set(n.id, [x, y]);
		order.push(n);
	};
	const place = (n: TNode, x: number, y: number, dir: number | null) => {
		put(n, x, y);
		const ch = kids.get(n.id);
		if (!ch) return;
		const r = ring.get(n.id)!;
		const range = dir === null ? TAU : TAU - WEDGE;
		let a = dir === null ? -Math.PI / 2 : dir - range / 2;
		const total = ch.reduce((sum, c) => sum + 2 * size.get(c.id)! + gap(c), 0);
		for (const c of ch) {
			const w = ((2 * size.get(c.id)! + gap(c)) / total) * range;
			const mid = a + w / 2;
			place(c, x + r * Math.cos(mid), y + r * Math.sin(mid), mid);
			a += w;
		}
	};

	// groups: the trees (largest first) and one round cluster of all single nodes
	type Group = { size: number; put: (x: number, y: number) => void };
	const groups: Group[] = [];
	const single = roots.filter((r) => !kids.has(r.id));
	for (const r of roots.filter((r) => kids.has(r.id)).sort((a, b) => size.get(b.id)! - size.get(a.id)!)) {
		groups.push({ size: size.get(r.id)!, put: (x, y) => place(r, x, y, null) });
	}
	if (single.length) {
		// sunflower packing: round and dense, in label order from the centre outwards
		const d = 2 * Math.max(...single.map(own)) + 6;
		const c = d * 0.56;
		const golden = Math.PI * (3 - Math.sqrt(5));
		groups.push({
			size: c * Math.sqrt(single.length) + d / 2,
			put: (x, y) =>
				single.forEach((n, i) => {
					const rr = c * Math.sqrt(i + (single.length > 1 ? 0.5 : 0));
					put(n, x + rr * Math.cos(i * golden), y + rr * Math.sin(i * golden));
				})
		});
	}
	if (!groups.length) return { pos, kids };

	// the first group in the centre, every further one at the nearest free spot around what
	// is placed: a polar skyline (outermost extent per angle sector) lets a group use the
	// space next to short branches instead of a ring outside the whole bounding circle
	const BINS = 96;
	const sector = TAU / BINS;
	const sky = new Array<number>(BINS).fill(0);
	/** sectors touched by the angle range a ± half */
	const sectors = (a: number, half: number, fn: (b: number) => void) => {
		if (half >= Math.PI) {
			for (let b = 0; b < BINS; b++) fn(b);
			return;
		}
		const to = Math.floor((a + half) / sector);
		for (let k = Math.floor((a - half) / sector); k <= to; k++) fn(((k % BINS) + BINS) % BINS);
	};
	let marked = 0;
	const mark = () => {
		for (; marked < order.length; marked++) {
			const n = order[marked];
			const [x, y] = pos.get(n.id)!;
			const d = Math.hypot(x, y);
			const o = own(n);
			const ext = d + o;
			sectors(Math.atan2(y, x), d > o ? Math.asin(o / d) : Math.PI, (b) => {
				if (sky[b] < ext) sky[b] = ext;
			});
		}
	};
	/** the circle of radius s at distance r in direction a stays clear of the skyline */
	const clear = (s: number, r: number, a: number) => {
		let ok = true;
		sectors(a, r > s ? Math.asin(s / r) : Math.PI, (b) => {
			if (sky[b] + GAP > r - s) ok = false;
		});
		return ok;
	};
	groups[0].put(0, 0);
	mark();
	for (const g of groups.slice(1)) {
		const far = Math.max(...sky) + g.size + GAP;
		let best = { cost: Infinity, r: far, a: 0 };
		for (let k = 0; k < BINS; k++) {
			const a = -Math.PI / 2 + k * sector;
			let lo = g.size;
			let hi = far;
			while (hi - lo > 1) {
				const mid = (lo + hi) / 2;
				if (clear(g.size, mid, a)) hi = mid;
				else lo = mid;
			}
			// screens are wider than high: a slight preference for the sides
			const cost = hi * (1 + 0.25 * Math.sin(a) ** 2);
			if (cost < best.cost) best = { cost, r: hi, a };
		}
		g.put(best.r * Math.cos(best.a), best.r * Math.sin(best.a));
		mark();
	}
	return { pos, kids };
}
