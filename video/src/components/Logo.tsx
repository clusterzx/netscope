import { C } from '../theme';

// The NetScope logo (favicon). draw: 0 → 1 draws the rings, needle: extra rotation in degrees.
export const Logo = ({
	size,
	draw = 1,
	needle = 0,
	glow = 0
}: {
	size: number;
	draw?: number;
	needle?: number;
	glow?: number;
}) => {
	const outer = 2 * Math.PI * 18;
	const inner = 2 * Math.PI * 8;
	return (
		<svg
			width={size}
			height={size}
			viewBox="0 0 64 64"
			style={{
				overflow: 'visible',
				filter: glow ? `drop-shadow(0 0 ${glow * 40}px rgba(56,189,248,${0.55 * glow}))` : undefined
			}}
		>
			<rect width="64" height="64" rx="14" fill={C.logoBg} />
			<rect
				x="0.5"
				y="0.5"
				width="63"
				height="63"
				rx="13.5"
				fill="none"
				stroke={C.logoRing}
				strokeOpacity="0.25"
			/>
			<circle
				cx="32"
				cy="32"
				r="18"
				fill="none"
				stroke={C.logoRing}
				strokeWidth="4"
				strokeDasharray={outer}
				strokeDashoffset={outer * (1 - draw)}
				transform="rotate(-90 32 32)"
			/>
			<circle
				cx="32"
				cy="32"
				r="8"
				fill="none"
				stroke={C.logoRing}
				strokeWidth="3"
				opacity=".7"
				strokeDasharray={inner}
				strokeDashoffset={inner * (1 - Math.min(1, draw * 1.4))}
				transform="rotate(-90 32 32)"
			/>
			<g transform={`rotate(${needle} 32 32)`} opacity={Math.min(1, draw * 2)}>
				<path d="M32 32 L46 18" stroke={C.logoNeedle} strokeWidth="4" strokeLinecap="round" />
				<circle cx="46" cy="18" r="4" fill={C.logoNeedle} />
			</g>
		</svg>
	);
};
