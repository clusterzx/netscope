<!--
	Modal dialog (native <dialog>: focus trap, Escape, top layer).
	<Modal bind:open title="Gerät anlegen" size="md">
		…form…
		{#snippet footer()}<Button onclick={() => (open = false)}>Abbrechen</Button><Button variant="primary">Speichern</Button>{/snippet}
	</Modal>
	Wrap content + footer in <form> yourself if you want Enter-to-submit: use `as="form"` and `onsubmit`.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Button from './Button.svelte';

	interface Props {
		open?: boolean;
		title: string;
		description?: string;
		size?: 'sm' | 'md' | 'lg' | 'xl';
		/** render the body as a <form> and call onsubmit (preventDefault is done for you) */
		as?: 'div' | 'form';
		onsubmit?: () => void;
		/** prevents closing (e.g. while saving) */
		busy?: boolean;
		onclose?: () => void;
		class?: string;
		children: Snippet;
		footer?: Snippet;
	}

	let {
		open = $bindable(false),
		title,
		description,
		size = 'md',
		as = 'div',
		onsubmit,
		busy = false,
		onclose,
		class: klass = '',
		children,
		footer
	}: Props = $props();

	let dlg: HTMLDialogElement | null = $state(null);
	const uid = $props.id();

	$effect(() => {
		if (!dlg) return;
		if (open && !dlg.open) {
			dlg.showModal();
			// focus the first form control, otherwise the first footer button (e.g. "Abbrechen")
			const d = dlg;
			requestAnimationFrame(() => {
				const el =
					d.querySelector<HTMLElement>(
						'[data-modal-body] :is(input:not([type=hidden]):not([disabled]), select, textarea, [autofocus])'
					) ?? d.querySelector<HTMLElement>('footer button:not([disabled])');
				el?.focus();
			});
		} else if (!open && dlg.open) dlg.close();
	});

	function requestClose() {
		if (busy) return;
		open = false;
		onclose?.();
	}

	function onCancel(e: Event) {
		e.preventDefault();
		requestClose();
	}

	function onBackdrop(e: MouseEvent) {
		if (e.target === dlg) requestClose();
	}

	function submit(e: SubmitEvent) {
		e.preventDefault();
		onsubmit?.();
	}

	const widths = { sm: 'max-w-sm', md: 'max-w-lg', lg: 'max-w-2xl', xl: 'max-w-4xl' };
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dlg}
	aria-labelledby="{uid}-title"
	aria-describedby={description ? `${uid}-desc` : undefined}
	oncancel={onCancel}
	onclick={onBackdrop}
	class="m-auto w-[calc(100%-1.5rem)] {widths[
		size
	]} max-h-[calc(100dvh-2rem)] overflow-visible rounded-xl border border-border bg-surface p-0 text-fg shadow-lg backdrop:bg-overlay {klass}"
>
	{#if open}
		<svelte:element
			this={as}
			class="flex max-h-[calc(100dvh-2rem)] flex-col"
			onsubmit={as === 'form' ? submit : undefined}
			novalidate={as === 'form' ? true : undefined}
		>
			<header class="flex items-start gap-3 border-b border-border px-5 py-3.5">
				<div class="min-w-0 flex-1">
					<h2 id="{uid}-title" class="text-base font-semibold">{title}</h2>
					{#if description}<p id="{uid}-desc" class="mt-0.5 text-sm text-fg-muted">{description}</p>{/if}
				</div>
				<Button variant="ghost" size="sm" icon="x" label="Schließen" onclick={requestClose} disabled={busy} />
			</header>
			<div class="min-h-0 flex-1 overflow-y-auto px-5 py-4" data-modal-body>
				{@render children()}
			</div>
			{#if footer}
				<footer
					class="flex flex-wrap items-center justify-end gap-2 border-t border-border bg-surface-2 px-5 py-3 rounded-b-xl"
				>
					{@render footer()}
				</footer>
			{/if}
		</svelte:element>
	{/if}
</dialog>
