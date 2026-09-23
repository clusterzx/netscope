<!--
	Placeholder for empty lists.
	<EmptyState icon="devices" title="Keine Geräte" description="…">
		{#snippet actions()}<Button …>Gerät anlegen</Button>{/snippet}
	</EmptyState>
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';
	import type { IconName } from './icons';

	interface Props {
		title: string;
		description?: string;
		icon?: IconName;
		compact?: boolean;
		class?: string;
		actions?: Snippet;
	}

	let { title, description, icon = 'info', compact = false, class: klass = '', actions }: Props = $props();
</script>

<div
	class="flex flex-col items-center justify-center text-center {compact
		? 'gap-1.5 py-6'
		: 'gap-2 py-12'} {klass}"
>
	<div
		class="flex items-center justify-center rounded-full bg-surface-3 text-fg-subtle {compact
			? 'h-9 w-9'
			: 'h-12 w-12'}"
	>
		<Icon name={icon} size={compact ? 18 : 22} />
	</div>
	<p class="font-medium text-fg {compact ? 'text-sm' : ''}">{title}</p>
	{#if description}<p class="max-w-md text-sm text-fg-muted">{description}</p>{/if}
	{#if actions}<div class="mt-2 flex flex-wrap justify-center gap-2">{@render actions()}</div>{/if}
</div>
