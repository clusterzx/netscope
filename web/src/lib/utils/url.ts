// Helpers for URL-driven page state (filters in the query string).
//
//   const q = $derived(page.url.searchParams.get('q') ?? '');          // read (reactive via $app/state)
//   setParams({ q: 'tag:iot', offset: null });                          // write (replaceState, keeps focus/scroll)
import { goto } from '$app/navigation';
import { page } from '$app/state';

export type ParamValue = string | number | boolean | null | undefined | string[];

/**
 * Updates query parameters of the current URL. null/undefined/"" (and empty arrays) remove
 * a parameter; arrays are joined with commas. Uses replaceState unless `push` is set.
 */
export function setParams(params: Record<string, ParamValue>, opts: { push?: boolean } = {}): Promise<void> {
	const url = new URL(page.url);
	for (const [k, v] of Object.entries(params)) {
		if (v === null || v === undefined || v === '' || (Array.isArray(v) && v.length === 0))
			url.searchParams.delete(k);
		else url.searchParams.set(k, Array.isArray(v) ? v.join(',') : String(v));
	}
	if (url.search === page.url.search) return Promise.resolve();
	return goto(url.pathname + url.search + url.hash, {
		replaceState: !opts.push,
		keepFocus: true,
		noScroll: true
	});
}

/** Integer query parameter with default. */
export function intParam(sp: URLSearchParams, key: string, def: number): number {
	const v = sp.get(key);
	if (v === null || v === '') return def;
	const n = parseInt(v, 10);
	return isFinite(n) ? n : def;
}

/** Comma separated list parameter. */
export function listParam(sp: URLSearchParams, key: string): string[] {
	const v = sp.get(key);
	return v ? v.split(',').filter(Boolean) : [];
}

/** Returns a debounced version of fn. */
export function debounce<A extends unknown[]>(
	fn: (...args: A) => void,
	ms: number
): ((...args: A) => void) & {
	cancel: () => void;
} {
	let t: ReturnType<typeof setTimeout> | null = null;
	const d = (...args: A) => {
		if (t) clearTimeout(t);
		t = setTimeout(() => fn(...args), ms);
	};
	d.cancel = () => {
		if (t) clearTimeout(t);
	};
	return d;
}

/** Persisted per-browser preference (localStorage, JSON). */
export function loadPref<T>(key: string, def: T): T {
	try {
		const v = localStorage.getItem('netscope.' + key);
		return v === null ? def : (JSON.parse(v) as T);
	} catch {
		return def;
	}
}

export function savePref(key: string, value: unknown) {
	try {
		localStorage.setItem('netscope.' + key, JSON.stringify(value));
	} catch {
		// storage unavailable
	}
}
