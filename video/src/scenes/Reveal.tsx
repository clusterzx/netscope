import { interpolate, useCurrentFrame, useVideoConfig } from 'remotion';
import { easeInOut, easeOut, enter, pop, prog } from '../anim';
import { Logo } from '../components/Logo';
import { Radar } from '../components/Radar';
import { Pill, SceneShell } from '../components/ui';
import { C } from '../theme';
import { REVEAL_IMPACT } from '../timeline';
import type { SceneProps } from './types';

const WORDMARK = 850;

export const Reveal = ({ s, dur, vo }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const hit = REVEAL_IMPACT;
	const logo = pop(f, fps, hit, 11);
	const draw = prog(f, hit, 26);
	const needle = interpolate(f, [hit, hit + 50], [-600, 0], {
		extrapolateLeft: 'clamp',
		extrapolateRight: 'clamp',
		easing: easeOut
	});
	// lockup: the wordmark opens next to the logo once "NetScope" is spoken
	const open = prog(f, vo.start - 4, 26, easeInOut);
	const tag = vo.start + vo.len * 0.45;

	return (
		<SceneShell dur={dur} zoom={0.035}>
			<Radar cx={960} cy={540} r={480} angle={-90 + f * 3.2} opacity={prog(f, hit, 30) * (1 - 0.55 * open)} />
			<svg width={1920} height={1080} style={{ position: 'absolute', inset: 0 }}>
				{[0, 9, 18].map((d) => {
					const p = prog(f, hit + d, 60);
					return f >= hit + d ? (
						<circle
							key={d}
							cx={960}
							cy={540}
							r={60 + p * 1000}
							fill="none"
							stroke={C.accent}
							strokeWidth={2 + (1 - p) * 5}
							opacity={(1 - p) * 0.55}
						/>
					) : null;
				})}
			</svg>
			<div
				style={{
					position: 'absolute',
					inset: 0,
					background: 'radial-gradient(circle at center, rgba(180,225,255,0.5), transparent 45%)',
					opacity: interpolate(f, [hit, hit + 3, hit + 20], [0, 1, 0], {
						extrapolateLeft: 'clamp',
						extrapolateRight: 'clamp'
					})
				}}
			/>

			<div
				style={{
					position: 'absolute',
					left: 0,
					right: 0,
					top: 0,
					height: 1080,
					display: 'flex',
					flexDirection: 'column',
					alignItems: 'center',
					justifyContent: 'center'
				}}
			>
				<div style={{ display: 'flex', alignItems: 'center', transform: `translateY(${-70 * open}px)` }}>
					<div style={{ transform: `scale(${(1.25 - 0.25 * open) * logo})` }}>
						<Logo size={210} draw={draw} needle={needle} glow={0.6 + 0.4 * Math.sin(f / 12)} />
					</div>
					<div style={{ width: (WORDMARK + 46) * open, overflow: 'hidden', flexShrink: 0 }}>
						<div
							style={{
								paddingLeft: 46,
								width: WORDMARK + 46,
								fontSize: 176,
								fontWeight: 800,
								letterSpacing: '-0.035em',
								lineHeight: 1,
								opacity: open,
								transform: `translateX(${(1 - open) * -60}px)`
							}}
						>
							Net<span style={{ color: C.accent }}>Scope</span>
						</div>
					</div>
				</div>
				<div
					style={{
						marginTop: 10,
						fontSize: 42,
						fontWeight: 500,
						color: C.muted,
						letterSpacing: '0.01em',
						...enter(f, tag, { dy: 24 }),
						transform: `translateY(${-40 * open + (1 - prog(f, tag, 20)) * 24}px)`
					}}
				>
					{s.reveal.tagline}
				</div>
				<div style={{ display: 'flex', gap: 16, marginTop: 24 }}>
					{s.reveal.claims.map((c, i) => {
						const p = pop(f, fps, tag + 10 + i * 5, 14);
						return (
							<div
								key={c}
								style={{
									opacity: Math.min(1, p),
									transform: `translateY(${(1 - p) * 20}px) scale(${0.9 + 0.1 * p})`
								}}
							>
								<Pill label={c} t="accent" active={1} size={24} />
							</div>
						);
					})}
				</div>
			</div>
		</SceneShell>
	);
};
