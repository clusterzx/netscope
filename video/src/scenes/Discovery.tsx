import { Audio } from '@remotion/media';
import { Sequence, staticFile, useCurrentFrame, useVideoConfig } from 'remotion';
import { enter, pop, prog, stagger } from '../anim';
import { Icon, type IconName } from '../components/Icon';
import { Radar } from '../components/Radar';
import { Card, Heading, Node, Pill, SceneShell } from '../components/ui';
import { C, MONO } from '../theme';
import type { SceneProps } from './types';

const CX = 520;
const CY = 560;
const R = 370;
const PERIOD = 96; // frames per sweep revolution
const START = 4;

// devices on the radar: angle (deg, 0 = right, clockwise), distance, found on sweep pass 0 or 1
const devices: {
	a: number;
	r: number;
	icon: IconName;
	name: string;
	ip: string;
	color: string;
	pass: number;
	label?: boolean;
}[] = [
	{ a: -60, r: 0.5, icon: 'server', name: 'nas-01', ip: '192.168.1.20', color: C.ok, pass: 0, label: true },
	{ a: -15, r: 0.86, icon: 'laptop', name: 'mbp-anna', ip: '192.168.1.34', color: C.ok, pass: 0 },
	{
		a: 28,
		r: 0.6,
		icon: 'printer',
		name: 'printer-og',
		ip: '192.168.1.50',
		color: C.ok,
		pass: 1,
		label: true
	},
	{
		a: 70,
		r: 0.88,
		icon: 'camera',
		name: 'esp32-cam',
		ip: '192.168.1.87',
		color: C.amber,
		pass: 1,
		label: true
	},
	{ a: 112, r: 0.42, icon: 'tv', name: 'lg-oled', ip: '192.168.1.61', color: C.ok, pass: 0 },
	{ a: 150, r: 0.8, icon: 'phone', name: 'pixel-8', ip: '192.168.1.73', color: C.ok, pass: 0, label: true },
	{ a: 196, r: 0.58, icon: 'wifi', name: 'ap-flur', ip: '192.168.1.4', color: C.ok, pass: 1 },
	{ a: 232, r: 0.86, icon: 'speaker', name: 'sonos-wz', ip: '192.168.1.66', color: C.ok, pass: 1 },
	{ a: 252, r: 0.38, icon: 'cpu', name: 'pve-01', ip: '192.168.1.10', color: C.ok, pass: 0, label: true },
	{ a: -100 + 360, r: 0.72, icon: 'gamepad', name: 'ps5', ip: '192.168.1.91', color: C.offline, pass: 1 }
];

// frame at which the sweep (starting at -90°) passes a device
const foundAt = (a: number, pass: number) => START + (((a + 90 + 360) % 360) / 360) * PERIOD + pass * PERIOD;

export const Discovery = ({ s, dur, vo, sound }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const angle = -90 + ((f - START) / PERIOD) * 360;
	const d = s.discovery;
	const scanStart = vo.start + 10;
	const scanSpan = vo.len * 0.5;
	const card = vo.start + vo.len * 0.6;

	const ports: [string, string, string][] = [
		['22/tcp', 'ssh', 'OpenSSH 9.6'],
		['443/tcp', 'https', 'nginx 1.25.4'],
		['5000/tcp', 'http', 'Synology DSM 7.2'],
		['445/tcp', 'smb', 'Samba 4.15']
	];

	return (
		<SceneShell dur={dur}>
			<Radar cx={CX} cy={CY} r={R} angle={angle} opacity={prog(f, 0, 16)} />
			<svg width={1920} height={1080} style={{ position: 'absolute', inset: 0 }}>
				{devices.map((dv) => {
					const t = foundAt(dv.a, dv.pass);
					const p = prog(f, t, 14);
					const x = CX + Math.cos((dv.a * Math.PI) / 180) * dv.r * R;
					const y = CY + Math.sin((dv.a * Math.PI) / 180) * dv.r * R;
					return (
						<line
							key={dv.name}
							x1={CX}
							y1={CY}
							x2={CX + (x - CX) * p}
							y2={CY + (y - CY) * p}
							stroke={C.accentLine}
							strokeWidth={1.5}
							opacity={0.7}
						/>
					);
				})}
			</svg>
			<div
				style={{
					position: 'absolute',
					left: CX,
					top: CY,
					transform: `translate(-50%, -50%) scale(${pop(f, fps, 0)})`
				}}
			>
				<Node icon="router" size={92} color={C.accent} glow={0.6} />
			</div>
			{devices.map((dv) => {
				const t = foundAt(dv.a, dv.pass);
				const p = pop(f, fps, t, 12);
				const x = CX + Math.cos((dv.a * Math.PI) / 180) * dv.r * R;
				const y = CY + Math.sin((dv.a * Math.PI) / 180) * dv.r * R;
				const ping = prog(f, t, 30);
				const right = x >= CX - 40;
				return (
					<div
						key={dv.name}
						style={{ position: 'absolute', left: x, top: y, width: 0, height: 0, opacity: f >= t ? 1 : 0 }}
					>
						<div
							style={{
								position: 'absolute',
								left: -40,
								top: -40,
								width: 80,
								height: 80,
								borderRadius: '50%',
								border: `3px solid ${dv.color}`,
								transform: `scale(${0.6 + ping * 1.4})`,
								opacity: (1 - ping) * 0.8
							}}
						/>
						<div
							style={{
								position: 'absolute',
								left: 0,
								top: 0,
								transform: `translate(-50%, -50%) scale(${p})`
							}}
						>
							<Node icon={dv.icon} size={62} color={dv.color} />
						</div>
						{dv.label && (
							<div
								style={{
									position: 'absolute',
									top: -27,
									[right ? 'left' : 'right']: 44,
									opacity: prog(f, t + 6, 14),
									whiteSpace: 'nowrap',
									textAlign: right ? 'left' : 'right'
								}}
							>
								<div style={{ fontSize: 22, fontWeight: 650, color: C.fg }}>{dv.name}</div>
								<div style={{ fontFamily: MONO, fontSize: 18, color: C.muted }}>{dv.ip}</div>
							</div>
						)}
					</div>
				);
			})}
			{sound &&
				devices.slice(0, 7).map((dv, i) => (
					<Sequence
						key={dv.name}
						from={Math.round(foundAt(dv.a, dv.pass))}
						durationInFrames={20}
						layout="none"
					>
						<Audio src={staticFile('audio/blip.wav')} volume={0.07 + (i % 2) * 0.03} />
					</Sequence>
				))}

			<div style={{ position: 'absolute', left: 1040, top: 150, width: 800 }}>
				<Heading
					icon="radar"
					eyebrow={d.eyebrow}
					title={d.title}
					sub={d.sub}
					start={vo.start - 8}
					size={84}
				/>
				<div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, marginTop: 38 }}>
					{d.scanners.map((name, i) => {
						const t = stagger(i, d.scanners.length, scanStart, scanSpan);
						const p = prog(f, t, 8);
						return (
							<div key={name} style={{ ...enter(f, scanStart - 14 + i * 1.2, { dy: 12, blur: 0 }) }}>
								<Pill label={name} t="accent" active={p} size={21} mono />
							</div>
						);
					})}
				</div>
				<Card style={{ marginTop: 40, padding: '26px 30px', ...enter(f, card, { dy: 30 }) }}>
					<div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 18 }}>
						<Icon name="server" size={30} color={C.ok} />
						<div style={{ fontSize: 26, fontWeight: 700 }}>nas-01</div>
						<div style={{ fontFamily: MONO, fontSize: 20, color: C.muted }}>192.168.1.20 · Synology</div>
					</div>
					{ports.map(([port, svc, ver], i) => (
						<div
							key={port}
							style={{
								display: 'flex',
								fontFamily: MONO,
								fontSize: 21,
								lineHeight: '36px',
								...enter(f, card + 8 + i * 5, { dy: 10, blur: 0 })
							}}
						>
							<span style={{ width: 150, color: C.accent }}>{port}</span>
							<span style={{ width: 110, color: C.muted }}>{svc}</span>
							<span style={{ color: C.fg }}>{ver}</span>
						</div>
					))}
					<div
						style={{
							display: 'flex',
							fontFamily: MONO,
							fontSize: 21,
							lineHeight: '36px',
							marginTop: 6,
							paddingTop: 8,
							borderTop: `1px solid ${C.border}`,
							...enter(f, card + 30, { dy: 10, blur: 0 })
						}}
					>
						<span style={{ width: 260, color: C.violet }}>{d.os}</span>
						<span style={{ color: C.fg }}>Linux 5.10 (DSM)</span>
					</div>
				</Card>
			</div>
		</SceneShell>
	);
};
