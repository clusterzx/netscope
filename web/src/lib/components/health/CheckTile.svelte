<!--
	Status board tile of one health check.
	<CheckTile check={c} points={latency[c.id]} onopen={(c) => …} />
-->
<script lang="ts">
	import type { HealthCheck, SeriesPoint } from '$lib/api';
	import { Badge, RelativeTime, Sparkline, StatusDot } from '$lib/components/ui';
	import { formatMs, formatPercent } from '$lib/utils/format';
	import { healthStateLabel, healthTone } from '$lib/utils/labels';
	import {
		availabilityText,
		availabilityTone,
		checkTypeLabel,
		sectionOf,
		stateEdge,
		targetText
	} from './health';

	interface Props {
		check: HealthCheck;
		points?: SeriesPoint[];
		onopen: (c: HealthCheck) => void;
	}

	let { check: c, points, onopen }: Props = $props();

	const section = $derived(sectionOf(c));
	const windows = [
		['24h', '24 h'],
		['7d', '7 T'],
		['30d', '30 T']
	] as const;
</script>

<article
	class="flex min-w-0 flex-col gap-2.5 rounded-lg border border-l-4 border-border bg-surface p-3.5 shadow-sm transition-colors hover:border-border-strong
		{stateEdge[section]} {c.enabled ? '' : 'opacity-70'}"
>
	<header class="flex items-start gap-2">
		<StatusDot status={c.enabled ? c.state : 'idle'} class="mt-1.5" />
		<div class="min-w-0 flex-1">
			<h3 class="truncate text-sm font-semibold">
				<button
					type="button"
					class="max-w-full truncate text-left text-fg hover:text-accent hover:underline focus-visible:outline-2 focus-visible:outline-focus"
					onclick={() => onopen(c)}
					aria-label="{c.name} – Details">{c.name}</button
				>
			</h3>
			<p class="truncate text-xs text-fg-subtle">
				{#if c.deviceId}
					<a href="/devices/{c.deviceId}" class="link">{c.deviceName || `Gerät #${c.deviceId}`}</a> ·
				{/if}
				<span class="mono">{targetText(c)}</span>
			</p>
		</div>
		<div class="flex shrink-0 flex-col items-end gap-1">
			<Badge tone={c.enabled ? healthTone(c.state) : 'neutral'}>
				{c.enabled ? (healthStateLabel[c.state] ?? c.state) : 'Deaktiviert'}
			</Badge>
			<span class="text-[0.7rem] font-medium tracking-wide text-fg-subtle"
				>{checkTypeLabel[c.type] ?? c.type}</span
			>
		</div>
	</header>

	<div class="flex items-center gap-3">
		<div class="min-w-0">
			<div class="text-lg leading-tight font-semibold tabular text-fg">
				{c.lastLatencyMs !== undefined && c.lastLatencyMs !== null && c.lastOk !== false
					? formatMs(c.lastLatencyMs)
					: '–'}
			</div>
			<div class="text-[0.7rem] text-fg-subtle">
				{#if c.lastCheckAt}geprüft <RelativeTime value={c.lastCheckAt} />{:else}noch nicht geprüft{/if}
			</div>
		</div>
		<div class="ml-auto">
			<Sparkline
				points={points ?? []}
				label="Latenz von {c.name}, letzte 24 Stunden"
				width={110}
				height={30}
			/>
		</div>
	</div>

	<dl class="grid grid-cols-3 gap-1 rounded-md bg-surface-2 px-2 py-1.5 text-center">
		{#each windows as [k, lbl] (k)}
			{@const v = c.availability?.[k]}
			<div>
				<dt class="text-[0.68rem] text-fg-subtle">{lbl}</dt>
				<dd class="text-[0.8125rem] font-semibold tabular {availabilityText[availabilityTone(v)]}">
					{v === undefined ? '–' : formatPercent(v, v >= 99.95 || v === 0 ? 0 : 2)}
				</dd>
			</div>
		{/each}
	</dl>

	{#if c.enabled && (c.state === 'down' || c.state === 'degraded') && c.lastError}
		<p class="line-clamp-2 text-xs {c.state === 'down' ? 'text-danger' : 'text-warn'}" title={c.lastError}>
			{c.lastError}
		</p>
	{:else if c.stateSince && c.enabled}
		<p class="text-xs text-fg-subtle">
			{healthStateLabel[c.state] ?? c.state} seit <RelativeTime value={c.stateSince} absolute />
		</p>
	{/if}
</article>
