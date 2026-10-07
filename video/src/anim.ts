import { Easing, interpolate, spring } from 'remotion';

export const easeOut = Easing.bezier(0.16, 1, 0.3, 1);
export const easeInOut = Easing.bezier(0.65, 0, 0.35, 1);

const clamp = { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' } as const;

// 0 → 1 between start and start + dur (frames), eased
export const prog = (frame: number, start: number, dur = 18, easing = easeOut) =>
	interpolate(frame, [start, start + dur], [0, 1], { ...clamp, easing });

export const lerp = (p: number, a: number, b: number) => a + (b - a) * p;

// springy 0 → 1 (slight overshoot)
export const pop = (frame: number, fps: number, start: number, damping = 13) =>
	spring({ frame: frame - start, fps, config: { damping, stiffness: 170, mass: 0.7 } });

// fade + rise for elements entering the scene
export const enter = (frame: number, start: number, { dur = 20, dy = 26, blur = 6 } = {}) => {
	const p = prog(frame, start, dur);
	return {
		opacity: p,
		transform: `translateY(${(1 - p) * dy}px)`,
		filter: blur ? `blur(${(1 - p) * blur}px)` : undefined
	} as const;
};

// spread n items over [start, start + span]
export const stagger = (i: number, n: number, start: number, span: number) =>
	start + (n <= 1 ? 0 : (i * span) / (n - 1));
