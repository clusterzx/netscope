<!--
	Secret input: shows "gesetzt"/"nicht gesetzt". value === SECRET_MASK keeps the stored secret,
	"" clears it, any other string replaces it.
-->
<script lang="ts">
	import { SECRET_MASK } from '$lib/api/types';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import { untrack } from 'svelte';

	interface Props {
		value: string;
		label: string;
		hint?: string;
		error?: string | null;
		required?: boolean;
		multiline?: boolean;
		placeholder?: string;
		disabled?: boolean;
		id: string;
	}

	let {
		value = $bindable(''),
		label,
		hint,
		error,
		required = false,
		multiline = false,
		placeholder,
		disabled = false,
		id
	}: Props = $props();

	// was a secret stored when the form opened?
	const wasSet = untrack(() => value === SECRET_MASK);
	let editing = $state(untrack(() => value !== SECRET_MASK && value !== ''));
	let text = $state(untrack(() => (value !== SECRET_MASK ? value : '')));

	const status = $derived(
		value === SECRET_MASK ? 'set' : value === '' ? (wasSet ? 'cleared' : 'unset') : 'new'
	);

	function startEdit() {
		editing = true;
		text = '';
	}
	function cancel() {
		editing = false;
		text = '';
		value = wasSet ? SECRET_MASK : '';
	}
	function clear() {
		editing = false;
		text = '';
		value = '';
	}
	function oninput() {
		value = text === '' ? (wasSet ? SECRET_MASK : '') : text;
	}

	const inputCls =
		'w-full min-w-0 rounded-md border bg-surface px-2.5 text-sm text-fg shadow-sm placeholder:text-fg-subtle focus:border-accent focus:outline-none focus:ring-2 focus:ring-focus';
</script>

<FormField {label} {hint} {error} {required} {id}>
	{#snippet labelExtra()}
		{#if status === 'set'}
			<Badge tone="ok" dot>gesetzt</Badge>
		{:else if status === 'cleared'}
			<Badge tone="warn" dot>wird entfernt</Badge>
		{:else if status === 'new'}
			<Badge tone="accent" dot>{wasSet ? 'wird ersetzt' : 'neu'}</Badge>
		{:else}
			<Badge dot>nicht gesetzt</Badge>
		{/if}
	{/snippet}
	{#snippet children(fid, describedby)}
		{#if editing || (!wasSet && status !== 'cleared')}
			<div class="flex items-start gap-2">
				{#if multiline}
					<textarea
						id={fid}
						bind:value={text}
						{oninput}
						{disabled}
						rows="5"
						spellcheck="false"
						autocomplete="off"
						aria-describedby={describedby}
						aria-invalid={error ? 'true' : undefined}
						placeholder={placeholder ?? (wasSet ? 'Neuen Wert eingeben (leer = unverändert)' : '')}
						class="mono py-1.5 {inputCls} {error ? 'border-danger' : 'border-border'}"></textarea>
				{:else}
					<input
						id={fid}
						type="password"
						bind:value={text}
						{oninput}
						{disabled}
						autocomplete="new-password"
						aria-describedby={describedby}
						aria-invalid={error ? 'true' : undefined}
						placeholder={placeholder ?? (wasSet ? 'Neuen Wert eingeben (leer = unverändert)' : '')}
						class="h-8.5 {inputCls} {error ? 'border-danger' : 'border-border'}"
					/>
				{/if}
				{#if wasSet}
					<Button size="md" variant="ghost" onclick={cancel} {disabled}>Abbrechen</Button>
				{/if}
			</div>
		{:else}
			<div id={fid} class="flex flex-wrap items-center gap-2" aria-describedby={describedby}>
				<span class="mono text-sm text-fg-subtle">{status === 'cleared' ? '(leer)' : '••••••••'}</span>
				{#if status === 'cleared'}
					<Button size="sm" variant="ghost" icon="history" onclick={cancel} {disabled}>Rückgängig</Button>
				{:else}
					<Button size="sm" icon="edit" onclick={startEdit} {disabled}>Ändern</Button>
					<Button size="sm" variant="ghost" icon="trash" onclick={clear} {disabled}>Entfernen</Button>
				{/if}
			</div>
		{/if}
	{/snippet}
</FormField>
