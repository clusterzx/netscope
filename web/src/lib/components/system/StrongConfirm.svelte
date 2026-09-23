<!--
	Confirmation dialog that requires typing a word (for destructive, irreversible actions).
	<StrongConfirm bind:open title="…" word="WIEDERHERSTELLEN" confirmLabel="Wiederherstellen" onconfirm={…}>…warning…</StrongConfirm>
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { untrack } from 'svelte';
	import { Button, Input, Modal } from '$lib/components/ui';

	interface Props {
		open?: boolean;
		title: string;
		word: string;
		confirmLabel: string;
		busy?: boolean;
		onconfirm: () => void;
		children: Snippet;
	}

	let {
		open = $bindable(false),
		title,
		word,
		confirmLabel,
		busy = false,
		onconfirm,
		children
	}: Props = $props();

	let typed = $state('');
	$effect(() => {
		if (open) untrack(() => (typed = ''));
	});
	const ok = $derived(typed.trim() === word);
</script>

<Modal bind:open {title} size="md" as="form" onsubmit={() => ok && !busy && onconfirm()} {busy}>
	<div class="flex flex-col gap-4 text-sm">
		{@render children()}
		<Input
			label="Zur Bestätigung „{word}“ eingeben"
			bind:value={typed}
			mono
			autocomplete="off"
			spellcheck={false}
			error={typed && !ok && typed.length >= word.length ? 'Eingabe stimmt nicht überein' : undefined}
		/>
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="danger" disabled={!ok} loading={busy}>{confirmLabel}</Button>
	{/snippet}
</Modal>
