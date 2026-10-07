import { Audio } from '@remotion/media';
import type { ComponentType } from 'react';
import { AbsoluteFill, interpolate, Sequence, staticFile, useCurrentFrame, useVideoConfig } from 'remotion';
import { Background } from './components/Background';
import { strings } from './i18n';
import { Alerts } from './scenes/Alerts';
import { Discovery } from './scenes/Discovery';
import { Hook } from './scenes/Hook';
import { Inventory } from './scenes/Inventory';
import { Outro } from './scenes/Outro';
import { Questions } from './scenes/Questions';
import { Reveal } from './scenes/Reveal';
import { SelfHosted } from './scenes/SelfHosted';
import { Sources } from './scenes/Sources';
import { Topology } from './scenes/Topology';
import type { SceneProps } from './scenes/types';
import { Vulns } from './scenes/Vulns';
import {
	buildTimeline,
	FPS,
	MUSIC_DROP_SECONDS,
	REVEAL_IMPACT,
	XF,
	type Lang,
	type Manifest,
	type SceneId
} from './timeline';

export type IntroProps = { lang: Lang; manifest: Manifest | null; sound: boolean };

const components: Record<SceneId, ComponentType<SceneProps>> = {
	hook: Hook,
	questions: Questions,
	reveal: Reveal,
	discovery: Discovery,
	sources: Sources,
	inventory: Inventory,
	vulns: Vulns,
	topology: Topology,
	alerts: Alerts,
	selfhosted: SelfHosted,
	outro: Outro
};

const clamp = { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' } as const;

export const Intro = ({ lang, manifest, sound }: IntroProps) => {
	const { durationInFrames } = useVideoConfig();
	const { scenes } = buildTimeline(lang, manifest);
	const s = strings[lang];
	const scene = (id: SceneId) => scenes.find((x) => x.id === id)!;

	// music: the beat drops on the logo; start earlier (or trim the intro) to line it up
	const drop = scene('reveal').from + REVEAL_IMPACT;
	const musicStart = drop - Math.round(MUSIC_DROP_SECONDS * FPS);
	const musicFrom = Math.max(0, musicStart);
	const musicTrim = Math.max(0, -musicStart);
	// duck the music while someone speaks
	const speaking = (frame: number) =>
		Math.max(
			0,
			...scenes
				.filter((x) => x.file)
				.map((x) => {
					const a = x.from + x.voFrom;
					const b = a + x.voLen;
					return Math.min(
						interpolate(frame, [a - 8, a], [0, 1], clamp),
						interpolate(frame, [b, b + 14], [1, 0], clamp)
					);
				})
		);
	const musicVolume = (f: number) => {
		const frame = f + musicFrom;
		const fadeIn = interpolate(frame, [0, 20], [0, 1], clamp);
		const fadeOut = interpolate(frame, [durationInFrames - 75, durationInFrames - 4], [1, 0], clamp);
		return fadeIn * fadeOut * (0.3 - 0.2 * speaking(frame));
	};

	return (
		<AbsoluteFill style={{ background: '#000' }}>
			<Background />
			{scenes.map((x) => {
				const Scene = components[x.id];
				return (
					<Sequence key={x.id} name={x.id} from={x.from} durationInFrames={x.dur + XF}>
						<Scene s={s} dur={x.dur} vo={{ start: x.voFrom, len: x.voLen }} sound={sound} />
					</Sequence>
				);
			})}

			{scenes.map((x) =>
				x.file ? (
					<Sequence
						key={`vo-${x.id}`}
						name={`voice ${x.id}`}
						from={x.from + x.voFrom}
						durationInFrames={x.voLen + 15}
						layout="none"
					>
						<Audio src={staticFile(x.file)} volume={1} />
					</Sequence>
				) : null
			)}

			{sound && (
				<>
					<Sequence name="music" from={musicFrom} layout="none">
						<Audio src={staticFile('audio/music.wav')} trimBefore={musicTrim} volume={musicVolume} />
					</Sequence>
					{scenes
						.filter((x) => !['hook', 'reveal', 'outro'].includes(x.id))
						.map((x) => (
							<Sequence
								key={`sfx-${x.id}`}
								from={Math.max(0, x.from - 8)}
								durationInFrames={35}
								layout="none"
							>
								<Audio src={staticFile('audio/whoosh.wav')} volume={0.13} />
							</Sequence>
						))}
					<Sequence name="impact" from={drop - 1} durationInFrames={100} layout="none">
						<Audio src={staticFile('audio/impact.wav')} volume={0.5} />
					</Sequence>
					<Sequence name="impact outro" from={scene('outro').from + 3} durationInFrames={100} layout="none">
						<Audio src={staticFile('audio/impact.wav')} volume={0.32} />
					</Sequence>
				</>
			)}

			<FadeToBlack />
		</AbsoluteFill>
	);
};

const FadeToBlack = () => {
	const frame = useCurrentFrame();
	const { durationInFrames } = useVideoConfig();
	return (
		<AbsoluteFill
			style={{
				background: '#000',
				opacity: interpolate(frame, [durationInFrames - 22, durationInFrames - 1], [0, 1], clamp)
			}}
		/>
	);
};
