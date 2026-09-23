// One shared Server-Sent-Events connection (/api/v1/stream) for the whole app.
//
//   import { live } from '$lib/stores/live.svelte';
//   $effect(() => live.on('device', (m) => { … m.type, m.data … }));   // returns the unsubscribe
//   $effect(() => live.onReconnect(() => reload()));                 // refetch after a gap
//
// The app shell calls live.connect() after login. The browser reconnects by itself after
// network hiccups; if the stream is closed for good (HTTP error, e.g. expired session) we
// reconnect with backoff and let the API client redirect to the login on 401.
import type { LiveMessage, LiveTopic } from '$lib/api/types';
import { request } from '$lib/api/client';

export const LIVE_TOPICS: LiveTopic[] = [
	'run',
	'run.log',
	'device',
	'event',
	'plugin',
	'notification',
	'health',
	'system',
	'log'
];

export type LiveStatus = 'idle' | 'connecting' | 'open' | 'reconnecting';

type Handler = (m: LiveMessage<never>) => void;

class LiveStream {
	status = $state<LiveStatus>('idle');
	/** time of the last received message (ms) */
	lastMessageAt = $state<number | null>(null);
	/** number of (re)connects since page load */
	connects = $state(0);

	#es: EventSource | null = null;
	#handlers = new Map<string, Set<Handler>>();
	#reconnectHandlers = new Set<() => void>();
	#attempt = 0;
	#timer: ReturnType<typeof setTimeout> | null = null;
	#wanted = false;
	#hadOpen = false;

	connect() {
		this.#wanted = true;
		if (this.#es || typeof window === 'undefined') return;
		this.#clearTimer();
		this.status = this.#hadOpen ? 'reconnecting' : 'connecting';
		const es = new EventSource('/api/v1/stream?topics=' + LIVE_TOPICS.join(','));
		this.#es = es;
		es.onopen = () => {
			const again = this.#hadOpen;
			this.#hadOpen = true;
			this.#attempt = 0;
			this.status = 'open';
			this.connects++;
			if (again) for (const h of this.#reconnectHandlers) this.#safe(() => h());
		};
		es.onerror = () => {
			if (es.readyState === EventSource.CLOSED) {
				// closed for good (HTTP error) – retry ourselves
				es.close();
				if (this.#es === es) this.#es = null;
				this.status = 'reconnecting';
				this.#scheduleReconnect();
			} else {
				this.status = 'reconnecting';
			}
		};
		for (const topic of LIVE_TOPICS) {
			es.addEventListener(topic, (e) => this.#dispatch(topic, e as MessageEvent<string>));
		}
	}

	disconnect() {
		this.#wanted = false;
		this.#clearTimer();
		this.#es?.close();
		this.#es = null;
		this.#hadOpen = false;
		this.status = 'idle';
	}

	/**
	 * Subscribes to a topic ('*' = all topics). Returns the unsubscribe function, so it can
	 * be returned from $effect directly.
	 */
	on<T = unknown>(topic: LiveTopic | '*', handler: (m: LiveMessage<T>) => void): () => void {
		let set = this.#handlers.get(topic);
		if (!set) {
			set = new Set();
			this.#handlers.set(topic, set);
		}
		const h = handler as Handler;
		set.add(h);
		return () => {
			set.delete(h);
		};
	}

	/** Called after the stream reconnected (messages may have been missed – refetch). */
	onReconnect(handler: () => void): () => void {
		this.#reconnectHandlers.add(handler);
		return () => {
			this.#reconnectHandlers.delete(handler);
		};
	}

	#dispatch(topic: LiveTopic, e: MessageEvent<string>) {
		let msg: LiveMessage<never>;
		try {
			msg = JSON.parse(e.data);
		} catch {
			return;
		}
		this.lastMessageAt = Date.now();
		for (const key of [topic, '*']) {
			const set = this.#handlers.get(key);
			if (set) for (const h of set) this.#safe(() => h(msg));
		}
	}

	#safe(fn: () => void) {
		try {
			fn();
		} catch (err) {
			console.error('live handler failed', err);
		}
	}

	#scheduleReconnect() {
		if (!this.#wanted) return;
		this.#clearTimer();
		const delay = Math.min(30000, 1000 * 2 ** Math.min(this.#attempt, 5));
		this.#attempt++;
		this.#timer = setTimeout(async () => {
			this.#timer = null;
			if (!this.#wanted) return;
			try {
				// redirects to the login page on 401
				await request('GET', '/api/v1/auth/me');
			} catch {
				if (this.#wanted) this.#scheduleReconnect();
				return;
			}
			this.connect();
		}, delay);
	}

	#clearTimer() {
		if (this.#timer) clearTimeout(this.#timer);
		this.#timer = null;
	}
}

export const live = new LiveStream();
