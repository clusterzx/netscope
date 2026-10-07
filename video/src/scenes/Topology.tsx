import { random, useCurrentFrame, useVideoConfig } from 'remotion';
import { enter, pop, prog } from '../anim';
import { Icon, type IconName } from '../components/Icon';
import { Heading, Node, SceneShell } from '../components/ui';
import { C, MONO } from '../theme';
import type { SceneProps } from './types';

// ---------------------------------------------------------------- topology graph

type TNode = { id: string; icon: IconName; x: number; y: number; parent?: string; depth: number };
const tnodes: TNode[] = [
	{ id: 'internet', icon: 'globe', x: 600, y: 330, depth: 0 },
	{ id: 'opnsense', icon: 'shield', x: 600, y: 455, parent: 'internet', depth: 1 },
	{ id: 'sw-core', icon: 'network', x: 600, y: 585, parent: 'opnsense', depth: 2 },
	{ id: 'sw-og', icon: 'network', x: 290, y: 720, parent: 'sw-core', depth: 3 },
	{ id: 'pve-01', icon: 'cpu', x: 600, y: 720, parent: 'sw-core', depth: 3 },
	{ id: 'ap-flur', icon: 'wifi', x: 910, y: 720, parent: 'sw-core', depth: 3 },
	{ id: 'printer-og', icon: 'printer', x: 180, y: 870, parent: 'sw-og', depth: 4 },
	{ id: 'nas-01', icon: 'disk', x: 370, y: 870, parent: 'sw-og', depth: 4 },
	{ id: 'vm-docker', icon: 'package', x: 530, y: 870, parent: 'pve-01', depth: 4 },
	{ id: 'vm-ha', icon: 'home', x: 680, y: 870, parent: 'pve-01', depth: 4 },
	{ id: 'pixel-8', icon: 'phone', x: 840, y: 870, parent: 'ap-flur', depth: 4 },
	{ id: 'esp32-cam', icon: 'camera', x: 1000, y: 870, parent: 'ap-flur', depth: 4 }
];
const byId = Object.fromEntries(tnodes.map((n) => [n.id, n]));

// ---------------------------------------------------------------- rack

const RACK = { x: 1250, y: 300, w: 560, u: 46 };
const PORT_X = (i: number) => RACK.x + 70 + i * 17.5;

const Ports = ({ y, count, leds, f }: { y: number; count: number; leds?: boolean; f: number }) => (
	<>
		{Array.from({ length: count }, (_, i) => {
			const on = leds && random(`led${i}`) < 0.75;
			const blink = on && Math.floor((f + random(`b${i}`) * 20) / (4 + random(`r${i}`) * 6)) % 3 !== 0;
			return (
				<div
					key={i}
					style={{
						position: 'absolute',
						left: PORT_X(i) - RACK.x,
						top: y,
						width: 13,
						height: 11,
						borderRadius: 2,
						background: '#0a0e14',
						border: '1px solid #3a4658'
					}}
				>
					{leds && (
						<div
							style={{
								position: 'absolute',
								left: 2,
								top: -6,
								width: 4,
								height: 3,
								borderRadius: 1,
								background: on ? (blink ? C.ok : '#1f5f3c') : '#2a3240'
							}}
						/>
					)}
				</div>
			);
		})}
	</>
);

const Unit = ({
	u,
	h = 1,
	label,
	children
}: {
	u: number;
	h?: number;
	label?: string;
	children?: React.ReactNode;
}) => (
	<div
		style={{
			position: 'absolute',
			left: 34,
			right: 34,
			top: 20 + u * RACK.u + 2,
			height: h * RACK.u - 4,
			borderRadius: 5,
			background: 'linear-gradient(180deg, #232d3d, #1a2230)',
			border: '1px solid #36445a',
			boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.05)'
		}}
	>
		{label && (
			<div
				style={{ position: 'absolute', right: 14, top: 8, fontFamily: MONO, fontSize: 15, color: C.muted }}
			>
				{label}
			</div>
		)}
		{children}
	</div>
);

export const Topology = ({ s, dur, vo }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const t = s.topology;
	const level = (d: number) => vo.start - 6 + d * 9;
	const downAt = vo.start + vo.len * 0.42;
	const cableAt = vo.start + vo.len * 0.66;
	const down = prog(f, downAt, 8);
	const cable = prog(f, cableAt, 26);

	// patch cable from switch port 7 (unit 2) to patch panel port 7 (unit 0)
	const p1 = { x: PORT_X(6) + 6.5, y: RACK.y + 20 + 2 * RACK.u + 16 };
	const p2 = { x: PORT_X(6) + 6.5, y: RACK.y + 20 + RACK.u * 0 + 24 };
	const cablePath = `M${p1.x} ${p1.y} C${p1.x - 70} ${p1.y + 30} ${p2.x - 70} ${p2.y - 40} ${p2.x} ${p2.y}`;

	return (
		<SceneShell dur={dur}>
			<Heading
				icon="topology"
				eyebrow={t.eyebrow}
				title={t.title}
				start={vo.start - 14}
				style={{ position: 'absolute', left: 100, top: 64 }}
			/>

			<svg width={1920} height={1080} style={{ position: 'absolute', inset: 0 }}>
				{tnodes.map((n) => {
					if (!n.parent) return null;
					const pa = byId[n.parent];
					const p = prog(f, level(n.depth) - 4, 14);
					const isDown = n.id === 'printer-og' && down > 0.5;
					return (
						<line
							key={n.id}
							x1={pa.x}
							y1={pa.y}
							x2={pa.x + (n.x - pa.x) * p}
							y2={pa.y + (n.y - pa.y) * p}
							stroke={isDown ? C.danger : C.accentLine}
							strokeWidth={2}
							strokeDasharray={isDown ? '6 6' : undefined}
						/>
					);
				})}
			</svg>
			{tnodes.map((n) => {
				const p = pop(f, fps, level(n.depth), 13);
				const isDown = n.id === 'printer-og';
				const color = isDown ? (down > 0.5 ? C.danger : C.ok) : n.depth < 3 ? C.accent : C.ok;
				const pulse = isDown && down > 0 ? ((f - downAt) % 30) / 30 : 0;
				return (
					<div key={n.id} style={{ position: 'absolute', left: n.x, top: n.y }}>
						{pulse > 0 && (
							<div
								style={{
									position: 'absolute',
									left: -40,
									top: -40,
									width: 80,
									height: 80,
									borderRadius: '50%',
									border: `3px solid ${C.danger}`,
									transform: `scale(${1 + pulse})`,
									opacity: 1 - pulse
								}}
							/>
						)}
						<div style={{ transform: `translate(-50%, -50%) scale(${p})` }}>
							<Node icon={n.icon} size={n.depth < 3 ? 70 : 60} color={color} glow={isDown ? down : 0} />
						</div>
						<div
							style={{
								position: 'absolute',
								top: n.depth < 3 ? -12 : 38,
								left: n.depth < 3 ? 50 : 0,
								transform: n.depth < 3 ? 'translateY(-50%)' : 'translateX(-50%)',
								fontSize: 18,
								fontWeight: 600,
								color: isDown && down > 0.5 ? C.danger : C.muted,
								whiteSpace: 'nowrap',
								opacity: prog(f, level(n.depth) + 4, 12)
							}}
						>
							{n.id}
						</div>
					</div>
				);
			})}
			{/* health check on the NAS, outage on the printer */}
			<div style={{ position: 'absolute', left: 300, top: 930, ...enter(f, level(4) + 10, { dy: 10 }) }}>
				<div
					style={{
						display: 'inline-flex',
						alignItems: 'center',
						gap: 8,
						padding: '6px 12px',
						borderRadius: 10,
						background: C.okSoft,
						border: `1px solid ${C.ok}66`,
						color: C.ok,
						fontSize: 17,
						fontFamily: MONO
					}}
				>
					<Icon name="check-circle" size={18} color={C.ok} />
					{t.check}
				</div>
			</div>
			<div style={{ position: 'absolute', left: 104, top: 958, opacity: down }}>
				<div
					style={{
						display: 'inline-flex',
						alignItems: 'center',
						gap: 8,
						padding: '6px 12px',
						borderRadius: 10,
						background: C.dangerSoft,
						border: `1px solid ${C.danger}88`,
						color: C.danger,
						fontSize: 17,
						fontWeight: 650
					}}
				>
					<Icon name="x-circle" size={18} color={C.danger} />
					{t.down}
				</div>
			</div>

			{/* rack */}
			<div
				style={{
					position: 'absolute',
					left: RACK.x,
					top: RACK.y - 50,
					fontSize: 22,
					fontWeight: 650,
					color: C.muted,
					display: 'flex',
					alignItems: 'center',
					gap: 10,
					...enter(f, vo.start)
				}}
			>
				<Icon name="rack" size={24} color={C.accent} />
				{t.rack}
			</div>
			<div
				style={{
					position: 'absolute',
					left: RACK.x,
					top: RACK.y,
					width: RACK.w,
					height: 40 + RACK.u * 12,
					borderRadius: 14,
					background: '#0d121a',
					border: `2px solid ${C.borderStrong}`,
					boxShadow: '0 40px 90px rgba(0,0,0,0.55)',
					...enter(f, vo.start + 2, { dy: 40 })
				}}
			>
				{/* rails with holes */}
				{[10, RACK.w - 30].map((x) => (
					<div
						key={x}
						style={{
							position: 'absolute',
							left: x,
							top: 16,
							width: 16,
							bottom: 16,
							borderRadius: 3,
							background: '#161d28'
						}}
					>
						{Array.from({ length: 12 }, (_, i) => (
							<div
								key={i}
								style={{
									position: 'absolute',
									left: 5,
									top: 12 + i * RACK.u,
									width: 6,
									height: 6,
									borderRadius: 1,
									background: '#06090d'
								}}
							/>
						))}
					</div>
				))}
				<Unit u={0} label="patch-a">
					<Ports y={16} count={20} f={f} />
				</Unit>
				<Unit u={1}>
					<div
						style={{
							position: 'absolute',
							left: 20,
							right: 20,
							top: 16,
							height: 10,
							borderRadius: 5,
							background: 'repeating-linear-gradient(90deg, #0a0e14 0 3px, #1a2230 3px 6px)'
						}}
					/>
				</Unit>
				<Unit u={2} label="sw-core">
					<Ports y={18} count={20} leds f={f} />
				</Unit>
				<Unit u={3} label="opnsense">
					<Ports y={18} count={6} leds f={f} />
				</Unit>
				<Unit u={4} h={2} label="pve-01">
					{Array.from({ length: 8 }, (_, i) => (
						<div
							key={i}
							style={{
								position: 'absolute',
								left: 30 + i * 42,
								top: 14,
								width: 34,
								height: 60,
								borderRadius: 3,
								background: '#121924',
								border: '1px solid #3a4658'
							}}
						>
							<div
								style={{
									position: 'absolute',
									left: 4,
									bottom: 5,
									width: 5,
									height: 5,
									borderRadius: '50%',
									background: random(`d${i}${Math.floor(f / 6)}`) < 0.7 ? C.ok : '#1f5f3c'
								}}
							/>
						</div>
					))}
				</Unit>
				<Unit u={6} h={2} label="nas-01">
					{Array.from({ length: 4 }, (_, i) => (
						<div
							key={i}
							style={{
								position: 'absolute',
								left: 30 + i * 70,
								top: 14,
								width: 60,
								height: 60,
								borderRadius: 4,
								background: '#121924',
								border: '1px solid #3a4658'
							}}
						>
							<div
								style={{
									position: 'absolute',
									left: 6,
									bottom: 6,
									width: 6,
									height: 6,
									borderRadius: '50%',
									background: C.accent,
									opacity: random(`n${i}${Math.floor(f / 5)}`) < 0.6 ? 1 : 0.3
								}}
							/>
						</div>
					))}
				</Unit>
				<Unit u={8} />
				<Unit u={9} h={2} label="ups">
					<div
						style={{
							position: 'absolute',
							left: 30,
							top: 22,
							width: 120,
							height: 40,
							borderRadius: 4,
							background: '#0b2a1b',
							border: '1px solid #1f5f3c',
							fontFamily: MONO,
							fontSize: 16,
							color: C.ok,
							display: 'flex',
							alignItems: 'center',
							justifyContent: 'center'
						}}
					>
						100%
					</div>
				</Unit>
				<Unit u={11} />
			</div>
			<svg width={1920} height={1080} style={{ position: 'absolute', inset: 0 }}>
				<path
					d={cablePath}
					fill="none"
					stroke={C.amber}
					strokeWidth={5}
					strokeLinecap="round"
					pathLength={1}
					strokeDasharray="1 1"
					strokeDashoffset={1 - cable}
					style={{ filter: `drop-shadow(0 0 6px ${C.amber}88)` }}
				/>
				{cable > 0 && <circle cx={p1.x} cy={p1.y} r={6} fill={C.amber} />}
				{cable >= 1 && <circle cx={p2.x} cy={p2.y} r={6} fill={C.amber} />}
			</svg>
			<div
				style={{
					position: 'absolute',
					left: RACK.x,
					top: RACK.y + 40 + RACK.u * 12 + 26,
					display: 'flex',
					alignItems: 'center',
					gap: 12,
					padding: '12px 20px',
					borderRadius: 14,
					background: C.amberSoft,
					border: `1px solid ${C.amber}88`,
					color: C.fg,
					fontSize: 22,
					fontWeight: 600,
					...enter(f, cableAt + 22, { dy: 16 })
				}}
			>
				<Icon name="link" size={22} color={C.amber} />
				<span style={{ fontFamily: MONO }}>{t.cable[0]}</span>
				<Icon name="arrow-right" size={22} color={C.amber} />
				<span>{t.cable[1]}</span>
			</div>
		</SceneShell>
	);
};
