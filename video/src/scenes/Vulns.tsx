import { spring, useCurrentFrame, useVideoConfig } from 'remotion';
import { enter, prog } from '../anim';
import { Icon } from '../components/Icon';
import { Card, Heading, SceneShell } from '../components/ui';
import { C, MONO } from '../theme';
import type { SceneProps } from './types';

// sample findings as the CVE page lists them
const findings = [
	{ id: 'CVE-2021-44228', pkg: 'log4j-core 2.14.1', device: 'unifi-ctrl', cvss: 10.0, epss: 94.4, kev: true },
	{ id: 'CVE-2024-3094', pkg: 'xz-utils 5.6.0', device: 'build-02', cvss: 10.0, epss: 8.3, kev: false },
	{ id: 'CVE-2023-38545', pkg: 'curl 8.3.0', device: 'web-01', cvss: 9.8, epss: 2.1, kev: false },
	{ id: 'CVE-2024-6387', pkg: 'OpenSSH 9.6p1', device: 'nas-01', cvss: 8.1, epss: 37.0, kev: false },
	{ id: 'CVE-2023-44487', pkg: 'nginx 1.25.2', device: 'proxy-01', cvss: 7.5, epss: 81.2, kev: true }
];
// listed by CVSS first, then re-sorted by priority: known exploited (KEV) first, then EPSS
const byPriority = [...findings]
	.sort((a, b) => Number(b.kev) - Number(a.kev) || b.epss - a.epss)
	.map((x) => x.id);

const COLS = [330, 300, 230, 280, 260, 230];
const ROW_H = 110;
const sevColor = (cvss: number) => (cvss >= 9 ? C.danger : cvss >= 7 ? C.high : C.medium);

export const Vulns = ({ s, dur, vo }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const t = s.vulns;
	const sortAt = vo.start + vo.len * 0.5;
	const toggle = prog(f, sortAt - 6, 12);
	const top = prog(f, sortAt + 24, 20);

	return (
		<SceneShell dur={dur}>
			<Heading
				icon="shield"
				eyebrow={t.eyebrow}
				title={t.title}
				sub={t.sub}
				start={vo.start - 12}
				style={{ position: 'absolute', left: 100, top: 64 }}
			/>

			<div
				style={{
					position: 'absolute',
					right: 100,
					top: 222,
					display: 'flex',
					alignItems: 'center',
					gap: 16,
					...enter(f, vo.start + 4)
				}}
			>
				<span style={{ fontSize: 22, color: C.muted }}>{t.sortedBy}</span>
				<div
					style={{
						position: 'relative',
						display: 'flex',
						padding: 5,
						borderRadius: 14,
						background: C.card,
						border: `1px solid ${C.border}`
					}}
				>
					<div
						style={{
							position: 'absolute',
							top: 5,
							bottom: 5,
							left: 5 + toggle * 150,
							width: 150,
							borderRadius: 10,
							background: toggle > 0.5 ? C.dangerSoft : C.accentSoft,
							border: `1px solid ${toggle > 0.5 ? C.danger : C.accent}66`
						}}
					/>
					{[t.cvss, t.priority].map((label, i) => (
						<div
							key={label}
							style={{
								position: 'relative',
								width: 150,
								textAlign: 'center',
								padding: '10px 0',
								fontSize: 21,
								fontWeight: 650,
								color: (i === 1) === toggle > 0.5 ? (i ? C.danger : C.accent) : C.subtle
							}}
						>
							{label}
						</div>
					))}
				</div>
			</div>

			<Card
				style={{
					position: 'absolute',
					left: 100,
					top: 310,
					width: 1720,
					height: 64 + ROW_H * findings.length + 10,
					overflow: 'hidden',
					...enter(f, vo.start - 4, { dy: 40 })
				}}
			>
				<div
					style={{
						display: 'flex',
						padding: '0 36px',
						height: 64,
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
					{findings.map((x, i) => {
						const j = byPriority.indexOf(x.id);
						const move = spring({
							frame: f - sortAt - Math.abs(j - i) * 2,
							fps,
							config: { damping: 16, stiffness: 120 }
						});
						const y = (i + (j - i) * move) * ROW_H;
						const enterP = prog(f, vo.start + i * 4, 16);
						const sev = sevColor(x.cvss);
						const first = j === 0 ? top : 0;
						return (
							<div
								key={x.id}
								style={{
									position: 'absolute',
									left: 0,
									right: 0,
									top: y,
									height: ROW_H,
									display: 'flex',
									alignItems: 'center',
									padding: '0 36px',
									borderTop: `1px solid ${C.border}`,
									boxShadow: `inset 5px 0 0 ${sev}`,
									background: `rgba(251,95,126,${0.08 * first})`,
									opacity: enterP,
									zIndex: Math.round(Math.abs(j - i) * move * 10)
								}}
							>
								<div style={{ width: COLS[0], fontFamily: MONO, fontSize: 24, fontWeight: 700, color: C.fg }}>
									{x.id}
								</div>
								<div style={{ width: COLS[1], fontSize: 23, color: C.fg }}>{x.pkg}</div>
								<div style={{ width: COLS[2], fontFamily: MONO, fontSize: 21, color: C.muted }}>
									{x.device}
								</div>
								<div style={{ width: COLS[3], display: 'flex', alignItems: 'center', gap: 14 }}>
									<span style={{ fontFamily: MONO, fontSize: 24, fontWeight: 700, color: sev, width: 64 }}>
										{x.cvss.toFixed(1)}
									</span>
									<div style={{ width: 150, height: 8, borderRadius: 4, background: C.card3 }}>
										<div
											style={{
												width: `${x.cvss * 10 * enterP}%`,
												height: '100%',
												borderRadius: 4,
												background: sev
											}}
										/>
									</div>
								</div>
								<div style={{ width: COLS[4], display: 'flex', alignItems: 'center', gap: 14 }}>
									<span
										style={{
											fontFamily: MONO,
											fontSize: 22,
											color: x.epss > 30 ? C.amber : C.muted,
											width: 86
										}}
									>
										{x.epss.toFixed(1)}%
									</span>
									<div style={{ width: 120, height: 8, borderRadius: 4, background: C.card3 }}>
										<div
											style={{
												width: `${x.epss * enterP}%`,
												height: '100%',
												borderRadius: 4,
												background: x.epss > 30 ? C.amber : C.subtle
											}}
										/>
									</div>
								</div>
								<div style={{ width: COLS[5] }}>
									{x.kev ? (
										<div
											style={{
												display: 'inline-flex',
												alignItems: 'center',
												gap: 10,
												padding: '8px 16px',
												borderRadius: 999,
												background: C.dangerSoft,
												border: `1px solid ${C.danger}88`,
												color: C.danger,
												fontSize: 19,
												fontWeight: 650,
												boxShadow: `0 0 ${20 * toggle}px ${C.danger}44`
											}}
										>
											<Icon name="alert" size={20} color={C.danger} />
											{t.kevYes}
										</div>
									) : (
										<span style={{ color: C.subtle, fontSize: 22 }}>–</span>
									)}
								</div>
							</div>
						);
					})}
				</div>
			</Card>
		</SceneShell>
	);
};
