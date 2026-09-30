// Time zones for selection fields (IANA names known to the browser).

/** Every time zone the browser knows (UTC first), sorted by name. */
export const timezones: string[] = (() => {
	let list: string[] = [];
	try {
		const intl = Intl as unknown as { supportedValuesOf?: (key: string) => string[] };
		list = intl.supportedValuesOf?.('timeZone') ?? [];
	} catch {
		// older browsers: free input only
	}
	return ['UTC', ...list.filter((z) => z !== 'UTC')];
})();

/** The time zone of the browser ('' if unknown). */
export function browserTimezone(): string {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone ?? '';
	} catch {
		return '';
	}
}

/** Whether a time zone name is known (the server has the final word). */
export function knownTimezone(z: string): boolean {
	if (!z.trim()) return false;
	if (timezones.includes(z)) return true;
	try {
		new Intl.DateTimeFormat('en', { timeZone: z });
		return true;
	} catch {
		return false;
	}
}
