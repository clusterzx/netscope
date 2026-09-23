<!--
	CVSS score with a readable breakdown of the vector.
	<CvssCard cvss={info.cvss} vector={info.vector} version={info.cvssVersion} severity={info.severity} />
-->
<script lang="ts">
	import { Badge, Card, CopyButton } from '$lib/components/ui';
	import { formatNumber } from '$lib/utils/format';
	import { severityFromCvss, severityTone } from '$lib/utils/labels';
	import { cveSeverityLabel, parseVector } from './cve';

	interface Props {
		cvss?: number | null;
		vector?: string;
		version?: string;
		severity?: string;
		class?: string;
	}

	let { cvss, vector = '', version = '', severity = '', class: klass = '' }: Props = $props();

	const sev = $derived(
		severity && severity !== 'unknown' ? severity : cvss != null ? severityFromCvss(cvss) : 'unknown'
	);
	const parsed = $derived(parseVector(vector, version));
	const scoreColor: Record<string, string> = {
		critical: 'text-sev-critical',
		high: 'text-sev-high',
		medium: 'text-sev-medium',
		low: 'text-sev-low',
		info: 'text-sev-info',
		none: 'text-fg-subtle',
		unknown: 'text-fg-subtle'
	};
	const toneText: Record<string, string> = {
		danger: 'text-danger',
		warn: 'text-warn',
		ok: 'text-ok',
		neutral: 'text-fg-muted'
	};
</script>

<Card title="CVSS" icon="shield" class={klass}>
	{#snippet actions()}
		{#if parsed.version}<Badge>Version {parsed.version}</Badge>{/if}
	{/snippet}
	<div class="flex items-end gap-3">
		<span class="text-4xl leading-none font-semibold tracking-tight tabular {scoreColor[sev] ?? 'text-fg'}">
			{cvss != null ? formatNumber(cvss, 1, 1) : '–'}
		</span>
		<span class="pb-0.5">
			<Badge tone={severityTone(sev)} size="md" variant="solid">{cveSeverityLabel[sev] ?? sev}</Badge>
		</span>
	</div>
	{#if vector}
		<div class="mt-3 flex items-center gap-1 rounded-md bg-surface-2 py-1 pr-1 pl-2.5">
			<code class="mono min-w-0 flex-1 truncate text-xs text-fg-muted" title={vector}>{vector}</code>
			<CopyButton text={vector} label="Vektor kopieren" />
		</div>
		{#if parsed.metrics.length}
			<dl
				class="mt-3 grid grid-cols-1 gap-x-4 gap-y-1.5 text-sm sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2"
			>
				{#each parsed.metrics as m (m.key)}
					<div class="flex items-baseline justify-between gap-2 border-b border-border/60 pb-1">
						<dt class="text-fg-muted">{m.name}</dt>
						<dd
							class="font-medium whitespace-nowrap {toneText[m.tone] ?? 'text-fg'}"
							title="{m.key}:{m.value}"
						>
							{m.text}
						</dd>
					</div>
				{/each}
			</dl>
		{/if}
	{:else}
		<p class="mt-3 text-sm text-fg-subtle">Kein CVSS-Vektor vorhanden.</p>
	{/if}
</Card>
