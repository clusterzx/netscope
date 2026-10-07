import { Composition, staticFile } from 'remotion';
import './fonts';
import { Intro, type IntroProps } from './Intro';
import { buildTimeline, FPS, HEIGHT, WIDTH, type Lang, type Manifest } from './timeline';

const fetchJson = async <T,>(path: string): Promise<T | null> => {
	try {
		const res = await fetch(staticFile(path));
		return res.ok ? ((await res.json()) as T) : null;
	} catch {
		return null;
	}
};

const exists = async (path: string) => {
	try {
		return (await fetch(staticFile(path), { method: 'HEAD' })).ok;
	} catch {
		return false;
	}
};

// Without a voice-over (scripts/voiceover.mjs) the scenes use estimated lengths and stay
// silent; without scripts/sound.mjs there is no music or effects.
export const Root = () => (
	<>
		{(['de', 'en'] as Lang[]).map((lang) => (
			<Composition
				key={lang}
				id={`NetScopeIntro-${lang}`}
				component={Intro}
				fps={FPS}
				width={WIDTH}
				height={HEIGHT}
				durationInFrames={FPS * 60}
				defaultProps={{ lang, manifest: null, sound: false } satisfies IntroProps}
				calculateMetadata={async ({ props }) => {
					const manifest = await fetchJson<Manifest>(`voiceover/${props.lang}/manifest.json`);
					const sound = await exists('audio/music.wav');
					return {
						durationInFrames: buildTimeline(props.lang, manifest).total,
						props: { ...props, manifest, sound }
					};
				}}
			/>
		))}
	</>
);
