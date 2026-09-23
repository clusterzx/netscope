<!--
	Stat tile: label + big value (+ optional detail). `href` makes it a link.
	<StatCard label="Online" value={12} tone="ok" icon="wifi" href="/devices?q=is:online">von 40</StatCard>
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';
	import type { IconName } from './icons';
	import { formatNumber } from '$lib/utils/format';

	interface Props {
		label: string;
		value: number | string | null | undefined;
		icon?: IconName;
		tone?: 'neutral' | 'accent' | 'ok' | 'warn' | 'danger' | 'unknown';
		href?: string;
		loading?: boolean;
		class?: string;
		children?: Snippet;
	}

	let {
		label,
		value,
		icon,
		tone = 'neutral',
		href,
		loading = false,
		class: klass = '',
		children
	}: Props = $props();

	const toneCls = {
		neutral: 'text-fg-subtle bg-surface-3',
		accent: 'text-accent bg-accent-soft',
		ok: 'text-ok bg-ok-soft',
		warn: 'text-warn bg-warn-soft',
		danger: 'text-danger bg-danger-soft',
		unknown: 'text-unknown bg-unknown-soft'
	};
	const display = $derived(typeof value === 'number' ? formatNumber(value) : (value ?? '–'));
</script>

<svelte:element
	this={href ? 'a' : 'div'}
	{href}
	class="group flex min-w-0 flex-col gap-1 rounded-lg border border-border bg-surface p-4 shadow-sm
		{href ? 'transition-colors hover:border-border-strong hover:bg-surface-2' : ''} {klass}"
>
	<div class="flex items-center justify-between gap-2">
		<span class="truncate text-[0.8125rem] font-medium text-fg-muted">{label}</span>
		{#if icon}
			<span class="flex h-7 w-7 items-center justify-center rounded-md {toneCls[tone]}"
				><Icon name={icon} size={15} /></span
			>
		{/if}
	</div>
	<div class="text-2xl font-semibold tracking-tight text-fg">
		{#if loading}<span class="ns-skeleton inline-block h-7 w-12 align-middle"></span>{:else}{display}{/if}
	</div>
	{#if children}<div class="text-xs text-fg-subtle">{@render children()}</div>{/if}
</svelte:element>
