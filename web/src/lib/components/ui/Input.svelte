<!--
	Text input with optional label, hint, error, leading icon and trailing snippet.
	<Input label="Name" bind:value={name} required error={errors.name} />
	<Input type="number" bind:value={n} min={1} />         (value is a number for type=number)
	<Input icon="search" placeholder="Suchen …" bind:value={q} size="sm" />
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLInputAttributes } from 'svelte/elements';
	import FormField from './FormField.svelte';
	import Icon from './Icon.svelte';
	import type { IconName } from './icons';

	interface Props extends Omit<HTMLInputAttributes, 'size' | 'class' | 'value'> {
		value?: string | number | null;
		label?: string;
		hint?: string;
		error?: string | null;
		icon?: IconName;
		size?: 'sm' | 'md';
		mono?: boolean;
		class?: string;
		inputClass?: string;
		trailing?: Snippet;
		labelExtra?: Snippet;
		ref?: HTMLInputElement | null;
	}

	let {
		value = $bindable(),
		label,
		hint,
		error,
		icon,
		size = 'md',
		mono = false,
		required = false,
		id,
		class: klass = '',
		inputClass = '',
		trailing,
		labelExtra,
		ref = $bindable(null),
		...rest
	}: Props = $props();
</script>

<FormField {label} {hint} {error} required={!!required} id={id ?? undefined} class={klass} {labelExtra}>
	{#snippet children(fid, describedby)}
		<div class="relative flex items-center">
			{#if icon}
				<span class="pointer-events-none absolute left-2.5 text-fg-subtle">
					<Icon name={icon} size={size === 'sm' ? 14 : 16} />
				</span>
			{/if}
			<input
				bind:this={ref}
				bind:value
				id={fid}
				{required}
				aria-invalid={error ? 'true' : undefined}
				aria-describedby={describedby}
				class="w-full min-w-0 rounded-md border bg-surface text-fg placeholder:text-fg-subtle shadow-sm transition-colors
					focus:border-accent focus:outline-none focus:ring-2 focus:ring-focus disabled:cursor-not-allowed disabled:opacity-60
					{error ? 'border-danger' : 'border-border hover:border-border-strong'}
					{size === 'sm' ? 'h-7 px-2 text-[0.8125rem]' : 'h-8.5 px-2.5 text-sm'}
					{icon ? (size === 'sm' ? 'pl-7' : 'pl-8') : ''}
					{trailing ? 'pr-9' : ''}
					{mono ? 'mono' : ''} {inputClass}"
				{...rest}
			/>
			{#if trailing}
				<span class="absolute right-1.5 flex items-center">{@render trailing()}</span>
			{/if}
		</div>
	{/snippet}
</FormField>
