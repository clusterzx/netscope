// Brand colours of the NetScope web UI (dark theme) and README graphics.
export const C = {
	bg0: '#0b1017',
	bg1: '#0f1826',
	card: '#121a25',
	card2: '#172131',
	card3: '#1c2738',
	border: '#253245',
	borderStrong: '#34435c',
	fg: '#e8ecf2',
	muted: '#a3adbd',
	subtle: '#6f7a8c',
	accent: '#4cb4ec',
	accentSoft: 'rgba(76,180,236,0.14)',
	accentLine: 'rgba(76,180,236,0.38)',
	amber: '#f5a524',
	amberSoft: 'rgba(245,165,36,0.15)',
	ok: '#3ec27a',
	okSoft: 'rgba(62,194,122,0.15)',
	violet: '#a78bfa',
	violetSoft: 'rgba(167,139,250,0.16)',
	danger: '#fb5f7e',
	dangerSoft: 'rgba(251,95,126,0.16)',
	high: '#fb8a3c',
	highSoft: 'rgba(251,138,60,0.15)',
	medium: '#eab308',
	offline: '#5d6677',
	logoBg: '#0f172a',
	logoRing: '#38bdf8',
	logoNeedle: '#f59e0b'
} as const;

export type Tone = 'accent' | 'amber' | 'ok' | 'violet' | 'danger' | 'muted' | 'high';
export const tone = (t: Tone) =>
	({
		accent: [C.accent, C.accentSoft],
		amber: [C.amber, C.amberSoft],
		ok: [C.ok, C.okSoft],
		violet: [C.violet, C.violetSoft],
		danger: [C.danger, C.dangerSoft],
		high: [C.high, C.highSoft],
		muted: [C.muted, 'rgba(163,173,189,0.12)']
	})[t] as [string, string];

export const SANS = "'Inter', 'Segoe UI', sans-serif";
export const MONO = "'JetBrains Mono', 'SFMono-Regular', Consolas, monospace";
