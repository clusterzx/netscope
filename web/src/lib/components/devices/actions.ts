// Helpers for plugin device actions (e.g. Wake-on-LAN).
import type { ActionOutcome, DeviceAction } from '$lib/api/types';
import { toast } from '$lib/stores/toast.svelte';

/** Shows the outcome of an action run as toast. */
export function outcomeToast(action: DeviceAction, out: ActionOutcome | undefined | null) {
	const title = action.label;
	if (!out) {
		toast.success('Aktion ausgeführt', { title });
		return;
	}
	if (out.error || out.status === 'failed' || out.status === 'timeout') {
		toast.error(out.error || 'Aktion fehlgeschlagen', { title });
	} else if (!out.finished) {
		toast.info(`Aktion läuft weiter im Hintergrund (Lauf #${out.runId}).`, { title });
	} else {
		toast.success(out.result?.message || 'Aktion ausgeführt', { title });
	}
}
