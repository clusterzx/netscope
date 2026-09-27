// Helpers for the reports page.
import { tn } from '$lib/i18n';

export const RANGE_PRESETS = [
	{ id: '24h', label: '24 h', ms: 24 * 3600_000 },
	{ id: '7d', label: tn(7, '{n} Tag', '{n} Tage'), ms: 7 * 86400_000 },
	{ id: '30d', label: tn(30, '{n} Tag', '{n} Tage'), ms: 30 * 86400_000 }
] as const;

export type RangePreset = (typeof RANGE_PRESETS)[number]['id'];
