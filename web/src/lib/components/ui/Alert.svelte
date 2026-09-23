<!--
	Inline message box.
	<Alert tone="warn" title="Heuristisch">Versionsabgleich …</Alert>
	tones: info | ok | warn | danger
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	interface Props {
		tone?: 'info' | 'ok' | 'warn' | 'danger';
		title?: string;
		class?: string;
		children?: Snippet;
		actions?: Snippet;
	}

	let { tone = 'info', title, class: klass = '', children, actions }: Props = $props();

	const styles = {
		info: 'border-accent/30 bg-accent-soft text-fg',
		ok: 'border-ok/30 bg-ok-soft text-fg',
		warn: 'border-warn/35 bg-warn-soft text-fg',
		danger: 'border-danger/35 bg-danger-soft text-fg'
	} as const;
	const iconColor = { info: 'text-accent', ok: 'text-ok', warn: 'text-warn', danger: 'text-danger' } as const;
	const iconName = { info: 'info', ok: 'check-circle', warn: 'alert', danger: 'x-circle' } as const;
</script>

<div
	class="flex gap-2.5 rounded-lg border px-3.5 py-2.5 text-sm {styles[tone]} {klass}"
	role={tone === 'danger' ? 'alert' : 'status'}
>
	<Icon name={iconName[tone]} size={17} class="mt-px {iconColor[tone]}" />
	<div class="min-w-0 flex-1">
		{#if title}<p class="font-medium">{title}</p>{/if}
		{#if children}<div class="text-fg-muted {title ? 'mt-0.5' : ''}">{@render children()}</div>{/if}
	</div>
	{#if actions}<div class="flex shrink-0 items-start gap-2">{@render actions()}</div>{/if}
</div>
