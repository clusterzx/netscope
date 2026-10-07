import { random, useCurrentFrame, useVideoConfig } from 'remotion';
import { enter, pop, prog } from '../anim';
import type { IconName } from '../components/Icon';
import { Node, SceneShell } from '../components/ui';
import { C } from '../theme';
import type { SceneProps } from './types';

const ICONS: IconName[] = [
	'laptop',
	'phone',
	'server',
	'printer',
	'camera',
	'tv',
	'tablet',
	'speaker',
	'gamepad',
	'monitor',
	'wifi',
	'home',
	'cpu',
	'disk'
];

// devices scattered in a ring around the question (kept apart), hubs in between
type HookNode = { x: number; y: number; icon: IconName; at: number; size: number; color: string };
const nodes: HookNode[] = [];
for (let i = 0, tries = 0; nodes.length < 30 && tries < 2000; tries++) {
	const a = random(`a${tries}`) * Math.PI * 2;
	const k = random(`k${tries}`);
	const x = 960 + Math.cos(a) * (690 + k * 200);
	const y = 540 + Math.sin(a) * (330 + k * 150);
	if (x < 60 || x > 1860 || y < 60 || y > 1020) continue;
	if (nodes.some((n) => Math.hypot(n.x - x, n.y - y) < 120)) continue;
	nodes.push({
		x,
		y,
		icon: ICONS[i % ICONS.length],
		at: 6 + random(`t${i}`) * 80,
		size: 50 + random(`s${i}`) * 22,
		color: random(`c${i}`) < 0.18 ? C.amber : random(`c2${i}`) < 0.2 ? C.offline : C.accent
	});
	i++;
}
// five hubs spread around the ring: the node closest to each anchor point
const hubs = [-0.5, 0.75, 2.0, 3.25, 4.6].map((a) => {
	const ax = 960 + Math.cos(a) * 780;
	const ay = 540 + Math.sin(a) * 400;
	return nodes.reduce((best, n) =>
		Math.hypot(n.x - ax, n.y - ay) < Math.hypot(best.x - ax, best.y - ay) ? n : best
	);
});
const nearestHub = (n: (typeof nodes)[number]) =>
	hubs.reduce((best, h) =>
		Math.hypot(h.x - n.x, h.y - n.y) < Math.hypot(best.x - n.x, best.y - n.y) ? h : best
	);

export const Hook = ({ s, dur, vo }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const sub = vo.start + vo.len * 0.52;
	const words = s.hook.lines.map((l) => l.split(' '));
	let w = 0;

	return (
		<SceneShell dur={dur} zoom={0.06}>
			<svg width={1920} height={1080} style={{ position: 'absolute', inset: 0 }}>
				{nodes.map((n, i) => {
					const h = nearestHub(n);
					if (h === n) return null;
					const p = prog(f, n.at + 4, 20);
					return (
						<line
							key={i}
							x1={h.x}
							y1={h.y}
							x2={h.x + (n.x - h.x) * p}
							y2={h.y + (n.y - h.y) * p}
							stroke={C.accentLine}
							strokeWidth={1.5}
							opacity={0.55}
						/>
					);
				})}
				{hubs.map((h, i) => {
					const g = hubs[(i + 1) % hubs.length];
					const p = prog(f, 30 + i * 6, 30);
					return (
						<line
							key={i}
							x1={h.x}
							y1={h.y}
							x2={h.x + (g.x - h.x) * p}
							y2={h.y + (g.y - h.y) * p}
							stroke={C.accentLine}
							strokeWidth={2}
							strokeDasharray="4 8"
							opacity={0.5}
						/>
					);
				})}
			</svg>
			{nodes.map((n, i) => {
				const p = pop(f, fps, n.at);
				const ping = ((f - n.at) % 70) / 70;
				return (
					<div
						key={i}
						style={{
							position: 'absolute',
							left: n.x,
							top: n.y,
							transform: `translate(-50%, -50%) scale(${p})`,
							opacity: Math.min(1, p) * 0.9
						}}
					>
						{f > n.at && random(`p${i}`) < 0.3 && (
							<div
								style={{
									position: 'absolute',
									inset: -4,
									borderRadius: '50%',
									border: `2px solid ${n.color}`,
									transform: `scale(${1 + ping * 1.2})`,
									opacity: (1 - ping) * 0.6
								}}
							/>
						)}
						<Node
							icon={hubs.includes(n) ? 'router' : n.icon}
							size={hubs.includes(n) ? 78 : n.size}
							color={n.color}
						/>
					</div>
				);
			})}

			<div
				style={{
					position: 'absolute',
					inset: 0,
					display: 'flex',
					flexDirection: 'column',
					alignItems: 'center',
					justifyContent: 'center',
					gap: 6
				}}
			>
				<div
					style={{
						position: 'absolute',
						width: 1300,
						height: 420,
						borderRadius: '50%',
						background:
							'radial-gradient(closest-side, rgba(11,16,23,0.92), rgba(11,16,23,0.6) 60%, transparent)'
					}}
				/>
				{words.map((line, li) => (
					<div
						key={li}
						style={{
							position: 'relative',
							display: 'flex',
							gap: 24,
							fontSize: 92,
							fontWeight: 780,
							letterSpacing: '-0.03em',
							lineHeight: 1.1
						}}
					>
						{line.map((word) => {
							const at = vo.start - 6 + w++ * 4.5;
							const hl = word.toLowerCase().startsWith(s.hook.highlight.toLowerCase());
							return (
								<span
									key={word}
									style={{
										...enter(f, at, { dy: 40, blur: 10 }),
										color: hl ? C.accent : C.fg,
										display: 'inline-block'
									}}
								>
									{word}
								</span>
							);
						})}
					</div>
				))}
				<div
					style={{
						position: 'relative',
						marginTop: 30,
						fontSize: 42,
						fontWeight: 500,
						color: C.muted,
						...enter(f, sub, { dy: 20 })
					}}
				>
					{s.hook.sub}
				</div>
			</div>
		</SceneShell>
	);
};
