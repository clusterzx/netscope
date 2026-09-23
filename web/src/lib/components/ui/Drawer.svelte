<!--
	Side panel (native <dialog> on the right, full height; full width on phones).
	<Drawer bind:open title="Event #42" size="lg">…{#snippet footer()}…{/snippet}</Drawer>
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Button from './Button.svelte';

	interface Props {
		open?: boolean;
		title: string;
		description?: string;
		size?: 'md' | 'lg' | 'xl';
		onclose?: () => void;
		class?: string;
		headerExtra?: Snippet;
		children: Snippet;
		footer?: Snippet;
	}

	let {
		open = $bindable(false),
		title,
		description,
		size = 'md',
		onclose,
		class: klass = '',
		headerExtra,
		children,
		footer
	}: Props = $props();

	let dlg: HTMLDialogElement | null = $state(null);
	const uid = $props.id();

	$effect(() => {
		if (!dlg) return;
		if (open && !dlg.open) dlg.showModal();
		else if (!open && dlg.open) dlg.close();
	});

	function requestClose() {
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

	const widths = { md: 'sm:max-w-md', lg: 'sm:max-w-xl', xl: 'sm:max-w-3xl' };
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dlg}
	aria-labelledby="{uid}-title"
	oncancel={onCancel}
	onclick={onBackdrop}
	class="fixed inset-y-0 right-0 left-auto m-0 h-dvh max-h-dvh w-full max-w-none {widths[
		size
	]} border-l border-border bg-surface p-0 text-fg shadow-lg backdrop:bg-overlay {klass}"
>
	{#if open}
		<div class="flex h-full flex-col">
			<header class="flex items-start gap-3 border-b border-border px-5 py-3.5">
				<div class="min-w-0 flex-1">
					<h2 id="{uid}-title" class="truncate text-base font-semibold">{title}</h2>
					{#if description}<p class="mt-0.5 text-sm text-fg-muted">{description}</p>{/if}
				</div>
				{@render headerExtra?.()}
				<Button variant="ghost" size="sm" icon="x" label="Schließen" onclick={requestClose} />
			</header>
			<div class="min-h-0 flex-1 overflow-y-auto px-5 py-4">
				{@render children()}
			</div>
			{#if footer}
				<footer
					class="flex flex-wrap items-center justify-end gap-2 border-t border-border bg-surface-2 px-5 py-3"
				>
					{@render footer()}
				</footer>
			{/if}
		</div>
	{/if}
</dialog>
