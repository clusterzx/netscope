import type { CSSProperties, ReactNode } from 'react';
import { AbsoluteFill, interpolate, useCurrentFrame } from 'remotion';
import { easeInOut, enter } from '../anim';
import { C, MONO, SANS, tone, type Tone } from '../theme';
import { XF } from '../timeline';
import { Icon, type IconName } from './Icon';

// Wraps a scene: fades in over the first XF frames and out over the XF frames after `dur`
// (the next scene is already fading in on top), with a gentle push-in.
export const SceneShell = ({
	dur,
	children,
	zoom = 0.03
}: {
	dur: number;
	children: ReactNode;
	zoom?: number;
}) => {
	const f = useCurrentFrame();
	const fadeIn = interpolate(f, [0, XF], [0, 1], {
		extrapolateLeft: 'clamp',
		extrapolateRight: 'clamp',
		easing: easeInOut
	});
	const fadeOut = interpolate(f, [dur, dur + XF], [1, 0], {
		extrapolateLeft: 'clamp',
		extrapolateRight: 'clamp',
		easing: easeInOut
	});
	const scale = 1 + zoom * (f / (dur + XF)) + (1 - fadeIn) * 0.02;
	return (
		<AbsoluteFill
			style={{
				opacity: Math.min(fadeIn, fadeOut),
				transform: `scale(${scale})`,
				filter: fadeOut < 1 ? `blur(${(1 - fadeOut) * 8}px)` : undefined,
				fontFamily: SANS,
				color: C.fg
			}}
		>
			{children}
		</AbsoluteFill>
	);
};

export const Eyebrow = ({
	icon,
	label,
	color = C.accent,
	style
}: {
	icon?: IconName;
	label: string;
	color?: string;
	style?: CSSProperties;
}) => (
	<div
		style={{
			display: 'inline-flex',
			alignItems: 'center',
			gap: 12,
			padding: '10px 20px',
			borderRadius: 999,
			background: C.accentSoft,
			border: `1px solid ${C.accentLine}`,
			color,
			fontSize: 22,
			fontWeight: 600,
			letterSpacing: '0.08em',
			textTransform: 'uppercase',
			...style
		}}
	>
		{icon && <Icon name={icon} size={24} color={color} />}
		{label}
	</div>
);

// eyebrow + title (+ subtitle), animated in from `start`
export const Heading = ({
	icon,
	eyebrow,
	title,
	sub,
	start = 0,
	align = 'left',
	size = 68,
	style
}: {
	icon?: IconName;
	eyebrow: string;
	title: string;
	sub?: string;
	start?: number;
	align?: 'left' | 'center';
	size?: number;
	style?: CSSProperties;
}) => {
	const f = useCurrentFrame();
	return (
		<div
			style={{
				display: 'flex',
				flexDirection: 'column',
				alignItems: align === 'center' ? 'center' : 'flex-start',
				gap: 22,
				...style
			}}
		>
			<div style={enter(f, start)}>
				<Eyebrow icon={icon} label={eyebrow} />
			</div>
			<div
				style={{
					...enter(f, start + 5, { dy: 34 }),
					fontSize: size,
					fontWeight: 750,
					letterSpacing: '-0.025em',
					lineHeight: 1.05,
					textAlign: align
				}}
			>
				{title}
			</div>
			{sub && (
				<div
					style={{ ...enter(f, start + 10), fontSize: 28, color: C.muted, fontWeight: 450, textAlign: align }}
				>
					{sub}
				</div>
			)}
		</div>
	);
};

export const Card = ({ children, style }: { children: ReactNode; style?: CSSProperties }) => (
	<div
		style={{
			background: `linear-gradient(180deg, ${C.card2} 0%, ${C.card} 100%)`,
			border: `1px solid ${C.border}`,
			borderRadius: 22,
			boxShadow: '0 30px 80px rgba(0,0,0,0.45), inset 0 1px 0 rgba(255,255,255,0.04)',
			...style
		}}
	>
		{children}
	</div>
);

export const Pill = ({
	label,
	icon,
	t = 'muted',
	active = 1,
	size = 22,
	mono = false,
	style
}: {
	label: string;
	icon?: IconName;
	t?: Tone;
	active?: number; // 0 = neutral, 1 = coloured
	size?: number;
	mono?: boolean;
	style?: CSSProperties;
}) => {
	const [fg, soft] = tone(t);
	return (
		<div
			style={{
				display: 'inline-flex',
				alignItems: 'center',
				gap: 10,
				padding: `${size * 0.42}px ${size * 0.85}px`,
				borderRadius: 999,
				fontSize: size,
				fontWeight: 550,
				fontFamily: mono ? MONO : SANS,
				whiteSpace: 'nowrap',
				color: active > 0.5 ? fg : C.muted,
				background: active > 0.5 ? soft : C.card2,
				border: `1px solid ${active > 0.5 ? fg + '66' : C.border}`,
				boxShadow: active > 0.5 ? `0 0 ${24 * active}px ${fg}33` : 'none',
				...style
			}}
		>
			{icon && <Icon name={icon} size={size * 1.05} color={active > 0.5 ? fg : C.subtle} />}
			{label}
		</div>
	);
};

// round device node with an icon, as in the README radar
export const Node = ({
	icon,
	size = 64,
	color = C.accent,
	style,
	glow = 0
}: {
	icon: IconName;
	size?: number;
	color?: string;
	style?: CSSProperties;
	glow?: number;
}) => (
	<div
		style={{
			width: size,
			height: size,
			borderRadius: '50%',
			background: C.card,
			border: `${Math.max(2, size / 28)}px solid ${color}`,
			display: 'flex',
			alignItems: 'center',
			justifyContent: 'center',
			boxShadow: glow ? `0 0 ${glow * 40}px ${color}66` : '0 8px 24px rgba(0,0,0,0.4)',
			...style
		}}
	>
		<Icon name={icon} size={size * 0.48} color={C.fg} stroke={1.8} />
	</div>
);

export const Dot = ({ color, size = 12, pulse = 0 }: { color: string; size?: number; pulse?: number }) => (
	<div style={{ position: 'relative', width: size, height: size, flexShrink: 0 }}>
		<div style={{ position: 'absolute', inset: 0, borderRadius: '50%', background: color }} />
		{pulse > 0 && (
			<div
				style={{
					position: 'absolute',
					inset: 0,
					borderRadius: '50%',
					border: `2px solid ${color}`,
					transform: `scale(${1 + pulse * 1.6})`,
					opacity: 1 - pulse
				}}
			/>
		)}
	</div>
);
