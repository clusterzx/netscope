// Theme mode (system | light | dark), persisted in localStorage("netscope.theme").
// The resolved theme toggles the .dark class on <html>; app.html applies it before paint.

export type ThemeMode = 'system' | 'light' | 'dark';
const KEY = 'netscope.theme';

function stored(): ThemeMode {
	try {
		const v = localStorage.getItem(KEY);
		if (v === 'light' || v === 'dark' || v === 'system') return v;
	} catch {
		// storage unavailable
	}
	return 'system';
}

class Theme {
	mode = $state<ThemeMode>('system');
	systemDark = $state(false);
	resolved = $derived<'light' | 'dark'>(
		this.mode === 'system' ? (this.systemDark ? 'dark' : 'light') : this.mode
	);
	#started = false;

	/** Reads the stored mode and follows system changes. Called once by the root layout. */
	init() {
		if (this.#started || typeof window === 'undefined') return;
		this.#started = true;
		this.mode = stored();
		const mq = window.matchMedia('(prefers-color-scheme: dark)');
		this.systemDark = mq.matches;
		mq.addEventListener('change', (e) => {
			this.systemDark = e.matches;
			this.#apply();
		});
		this.#apply();
	}

	set(mode: ThemeMode) {
		this.mode = mode;
		try {
			localStorage.setItem(KEY, mode);
		} catch {
			// ignore
		}
		this.#apply();
	}

	/** system → light → dark → system */
	cycle() {
		this.set(this.mode === 'system' ? 'light' : this.mode === 'light' ? 'dark' : 'system');
	}

	#apply() {
		document.documentElement.classList.toggle('dark', this.resolved === 'dark');
	}
}

export const theme = new Theme();
