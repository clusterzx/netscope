// Scale/tick helpers for the hand-written SVG charts.

/** "Nice" linear ticks covering [min, max]. */
export function niceTicks(min: number, max: number, count = 4): number[] {
	if (!isFinite(min) || !isFinite(max)) return [0];
	if (min === max) {
		max = min === 0 ? 1 : min * 1.5;
		if (min > 0) min = 0;
	}
	const span = max - min;
	const raw = span / Math.max(1, count);
	const mag = Math.pow(10, Math.floor(Math.log10(raw)));
	const norm = raw / mag;
	const step = (norm >= 5 ? 10 : norm >= 2 ? 5 : norm >= 1 ? 2 : 1) * mag;
	const start = Math.floor(min / step) * step;
	const out: number[] = [];
	for (let v = start; v <= max + step * 0.5; v += step) out.push(Math.round(v / step) * step);
	if (out[out.length - 1] < max) out.push(out[out.length - 1] + step);
	return out;
}

const HOUR = 3600_000;
const DAY = 24 * HOUR;
const steps = [
	5 * 60_000,
	15 * 60_000,
	30 * 60_000,
	HOUR,
	2 * HOUR,
	3 * HOUR,
	6 * HOUR,
	12 * HOUR,
	DAY,
	2 * DAY,
	7 * DAY,
	14 * DAY,
	30 * DAY
];

/** Time ticks (ms) for a range, about one per `px` pixels of width. */
export function timeTicks(from: number, to: number, width: number, px = 90): number[] {
	const span = to - from;
	if (span <= 0 || width <= 0) return [];
	const want = Math.max(2, Math.floor(width / px));
	const step = steps.find((s) => span / s <= want) ?? steps[steps.length - 1];
	// align to local midnight for day steps, to the step otherwise
	const tz = new Date(from).getTimezoneOffset() * 60_000;
	let t = Math.ceil((from - tz) / step) * step + tz;
	if (step >= DAY) {
		const d = new Date(from);
		d.setHours(0, 0, 0, 0);
		t = d.getTime();
		while (t < from) t += step;
	}
	const out: number[] = [];
	for (; t <= to; t += step) out.push(t);
	return out;
}

const pad = (n: number) => String(n).padStart(2, '0');

/** Tick label: HH:mm for short ranges, dd.MM. for long ranges, both at midnight for mid ranges. */
export function timeTickLabel(t: number, span: number): string {
	const d = new Date(t);
	if (span > 3 * DAY) return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.`;
	if (d.getHours() === 0 && d.getMinutes() === 0 && span > 12 * HOUR)
		return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.`;
	return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Splits points into runs where consecutive samples are closer than `gapFactor` × median interval. */
export function splitGaps<T>(pts: T[], time: (p: T) => number, gapFactor = 3): T[][] {
	if (pts.length < 3) return pts.length ? [pts] : [];
	const diffs: number[] = [];
	for (let i = 1; i < pts.length; i++) diffs.push(time(pts[i]) - time(pts[i - 1]));
	const sorted = [...diffs].sort((a, b) => a - b);
	const median = sorted[Math.floor(sorted.length / 2)] || 1;
	const runs: T[][] = [[pts[0]]];
	for (let i = 1; i < pts.length; i++) {
		if (diffs[i - 1] > median * gapFactor) runs.push([]);
		runs[runs.length - 1].push(pts[i]);
	}
	return runs;
}
