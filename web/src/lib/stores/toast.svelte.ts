// Toast notifications. Rendered by <Toaster /> in the app shell.
//
//   toast.success('Gespeichert');
//   toast.error(err);                       // ApiError/Error/string
//   toast.info('Scan gestartet', { title: 'ARP-Scan', timeout: 8000 });
//   toast.warning('…', { action: { label: 'Rückgängig', onClick: () => … } });
import { errorMessage } from '$lib/api/client';

export type ToastKind = 'success' | 'error' | 'info' | 'warning';

export interface ToastOptions {
	title?: string;
	/** ms until auto-dismiss; 0 = sticky. Default 5000 (errors 9000). */
	timeout?: number;
	action?: { label: string; onClick: () => void };
}

export interface ToastItem extends ToastOptions {
	id: number;
	kind: ToastKind;
	message: string;
}

class Toasts {
	items = $state<ToastItem[]>([]);
	#seq = 0;
	#timers = new Map<number, ReturnType<typeof setTimeout>>();

	show(kind: ToastKind, message: string, opts: ToastOptions = {}): number {
		const id = ++this.#seq;
		this.items = [...this.items.slice(-4), { id, kind, message, ...opts }];
		const timeout = opts.timeout ?? (kind === 'error' ? 9000 : 5000);
		if (timeout > 0)
			this.#timers.set(
				id,
				setTimeout(() => this.dismiss(id), timeout)
			);
		return id;
	}

	success(message: string, opts?: ToastOptions) {
		return this.show('success', message, opts);
	}
	info(message: string, opts?: ToastOptions) {
		return this.show('info', message, opts);
	}
	warning(message: string, opts?: ToastOptions) {
		return this.show('warning', message, opts);
	}
	/** Accepts an ApiError/Error or a message. */
	error(e: unknown, opts?: ToastOptions) {
		return this.show('error', typeof e === 'string' ? e : errorMessage(e), opts);
	}

	dismiss(id: number) {
		const t = this.#timers.get(id);
		if (t) clearTimeout(t);
		this.#timers.delete(id);
		this.items = this.items.filter((x) => x.id !== id);
	}
}

export const toast = new Toasts();
