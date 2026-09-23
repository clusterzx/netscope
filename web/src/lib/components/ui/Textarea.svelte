<!--
	Multi-line text input.
	<Textarea label="Notiz" bind:value={note} rows={4} />
-->
<script lang="ts">
	import type { HTMLTextareaAttributes } from 'svelte/elements';
	import FormField from './FormField.svelte';

	interface Props extends Omit<HTMLTextareaAttributes, 'class' | 'value'> {
		value?: string | null;
		label?: string;
		hint?: string;
		error?: string | null;
		mono?: boolean;
		class?: string;
		textareaClass?: string;
		ref?: HTMLTextAreaElement | null;
	}

	let {
		value = $bindable(),
		label,
		hint,
		error,
		mono = false,
		required = false,
		id,
		rows = 3,
		class: klass = '',
		textareaClass = '',
		ref = $bindable(null),
		...rest
	}: Props = $props();
</script>

<FormField {label} {hint} {error} required={!!required} id={id ?? undefined} class={klass}>
	{#snippet children(fid, describedby)}
		<textarea
			bind:this={ref}
			bind:value
			id={fid}
			{rows}
			{required}
			aria-invalid={error ? 'true' : undefined}
			aria-describedby={describedby}
			class="w-full min-w-0 rounded-md border bg-surface px-2.5 py-1.5 text-sm text-fg placeholder:text-fg-subtle shadow-sm transition-colors
				focus:border-accent focus:outline-none focus:ring-2 focus:ring-focus disabled:cursor-not-allowed disabled:opacity-60
				{error ? 'border-danger' : 'border-border hover:border-border-strong'} {mono ? 'mono' : ''} {textareaClass}"
			{...rest}></textarea>
	{/snippet}
</FormField>
