<!-- string-list / subnet-list: one entry per line; value is string[]. -->
<script lang="ts">
	import Textarea from '$lib/components/ui/Textarea.svelte';
	import { untrack } from 'svelte';

	interface Props {
		value: string[];
		label: string;
		hint?: string;
		error?: string | null;
		required?: boolean;
		placeholder?: string;
		mono?: boolean;
		disabled?: boolean;
		id: string;
	}

	let {
		value = $bindable([]),
		label,
		hint,
		error,
		required = false,
		placeholder,
		mono = false,
		disabled = false,
		id
	}: Props = $props();

	let text = $state(untrack(() => (value ?? []).join('\n')));
	const rows = $derived(Math.min(10, Math.max(3, text.split('\n').length + 1)));

	// external changes (e.g. reset) → text
	$effect(() => {
		const v = value ?? [];
		const current = untrack(() => text)
			.split(/\r?\n/)
			.map((s) => s.trim())
			.filter(Boolean);
		if (v.join('\n') !== current.join('\n')) text = v.join('\n');
	});

	function oninput() {
		value = text
			.split(/\r?\n/)
			.map((s) => s.trim())
			.filter(Boolean);
	}
</script>

<Textarea
	{label}
	hint={hint ?? 'Ein Eintrag pro Zeile'}
	{error}
	{required}
	{id}
	{placeholder}
	{mono}
	{disabled}
	{rows}
	spellcheck="false"
	bind:value={text}
	{oninput}
/>
