#!/usr/bin/env node
// Generates the voice-over of the intro video, one WAV file per scene, and a manifest
// with the measured durations (public/voiceover/<lang>/manifest.json). The video takes its
// scene lengths from that manifest, so changing a line or the voice needs no timing work.
//
//   node scripts/voiceover.mjs --lang de                 ElevenLabs (ELEVENLABS_API_KEY)
//   node scripts/voiceover.mjs --lang de --only reveal   regenerate a single scene
//   node scripts/voiceover.mjs --lang de --engine piper  local Piper TTS (PIPER_MODEL=…onnx)
//
// Settings come from the environment or video/.env (never committed):
//   ELEVENLABS_API_KEY   API key with text-to-speech permission
//   ELEVENLABS_VOICE_ID  voice (default: Brian, nPczCjzI2devNBz1zQrb)
//   ELEVENLABS_MODEL_ID  model (default: eleven_multilingual_v2)
//   PIPER_BIN            piper executable (default: piper)
//   PIPER_MODEL          Piper voice model for --engine piper
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

// KEY=value lines; values may be quoted, unquoted values may carry a trailing " # comment"
function loadDotEnv() {
	const file = path.join(root, '.env');
	if (!fs.existsSync(file)) return;
	for (const line of fs.readFileSync(file, 'utf8').split(/\r?\n/)) {
		const m = line.match(/^\s*([A-Z0-9_]+)\s*=(.*)$/);
		if (!m || process.env[m[1]] !== undefined) continue;
		const value = m[2].trim();
		const quoted = value.match(/^(["'])(.*?)\1/);
		process.env[m[1]] = quoted ? quoted[2] : value.replace(/(^|\s+)#.*$/, '').trim();
	}
}

const env = (name, fallback) => process.env[name]?.trim() || fallback;

// runs the Remotion CLI (ffmpeg/ffprobe ship with it) through node, so no shell is needed on Windows
const remotion = (...args) =>
	spawnSync(process.execPath, [path.join(root, 'node_modules/@remotion/cli/remotion-cli.js'), ...args], {
		cwd: root,
		encoding: 'utf8'
	});

function args() {
	const out = { lang: 'de', engine: undefined, only: undefined };
	const a = process.argv.slice(2);
	for (let i = 0; i < a.length; i++) {
		if (a[i] === '--lang') out.lang = a[++i];
		else if (a[i] === '--engine') out.engine = a[++i];
		else if (a[i] === '--only') out.only = a[++i];
		else throw new Error(`unknown argument ${a[i]}`);
	}
	return out;
}

// loudness-normalise to -16 LUFS / -1.5 dBTP so every engine and voice sits at the same level
// in the mix (ffmpeg that ships with Remotion)
function normalize(input, output) {
	const r = remotion(
		'ffmpeg',
		'-y',
		'-loglevel',
		'error',
		'-i',
		input,
		'-af',
		'loudnorm=I=-16:TP=-1.5:LRA=11',
		'-ar',
		'44100',
		'-ac',
		'1',
		output
	);
	if (r.status !== 0) throw new Error(`could not normalise ${input}: ${r.error ?? r.stderr}`);
}

// duration in seconds, measured with the ffprobe that ships with Remotion
function duration(file) {
	const r = remotion('ffprobe', '-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', file);
	// the CLI may print notices before the value; the duration is the last number
	const d = parseFloat((r.stdout ?? '').trim().split(/\s+/).pop());
	if (!Number.isFinite(d)) throw new Error(`could not measure ${file}: ${r.error ?? (r.stderr || r.stdout)}`);
	return d;
}

async function elevenlabs(lines, i, file) {
	const key = env('ELEVENLABS_API_KEY');
	if (!key) throw new Error('ELEVENLABS_API_KEY is not set (environment or video/.env)');
	const voice = env('ELEVENLABS_VOICE_ID', 'nPczCjzI2devNBz1zQrb');
	const res = await fetch(
		`https://api.elevenlabs.io/v1/text-to-speech/${encodeURIComponent(voice)}?output_format=mp3_44100_128`,
		{
			method: 'POST',
			headers: { 'xi-api-key': key, 'content-type': 'application/json' },
			body: JSON.stringify({
				text: lines[i].text,
				model_id: env('ELEVENLABS_MODEL_ID', 'eleven_multilingual_v2'),
				// no previous_text/next_text: the model sometimes speaks the start of next_text at the
				// end of a clip, which is then heard cut off at every scene change. A fixed seed keeps
				// the delivery consistent across the separate files instead.
				seed: 4242,
				voice_settings: { stability: 0.5, similarity_boost: 0.8, style: 0.25, use_speaker_boost: true }
			})
		}
	);
	if (!res.ok) throw new Error(`ElevenLabs ${res.status}: ${await res.text()}`);
	fs.writeFileSync(file, Buffer.from(await res.arrayBuffer()));
	return { engine: 'elevenlabs', voice };
}

function piper(lines, i, file) {
	const model = env('PIPER_MODEL');
	if (!model) throw new Error('PIPER_MODEL is not set (path to a Piper .onnx voice)');
	const r = spawnSync(env('PIPER_BIN', 'piper'), ['-m', model, '-f', file, '--length-scale', '0.95'], {
		input: lines[i].text,
		encoding: 'utf8'
	});
	if (r.status !== 0) throw new Error(`piper failed: ${r.error ?? r.stderr}`);
	return { engine: 'piper', voice: path.basename(model, '.onnx') };
}

async function main() {
	loadDotEnv();
	const opt = args();
	const script = JSON.parse(fs.readFileSync(path.join(root, 'src/voiceover.json'), 'utf8'));
	const lines = script[opt.lang];
	if (!lines) throw new Error(`no voice-over text for language "${opt.lang}"`);
	const engine = opt.engine || (env('ELEVENLABS_API_KEY') ? 'elevenlabs' : 'piper');

	const dir = path.join(root, 'public/voiceover', opt.lang);
	fs.mkdirSync(dir, { recursive: true });
	const manifestFile = path.join(dir, 'manifest.json');
	const manifest = fs.existsSync(manifestFile)
		? JSON.parse(fs.readFileSync(manifestFile, 'utf8'))
		: { lang: opt.lang, segments: {} };

	for (let i = 0; i < lines.length; i++) {
		const { id } = lines[i];
		if (opt.only && opt.only !== id) continue;
		const raw = path.join(dir, `${id}.raw.${engine === 'piper' ? 'wav' : 'mp3'}`);
		const file = path.join(dir, `${id}.wav`);
		process.stdout.write(`${opt.lang}/${id} … `);
		const info = engine === 'piper' ? piper(lines, i, raw) : await elevenlabs(lines, i, raw);
		normalize(raw, file);
		fs.rmSync(raw);
		const seconds = duration(file);
		manifest.segments[id] = {
			file: `voiceover/${opt.lang}/${id}.wav`,
			duration: seconds,
			text: lines[i].text,
			...info
		};
		console.log(`${seconds.toFixed(2)} s`);
	}
	manifest.generatedAt = new Date().toISOString();
	fs.writeFileSync(manifestFile, JSON.stringify(manifest, null, '\t') + '\n');
	console.log(`wrote ${path.relative(root, manifestFile)}`);
}

main().catch((err) => {
	console.error(err.message);
	process.exit(1);
});
