// Queued/running plugin runs, kept current via the live stream ("run" topic).
//
//   import { runs } from '$lib/stores/runs.svelte';
//   runs.active        // RunView[] (queued + running, newest first)
//   runs.recent        // finished during this session (max 20, newest first)
//   runs.get(id)       // a run known to the store
//   $effect(() => runs.onFinished((run) => { if (myRunIds.has(run.id)) reload(); }));
import { api } from '$lib/api/client';
import type { LiveMessage, RunMessageData, RunView } from '$lib/api/types';
import { live } from './live.svelte';

class RunsStore {
	active = $state<RunView[]>([]);
	recent = $state<RunView[]>([]);
	loaded = $state(false);
	#finishedHandlers = new Set<(run: RunView) => void>();
	/** ids of finished runs (late queued/started fetches must not re-add them) */
	#finished = new Set<number>();
	#started = false;

	/** Loads the active runs and subscribes to live updates (called by the app shell). */
	init() {
		if (this.#started) return;
		this.#started = true;
		this.reload();
		live.on<RunMessageData>('run', (m) => this.#onMessage(m));
		live.onReconnect(() => this.reload());
	}

	async reload() {
		try {
			const list = (await api.get('/api/v1/runs/active')) ?? [];
			const queued = this.active.filter((r) => r.status === 'queued' && !list.some((x) => x.id === r.id));
			this.active = [...list, ...queued].sort((a, b) => b.id - a.id);
			this.loaded = true;
		} catch {
			// shown as stale; the next live message or reconnect retries
		}
	}

	get(id: number): RunView | undefined {
		return this.active.find((r) => r.id === id) ?? this.recent.find((r) => r.id === id);
	}

	/** Runs of one plugin that are queued or running. */
	forPlugin(pluginId: string): RunView[] {
		return this.active.filter((r) => r.pluginId === pluginId);
	}

	/** Callback for every finished run (with its final RunView). Returns unsubscribe. */
	onFinished(fn: (run: RunView) => void): () => void {
		this.#finishedHandlers.add(fn);
		return () => {
			this.#finishedHandlers.delete(fn);
		};
	}

	#upsert(run: RunView) {
		const i = this.active.findIndex((r) => r.id === run.id);
		if (i >= 0) this.active[i] = run;
		else this.active = [run, ...this.active].sort((a, b) => b.id - a.id);
	}

	async #fetch(id: number): Promise<RunView | null> {
		try {
			return await api.get('/api/v1/runs/{id}', { path: { id } });
		} catch {
			return null;
		}
	}

	async #onMessage(m: LiveMessage<RunMessageData>) {
		const d = m.data;
		if (!d || typeof d.id !== 'number') return;
		switch (m.type) {
			case 'queued':
			case 'started': {
				// the message carries the current RunView; fetch only for older servers
				const run = d.run ?? (await this.#fetch(d.id));
				// a fast run may already be finished when a fetch returns
				if (run && (run.status === 'queued' || run.status === 'running') && !this.#finished.has(d.id))
					this.#upsert(run);
				break;
			}
			case 'progress': {
				const run = this.active.find((r) => r.id === d.id);
				if (run) {
					run.progress = { done: d.done ?? 0, total: d.total ?? 0 };
					run.status = 'running';
				} else if (!this.#finished.has(d.id)) {
					// missed the start (e.g. reconnect): fetch it once
					const fresh = await this.#fetch(d.id);
					if (fresh && fresh.status === 'running' && !this.#finished.has(d.id)) this.#upsert(fresh);
				}
				break;
			}
			case 'finished': {
				this.#finished.add(d.id);
				if (this.#finished.size > 500) this.#finished = new Set([...this.#finished].slice(-200));
				const before = this.active.find((r) => r.id === d.id);
				this.active = this.active.filter((r) => r.id !== d.id);
				let run: RunView | null = d.run ?? null;
				if (!run && !d.discarded) run = await this.#fetch(d.id);
				if (!run && before) {
					run = { ...before, status: d.status ?? 'success', error: d.error, durationMs: d.durationMs ?? 0 };
				}
				if (!run) break;
				// discarded = routine scheduled run without changes, not kept in the history
				if (!d.discarded) this.recent = [run, ...this.recent.filter((r) => r.id !== run.id)].slice(0, 20);
				for (const h of this.#finishedHandlers) {
					try {
						h(run);
					} catch (err) {
						console.error(err);
					}
				}
				break;
			}
		}
	}
}

export const runs = new RunsStore();
