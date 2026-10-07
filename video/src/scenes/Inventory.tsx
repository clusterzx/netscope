import { interpolate, useCurrentFrame, useVideoConfig } from 'remotion';
import { easeInOut, enter, pop, prog } from '../anim';
import { Icon, type IconName } from '../components/Icon';
import { Card, Dot, Heading, Pill, SceneShell } from '../components/ui';
import { C, MONO, tone } from '../theme';
import type { SceneProps } from './types';

type Row = {
	icon: IconName;
	name: string;
	ip: string;
	vendor: string;
	type: keyof SceneProps['s']['inventory']['types'];
	online: boolean;
};

const rows: Row[] = [
	{ icon: 'shield', name: 'opnsense', ip: '192.168.1.1', vendor: 'Deciso', type: 'firewall', online: true },
	{ icon: 'cpu', name: 'pve-01', ip: '192.168.1.10', vendor: 'Supermicro', type: 'hypervisor', online: true },
	{ icon: 'disk', name: 'nas-01', ip: '192.168.1.20', vendor: 'Synology', type: 'nas', online: true },
	{ icon: 'network', name: 'sw-core', ip: '192.168.1.2', vendor: 'MikroTik', type: 'switch', online: true },
	{ icon: 'wifi', name: 'ap-flur', ip: '192.168.1.4', vendor: 'Ubiquiti', type: 'ap', online: true },
	{
		icon: 'printer',
		name: 'printer-og',
		ip: '192.168.1.50',
		vendor: 'Brother',
		type: 'printer',
		online: true
	},
	{ icon: 'tv', name: 'lg-oled', ip: '192.168.1.61', vendor: 'LG Electronics', type: 'tv', online: true }
];
const fresh: Row = {
	icon: 'camera',
	name: 'esp32-cam',
	ip: '192.168.1.87',
	vendor: 'Espressif',
	type: 'camera',
	online: true
};

const COLS = [300, 220, 210, 170, 120];
const ROW_H = 74;

export const Inventory = ({ s, dur, vo }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const t = s.inventory;
	const at = (k: number) => vo.start + vo.len * k;
	const eventAt = [0.38, 0.55, 0.66, 0.76, 0.88].map(at);
	const newAt = eventAt[3];
	const offlineAt = eventAt[4];
	const insert = prog(f, newAt - 4, 16, easeInOut);
	const newGlow = interpolate(f, [newAt, newAt + 10, newAt + 70], [0, 1, 0.35], {
		extrapolateLeft: 'clamp',
		extrapolateRight: 'clamp'
	});

	const cells = (r: Row, isNew: boolean, offline: number) => {
		const status = offline > 0.5 ? ['offline', C.offline] : ['online', C.ok];
		return (
			<>
				<div style={{ width: COLS[0], display: 'flex', alignItems: 'center', gap: 16 }}>
					<div
						style={{
							width: 44,
							height: 44,
							borderRadius: 12,
							background: isNew ? C.amberSoft : C.card3,
							display: 'flex',
							alignItems: 'center',
							justifyContent: 'center'
						}}
					>
						<Icon name={r.icon} size={24} color={isNew ? C.amber : C.fg} />
					</div>
					<span style={{ fontSize: 24, fontWeight: 650 }}>{r.name}</span>
					{isNew && <Pill label={t.newBadge} t="amber" size={16} />}
				</div>
				<div style={{ width: COLS[1], fontFamily: MONO, fontSize: 21, color: C.muted }}>{r.ip}</div>
				<div style={{ width: COLS[2], fontSize: 22, color: C.muted }}>{r.vendor}</div>
				<div style={{ width: COLS[3], fontSize: 21, color: C.fg }}>{t.types[r.type]}</div>
				<div
					style={{
						width: COLS[4],
						display: 'flex',
						alignItems: 'center',
						gap: 10,
						fontSize: 21,
						color: status[1]
					}}
				>
					<Dot color={status[1]} size={11} pulse={offline > 0 && offline < 1 ? offline : 0} />
					{status[0]}
				</div>
			</>
		);
	};

	return (
		<SceneShell dur={dur}>
			<Heading
				icon="devices"
				eyebrow={t.eyebrow}
				title={t.title}
				start={vo.start - 12}
				style={{ position: 'absolute', left: 100, top: 70 }}
			/>

			<Card
				style={{
					position: 'absolute',
					left: 100,
					top: 270,
					width: 1060,
					height: 62 + 8 * ROW_H + 14,
					overflow: 'hidden',
					...enter(f, vo.start - 6, { dy: 40 })
				}}
			>
				<div
					style={{
						display: 'flex',
						padding: '0 30px',
						height: 62,
						alignItems: 'center',
						borderBottom: `1px solid ${C.border}`,
						background: 'rgba(0,0,0,0.15)'
					}}
				>
					{t.headers.map((h, i) => (
						<div
							key={h}
							style={{
								width: COLS[i],
								fontSize: 18,
								fontWeight: 600,
								color: C.subtle,
								textTransform: 'uppercase',
								letterSpacing: '0.06em'
							}}
						>
							{h}
						</div>
					))}
				</div>
				<div style={{ position: 'relative' }}>
					{/* the new device slides in on top and pushes the others down */}
					<div
						style={{
							position: 'absolute',
							left: 0,
							right: 0,
							top: 0,
							height: ROW_H,
							display: 'flex',
							alignItems: 'center',
							padding: '0 30px',
							background: `rgba(245,165,36,${0.1 * newGlow + 0.04 * insert})`,
							boxShadow: `inset 4px 0 0 ${C.amber}`,
							opacity: insert,
							transform: `translateX(${(1 - insert) * -60}px)`
						}}
					>
						{cells(fresh, true, 0)}
					</div>
					{rows.map((r, i) => {
						const p = prog(f, vo.start + i * 3, 16);
						const offline = r.name === 'printer-og' ? prog(f, offlineAt, 12) : 0;
						return (
							<div
								key={r.name}
								style={{
									position: 'absolute',
									left: 0,
									right: 0,
									top: (i + insert) * ROW_H,
									height: ROW_H,
									display: 'flex',
									alignItems: 'center',
									padding: '0 30px',
									borderTop: `1px solid ${C.border}`,
									opacity: p * (offline > 0.5 ? 0.6 : 1),
									transform: `translateY(${(1 - p) * 20}px)`
								}}
							>
								{cells(r, false, offline)}
							</div>
						);
					})}
				</div>
			</Card>

			<Card
				style={{
					position: 'absolute',
					left: 1210,
					top: 270,
					width: 610,
					padding: '28px 32px',
					...enter(f, vo.start + 6, { dy: 40 })
				}}
			>
				<div
					style={{
						display: 'flex',
						alignItems: 'center',
						gap: 14,
						fontSize: 26,
						fontWeight: 700,
						marginBottom: 24
					}}
				>
					<Icon name="history" size={28} color={C.accent} />
					{t.history}
				</div>
				<div style={{ position: 'relative' }}>
					<div
						style={{ position: 'absolute', left: 21, top: 10, bottom: 10, width: 2, background: C.border }}
					/>
					{t.events.map(([icon, tn, title, detail], i) => {
						const p = pop(f, fps, eventAt[i], 15);
						const [fg, soft] = tone(tn);
						return (
							<div
								key={title}
								style={{
									display: 'flex',
									gap: 20,
									alignItems: 'flex-start',
									marginBottom: 22,
									opacity: Math.min(1, p),
									transform: `translateX(${(1 - p) * 40}px)`
								}}
							>
								<div
									style={{
										position: 'relative',
										width: 44,
										height: 44,
										borderRadius: '50%',
										background: soft,
										border: `2px solid ${fg}`,
										display: 'flex',
										alignItems: 'center',
										justifyContent: 'center',
										flexShrink: 0,
										backgroundColor: C.card
									}}
								>
									<Icon name={icon} size={22} color={fg} />
								</div>
								<div>
									<div style={{ fontSize: 24, fontWeight: 650 }}>{title}</div>
									<div style={{ fontSize: 19, color: C.muted, fontFamily: MONO, marginTop: 4 }}>{detail}</div>
								</div>
							</div>
						);
					})}
				</div>
			</Card>

			<div
				style={{
					position: 'absolute',
					right: 100,
					top: 900,
					width: 610,
					display: 'flex',
					alignItems: 'center',
					gap: 18,
					padding: '18px 24px',
					borderRadius: 18,
					background: C.card2,
					border: `1.5px solid ${C.amber}`,
					boxShadow: `0 0 40px rgba(245,165,36,0.25), 0 20px 50px rgba(0,0,0,0.5)`,
					opacity: Math.min(1, pop(f, fps, newAt + 4)),
					transform: `translateY(${(1 - Math.min(1, pop(f, fps, newAt + 4))) * 60}px)`
				}}
			>
				<div
					style={{
						width: 52,
						height: 52,
						borderRadius: 14,
						background: C.amberSoft,
						display: 'flex',
						alignItems: 'center',
						justifyContent: 'center'
					}}
				>
					<Icon name="bell" size={28} color={C.amber} />
				</div>
				<div>
					<div style={{ fontSize: 24, fontWeight: 700 }}>{t.toast[0]}</div>
					<div style={{ fontSize: 19, color: C.muted, fontFamily: MONO, marginTop: 2 }}>{t.toast[1]}</div>
				</div>
			</div>
		</SceneShell>
	);
};
