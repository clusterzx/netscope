<!--
	The heuristic-matching disclaimer (text from the API).
	<Disclaimer text={data.disclaimer} />   compact: collapsed to two lines with "Mehr"
-->
<script lang="ts">
	import { Alert } from '$lib/components/ui';

	interface Props {
		text: string | null | undefined;
		compact?: boolean;
		class?: string;
	}

	let { text, compact = false, class: klass = '' }: Props = $props();
	let expanded = $state(false);
	const uid = $props.id();
</script>

{#if text}
	<Alert tone="warn" title="Heuristischer Abgleich – Treffer vor Maßnahmen prüfen" class={klass}>
		<p id="{uid}-txt" class={compact && !expanded ? 'line-clamp-2' : ''}>{text}</p>
		{#if compact}
			<button
				type="button"
				class="mt-1 text-xs font-medium text-fg hover:underline"
				aria-expanded={expanded}
				aria-controls="{uid}-txt"
				onclick={() => (expanded = !expanded)}>{expanded ? 'Weniger' : 'Mehr anzeigen'}</button
			>
		{/if}
	</Alert>
{/if}
