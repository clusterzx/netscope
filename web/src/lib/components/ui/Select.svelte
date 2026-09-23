<!--
	Native select (keyboard friendly, mobile friendly).
	<Select label="Zustand" bind:value={state} options={[{ value: 'known', label: 'Bekannt' }]} placeholder="— alle —" />
	Options may be strings or { value, label, disabled }. `placeholder` adds an empty option ("").
-->
<script lang="ts" module>
	export type SelectOption = string | { value: string; label: string; disabled?: boolean };
</script>

<script lang="ts">
	import type { HTMLSelectAttributes } from 'svelte/elements';
	import FormField from './FormField.svelte';
	import Icon from './Icon.svelte';

	interface Props extends Omit<HTMLSelectAttributes, 'size' | 'class' | 'value'> {
		value?: string | null;
		options: SelectOption[];
		label?: string;
		hint?: string;
		error?: string | null;
		placeholder?: string;
		size?: 'sm' | 'md';
		class?: string;
		selectClass?: string;
	}

	let {
		value = $bindable(),
		options,
		label,
		hint,
		error,
		placeholder,
		size = 'md',
		required = false,
		id,
		class: klass = '',
		selectClass = '',
		...rest
	}: Props = $props();

	const norm = $derived(
		options.map((o) => (typeof o === 'string' ? { value: o, label: o, disabled: false } : o))
	);
</script>

<FormField {label} {hint} {error} required={!!required} id={id ?? undefined} class={klass}>
	{#snippet children(fid, describedby)}
		<div class="relative flex items-center">
			<select
				bind:value
				id={fid}
				{required}
				aria-invalid={error ? 'true' : undefined}
				aria-describedby={describedby}
				class="w-full min-w-0 appearance-none rounded-md border bg-surface pr-8 text-fg shadow-sm transition-colors
					focus:border-accent focus:outline-none focus:ring-2 focus:ring-focus disabled:cursor-not-allowed disabled:opacity-60
					{error ? 'border-danger' : 'border-border hover:border-border-strong'}
					{size === 'sm' ? 'h-7 pl-2 text-[0.8125rem]' : 'h-8.5 pl-2.5 text-sm'} {selectClass}"
				{...rest}
			>
				{#if placeholder !== undefined}
					<option value="">{placeholder}</option>
				{/if}
				{#each norm as o (o.value)}
					<option value={o.value} disabled={o.disabled}>{o.label}</option>
				{/each}
			</select>
			<span class="pointer-events-none absolute right-2 text-fg-subtle"
				><Icon name="chevron-down" size={14} /></span
			>
		</div>
	{/snippet}
</FormField>
