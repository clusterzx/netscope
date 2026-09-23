// Small reactive helpers for loading data.
//
// Resource<T> – a cached, shared value (catalogs like meta, groups, tags):
//   const groups = new Resource(() => api.get('/api/v1/groups'));
//   groups.load();            // fetch once (deduplicated), returns a promise
//   groups.refresh();         // force reload
//   groups.value / groups.loading / groups.error
//
// AsyncData<T> – per-page request state with abort of stale requests:
//   const devices = new AsyncData<DeviceList>();
//   $effect(() => { const q = query; devices.run((signal) => api.get('/api/v1/devices', { query: { q }, signal })); });
//   {#if devices.loading && !devices.data}<Skeleton/>{:else if devices.error}<ErrorState error={devices.error} onretry={() => devices.reload()} />…
import { untrack } from 'svelte';

export class Resource<T> {
	value = $state<T | undefined>(undefined);
	loading = $state(false);
	error = $state<unknown>(null);
	#loader: () => Promise<T>;
	#pending: Promise<T> | null = null;

	constructor(loader: () => Promise<T>) {
		this.#loader = loader;
	}

	/** Loads once; later calls return the cached value. */
	load(): Promise<T> {
		const v = untrack(() => this.value);
		if (v !== undefined) return Promise.resolve(v);
		return this.refresh();
	}

	/** Forces a reload (concurrent calls share one request). */
	refresh(): Promise<T> {
		if (this.#pending) return this.#pending;
		this.loading = true;
		this.#pending = this.#loader()
			.then((v) => {
				this.value = v;
				this.error = null;
				return v;
			})
			.catch((e) => {
				this.error = e;
				throw e;
			})
			.finally(() => {
				this.loading = false;
				this.#pending = null;
			});
		return this.#pending;
	}

	/** Drops the cached value; the next load() fetches again. */
	invalidate() {
		this.value = undefined;
	}
}

export class AsyncData<T> {
	data = $state<T | undefined>(undefined);
	error = $state<unknown>(null);
	loading = $state(false);
	#ctrl: AbortController | null = null;
	#seq = 0;
	#last: ((signal: AbortSignal) => Promise<T>) | null = null;

	/**
	 * Runs fn (aborting a previous, still running request). Keeps the previous data while
	 * loading (set `reset` to clear it). Reactive reads inside fn are tracked when called
	 * from an $effect – the effect reruns when they change.
	 */
	run(fn: (signal: AbortSignal) => Promise<T>, reset = false): Promise<T | undefined> {
		this.#last = fn;
		this.#ctrl?.abort();
		const ctrl = new AbortController();
		this.#ctrl = ctrl;
		const seq = ++this.#seq;
		this.loading = true;
		if (reset) this.data = undefined;
		let p: Promise<T>;
		try {
			p = fn(ctrl.signal);
		} catch (e) {
			p = Promise.reject(e);
		}
		return p.then(
			(v) => {
				if (seq !== this.#seq) return undefined;
				this.data = v;
				this.error = null;
				this.loading = false;
				return v;
			},
			(e) => {
				if (seq !== this.#seq || (e instanceof DOMException && e.name === 'AbortError')) return undefined;
				this.error = e;
				this.loading = false;
				return undefined;
			}
		);
	}

	/** Reruns the last request (e.g. retry button, SSE refresh). */
	reload(): Promise<T | undefined> {
		const fn = this.#last;
		return fn ? untrack(() => this.run(fn)) : Promise.resolve(undefined);
	}

	/** Replaces the data locally (optimistic updates). */
	set(v: T) {
		this.data = v;
	}

	abort() {
		this.#ctrl?.abort();
		this.#seq++;
		this.loading = false;
	}
}
