import { useCurrentFrame, useVideoConfig } from 'remotion';
import { pop, prog } from '../anim';
import { Icon } from '../components/Icon';
import { SceneShell } from '../components/ui';
import { C, tone, type Tone } from '../theme';
import type { SceneProps } from './types';

const tones: Tone[] = ['accent', 'accent', 'violet', 'accent', 'amber', 'ok', 'danger'];

export const Questions = ({ s, dur, vo }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const n = s.questions.length;
	// cards follow the narration; the last one ("vulnerable?") lands near its end
	const at = (i: number) => vo.start + (i / (n - 1)) * vo.len * 0.86 - 4;
	const rows = [s.questions.slice(0, 4), s.questions.slice(4)];

	return (
		<SceneShell dur={dur} zoom={0.04}>
			<div
				style={{
					position: 'absolute',
					inset: 0,
					display: 'flex',
					flexDirection: 'column',
					alignItems: 'center',
					justifyContent: 'center',
					gap: 34
				}}
			>
				{rows.map((row, r) => (
					<div key={r} style={{ display: 'flex', gap: 34 }}>
						{row.map(([icon, label], j) => {
							const i = r * 4 + j;
							const p = pop(f, fps, at(i));
							const [fg, soft] = tone(tones[i]);
							const last = i === n - 1;
							const shake = last
								? Math.sin(f * 1.6) * 4 * (1 - prog(f, at(i) + 4, 14)) * prog(f, at(i), 4)
								: 0;
							const glow = last ? 0.6 + 0.4 * Math.sin(f / 5) : 0;
							return (
								<div
									key={label}
									style={{
										width: 400,
										height: 150,
										borderRadius: 24,
										padding: '0 30px',
										display: 'flex',
										alignItems: 'center',
										gap: 22,
										background: `linear-gradient(180deg, ${C.card2}, ${C.card})`,
										border: `1.5px solid ${last ? fg : C.border}`,
										boxShadow: last
											? `0 0 ${50 * glow}px ${fg}55, 0 30px 60px rgba(0,0,0,0.4)`
											: '0 30px 60px rgba(0,0,0,0.4)',
										opacity: Math.min(1, p),
										transform: `translate(${shake}px, ${(1 - p) * 40}px) scale(${0.85 + 0.15 * p})`
									}}
								>
									<div
										style={{
											width: 70,
											height: 70,
											borderRadius: 18,
											background: soft,
											display: 'flex',
											alignItems: 'center',
											justifyContent: 'center'
										}}
									>
										<Icon name={icon} size={38} color={fg} />
									</div>
									<div
										style={{
											fontSize: 34,
											fontWeight: 650,
											letterSpacing: '-0.01em',
											color: last ? fg : C.fg,
											lineHeight: 1.15
										}}
									>
										{label}
									</div>
								</div>
							);
						})}
					</div>
				))}
			</div>
		</SceneShell>
	);
};
