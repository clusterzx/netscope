#!/usr/bin/env node
// Synthesises the music bed and the sound effects of the intro video (no samples, no
// dependencies), so the whole soundtrack is reproducible:  node scripts/sound.mjs
//
//   public/audio/music.wav   100 BPM, Am–F–C–G; ambient intro, the beat drops at DROP_AT
//   public/audio/whoosh.wav  scene transition
//   public/audio/impact.wav  logo reveal
//   public/audio/blip.wav    device detected
//   public/audio/ding.wav    notification
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const outDir = path.join(root, 'public/audio');
const SR = 44100;
const TAU = Math.PI * 2;

export const BPM = 100;
const BEAT = 60 / BPM;
const BAR = BEAT * 4;
export const DROP_BAR = 6; // the video aligns this bar with the logo reveal (src/timeline.ts)
const BARS = 42;

// deterministic noise
let seed = 1337;
const rnd = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296) * 2 - 1;
const midi = (n) => 440 * 2 ** ((n - 69) / 12);

// ---------------------------------------------------------------- building blocks

// band-limited saw as a wavetable (harmonics up to ~6 kHz for the lowest pad note)
const TABLE = 4096;
const sawTable = new Float32Array(TABLE);
for (let k = 1; k <= 24; k++)
	for (let i = 0; i < TABLE; i++) sawTable[i] += Math.sin((TAU * k * i) / TABLE) / k;
const saw = (phase) => sawTable[Math.floor(phase * TABLE) & (TABLE - 1)];

// RBJ low-pass biquad; set() may be called per block for sweeps
function lowpass(cutoff, q = 0.707) {
	let b0, b1, b2, a1, a2;
	let x1 = 0,
		x2 = 0,
		y1 = 0,
		y2 = 0;
	const set = (fc) => {
		const w = (TAU * Math.min(fc, SR * 0.45)) / SR;
		const alpha = Math.sin(w) / (2 * q);
		const cos = Math.cos(w);
		const a0 = 1 + alpha;
		b0 = (1 - cos) / 2 / a0;
		b1 = (1 - cos) / a0;
		b2 = b0;
		a1 = (-2 * cos) / a0;
		a2 = (1 - alpha) / a0;
	};
	set(cutoff);
	const run = (x) => {
		const y = b0 * x + b1 * x1 + b2 * x2 - a1 * y1 - a2 * y2;
		x2 = x1;
		x1 = x;
		y2 = y1;
		y1 = y;
		return y;
	};
	return { set, run };
}

function bandpass(center, q = 1.2) {
	let b0, b2, a1, a2;
	let x1 = 0,
		x2 = 0,
		y1 = 0,
		y2 = 0;
	const set = (fc) => {
		const w = (TAU * Math.min(fc, SR * 0.45)) / SR;
		const alpha = Math.sin(w) / (2 * q);
		const a0 = 1 + alpha;
		b0 = alpha / a0;
		b2 = -alpha / a0;
		a1 = (-2 * Math.cos(w)) / a0;
		a2 = (1 - alpha) / a0;
	};
	set(center);
	const run = (x) => {
		const y = b0 * x + b2 * x2 - a1 * y1 - a2 * y2;
		x2 = x1;
		x1 = x;
		y2 = y1;
		y1 = y;
		return y;
	};
	return { set, run };
}

// small Schroeder/Freeverb-style reverb, returns [left, right] wet buffers
function reverb(input, { room = 0.86, damp = 0.35 } = {}) {
	const combs = [1557, 1617, 1491, 1422, 1277, 1356];
	const allp = [556, 441, 341];
	const out = [];
	for (const spread of [0, 23]) {
		const wet = new Float32Array(input.length);
		for (const len of combs) {
			const buf = new Float32Array(len + spread);
			let idx = 0,
				store = 0;
			for (let i = 0; i < input.length; i++) {
				const y = buf[idx];
				store = y * (1 - damp) + store * damp;
				buf[idx] = input[i] + store * room;
				idx = (idx + 1) % buf.length;
				wet[i] += y / combs.length;
			}
		}
		for (const len of allp) {
			const buf = new Float32Array(len + spread);
			let idx = 0;
			for (let i = 0; i < wet.length; i++) {
				const b = buf[idx];
				buf[idx] = wet[i] + b * 0.5;
				wet[i] = b - wet[i];
				idx = (idx + 1) % buf.length;
			}
		}
		out.push(wet);
	}
	return out;
}

function writeWav(file, channels) {
	const n = channels[0].length;
	const nc = channels.length;
	const buf = Buffer.alloc(44 + n * nc * 2);
	buf.write('RIFF', 0);
	buf.writeUInt32LE(36 + n * nc * 2, 4);
	buf.write('WAVE', 8);
	buf.write('fmt ', 12);
	buf.writeUInt32LE(16, 16);
	buf.writeUInt16LE(1, 20);
	buf.writeUInt16LE(nc, 22);
	buf.writeUInt32LE(SR, 24);
	buf.writeUInt32LE(SR * nc * 2, 28);
	buf.writeUInt16LE(nc * 2, 32);
	buf.writeUInt16LE(16, 34);
	buf.write('data', 36);
	buf.writeUInt32LE(n * nc * 2, 40);
	let peak = 1e-9;
	for (const ch of channels) for (let i = 0; i < n; i++) peak = Math.max(peak, Math.abs(ch[i]));
	const gain = 0.89 / peak;
	for (let i = 0; i < n; i++)
		for (let c = 0; c < nc; c++) {
			const v = Math.max(-1, Math.min(1, channels[c][i] * gain));
			buf.writeInt16LE(Math.round(v * 32767), 44 + (i * nc + c) * 2);
		}
	fs.writeFileSync(file, buf);
	console.log(`${path.relative(root, file)}  ${(n / SR).toFixed(1)} s`);
}

// ---------------------------------------------------------------- music

function music() {
	const n = Math.round(BARS * BAR * SR);
	const L = new Float32Array(n),
		R = new Float32Array(n);
	const padBus = new Float32Array(n); // mono, goes through the reverb
	const arpBus = new Float32Array(n);
	const drop = DROP_BAR * BAR;

	// Am(add9) – Fmaj7 – C(add9) – G(add9), two bars each
	const chords = [
		{ root: 45, pad: [57, 60, 64, 71] },
		{ root: 41, pad: [53, 57, 60, 64] },
		{ root: 48, pad: [55, 60, 64, 74] },
		{ root: 43, pad: [55, 59, 62, 69] }
	];
	const chordAt = (t) => chords[Math.floor(t / (BAR * 2)) % chords.length];

	// sidechain: pad and bass duck under each kick after the drop
	const duck = (t) => {
		if (t < drop) return 1;
		const since = (t - drop) % BEAT;
		return 1 - 0.55 * Math.exp(-since / 0.11);
	};

	// pad: 4 notes × 3 detuned saws, filter opens through the intro
	{
		const lp = lowpass(500, 0.9);
		const segLen = BAR * 2;
		for (let seg = 0; seg * segLen < BARS * BAR; seg++) {
			const chord = chords[seg % chords.length];
			const start = seg * segLen;
			const end = Math.min(BARS * BAR, start + segLen + 0.9);
			const phases = chord.pad.flatMap(() => [rnd() * 0.5 + 0.5, rnd() * 0.5 + 0.5, rnd() * 0.5 + 0.5]);
			for (let i = Math.floor(start * SR); i < Math.floor(end * SR) && i < n; i++) {
				const t = i / SR;
				const local = t - start;
				const env = Math.min(1, local / 0.9) * Math.min(1, Math.max(0, (end - t) / 0.9));
				let s = 0;
				let p = 0;
				for (const note of chord.pad) {
					const f = midi(note);
					for (const det of [-0.07, 0, 0.07]) {
						phases[p] = (phases[p] + (f * 2 ** (det / 12)) / SR) % 1;
						s += saw(phases[p]);
						p++;
					}
				}
				padBus[i] += s * env * 0.05;
			}
		}
		for (let i = 0; i < n; i++) {
			const t = i / SR;
			if (i % 64 === 0) {
				const open = t < drop ? 380 + (t / drop) ** 2 * 1400 : 1900 + 500 * Math.sin((TAU * t) / (BAR * 8));
				lp.set(open);
			}
			padBus[i] = lp.run(padBus[i]) * duck(t);
		}
	}

	// bass: eighth-note pulse from the drop on
	{
		const lp = lowpass(420, 1.1);
		let ph = 0;
		for (let i = Math.floor(drop * SR); i < n; i++) {
			const t = i / SR;
			const f = midi(chordAt(t).root);
			ph = (ph + f / SR) % 1;
			const local = (t - drop) % (BEAT / 2);
			const env =
				Math.min(1, local / 0.006) *
				(0.55 + 0.45 * Math.exp(-local / 0.09)) *
				Math.min(1, (BEAT / 2 - local) / 0.02);
			const s = lp.run(Math.sin(TAU * ph) * 0.8 + saw(ph) * 0.35) * env * duck(t) * 0.36;
			L[i] += s;
			R[i] += s;
		}
	}

	// arpeggio: quarter notes in the intro, sixteenths after the drop
	{
		const pattern = [0, 1, 2, 3, 2, 1, 3, 2];
		const step = BEAT / 4;
		for (let k = 0; k * step < BARS * BAR; k++) {
			const t0 = k * step;
			if (t0 < drop && k % 4 !== 0) continue;
			if (t0 < BAR * 2) continue;
			const chord = chordAt(t0);
			const note = chord.pad[pattern[k % pattern.length]] + 12;
			const f = midi(note);
			const vel = t0 < drop ? 0.5 : k % 4 === 0 ? 0.85 : 0.55;
			const len = Math.floor(0.5 * SR);
			const i0 = Math.floor(t0 * SR);
			for (let j = 0; j < len && i0 + j < n; j++) {
				const t = j / SR;
				const env = Math.min(1, t / 0.004) * Math.exp(-t / 0.16);
				arpBus[i0 + j] +=
					(Math.sin(TAU * f * t) + 0.35 * Math.sin(TAU * 2 * f * t) + 0.12 * Math.sin(TAU * 3 * f * t)) *
					env *
					vel *
					0.07;
			}
		}
	}

	// drums from the drop on: kick on every beat, hat off-beat, soft snare on 2 and 4
	{
		const hp = lowpass(7000);
		for (let b = 0; drop + b * BEAT < BARS * BAR; b++) {
			const t0 = drop + b * BEAT;
			const i0 = Math.floor(t0 * SR);
			// kick
			let ph = 0;
			for (let j = 0; j < 0.45 * SR && i0 + j < n; j++) {
				const t = j / SR;
				const f = 46 + 110 * Math.exp(-t / 0.035);
				ph += f / SR;
				const s =
					Math.sin(TAU * ph) * Math.exp(-t / 0.22) * 0.75 + (j < 90 ? rnd() * 0.08 * (1 - j / 90) : 0);
				L[i0 + j] += s;
				R[i0 + j] += s;
			}
			// snare
			if (b % 2 === 1) {
				const bp = bandpass(1800, 0.8);
				for (let j = 0; j < 0.25 * SR && i0 + j < n; j++) {
					const t = j / SR;
					const s = (bp.run(rnd()) * 0.9 + Math.sin(TAU * 185 * t) * 0.3) * Math.exp(-t / 0.075) * 0.22;
					L[i0 + j] += s * 0.9;
					R[i0 + j] += s;
				}
			}
			// hat on the off-beat
			const ih = Math.floor((t0 + BEAT / 2) * SR);
			for (let j = 0; j < 0.06 * SR && ih + j < n; j++) {
				const t = j / SR;
				const x = rnd();
				const s = (x - hp.run(x)) * Math.exp(-t / 0.018) * 0.16;
				L[ih + j] += s * 0.8;
				R[ih + j] += s;
			}
		}
	}

	// riser into the drop and into every 8-bar section after it
	{
		const risers = [drop];
		for (let b = DROP_BAR + 8; b < BARS; b += 8) risers.push(b * BAR);
		for (const end of risers) {
			const len = BAR * 1.5;
			const bp = bandpass(300, 2);
			const i0 = Math.floor((end - len) * SR);
			for (let j = 0; j < len * SR; j++) {
				const x = j / (len * SR);
				if (j % 32 === 0) bp.set(300 + 5200 * x * x);
				const s = bp.run(rnd()) * x * x * 0.32;
				L[i0 + j] += s * (1 - x * 0.5);
				R[i0 + j] += s * (0.5 + x * 0.5);
			}
		}
	}

	// arp: stereo ping-pong delay (dotted eighth)
	{
		const d = Math.floor(BEAT * 0.75 * SR);
		const dl = new Float32Array(n),
			dr = new Float32Array(n);
		for (let i = 0; i < n; i++) {
			dl[i] = arpBus[i] + (i >= d ? dr[i - d] * 0.38 : 0);
			dr[i] = i >= d ? dl[i - d] * 0.38 : 0;
			L[i] += arpBus[i] * 0.8 + (dl[i] - arpBus[i]) * 0.3; // echoes only
			R[i] += arpBus[i] * 0.8 + dr[i] * 0.3;
		}
	}

	const [wl, wr] = reverb(
		Float32Array.from(padBus, (v, i) => v + arpBus[i] * 0.6),
		{ room: 0.88, damp: 0.4 }
	);
	for (let i = 0; i < n; i++) {
		L[i] = Math.tanh((L[i] + padBus[i] * 0.85 + wl[i] * 0.55) * 1.1);
		R[i] = Math.tanh((R[i] + padBus[i] * 0.85 + wr[i] * 0.55) * 1.1);
	}
	writeWav(path.join(outDir, 'music.wav'), [L, R]);
}

// ---------------------------------------------------------------- effects

function whoosh() {
	const len = 1.0;
	const n = Math.floor(len * SR);
	const L = new Float32Array(n),
		R = new Float32Array(n);
	const bp = bandpass(400, 1.6);
	for (let i = 0; i < n; i++) {
		const x = i / n;
		if (i % 32 === 0) bp.set(250 + 3200 * Math.sin(Math.PI * x) ** 2);
		const env = Math.sin(Math.PI * Math.min(1, x * 1.15)) ** 2;
		const s = bp.run(rnd()) * env;
		L[i] = s * (1 - x);
		R[i] = s * x;
	}
	const [wl, wr] = reverb(
		Float32Array.from(L, (v, i) => v + R[i]),
		{ room: 0.7 }
	);
	writeWav(path.join(outDir, 'whoosh.wav'), [
		L.map((v, i) => v + wl[i] * 0.3),
		R.map((v, i) => v + wr[i] * 0.3)
	]);
}

function impact() {
	const len = 3.2;
	const n = Math.floor(len * SR);
	const dry = new Float32Array(n);
	const lp = lowpass(900);
	let ph = 0;
	for (let i = 0; i < n; i++) {
		const t = i / SR;
		const f = 34 + 50 * Math.exp(-t / 0.18);
		ph += f / SR;
		dry[i] =
			Math.sin(TAU * ph) * Math.exp(-t / 0.9) * 0.9 +
			lp.run(rnd()) * Math.exp(-t / 0.12) * 0.6 +
			(Math.sin(TAU * 1760 * t) * 0.05 + Math.sin(TAU * 2637 * t) * 0.035 + Math.sin(TAU * 3520 * t) * 0.02) *
				Math.min(1, t / 0.05) *
				Math.exp(-t / 1.1);
	}
	const [wl, wr] = reverb(dry, { room: 0.9, damp: 0.3 });
	writeWav(path.join(outDir, 'impact.wav'), [
		dry.map((v, i) => v + wl[i] * 0.7),
		dry.map((v, i) => v + wr[i] * 0.7)
	]);
}

function tone(file, notes, { len, decay, partial = 2.76, level = 1 }) {
	const n = Math.floor(len * SR);
	const dry = new Float32Array(n);
	for (const { f, at } of notes) {
		const i0 = Math.floor(at * SR);
		for (let i = i0; i < n; i++) {
			const t = (i - i0) / SR;
			const env = Math.min(1, t / 0.003) * Math.exp(-t / decay);
			dry[i] +=
				(Math.sin(TAU * f * t) + 0.25 * Math.sin(TAU * f * partial * t) * Math.exp(-t / (decay * 0.3))) *
				env *
				level;
		}
	}
	const [wl, wr] = reverb(dry, { room: 0.75, damp: 0.5 });
	writeWav(path.join(outDir, file), [dry.map((v, i) => v + wl[i] * 0.4), dry.map((v, i) => v + wr[i] * 0.4)]);
}

fs.mkdirSync(outDir, { recursive: true });
music();
whoosh();
impact();
tone(
	'blip.wav',
	[
		{ f: 1318.5, at: 0 },
		{ f: 1975.5, at: 0.05 }
	],
	{ len: 0.6, decay: 0.07, partial: 2 }
);
tone(
	'ding.wav',
	[
		{ f: 987.8, at: 0 },
		{ f: 1318.5, at: 0.11 }
	],
	{ len: 1.2, decay: 0.22 }
);
