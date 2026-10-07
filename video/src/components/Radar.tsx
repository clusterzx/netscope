import { C } from '../theme';

// concentric rings, cross hair and a rotating sweep (angle in degrees, 0 = right, clockwise)
export const Radar = ({
	cx,
	cy,
	r,
	angle,
	opacity = 1
}: {
	cx: number;
	cy: number;
	r: number;
	angle: number;
	opacity?: number;
}) => {
	const id = `sweep-${cx}-${cy}`;
	const a = (angle * Math.PI) / 180;
	const tail = ((angle - 55) * Math.PI) / 180;
	return (
		<svg width={1920} height={1080} style={{ position: 'absolute', inset: 0, opacity, overflow: 'visible' }}>
			<defs>
				<radialGradient id={`${id}-glow`} cx={cx} cy={cy} r={r * 1.25} gradientUnits="userSpaceOnUse">
					<stop offset="0" stopColor={C.accent} stopOpacity="0.16" />
					<stop offset="1" stopColor={C.accent} stopOpacity="0" />
				</radialGradient>
				<linearGradient
					id={id}
					gradientUnits="userSpaceOnUse"
					x1={cx + Math.cos(tail) * r}
					y1={cy + Math.sin(tail) * r}
					x2={cx + Math.cos(a) * r}
					y2={cy + Math.sin(a) * r}
				>
					<stop offset="0" stopColor={C.accent} stopOpacity="0" />
					<stop offset="1" stopColor={C.accent} stopOpacity="0.34" />
				</linearGradient>
			</defs>
			<circle cx={cx} cy={cy} r={r * 1.25} fill={`url(#${id}-glow)`} />
			{[0.36, 0.68].map((k) => (
				<circle
					key={k}
					cx={cx}
					cy={cy}
					r={r * k}
					fill="none"
					stroke={C.accentLine}
					strokeWidth={1.5}
					opacity={0.5}
				/>
			))}
			<circle
				cx={cx}
				cy={cy}
				r={r}
				fill="none"
				stroke={C.accentLine}
				strokeWidth={1.5}
				strokeDasharray="4 9"
				opacity={0.6}
			/>
			<line x1={cx - r} y1={cy} x2={cx + r} y2={cy} stroke={C.accentLine} strokeWidth={1} opacity={0.18} />
			<line x1={cx} y1={cy - r} x2={cx} y2={cy + r} stroke={C.accentLine} strokeWidth={1} opacity={0.18} />
			<path
				d={`M${cx} ${cy} L${cx + Math.cos(tail) * r} ${cy + Math.sin(tail) * r} A${r} ${r} 0 0 1 ${cx + Math.cos(a) * r} ${cy + Math.sin(a) * r} Z`}
				fill={`url(#${id})`}
			/>
			<line
				x1={cx}
				y1={cy}
				x2={cx + Math.cos(a) * r}
				y2={cy + Math.sin(a) * r}
				stroke={C.accent}
				strokeWidth={2.5}
				strokeLinecap="round"
				opacity={0.85}
			/>
		</svg>
	);
};
