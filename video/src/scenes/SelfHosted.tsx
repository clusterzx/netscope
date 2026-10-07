import { interpolate, useCurrentFrame, useVideoConfig } from 'remotion';
import { easeOut, enter, pop } from '../anim';
import { Icon, type IconName } from '../components/Icon';
import { Eyebrow, Pill, SceneShell } from '../components/ui';
import { C, MONO } from '../theme';
import type { SceneProps } from './types';

const statIcons: IconName[] = ['box', 'plugins', 'cloud'];
const pillIcons: IconName[] = ['key', 'user', 'lock', 'globe', 'link', 'activity'];

export const SelfHosted = ({ s, dur, vo }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const t = s.selfhosted;
	const at = (k: number) => vo.start + vo.len * k;
	const statAt = [at(0.2), at(0.34), at(0.46)];
	const titleAt = at(0.6);

	return (
		<SceneShell dur={dur}>
			<div
				style={{
					position: 'absolute',
					top: 120,
					left: 0,
					right: 0,
					display: 'flex',
					flexDirection: 'column',
					alignItems: 'center',
					gap: 24
				}}
			>
				<div style={enter(f, vo.start - 8)}>
					<Eyebrow icon="home" label={t.eyebrow} />
				</div>
				<div
					style={{
						fontSize: 76,
						fontWeight: 780,
						letterSpacing: '-0.03em',
						...enter(f, titleAt, { dy: 34 })
					}}
				>
					{t.title}
				</div>
			</div>

			<div
				style={{
					position: 'absolute',
					top: 360,
					left: 0,
					right: 0,
					display: 'flex',
					justifyContent: 'center',
					gap: 40
				}}
			>
				{t.stats.map(([num, label], i) => {
					const p = pop(f, fps, statAt[i], 13);
					const target = Number(num);
					const shown =
						target > 1
							? Math.round(
									interpolate(f, [statAt[i], statAt[i] + 24], [0, target], {
										extrapolateLeft: 'clamp',
										extrapolateRight: 'clamp',
										easing: easeOut
									})
								)
							: target;
					const color = i === 2 ? C.ok : C.accent;
					return (
						<div
							key={label}
							style={{
								width: 440,
								height: 280,
								borderRadius: 28,
								background: `linear-gradient(180deg, ${C.card2}, ${C.card})`,
								border: `1.5px solid ${C.border}`,
								boxShadow: `0 30px 70px rgba(0,0,0,0.45), 0 0 ${50 * Math.min(1, p)}px ${color}22`,
								display: 'flex',
								flexDirection: 'column',
								alignItems: 'center',
								justifyContent: 'center',
								gap: 6,
								opacity: Math.min(1, p),
								transform: `translateY(${(1 - p) * 50}px) scale(${0.9 + 0.1 * p})`
							}}
						>
							<Icon name={statIcons[i]} size={36} color={color} />
							<div
								style={{
									fontSize: 132,
									fontWeight: 800,
									letterSpacing: '-0.04em',
									lineHeight: 1,
									color,
									fontVariantNumeric: 'tabular-nums'
								}}
							>
								{shown}
							</div>
							<div style={{ fontSize: 28, fontWeight: 550, color: C.muted }}>{label}</div>
						</div>
					);
				})}
			</div>

			<div
				style={{
					position: 'absolute',
					top: 700,
					left: 0,
					right: 0,
					display: 'flex',
					justifyContent: 'center',
					...enter(f, titleAt + 6, { dy: 20 })
				}}
			>
				<div
					style={{
						display: 'flex',
						alignItems: 'center',
						gap: 16,
						padding: '16px 28px',
						borderRadius: 16,
						background: '#0a0e14',
						border: `1px solid ${C.border}`,
						fontFamily: MONO,
						fontSize: 28
					}}
				>
					<span style={{ color: C.accent }}>$</span>
					<span>make docker-up</span>
					<Icon
						name="check"
						size={26}
						color={C.ok}
						stroke={2.6}
						style={{ marginLeft: 10, opacity: f > titleAt + 26 ? 1 : 0 }}
					/>
				</div>
			</div>

			<div
				style={{
					position: 'absolute',
					top: 820,
					left: 120,
					right: 120,
					display: 'flex',
					flexWrap: 'wrap',
					justifyContent: 'center',
					gap: 14
				}}
			>
				{t.pills.map((p, i) => {
					const k = pop(f, fps, titleAt + 14 + i * 4, 14);
					return (
						<div key={p} style={{ opacity: Math.min(1, k), transform: `translateY(${(1 - k) * 16}px)` }}>
							<Pill label={p} icon={pillIcons[i]} t="accent" active={1} size={23} />
						</div>
					);
				})}
			</div>
		</SceneShell>
	);
};
