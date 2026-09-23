<!--
	Severity label with colour (info | low | medium | high | critical).
	<SeverityBadge severity={ev.severity} />   <SeverityBadge severity="high" variant="solid" />
	<SeverityBadge cvss={9.8} />   (derives the severity and shows the score)
-->
<script lang="ts">
	import Badge from './Badge.svelte';
	import { severityFromCvss, severityLabel, severityTone } from '$lib/utils/labels';
	import { formatNumber } from '$lib/utils/format';

	interface Props {
		severity?: string | null;
		cvss?: number | null;
		variant?: 'soft' | 'solid';
		size?: 'sm' | 'md';
		class?: string;
	}

	let { severity, cvss, variant = 'soft', size = 'sm', class: klass = '' }: Props = $props();

	const sev = $derived(severity ?? (cvss !== undefined && cvss !== null ? severityFromCvss(cvss) : 'info'));
</script>

<Badge tone={severityTone(sev)} {variant} {size} class={klass} title={severityLabel[sev] ?? sev}>
	{#if cvss !== undefined && cvss !== null}
		<span class="tabular">{formatNumber(cvss, 1, 1)}</span>
	{:else}
		{severityLabel[sev] ?? sev}
	{/if}
</Badge>
