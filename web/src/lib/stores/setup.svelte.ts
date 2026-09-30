// State of the setup wizard (GET /api/v1/setup, no login needed). While the setup is
// pending every page leads to /welcome.
//
//   const st = await setupState.check(fetch)   // { pending, account }
//   setupState.finished()                        // after POST /api/v1/setup/complete
import { api } from '$lib/api/client';
import type { SetupStatus } from '$lib/api/types';

class SetupState {
	status = $state<SetupStatus | null>(null);
	#pending: Promise<SetupStatus | null> | null = null;

	/** Reads the status once (cached; null if the server cannot be reached). */
	check(fetchFn?: typeof fetch, force = false): Promise<SetupStatus | null> {
		if (this.status && !force) return Promise.resolve(this.status);
		if (this.#pending) return this.#pending;
		this.#pending = api
			.get('/api/v1/setup', { auth: false, fetch: fetchFn })
			.then((st) => (this.status = st))
			.catch(() => null)
			.finally(() => (this.#pending = null));
		return this.#pending;
	}

	/** The wizard finished: the application opens normally. */
	finished() {
		this.status = { pending: false, account: true };
	}
}

export const setupState = new SetupState();
