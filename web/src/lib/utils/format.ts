// Formatting helpers for dates, durations, sizes and numbers in the language of the UI
// (German: 27.09.2026 14:05, 1.234,5; English: the browser's English variant, en-GB by default).
import { intlLocale, locale, t } from '$lib/i18n';

const pad = (n: number) => String(n).padStart(2, '0');

/** Parses an API timestamp (RFC3339), Date or epoch millis; invalid/zero → null. */
export function toDate(v: string | number | Date | null | undefined): Date | null {
	if (v === null || v === undefined || v === '') return null;
	const d = v instanceof Date ? v : new Date(v);
	if (isNaN(d.getTime()) || d.getFullYear() < 1971) return null;
	return d;
}

const dtfCache = new Map<string, Intl.DateTimeFormat>();
function dtf(key: string, opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
	let f = dtfCache.get(key);
	if (!f) {
		f = new Intl.DateTimeFormat(intlLocale, opts);
		dtfCache.set(key, f);
	}
	return f;
}

/** dd.MM.yyyy HH:mm (English: e.g. 27/09/2026, 14:05) */
export function formatDateTime(v: string | number | Date | null | undefined, withSeconds = false): string {
	const d = toDate(v);
	if (!d) return '–';
	if (locale === 'en')
		return dtf(withSeconds ? 'dts' : 'dt', {
			day: '2-digit',
			month: '2-digit',
			year: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			second: withSeconds ? '2-digit' : undefined
		}).format(d);
	const s = `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
	return withSeconds ? `${s}:${pad(d.getSeconds())}` : s;
}

/** dd.MM.yyyy (English: e.g. 27/09/2026) */
export function formatDate(v: string | number | Date | null | undefined): string {
	const d = toDate(v);
	if (!d) return '–';
	if (locale === 'en') return dtf('d', { day: '2-digit', month: '2-digit', year: 'numeric' }).format(d);
	return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()}`;
}

/** dd.MM. – day and month without the year (chart axes, compact lists) */
export function formatDayMonth(v: string | number | Date | null | undefined): string {
	const d = toDate(v);
	if (!d) return '–';
	if (locale === 'en') return dtf('dm', { day: '2-digit', month: '2-digit' }).format(d);
	return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.`;
}

/** HH:mm(:ss) (English: the variant's clock, e.g. 2:05 pm in en-US) */
export function formatTime(v: string | number | Date | null | undefined, withSeconds = false): string {
	const d = toDate(v);
	if (!d) return '–';
	if (locale === 'en')
		return dtf(withSeconds ? 'ts' : 't', {
			hour: '2-digit',
			minute: '2-digit',
			second: withSeconds ? '2-digit' : undefined
		}).format(d);
	return withSeconds
		? `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
		: `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Weekday and date, e.g. "Mo., 27.09." / "Mon 27/09" */
export function formatWeekdayDate(v: string | number | Date | null | undefined): string {
	const d = toDate(v);
	if (!d) return '–';
	return dtf('wd', { weekday: 'short', day: '2-digit', month: '2-digit' }).format(d);
}

/** Value for <input type="datetime-local"> (local time, yyyy-MM-ddTHH:mm). */
export function toDateTimeLocal(v: string | number | Date | null | undefined): string {
	const d = toDate(v);
	if (!d) return '';
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Parses a datetime-local value into an RFC3339 string (with offset) for the API. */
export function fromDateTimeLocal(v: string): string {
	if (!v) return '';
	const d = new Date(v);
	return isNaN(d.getTime()) ? '' : d.toISOString();
}

const rtf = new Intl.RelativeTimeFormat(intlLocale, { numeric: 'auto' });

/** "vor 5 Minuten", "in 2 Stunden", "gerade eben" ("5 minutes ago" …). */
export function formatRelative(v: string | number | Date | null | undefined, now = Date.now()): string {
	const d = toDate(v);
	if (!d) return '–';
	const diff = (d.getTime() - now) / 1000;
	const abs = Math.abs(diff);
	// up to 10 s in the future is clock skew between server and browser, not a future time
	if (abs < 45) return diff <= 10 ? t('gerade eben') : t('in wenigen Sekunden');
	if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute');
	if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour');
	if (abs < 86400 * 30) return rtf.format(Math.round(diff / 86400), 'day');
	if (abs < 86400 * 365) return rtf.format(Math.round(diff / (86400 * 30)), 'month');
	return rtf.format(Math.round(diff / (86400 * 365)), 'year');
}

/** Duration in milliseconds → "850 ms", "12,4 s", "3 min 20 s", "2 h 5 min", "3 d 4 h". */
export function formatDuration(ms: number | null | undefined): string {
	if (ms === null || ms === undefined || !isFinite(ms)) return '–';
	if (ms < 1000) return `${Math.round(ms)} ms`;
	const s = ms / 1000;
	if (s < 60) return `${formatNumber(s, s < 10 ? 1 : 0)} s`;
	const m = Math.floor(s / 60);
	if (m < 60) return `${m} min${Math.round(s % 60) ? ` ${Math.round(s % 60)} s` : ''}`;
	const h = Math.floor(m / 60);
	if (h < 48) return `${h} h${m % 60 ? ` ${m % 60} min` : ''}`;
	const d = Math.floor(h / 24);
	return `${d} d${h % 24 ? ` ${h % 24} h` : ''}`;
}

/** Seconds → duration text. */
export function formatSeconds(s: number | null | undefined): string {
	return s === null || s === undefined ? '–' : formatDuration(s * 1000);
}

const nfCache = new Map<string, Intl.NumberFormat>();
function nf(min: number, max: number) {
	const k = `${min}:${max}`;
	let f = nfCache.get(k);
	if (!f) {
		f = new Intl.NumberFormat(intlLocale, { minimumFractionDigits: min, maximumFractionDigits: max });
		nfCache.set(k, f);
	}
	return f;
}

/** Number in the UI language ("1.234,5" / "1,234.5"). */
export function formatNumber(n: number | null | undefined, digits = 0, minDigits = 0): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	return nf(Math.min(minDigits, digits), digits).format(n);
}

/** Percentage 0..100 → "99,95 %" (German) / "99.95%" (English). */
export function formatPercent(n: number | null | undefined, digits = 1): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	return percentUnit(formatNumber(n, digits));
}

/** Appends the percent sign the way the language writes it ("12 %" / "12%"). */
export function percentUnit(s: string | number): string {
	return locale === 'de' ? `${s} %` : `${s}%`;
}

/** Bytes → "1,2 GB". */
export function formatBytes(n: number | null | undefined): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
	let i = 0;
	let v = n;
	while (Math.abs(v) >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${formatNumber(v, i === 0 ? 0 : v < 10 ? 1 : 0)} ${units[i]}`;
}

/** Bits per second → "950 Mbit/s" (decimal units, as port speeds are given). */
export function formatBps(n: number | null | undefined): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	const units = ['bit/s', 'kbit/s', 'Mbit/s', 'Gbit/s', 'Tbit/s'];
	let i = 0;
	let v = n;
	while (Math.abs(v) >= 1000 && i < units.length - 1) {
		v /= 1000;
		i++;
	}
	return `${formatNumber(v, i === 0 ? 0 : v < 10 ? 1 : 0)} ${units[i]}`;
}

/** Latency in ms → "0,42 ms" / "12 ms". */
export function formatMs(n: number | null | undefined): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	return `${formatNumber(n, n < 10 ? 2 : n < 100 ? 1 : 0)} ms`;
}

/**
 * Singular/plural with the count in front: plural(3, t('Gerät'), t('Geräte')) → "3 Geräte".
 * For sentences use tn() from $lib/i18n.
 */
export function plural(n: number, one: string, many: string): string {
	return `${formatNumber(n)} ${n === 1 ? one : many}`;
}

/** Truncates long text with an ellipsis. */
export function truncate(s: string, max: number): string {
	return s.length > max ? s.slice(0, max - 1) + '…' : s;
}
