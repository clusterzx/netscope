<!--
	Button (renders <a> when href is set).
	<Button variant="primary" icon="plus" onclick={…}>Anlegen</Button>
	<Button icon="refresh" label="Neu laden" />            icon-only: label → aria-label + tooltip
	<Button href="/devices" variant="ghost">Geräte</Button>
	Variants: primary | secondary (default) | ghost | danger | subtle; sizes: xs | sm | md (default).
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import Icon from './Icon.svelte';
	import Spinner from './Spinner.svelte';
	import type { IconName } from './icons';

	type Variant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'subtle';
	type Size = 'xs' | 'sm' | 'md';

	interface Props extends Omit<HTMLButtonAttributes, 'class'> {
		variant?: Variant;
		size?: Size;
		icon?: IconName;
		iconRight?: IconName;
		loading?: boolean;
		href?: string;
		/** accessible label (required for icon-only buttons) */
		label?: string;
		full?: boolean;
		/** marks toggle buttons as pressed */
		active?: boolean;
		class?: string;
		children?: Snippet;
	}

	let {
		variant = 'secondary',
		size = 'md',
		icon,
		iconRight,
		loading = false,
		href,
		label,
		full = false,
		active = false,
		disabled = false,
		type = 'button',
		class: klass = '',
		children,
		...rest
	}: Props = $props();

	const iconOnly = $derived(!children);
	const iconSize = $derived(size === 'xs' ? 13 : size === 'sm' ? 14 : 16);

	const base =
		'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium select-none transition-colors duration-100 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50';
	const variants: Record<Variant, string> = {
		primary: 'bg-accent text-accent-fg hover:bg-accent-hover shadow-sm',
		secondary:
			'bg-surface text-fg border border-border hover:bg-surface-2 hover:border-border-strong shadow-sm',
		ghost: 'text-fg-muted hover:text-fg hover:bg-surface-3',
		danger: 'bg-danger text-white hover:bg-danger-hover shadow-sm',
		subtle: 'bg-surface-3 text-fg hover:bg-border'
	};
	const sizes = $derived(
		iconOnly
			? { xs: 'h-6 w-6', sm: 'h-7 w-7', md: 'h-8.5 w-8.5' }[size]
			: { xs: 'h-6 px-2 text-xs', sm: 'h-7 px-2.5 text-[0.8125rem]', md: 'h-8.5 px-3.5 text-sm' }[size]
	);
	const cls = $derived(
		`${base} ${variants[variant]} ${sizes} ${full ? 'w-full' : ''} ${active ? 'bg-accent-soft! text-accent! border-accent/40!' : ''} ${klass}`
	);
</script>

{#snippet content()}
	{#if loading}
		<Spinner size={iconSize} />
	{:else if icon}
		<Icon name={icon} size={iconSize} />
	{/if}
	{@render children?.()}
	{#if iconRight}
		<Icon name={iconRight} size={iconSize} />
	{/if}
{/snippet}

{#if href && !disabled}
	<a {href} class={cls} aria-label={label} title={label}>
		{@render content()}
	</a>
{:else}
	<button
		{type}
		class={cls}
		disabled={disabled || loading}
		aria-label={label}
		title={label}
		aria-busy={loading || undefined}
		aria-pressed={active || undefined}
		{...rest}
	>
		{@render content()}
	</button>
{/if}
