// Language of the user interface (German or English).
//
// German is the source language: texts are written in German and wrapped in t(), the
// English catalog (./en/*.json) maps each German text to its translation. A German text
// without an English entry is a type error (svelte-check), `npm run check` also runs
// scripts/i18n-check.mjs (unused or duplicate keys, placeholders, untranslated text).
//
//   import { t, tn, msg } from '$lib/i18n';
//   t('Gerät löschen')                       // "Delete device" in English
//   t('Gerät {name} gelöscht', { name })     // placeholders in braces
//   tn(n, '{n} Gerät', '{n} Geräte')         // singular/plural, {n} formatted
//   const label = msg('Geräte');             // a key kept in data, translated later with t(label)
//   t('Benutzer@@Mehrzahl')                  // context after @@ for a German word with two
//                                            // English meanings ("Users"; German: "Benutzer")
//
// The language is fixed for the lifetime of the page: it is decided once when the app
// loads (stored preference of the signed-in user, otherwise the browser language, otherwise
// German) and changing it reloads the page. So t() may be used anywhere, also in module
// constants. The server gets the language as Accept-Language and answers in it (plugin
// settings, event catalog, error messages).
import en from './en';

export type Locale = 'de' | 'en';
/** A German source text that has an English translation. */
export type Msg = keyof typeof en;
/** Placeholder values; null and undefined render as an empty text. */
export type Vars = Record<string, string | number | null | undefined>;
/** Stored preference: '' follows the browser. */
export type LocalePreference = '' | Locale;

export const LOCALES: { value: Locale; label: string }[] = [
	{ value: 'de', label: 'Deutsch' },
	{ value: 'en', label: 'English' }
];

const KEY = 'netscope.locale';
const catalog: Record<string, string> = en;

function stored(): Locale | null {
	try {
		const v = localStorage.getItem(KEY);
		if (v === 'de' || v === 'en') return v;
	} catch {
		// storage unavailable
	}
	return null;
}

/** First German or English entry of the browser languages, otherwise German. */
export function browserLocale(): Locale {
	if (typeof navigator === 'undefined') return 'de';
	const list = navigator.languages?.length ? navigator.languages : [navigator.language];
	for (const l of list) {
		const p = (l ?? '').slice(0, 2).toLowerCase();
		if (p === 'de' || p === 'en') return p;
	}
	return 'de';
}

function detect(): Locale {
	if (typeof window === 'undefined') return 'de';
	// before signing in the page follows the browser, not the last user's preference
	if (window.location.pathname === '/login') return browserLocale();
	return stored() ?? browserLocale();
}

/** The language of this page. */
export const locale: Locale = detect();

if (typeof document !== 'undefined') document.documentElement.lang = locale;

function englishVariant(): string {
	if (typeof navigator !== 'undefined') {
		// the browser's English variant counts only if the browser prefers English over German
		for (const l of navigator.languages?.length ? navigator.languages : [navigator.language]) {
			const p = (l ?? '').slice(0, 2).toLowerCase();
			if (p === 'de') break;
			if (p === 'en') {
				if (/^en-[a-z]{2}$/i.test(l)) return l;
				break;
			}
		}
	}
	// day-month-year and a 24 hour clock, the closest to the German format
	return 'en-GB';
}

/** BCP 47 tag for Intl formatting: de-DE, or the browser's English variant (en-GB default). */
export const intlLocale: string = locale === 'de' ? 'de-DE' : englishVariant();

/** Translates a German text; {name} placeholders are replaced by vars. */
export function t(key: Msg, vars?: Vars): string {
	const s = locale === 'en' ? (catalog[key] ?? key) : withoutContext(key);
	return vars ? fill(s, vars) : s;
}

/**
 * A German word with two English meanings gets a context after "@@" in its key, e.g.
 * 'Benutzer@@Mehrzahl' ("Users") next to 'Benutzer' ("User"); German shows the part before.
 */
function withoutContext(key: string): string {
	const i = key.indexOf('@@');
	return i < 0 ? key : key.slice(0, i);
}

/** Singular or plural by count; {n} is the formatted count. */
export function tn(n: number, one: Msg, other: Msg, vars?: Vars): string {
	return t(n === 1 ? one : other, { n: new Intl.NumberFormat(intlLocale).format(n), ...vars });
}

/** Marks a German text as translation key without translating it (for data kept in constants). */
export function msg<K extends Msg>(key: K): K {
	return key;
}

function fill(s: string, vars: Vars): string {
	return s.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k] ?? '') : m));
}

/**
 * Applies the signed-in user's preference ('' = browser language) and reports whether the
 * page has to be reloaded to show it.
 */
export function applyPreference(pref: string | null | undefined): boolean {
	const want = pref === 'de' || pref === 'en' ? pref : null;
	try {
		if (want) localStorage.setItem(KEY, want);
		else localStorage.removeItem(KEY);
	} catch {
		// storage unavailable: the preference applies from the server's answer only
	}
	return (want ?? browserLocale()) !== locale;
}

/** Forgets the stored preference (sign-out); true if the login page needs another language. */
export function forgetPreference(): boolean {
	try {
		localStorage.removeItem(KEY);
	} catch {
		// ignore
	}
	return browserLocale() !== locale;
}
