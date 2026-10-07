import { useCurrentFrame, useVideoConfig } from 'remotion';
import { enter, pop, prog, stagger } from '../anim';
import { Icon, type IconName } from '../components/Icon';
import { Logo } from '../components/Logo';
import { Card, Heading, Pill, SceneShell } from '../components/ui';
import { C, MONO, tone } from '../theme';
import type { SceneProps } from './types';

const importers: [IconName, string][] = [
	['server', 'Proxmox'],
	['shield', 'OPNsense'],
	['shield', 'pfSense'],
	['wifi', 'UniFi'],
	['router', 'MikroTik'],
	['shield', 'FortiGate'],
	['cloud', 'Meraki'],
	['home', 'FRITZ!Box'],
	['globe', 'Pi-hole'],
	['package', 'Docker']
];

const HUB = { x: 960, y: 610 };
const CARD_W = 270;
const CARD_H = 72;
const cardPos = (i: number) => ({ x: 90 + (i % 2) * (CARD_W + 26), y: 300 + Math.floor(i / 2) * 94 });
const TERM = { x: 1150, y: 300, w: 690 };

const CMD = [
	'$ curl -fsSL http://netscope.lan:8080/agent/install.sh \\',
	'    | sudo sh -s -- --token nse_8f2c41d9…'
];

// point on a cubic Bézier between a and b with horizontal tangents
const bez = (a: { x: number; y: number }, b: { x: number; y: number }, t: number) => {
	const mx = (a.x + b.x) / 2;
	const u = 1 - t;
	return {
		x: u * u * u * a.x + 3 * u * u * t * mx + 3 * u * t * t * mx + t * t * t * b.x,
		y: u * u * u * a.y + 3 * u * u * t * a.y + 3 * u * t * t * b.y + t * t * t * b.y
	};
};
const bezPath = (a: { x: number; y: number }, b: { x: number; y: number }) => {
	const mx = (a.x + b.x) / 2;
	return `M${a.x} ${a.y} C${mx} ${a.y} ${mx} ${b.y} ${b.x} ${b.y}`;
};

export const Sources = ({ s, dur, vo }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const t = s.sources;
	const cardsAt = (i: number) => stagger(i, importers.length, vo.start - 4, vo.len * 0.36);
	const agent = vo.start + vo.len * 0.5;
	const typed = Math.max(0, (f - agent) * 2.6);
	const cmdLen = CMD.join('').length;
	const outAt = (i: number) => agent + cmdLen / 2.6 + 6 + i * 7;
	const flows = [
		...importers.map((_, i) => ({
			from: { x: cardPos(i).x + CARD_W, y: cardPos(i).y + CARD_H / 2 },
			to: { x: HUB.x - 92, y: HUB.y },
			at: cardsAt(i) + 6,
			color: C.violet
		})),
		{ from: { x: TERM.x, y: TERM.y + 170 }, to: { x: HUB.x + 92, y: HUB.y }, at: outAt(1), color: C.ok }
	];

	return (
		<SceneShell dur={dur}>
			<Heading
				icon="download"
				eyebrow={t.eyebrow}
				title={t.title}
				start={vo.start - 14}
				align="center"
				size={64}
				style={{ position: 'absolute', top: 64, left: 0, right: 0 }}
			/>

			<svg width={1920} height={1080} style={{ position: 'absolute', inset: 0 }}>
				{flows.map((fl, i) => {
					const p = prog(f, fl.at, 18);
					return (
						<path
							key={i}
							d={bezPath(fl.from, fl.to)}
							fill="none"
							stroke={fl.color}
							strokeOpacity={0.3 * p}
							strokeWidth={2}
							strokeDasharray="5 7"
						/>
					);
				})}
				{flows.map((fl, i) =>
					f < fl.at + 6
						? null
						: [0, 0.5].map((off) => {
								const k = ((f - fl.at) / 38 + off + i * 0.13) % 1;
								const pt = bez(fl.from, fl.to, k);
								return (
									<circle
										key={`${i}-${off}`}
										cx={pt.x}
										cy={pt.y}
										r={4.5}
										fill={fl.color}
										opacity={Math.sin(Math.PI * k) * 0.95}
									/>
								);
							})
				)}
			</svg>

			{importers.map(([icon, name], i) => {
				const p = pop(f, fps, cardsAt(i), 14);
				const { x, y } = cardPos(i);
				const lit = prog(f, cardsAt(i) + 2, 10);
				return (
					<div
						key={name}
						style={{
							position: 'absolute',
							left: x,
							top: y,
							width: CARD_W,
							height: CARD_H,
							borderRadius: 16,
							display: 'flex',
							alignItems: 'center',
							gap: 16,
							padding: '0 20px',
							background: `linear-gradient(180deg, ${C.card2}, ${C.card})`,
							border: `1.5px solid ${lit > 0.5 ? 'rgba(167,139,250,0.55)' : C.border}`,
							boxShadow: `0 0 ${30 * lit}px rgba(167,139,250,0.18), 0 16px 40px rgba(0,0,0,0.35)`,
							opacity: Math.min(1, p),
							transform: `translateX(${(1 - p) * -40}px)`
						}}
					>
						<div
							style={{
								width: 42,
								height: 42,
								borderRadius: 12,
								background: C.violetSoft,
								display: 'flex',
								alignItems: 'center',
								justifyContent: 'center'
							}}
						>
							<Icon name={icon} size={24} color={C.violet} />
						</div>
						<div style={{ fontSize: 25, fontWeight: 650 }}>{name}</div>
					</div>
				);
			})}
			<div
				style={{
					position: 'absolute',
					left: 90,
					top: 300 + 5 * 94 + 6,
					fontSize: 22,
					color: C.subtle,
					fontWeight: 500,
					...enter(f, cardsAt(9) + 6)
				}}
			>
				{t.more}
			</div>

			<div
				style={{
					position: 'absolute',
					left: HUB.x,
					top: HUB.y,
					transform: `translate(-50%, -50%) scale(${pop(f, fps, vo.start - 10)})`
				}}
			>
				{[0, 1, 2].map((k) => {
					const ph = ((f + k * 20) % 60) / 60;
					return (
						<div
							key={k}
							style={{
								position: 'absolute',
								left: 85 - 90,
								top: 85 - 90,
								width: 180,
								height: 180,
								borderRadius: 40,
								border: `2px solid ${C.accent}`,
								transform: `scale(${1 + ph * 0.6})`,
								opacity: (1 - ph) * 0.35
							}}
						/>
					);
				})}
				<Logo size={170} glow={0.8} />
			</div>

			<Card
				style={{
					position: 'absolute',
					left: TERM.x,
					top: TERM.y,
					width: TERM.w,
					overflow: 'hidden',
					...enter(f, agent - 12, { dy: 30 })
				}}
			>
				<div
					style={{
						display: 'flex',
						alignItems: 'center',
						gap: 10,
						padding: '16px 22px',
						borderBottom: `1px solid ${C.border}`,
						background: 'rgba(0,0,0,0.18)'
					}}
				>
					{['#fb5f7e', '#f5a524', '#3ec27a'].map((c) => (
						<div
							key={c}
							style={{ width: 13, height: 13, borderRadius: '50%', background: c, opacity: 0.85 }}
						/>
					))}
					<div style={{ marginLeft: 12, fontSize: 19, color: C.muted, fontWeight: 550 }}>{t.terminal}</div>
					<div style={{ marginLeft: 'auto', display: 'flex', gap: 8 }}>
						<Pill label="Linux" icon="terminal" t="accent" active={1} size={16} />
						<Pill label="Windows" icon="monitor" t="accent" active={0} size={16} />
					</div>
				</div>
				<div
					style={{
						padding: '22px 26px 26px',
						fontFamily: MONO,
						fontSize: 18.5,
						lineHeight: '32px',
						minHeight: 260
					}}
				>
					{CMD.map((line, i) => {
						const before = CMD.slice(0, i).join('').length;
						const shown = line.slice(0, Math.max(0, Math.floor(typed - before)));
						const cursor = typed >= before && typed < before + line.length + 1;
						return (
							<div key={i} style={{ whiteSpace: 'pre', color: C.fg }}>
								{shown.startsWith('$') ? (
									<>
										<span style={{ color: C.accent }}>$</span>
										{shown.slice(1)}
									</>
								) : (
									shown
								)}
								{cursor && (
									<span style={{ background: C.fg, opacity: Math.floor(f / 8) % 2 ? 0.9 : 0.2 }}>&nbsp;</span>
								)}
							</div>
						);
					})}
					<div style={{ height: 14 }} />
					{t.output.map(([tn, line], i) => {
						const [c] = tone(tn);
						return (
							<div
								key={line}
								style={{
									display: 'flex',
									alignItems: 'center',
									gap: 12,
									color: C.fg,
									...enter(f, outAt(i), { dy: 8, blur: 0, dur: 10 })
								}}
							>
								<Icon name={tn === 'ok' ? 'check' : 'refresh'} size={20} color={c} stroke={2.4} />
								<span style={{ fontFamily: MONO }}>{line}</span>
							</div>
						);
					})}
				</div>
			</Card>
			<div
				style={{
					position: 'absolute',
					left: TERM.x,
					top: 740,
					width: TERM.w,
					display: 'flex',
					flexWrap: 'wrap',
					gap: 12
				}}
			>
				{t.badges.map((b, i) => {
					const p = pop(f, fps, outAt(3) + 4 + i * 5, 14);
					return (
						<div key={b} style={{ opacity: Math.min(1, p), transform: `scale(${0.85 + 0.15 * p})` }}>
							<Pill label={b} icon={(['laptop', 'lock', 'refresh'] as IconName[])[i]} t="ok" size={20} />
						</div>
					);
				})}
			</div>
		</SceneShell>
	);
};
