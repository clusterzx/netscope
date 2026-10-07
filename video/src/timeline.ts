import script from './voiceover.json';

export const FPS = 30;
export const WIDTH = 1920;
export const HEIGHT = 1080;

export type Lang = 'de' | 'en';

export const SCENES = [
	'hook',
	'questions',
	'reveal',
	'discovery',
	'sources',
	'inventory',
	'vulns',
	'topology',
	'alerts',
	'selfhosted',
	'outro'
] as const;
export type SceneId = (typeof SCENES)[number];

// public/voiceover/<lang>/manifest.json, written by scripts/voiceover.mjs
export type Manifest = { segments: Partial<Record<SceneId, { file: string; duration: number }>> };

export type TimedScene = {
	id: SceneId;
	from: number; // absolute start frame
	dur: number; // frames until the next scene starts
	voFrom: number; // voice-over start, relative to the scene
	voLen: number; // voice-over length in frames
	file?: string; // voice-over audio (staticFile path)
};

// crossfade: every scene keeps running XF frames into the next one
export const XF = 14;

// frames before the voice starts and after it ends, per scene
const LEAD: Partial<Record<SceneId, number>> = { hook: 26, questions: 8, reveal: 44, outro: 26 };
const TAIL: Partial<Record<SceneId, number>> = {
	questions: 26,
	reveal: 30,
	vulns: 26,
	selfhosted: 24,
	outro: 105
};
const MIN: Partial<Record<SceneId, number>> = { reveal: 6 * FPS };

// music (scripts/sound.mjs): 100 BPM, the beat drops after 6 bars; the drop lands on the logo
export const MUSIC_DROP_SECONDS = 6 * 4 * (60 / 100);
export const REVEAL_IMPACT = 4; // frame of the logo hit within the reveal scene

const estimate = (lang: Lang, id: SceneId) => {
	const line = script[lang].find((l) => l.id === id);
	return (line?.text.length ?? 40) / 15;
};

export function buildTimeline(lang: Lang, manifest: Manifest | null) {
	let t = 0;
	const scenes: TimedScene[] = SCENES.map((id) => {
		const seg = manifest?.segments[id];
		const voLen = Math.ceil((seg?.duration ?? estimate(lang, id)) * FPS);
		const voFrom = LEAD[id] ?? 12;
		const dur = Math.max(MIN[id] ?? 0, voFrom + voLen + (TAIL[id] ?? 16));
		const scene = { id, from: t, dur, voFrom, voLen, file: seg?.file };
		t += dur;
		return scene;
	});
	return { scenes, total: t };
}
