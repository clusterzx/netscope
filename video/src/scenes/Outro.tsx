import { useCurrentFrame, useVideoConfig } from 'remotion';
import { enter, pop, prog } from '../anim';
import { Logo } from '../components/Logo';
import { Radar } from '../components/Radar';
import { SceneShell } from '../components/ui';
import { C } from '../theme';
import type { SceneProps } from './types';

export const Outro = ({ s, dur, vo }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const logo = pop(f, fps, 4, 12);
	const tag = vo.start + vo.len * 0.45;

	return (
		<SceneShell dur={dur} zoom={0.025}>
			<Radar cx={960} cy={470} r={520} angle={-90 + f * 2.6} opacity={0.55 * prog(f, 0, 30)} />
			<div
				style={{
					position: 'absolute',
					inset: 0,
					display: 'flex',
					flexDirection: 'column',
					alignItems: 'center',
					justifyContent: 'center'
				}}
			>
				<div
					style={{
						display: 'flex',
						alignItems: 'center',
						gap: 44,
						transform: `translateY(-40px) scale(${0.9 + 0.1 * logo})`,
						opacity: Math.min(1, logo)
					}}
				>
					<Logo size={180} glow={0.7 + 0.3 * Math.sin(f / 14)} needle={(1 - prog(f, 4, 40)) * -360} />
					<div style={{ fontSize: 160, fontWeight: 800, letterSpacing: '-0.035em', lineHeight: 1 }}>
						Net<span style={{ color: C.accent }}>Scope</span>
					</div>
				</div>
				<div
					style={{
						marginTop: 30,
						fontSize: 64,
						fontWeight: 700,
						letterSpacing: '-0.02em',
						color: C.fg,
						...enter(f, tag, { dy: 26, blur: 10 })
					}}
				>
					{s.outro.tagline}
				</div>
				<div
					style={{
						marginTop: 22,
						fontSize: 28,
						color: C.muted,
						fontWeight: 500,
						...enter(f, tag + 14, { dy: 16 })
					}}
				>
					{s.outro.sub}
				</div>
			</div>
		</SceneShell>
	);
};
