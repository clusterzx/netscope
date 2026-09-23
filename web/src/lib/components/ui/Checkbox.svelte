<!--
	Checkbox with label. Supports indeterminate (e.g. "select all" in tables).
	<Checkbox bind:checked={x} label="Nur offene" />
	<Checkbox checked={all} indeterminate={some} onchange={toggleAll} label="Alle auswählen" hideLabel />
-->
<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props extends Omit<HTMLInputAttributes, 'type' | 'class' | 'checked'> {
		checked?: boolean;
		indeterminate?: boolean;
		label?: string;
		/** visually hide the label (still read by screen readers) */
		hideLabel?: boolean;
		description?: string;
		class?: string;
	}

	let {
		checked = $bindable(false),
		indeterminate = false,
		label,
		hideLabel = false,
		description,
		id,
		class: klass = '',
		...rest
	}: Props = $props();

	const uid = $props.id();
	const fid = $derived(id ?? `cb-${uid}`);
</script>

<label for={fid} class="inline-flex cursor-pointer items-start gap-2 text-sm select-none {klass}">
	<input
		type="checkbox"
		id={fid}
		bind:checked
		{indeterminate}
		class="mt-0.5 h-4 w-4 shrink-0 cursor-pointer rounded border-border-strong accent-(--accent)"
		{...rest}
	/>
	{#if label}
		<span class={hideLabel ? 'sr-only' : ''}>
			<span class="text-fg">{label}</span>
			{#if description}<span class="block text-xs text-fg-subtle">{description}</span>{/if}
		</span>
	{/if}
</label>
