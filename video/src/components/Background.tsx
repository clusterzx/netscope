import { AbsoluteFill, useCurrentFrame } from 'remotion';
import { C } from '../theme';

// dark gradient, dot grid (as in the README graphics), a slowly drifting glow and a vignette
export const Background = () => {
	const f = useCurrentFrame();
	const gx = 50 + 18 * Math.sin(f / 260);
	const gy = 42 + 12 * Math.cos(f / 330);
	return (
		<AbsoluteFill style={{ background: `linear-gradient(135deg, ${C.bg0} 0%, ${C.bg1} 100%)` }}>
			<AbsoluteFill
				style={{
					backgroundImage: 'radial-gradient(rgba(148,163,184,0.11) 1.3px, transparent 1.6px)',
					backgroundSize: '30px 30px',
					backgroundPosition: `${(f * 0.12) % 30}px ${(f * 0.06) % 30}px`
				}}
			/>
			<AbsoluteFill
				style={{
					background: `radial-gradient(900px 700px at ${gx}% ${gy}%, rgba(76,180,236,0.13), transparent 70%)`
				}}
			/>
			<AbsoluteFill
				style={{
					background: `radial-gradient(700px 600px at ${100 - gx}% ${100 - gy}%, rgba(167,139,250,0.05), transparent 70%)`
				}}
			/>
			<AbsoluteFill
				style={{ background: 'radial-gradient(ellipse at center, transparent 55%, rgba(0,0,0,0.45) 100%)' }}
			/>
		</AbsoluteFill>
	);
};
