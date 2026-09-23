<!--
	Surface container with optional header.
	<Card title="Ports" description="…" icon="network">
		{#snippet actions()}<Button size="sm">…</Button>{/snippet}
		…content…
	</Card>
	padding: none | sm | md (default). `href` makes the whole card a link.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';
	import type { IconName } from './icons';

	interface Props {
		title?: string;
		description?: string;
		icon?: IconName;
		padding?: 'none' | 'sm' | 'md';
		class?: string;
		bodyClass?: string;
		/** heading level of the title */
		level?: 2 | 3;
		actions?: Snippet;
		header?: Snippet;
		footer?: Snippet;
		children?: Snippet;
	}

	let {
		title,
		description,
		icon,
		padding = 'md',
		class: klass = '',
		bodyClass = '',
		level = 2,
		actions,
		header,
		footer,
		children
	}: Props = $props();

	const pad = $derived({ none: '', sm: 'p-3', md: 'p-4' }[padding]);
</script>

<section class="flex min-w-0 flex-col rounded-lg border border-border bg-surface shadow-sm {klass}">
	{#if title || header || actions}
		<header class="flex min-h-11 items-center gap-3 border-b border-border px-4 py-2">
			{#if header}
				{@render header()}
			{:else}
				<div class="flex min-w-0 flex-1 items-center gap-2">
					{#if icon}<Icon name={icon} size={16} class="text-fg-subtle" />{/if}
					<div class="min-w-0">
						<svelte:element this={level === 2 ? 'h2' : 'h3'} class="truncate text-sm font-semibold text-fg">
							{title}
						</svelte:element>
						{#if description}<p class="truncate text-xs text-fg-subtle">{description}</p>{/if}
					</div>
				</div>
			{/if}
			{#if actions}<div class="flex shrink-0 items-center gap-1.5">{@render actions()}</div>{/if}
		</header>
	{/if}
	{#if children}
		<div class="min-w-0 flex-1 {pad} {bodyClass}">{@render children()}</div>
	{/if}
	{#if footer}
		<footer class="border-t border-border px-4 py-2.5">{@render footer()}</footer>
	{/if}
</section>
