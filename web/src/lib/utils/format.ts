// German formatting helpers for dates, durations, sizes and numbers.

const pad = (n: number) => String(n).padStart(2, '0');

/** Parses an API timestamp (RFC3339), Date or epoch millis; invalid/zero → null. */
export function toDate(v: string | number | Date | null | undefined): Date | null {
	if (v === null || v === undefined || v === '') return null;
	const d = v instanceof Date ? v : new Date(v);
	if (isNaN(d.getTime()) || d.getFullYear() < 1971) return null;
	return d;
}

/** dd.MM.yyyy HH:mm */
export function formatDateTime(v: string | number | Date | null | undefined, withSeconds = false): string {
	const d = toDate(v);
	if (!d) return '–';
	const s = `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
	return withSeconds ? `${s}:${pad(d.getSeconds())}` : s;
}

/** dd.MM.yyyy */
export function formatDate(v: string | number | Date | null | undefined): string {
	const d = toDate(v);
	if (!d) return '–';
	return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()}`;
}

/** HH:mm(:ss) */
export function formatTime(v: string | number | Date | null | undefined, withSeconds = false): string {
	const d = toDate(v);
	if (!d) return '–';
	return withSeconds
		? `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
		: `${pad(d.getHours())}:${pad(d.getMinutes())}`;
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

const rtf = new Intl.RelativeTimeFormat('de', { numeric: 'auto' });

/** "vor 5 Minuten", "in 2 Stunden", "gerade eben". */
export function formatRelative(v: string | number | Date | null | undefined, now = Date.now()): string {
	const d = toDate(v);
	if (!d) return '–';
	const diff = (d.getTime() - now) / 1000;
	const abs = Math.abs(diff);
	// up to 10 s in the future is clock skew between server and browser, not a future time
	if (abs < 45) return diff <= 10 ? 'gerade eben' : 'in wenigen Sekunden';
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
		f = new Intl.NumberFormat('de-DE', { minimumFractionDigits: min, maximumFractionDigits: max });
		nfCache.set(k, f);
	}
	return f;
}

/** German number format ("1.234,5"). */
export function formatNumber(n: number | null | undefined, digits = 0, minDigits = 0): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	return nf(Math.min(minDigits, digits), digits).format(n);
}

/** Percentage 0..100 → "99,95 %". */
export function formatPercent(n: number | null | undefined, digits = 1): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	return `${formatNumber(n, digits)} %`;
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

/** Latency in ms → "0,42 ms" / "12 ms". */
export function formatMs(n: number | null | undefined): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	return `${formatNumber(n, n < 10 ? 2 : n < 100 ? 1 : 0)} ms`;
}

/** Singular/plural: plural(3, 'Gerät', 'Geräte') → "3 Geräte". */
export function plural(n: number, one: string, many: string): string {
	return `${formatNumber(n)} ${n === 1 ? one : many}`;
}

/** Truncates long text with an ellipsis. */
export function truncate(s: string, max: number): string {
	return s.length > max ? s.slice(0, max - 1) + '…' : s;
}
