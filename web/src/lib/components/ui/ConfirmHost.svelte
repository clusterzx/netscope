<!-- Renders confirm() dialogs (mounted once in the root layout). -->
<script lang="ts">
	import { confirmState } from '$lib/stores/confirm.svelte';
	import Button from './Button.svelte';
	import Modal from './Modal.svelte';

	let open = $state(false);
	$effect(() => {
		open = confirmState.current !== null;
	});
</script>

{#if confirmState.current}
	{@const c = confirmState.current}
	<Modal bind:open title={c.title} size="sm" onclose={() => confirmState.answer(false)}>
		{#if c.message}<p class="text-sm whitespace-pre-line text-fg-muted">{c.message}</p>{/if}
		{#snippet footer()}
			<Button onclick={() => confirmState.answer(false)}>{c.cancelLabel ?? 'Abbrechen'}</Button>
			<Button variant={c.danger ? 'danger' : 'primary'} onclick={() => confirmState.answer(true)}>
				{c.confirmLabel ?? 'OK'}
			</Button>
		{/snippet}
	</Modal>
{/if}
