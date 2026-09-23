<!--
	Small status label.
	<Badge tone="ok">Aktiv</Badge>   tones: neutral | accent | ok | warn | danger | unknown | info | low | medium | high | critical
	<Badge tone="warn" dot>Läuft</Badge>   <Badge variant="outline">tag</Badge>
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { Tone } from '$lib/utils/labels';

	interface Props {
		tone?: Tone;
		variant?: 'soft' | 'solid' | 'outline';
		dot?: boolean;
		size?: 'sm' | 'md';
		title?: string;
		class?: string;
		children: Snippet;
	}

	let {
		tone = 'neutral',
		variant = 'soft',
		dot = false,
		size = 'sm',
		title,
		class: klass = '',
		children
	}: Props = $props();

	const soft: Record<Tone, string> = {
		neutral: 'bg-surface-3 text-fg-muted',
		accent: 'bg-accent-soft text-accent',
		ok: 'bg-ok-soft text-ok',
		warn: 'bg-warn-soft text-warn',
		danger: 'bg-danger-soft text-danger',
		unknown: 'bg-unknown-soft text-unknown',
		info: 'bg-sev-info-soft text-sev-info',
		low: 'bg-sev-low-soft text-sev-low',
		medium: 'bg-sev-medium-soft text-sev-medium',
		high: 'bg-sev-high-soft text-sev-high',
		critical: 'bg-sev-critical-soft text-sev-critical'
	};
	const solid: Record<Tone, string> = {
		neutral: 'bg-fg-muted text-surface',
		accent: 'bg-accent text-accent-fg',
		ok: 'bg-ok text-white',
		warn: 'bg-warn text-white',
		danger: 'bg-danger text-white',
		unknown: 'bg-unknown text-white',
		info: 'bg-sev-info text-white',
		low: 'bg-sev-low text-white',
		medium: 'bg-sev-medium text-white',
		high: 'bg-sev-high text-white',
		critical: 'bg-sev-critical text-white'
	};
	const dotColor: Record<Tone, string> = {
		neutral: 'bg-fg-subtle',
		accent: 'bg-accent',
		ok: 'bg-ok',
		warn: 'bg-warn',
		danger: 'bg-danger',
		unknown: 'bg-unknown',
		info: 'bg-sev-info',
		low: 'bg-sev-low',
		medium: 'bg-sev-medium',
		high: 'bg-sev-high',
		critical: 'bg-sev-critical'
	};

	const cls = $derived(
		variant === 'solid'
			? solid[tone]
			: variant === 'outline'
				? 'border border-border text-fg-muted bg-transparent'
				: soft[tone]
	);
</script>

<span
	{title}
	class="inline-flex max-w-full items-center gap-1 rounded font-medium whitespace-nowrap
		{size === 'sm' ? 'h-5 px-1.5 text-[0.72rem]' : 'h-6 px-2 text-xs'} {cls} {klass}"
>
	{#if dot}<span class="h-1.5 w-1.5 shrink-0 rounded-full {dotColor[tone]}"></span>{/if}
	<span class="truncate">{@render children()}</span>
</span>
