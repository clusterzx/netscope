import { Audio } from '@remotion/media';
import { Sequence, staticFile, useCurrentFrame, useVideoConfig } from 'remotion';
import { enter, pop, prog, stagger } from '../anim';
import { Icon, type IconName } from '../components/Icon';
import { Card, Heading, Pill, SceneShell } from '../components/ui';
import { C, MONO } from '../theme';
import type { SceneProps } from './types';

const apps: Record<string, { icon: IconName; color: string }> = {
	Telegram: { icon: 'send', color: '#2aabee' },
	ntfy: { icon: 'bell', color: '#3ec27a' },
	'E-Mail': { icon: 'note', color: '#4cb4ec' },
	'E-mail': { icon: 'note', color: '#4cb4ec' },
	n8n: { icon: 'zap', color: '#ea4b71' }
};
const publishers = ['Telegram', 'ntfy', 'E-Mail', 'Webhook', 'n8n'];
const PHONE = { x: 1170, y: 96, w: 500, h: 900 };

export const Alerts = ({ s, dur, vo, sound }: SceneProps) => {
	const f = useCurrentFrame();
	const { fps } = useVideoConfig();
	const t = s.alerts;
	const notes = t.notifications;
	const noteAt = (i: number) => stagger(i, notes.length, vo.start + vo.len * 0.3, vo.len * 0.58);
	const pubAt = (i: number) => stagger(i, publishers.length, vo.start + vo.len * 0.48, vo.len * 0.44);

	return (
		<SceneShell dur={dur}>
			<Heading
				icon="bell"
				eyebrow={t.eyebrow}
				title={t.title}
				start={vo.start - 14}
				style={{ position: 'absolute', left: 100, top: 64 }}
			/>

			<Card
				style={{
					position: 'absolute',
					left: 100,
					top: 290,
					width: 860,
					padding: '30px 36px',
					...enter(f, vo.start - 2, { dy: 40 })
				}}
			>
				<div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 22 }}>
					<div
						style={{
							width: 46,
							height: 46,
							borderRadius: 12,
							background: C.accentSoft,
							display: 'flex',
							alignItems: 'center',
							justifyContent: 'center'
						}}
					>
						<Icon name="rules" size={26} color={C.accent} />
					</div>
					<div style={{ fontSize: 28, fontWeight: 700 }}>{t.ruleName}</div>
				</div>
				{t.rule.map(([k, v], i) => (
					<div
						key={k}
						style={{
							display: 'flex',
							alignItems: 'center',
							padding: '14px 0',
							borderTop: `1px solid ${C.border}`,
							...enter(f, vo.start + 4 + i * 4, { dy: 12, blur: 0 })
						}}
					>
						<div
							style={{
								width: 200,
								fontSize: 21,
								color: C.subtle,
								fontWeight: 600,
								textTransform: 'uppercase',
								letterSpacing: '0.05em'
							}}
						>
							{k}
						</div>
						<div
							style={{
								fontSize: 24,
								color: C.fg,
								fontFamily: /\d/.test(v) && !/Min|min/.test(v) ? MONO : undefined,
								whiteSpace: 'pre'
							}}
						>
							{v}
						</div>
					</div>
				))}
				<div style={{ borderTop: `1px solid ${C.border}`, paddingTop: 20, marginTop: 4 }}>
					<div
						style={{
							fontSize: 21,
							color: C.subtle,
							fontWeight: 600,
							textTransform: 'uppercase',
							letterSpacing: '0.05em',
							marginBottom: 14
						}}
					>
						{t.to}
					</div>
					<div style={{ display: 'flex', gap: 12 }}>
						{publishers.map((p, i) => (
							<Pill key={p} label={p} icon="send" t="amber" active={prog(f, pubAt(i), 8)} size={22} />
						))}
					</div>
				</div>
			</Card>

			{/* phone */}
			<div
				style={{
					position: 'absolute',
					left: PHONE.x,
					top: PHONE.y,
					width: PHONE.w,
					height: PHONE.h,
					borderRadius: 64,
					padding: 14,
					background: '#05070a',
					border: `2px solid ${C.borderStrong}`,
					boxShadow: '0 50px 120px rgba(0,0,0,0.6)',
					...enter(f, vo.start, { dy: 60 })
				}}
			>
				<div
					style={{
						position: 'relative',
						width: '100%',
						height: '100%',
						borderRadius: 52,
						overflow: 'hidden',
						background: 'radial-gradient(120% 80% at 30% 0%, #17324a 0%, #0d1622 55%, #090d14 100%)'
					}}
				>
					<div
						style={{
							position: 'absolute',
							top: 14,
							left: '50%',
							width: 120,
							height: 34,
							borderRadius: 20,
							background: '#000',
							transform: 'translateX(-50%)'
						}}
					/>
					<div style={{ textAlign: 'center', marginTop: 90, fontSize: 24, color: C.muted, fontWeight: 500 }}>
						{t.lock}
					</div>
					<div
						style={{
							textAlign: 'center',
							fontSize: 110,
							fontWeight: 300,
							letterSpacing: '-0.03em',
							lineHeight: 1.05,
							color: '#f3f6fa'
						}}
					>
						09:41
					</div>
					<div style={{ position: 'absolute', left: 18, right: 18, top: 290 }}>
						{notes.map(([app, when, title, body], i) => {
							// newest on top: each later notification pushes the earlier ones down
							const p = pop(f, fps, noteAt(i), 14);
							const below = notes
								.slice(i + 1)
								.reduce((acc, _, k) => acc + Math.min(1, pop(f, fps, noteAt(i + 1 + k), 16)), 0);
							const a = apps[app] ?? apps.ntfy;
							return (
								<div
									key={title}
									style={{
										position: 'absolute',
										left: 0,
										right: 0,
										top: below * 132,
										height: 118,
										borderRadius: 26,
										padding: '16px 18px',
										background: 'rgba(30,40,56,0.86)',
										border: '1px solid rgba(255,255,255,0.08)',
										boxShadow: '0 10px 30px rgba(0,0,0,0.35)',
										opacity: Math.min(1, p),
										transform: `translateY(${(1 - p) * -50}px) scale(${0.92 + 0.08 * Math.min(1, p)})`,
										display: 'flex',
										gap: 14
									}}
								>
									<div
										style={{
											width: 46,
											height: 46,
											borderRadius: 12,
											background: a.color,
											display: 'flex',
											alignItems: 'center',
											justifyContent: 'center',
											flexShrink: 0
										}}
									>
										<Icon name={a.icon} size={26} color="#fff" />
									</div>
									<div style={{ flex: 1, minWidth: 0 }}>
										<div style={{ display: 'flex', fontSize: 17, color: C.muted, marginBottom: 3 }}>
											<span style={{ fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.04em' }}>
												{app} · NetScope
											</span>
											<span style={{ marginLeft: 'auto' }}>{when}</span>
										</div>
										<div
											style={{
												fontSize: 21,
												fontWeight: 650,
												color: '#f3f6fa',
												whiteSpace: 'nowrap',
												overflow: 'hidden',
												textOverflow: 'ellipsis'
											}}
										>
											{title}
										</div>
										<div
											style={{
												fontSize: 18,
												color: C.muted,
												whiteSpace: 'nowrap',
												overflow: 'hidden',
												textOverflow: 'ellipsis'
											}}
										>
											{body}
										</div>
									</div>
								</div>
							);
						})}
					</div>
				</div>
			</div>
			{sound &&
				notes.map((_, i) => (
					<Sequence key={i} from={Math.round(noteAt(i))} durationInFrames={40} layout="none">
						<Audio src={staticFile('audio/ding.wav')} volume={0.16} />
					</Sequence>
				))}
		</SceneShell>
	);
};
