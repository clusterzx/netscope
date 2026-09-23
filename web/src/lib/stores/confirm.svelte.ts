// Promise based confirm dialog. Rendered by <ConfirmHost /> in the root layout.
//
//   if (await confirm({ title: 'Gerät löschen?', message: '…', confirmLabel: 'Löschen', danger: true })) …

export interface ConfirmOptions {
	title: string;
	message?: string;
	confirmLabel?: string;
	cancelLabel?: string;
	/** red confirm button */
	danger?: boolean;
}

interface Pending extends ConfirmOptions {
	resolve: (ok: boolean) => void;
}

class ConfirmState {
	current = $state<Pending | null>(null);

	ask(opts: ConfirmOptions): Promise<boolean> {
		this.current?.resolve(false);
		return new Promise((resolve) => {
			this.current = { ...opts, resolve };
		});
	}

	answer(ok: boolean) {
		const c = this.current;
		this.current = null;
		c?.resolve(ok);
	}
}

export const confirmState = new ConfirmState();

export function confirm(opts: ConfirmOptions): Promise<boolean> {
	return confirmState.ask(opts);
}
