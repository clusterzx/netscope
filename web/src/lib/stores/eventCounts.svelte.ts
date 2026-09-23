// Open (unacknowledged) events per severity, live via SSE.
//   eventCounts.counts.critical, eventCounts.urgent (high + critical)
import { api } from '$lib/api/client';
import { live } from './live.svelte';

class EventCounts {
	counts = $state<Record<string, number>>({});
	urgent = $derived((this.counts.high ?? 0) + (this.counts.critical ?? 0));
	total = $derived(Object.values(this.counts).reduce((a, b) => a + b, 0));
	#started = false;
	#timer: ReturnType<typeof setTimeout> | null = null;

	init() {
		if (this.#started) return;
		this.#started = true;
		this.refresh();
		live.on('event', () => this.#debounced());
		live.onReconnect(() => this.refresh());
	}

	async refresh() {
		try {
			this.counts = (await api.get('/api/v1/events/counts')) ?? {};
		} catch {
			// keep last counts
		}
	}

	#debounced() {
		if (this.#timer) clearTimeout(this.#timer);
		this.#timer = setTimeout(() => this.refresh(), 800);
	}
}

export const eventCounts = new EventCounts();
