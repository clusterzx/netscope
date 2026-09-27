<!--
	Active (not ignored) device×CVE matches by severity; clicking a row sets the CVSS filter.
	<SeveritySummary summary={status.summary} activeMin={min} onpick={(min) => …} />
-->
<script lang="ts">
	import { Card, Icon, Skeleton } from '$lib/components/ui';
	import { formatNumber } from '$lib/utils/format';
	import { cveSeverityLabel, severityMin } from './cve';

	interface Props {
		summary: Record<string, number> | null | undefined;
		activeMin?: string;
		onpick: (min: string | null) => void;
		/** exploited (CISA KEV) row: filter toggle */
		exploitedActive?: boolean;
		onexploited?: () => void;
		class?: string;
	}

	let {
		summary,
		activeMin = '',
		onpick,
		exploitedActive = false,
		onexploited,
		class: klass = ''
	}: Props = $props();

	const order = ['critical', 'high', 'medium', 'low'] as const;
	const bar: Record<string, string> = {
		critical: 'bg-sev-critical',
		high: 'bg-sev-high',
		medium: 'bg-sev-medium',
		low: 'bg-sev-low'
	};
	const max = $derived(summary ? Math.max(1, ...order.map((s) => summary[s] ?? 0)) : 1);
</script>

<Card
	title="Übersicht"
	description="Aktive Treffer (Gerät × CVE), ohne irrelevante"
	icon="shield"
	class={klass}
>
	{#if !summary}
		<Skeleton lines={5} />
	{:else}
		<div class="flex items-baseline gap-2">
			<span class="text-3xl font-semibold tracking-tight tabular">{formatNumber(summary.total ?? 0)}</span>
			<span class="text-sm text-fg-muted">
				Treffer auf {formatNumber(summary.devices ?? 0)}
				{(summary.devices ?? 0) === 1 ? 'Gerät' : 'Geräten'}
			</span>
		</div>
		<ul class="mt-3 flex flex-col gap-1">
			{#each order as s (s)}
				{@const n = summary[s] ?? 0}
				{@const min = String(severityMin[s])}
				{@const active = activeMin === min}
				<li>
					<button
						type="button"
						aria-pressed={active}
						title="Liste auf {cveSeverityLabel[s]} filtern"
						onclick={() => onpick(active ? null : min)}
						class="grid w-full grid-cols-[5.5rem_1fr_3.5rem] items-center gap-3 rounded px-1.5 py-1 text-left text-sm transition-colors
							{active ? 'bg-accent-soft' : 'hover:bg-surface-2'}"
					>
						<span class={active ? 'font-medium text-fg' : 'text-fg-muted'}>{cveSeverityLabel[s]}</span>
						<span class="h-2 overflow-hidden rounded-full bg-surface-3">
							<span
								class="block h-full rounded-full {bar[s]}"
								style="width:{n ? Math.max(2, (n / max) * 100) : 0}%"
							></span>
						</span>
						<span class="text-right font-medium tabular">{formatNumber(n)}</span>
					</button>
				</li>
			{/each}
		</ul>
		{#if onexploited && (summary.exploited ?? 0) > 0}
			<button
				type="button"
				aria-pressed={exploitedActive}
				title="Nur Schwachstellen zeigen, die laut CISA aktiv ausgenutzt werden"
				onclick={onexploited}
				class="mt-3 flex w-full items-center gap-2 rounded-md border px-2.5 py-2 text-left text-sm transition-colors
					{exploitedActive ? 'border-danger bg-danger-soft' : 'border-danger/40 hover:bg-danger-soft'}"
			>
				<Icon name="zap" size={15} class="shrink-0 text-danger" />
				<span class="font-medium text-danger tabular">{formatNumber(summary.exploited)}</span>
				<span class="text-fg-muted">
					aktiv ausgenutzt auf {formatNumber(summary.exploitedDevices ?? 0)}
					{(summary.exploitedDevices ?? 0) === 1 ? 'Gerät' : 'Geräten'}
				</span>
			</button>
		{/if}
		{#if (summary.unknown ?? 0) + (summary.none ?? 0) > 0}
			<p class="mt-2 text-xs text-fg-subtle">
				{formatNumber((summary.unknown ?? 0) + (summary.none ?? 0))} ohne CVSS-Bewertung
			</p>
		{/if}
	{/if}
</Card>
