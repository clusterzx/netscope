<!--
	Label + control + hint/error wrapper for any control. The control snippet receives the
	generated id and the describedby id; pass them to the control for accessibility.
	<FormField label="Gruppe" hint="…" error={errors.group}>
		{#snippet children(id, describedby)}<select {id} aria-describedby={describedby}>…</select>{/snippet}
	</FormField>
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		label?: string;
		hint?: string;
		error?: string | null;
		required?: boolean;
		id?: string;
		/** render label and control side by side (for toggles/checkboxes) */
		inline?: boolean;
		class?: string;
		labelExtra?: Snippet;
		children: Snippet<[string, string | undefined]>;
	}

	let {
		label,
		hint,
		error,
		required = false,
		id,
		inline = false,
		class: klass = '',
		labelExtra,
		children
	}: Props = $props();

	const uid = $props.id();
	const fieldId = $derived(id ?? `f-${uid}`);
	const describedby = $derived(error || hint ? `${fieldId}-desc` : undefined);
</script>

<div class="flex {inline ? 'flex-row items-center justify-between gap-3' : 'flex-col gap-1'} {klass}">
	{#if label}
		<div class="flex items-center justify-between gap-2">
			<label for={fieldId} class="text-[0.8125rem] font-medium text-fg">
				{label}{#if required}<span class="ml-0.5 text-danger" aria-hidden="true">*</span>{/if}
			</label>
			{@render labelExtra?.()}
		</div>
	{/if}
	{@render children(fieldId, describedby)}
	{#if error}
		<p id={describedby} class="text-xs text-danger" role="alert">{error}</p>
	{:else if hint}
		<p id={describedby} class="text-xs text-fg-subtle">{hint}</p>
	{/if}
</div>
