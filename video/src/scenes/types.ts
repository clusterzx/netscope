import type { Strings } from '../i18n';

export type SceneProps = {
	s: Strings;
	dur: number; // frames until the next scene starts
	vo: { start: number; len: number }; // voice-over within the scene
	sound: boolean; // music and effects exist (scripts/sound.mjs)
};
