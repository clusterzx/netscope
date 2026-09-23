// Helpers for the reports page.

export const RANGE_PRESETS = [
	{ id: '24h', label: '24 h', ms: 24 * 3600_000 },
	{ id: '7d', label: '7 Tage', ms: 7 * 86400_000 },
	{ id: '30d', label: '30 Tage', ms: 30 * 86400_000 }
] as const;

export type RangePreset = (typeof RANGE_PRESETS)[number]['id'];
