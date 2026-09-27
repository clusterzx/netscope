// Helpers for plugin device actions (e.g. Wake-on-LAN).
import type { ActionOutcome, DeviceAction } from '$lib/api/types';
import { t } from '$lib/i18n';
import { toast } from '$lib/stores/toast.svelte';

/** Shows the outcome of an action run as toast. */
export function outcomeToast(action: DeviceAction, out: ActionOutcome | undefined | null) {
	const title = action.label;
	if (!out) {
		toast.success(t('Aktion ausgeführt'), { title });
		return;
	}
	if (out.error || out.status === 'failed' || out.status === 'timeout') {
		toast.error(out.error || t('Aktion fehlgeschlagen'), { title });
	} else if (!out.finished) {
		toast.info(t('Aktion läuft weiter im Hintergrund (Lauf #{id}).', { id: out.runId }), { title });
	} else {
		toast.success(out.result?.message || t('Aktion ausgeführt'), { title });
	}
}
