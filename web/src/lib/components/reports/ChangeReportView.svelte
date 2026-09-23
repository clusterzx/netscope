<!--
	Rendered change report (GET /api/v1/reports/changes, JSON).
	<ChangeReportView report={data} pluginNames={{ nmap: 'Nmap' }} />
-->
<script lang="ts">
	import type { ChangeReport } from '$lib/api';
	import { Badge, EmptyState, SeverityBadge } from '$lib/components/ui';
	import { eventTypeLabel, meta } from '$lib/stores/catalog.svelte';
	import { formatDateTime, formatNumber, formatSeconds } from '$lib/utils/format';
	import { eventCategoryLabel, healthStateLabel, healthTone, severityLabel } from '$lib/utils/labels';

	interface Props {
		report: ChangeReport;
		pluginNames?: Record<string, string>;
	}

	let { report: r, pluginNames = {} }: Props = $props();

	const LIMIT = 10;
	let allDevices = $state(false);
	let allEvents = $state(false);
	let allCves = $state(false);

	const eventRows = $derived(
		Object.entries(r.eventCounts ?? {})
			.map(([type, n]) => ({
				type,
				n,
				label: eventTypeLabel(type),
				category: meta.value?.eventTypes.find((e) => e.type === type)?.category ?? type.split('.')[0]
			}))
			.sort((a, b) => b.n - a.n)
	);
	const eventTotal = $derived(eventRows.reduce((a, e) => a + e.n, 0));
	const eventMax = $derived(Math.max(1, ...eventRows.map((e) => e.n)));

	const sevOrder = ['critical', 'high', 'medium', 'low'] as const;
	const cveTotal = $derived(sevOrder.reduce((a, s) => a + (r.cveBySeverity?.[s] ?? 0), 0));
	const cveMax = $derived(Math.max(1, ...sevOrder.map((s) => r.cveBySeverity?.[s] ?? 0)));
	const failed = $derived(Object.entries(r.failedRuns ?? {}).sort((a, b) => b[1] - a[1]));

	const newDevices = $derived(r.newDevices ?? []);
	const important = $derived(r.important ?? []);
	const newCves = $derived(r.newCves ?? []);
	const certs = $derived(r.expiringCerts ?? []);
	const outages = $derived(r.outages ?? []);

	const sevBar: Record<string, string> = {
		critical: 'bg-sev-critical',
		high: 'bg-sev-high',
		medium: 'bg-sev-medium',
		low: 'bg-sev-low'
	};

	const kpis = $derived([
		{
			label: 'Geräte',
			value: r.devices?.total ?? 0,
			detail: `${formatNumber(r.devices?.online ?? 0)} online`,
			href: '/devices'
		},
		{ label: 'Neu im Zeitraum', value: r.devices?.new ?? 0, detail: 'erstmals gesehen', href: '' },
		{
			label: 'Unbekannt',
			value: r.devices?.unknown ?? 0,
			detail: 'nicht als bekannt markiert',
			href: '/devices?q=state:unknown'
		},
		{ label: 'Events', value: eventTotal, detail: `${eventRows.length} Typen`, href: '' },
		{
			label: 'Offen kritisch / hoch',
			value: `${formatNumber(r.openCritical)} / ${formatNumber(r.openHigh)}`,
			detail: 'nicht quittiert (aktuell)',
			href: '/events?acked=0'
		}
	]);
</script>

{#snippet sectionTitle(title: string, count?: number)}
	<h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-fg">
		{title}
		{#if count !== undefined}
			<span class="rounded bg-surface-3 px-1.5 text-xs font-medium text-fg-subtle tabular"
				>{formatNumber(count)}</span
			>
		{/if}
	</h3>
{/snippet}

{#snippet more(total: number, open: boolean, toggle: () => void)}
	{#if total > LIMIT}
		<button type="button" class="mt-1.5 text-xs font-medium text-accent hover:underline" onclick={toggle}>
			{open ? 'Weniger anzeigen' : `Alle ${formatNumber(total)} anzeigen`}
		</button>
	{/if}
{/snippet}

<div class="flex flex-col gap-6">
	<!-- KPIs -->
	<dl class="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-5">
		{#each kpis as k (k.label)}
			<div class="rounded-lg border border-border bg-surface-2 px-3.5 py-3">
				<dt class="text-xs font-medium text-fg-muted">{k.label}</dt>
				<dd class="mt-0.5 text-xl font-semibold tracking-tight tabular">
					{#if k.href}<a href={k.href} class="hover:text-accent"
							>{typeof k.value === 'number' ? formatNumber(k.value) : k.value}</a
						>{:else}{typeof k.value === 'number' ? formatNumber(k.value) : k.value}{/if}
				</dd>
				<dd class="text-xs text-fg-subtle">{k.detail}</dd>
			</div>
		{/each}
	</dl>

	<div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
		<!-- new devices -->
		<section>
			{@render sectionTitle('Neue Geräte', newDevices.length)}
			{#if newDevices.length === 0}
				<p class="text-sm text-fg-subtle">Keine neuen Geräte im Zeitraum.</p>
			{:else}
				<ul class="divide-y divide-border rounded-md border border-border">
					{#each allDevices ? newDevices : newDevices.slice(0, LIMIT) as d (d.id)}
						<li class="flex items-center gap-3 px-3 py-1.5 text-sm">
							<span class="min-w-0 flex-1">
								<a href="/devices/{d.id}" class="link block truncate">{d.name}</a>
								{#if d.vendor}<span class="block truncate text-xs text-fg-subtle">{d.vendor}</span>{/if}
							</span>
							<span class="mono shrink-0 text-xs text-fg-muted">{d.ip}</span>
							<span class="hidden w-28 shrink-0 text-right text-xs text-fg-subtle sm:block"
								>{formatDateTime(d.at)}</span
							>
						</li>
					{/each}
				</ul>
				{@render more(newDevices.length, allDevices, () => (allDevices = !allDevices))}
			{/if}
		</section>

		<!-- events by type -->
		<section>
			{@render sectionTitle('Events nach Typ', eventTotal)}
			{#if eventRows.length === 0}
				<p class="text-sm text-fg-subtle">Keine Events im Zeitraum.</p>
			{:else}
				<ul class="flex flex-col gap-1">
					{#each allEvents ? eventRows : eventRows.slice(0, LIMIT) as e (e.type)}
						<li
							class="grid grid-cols-[minmax(0,1fr)_6rem_3rem] items-center gap-3 text-sm sm:grid-cols-[minmax(0,1fr)_10rem_3rem]"
						>
							<span class="min-w-0 truncate" title={e.type}>
								{e.label}
								<span class="text-xs text-fg-subtle">· {eventCategoryLabel[e.category] ?? e.category}</span>
							</span>
							<span class="h-2 overflow-hidden rounded-full bg-surface-3">
								<span
									class="block h-full rounded-full bg-chart-1"
									style="width:{Math.max(2, (e.n / eventMax) * 100)}%"
								></span>
							</span>
							<span class="text-right font-medium tabular">{formatNumber(e.n)}</span>
						</li>
					{/each}
				</ul>
				{@render more(eventRows.length, allEvents, () => (allEvents = !allEvents))}
			{/if}
		</section>
	</div>

	<!-- important events -->
	<section>
		{@render sectionTitle('Wichtige Events (hoch & kritisch)', important.length)}
		{#if important.length === 0}
			<p class="text-sm text-fg-subtle">Keine hohen oder kritischen Events im Zeitraum.</p>
		{:else}
			<ul class="divide-y divide-border rounded-md border border-border">
				{#each important as e (e.id)}
					<li class="flex flex-col gap-1 px-3 py-2 text-sm sm:flex-row sm:items-center sm:gap-3">
						<span class="flex min-w-0 flex-1 items-center gap-2">
							<SeverityBadge severity={e.severity} />
							<a href="/events?id={e.id}" class="link truncate">{e.title}</a>
						</span>
						<span class="flex shrink-0 items-center gap-2 text-xs text-fg-subtle">
							{#if e.device}<span class="truncate">{e.device}</span> ·{/if}
							<span>{formatDateTime(e.ts)}</span>
							<Badge tone={e.acked ? 'ok' : 'warn'}>{e.acked ? 'quittiert' : 'offen'}</Badge>
						</span>
					</li>
				{/each}
			</ul>
		{/if}
	</section>

	<div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
		<!-- CVEs -->
		<section>
			{@render sectionTitle('Schwachstellen (aktuell offen)', cveTotal)}
			<ul class="flex flex-col gap-1">
				{#each sevOrder as s (s)}
					{@const n = r.cveBySeverity?.[s] ?? 0}
					<li class="grid grid-cols-[5.5rem_1fr_3.5rem] items-center gap-3 text-sm">
						<span class="text-fg-muted">{severityLabel[s]}</span>
						<span class="h-2 overflow-hidden rounded-full bg-surface-3">
							<span
								class="block h-full rounded-full {sevBar[s]}"
								style="width:{n ? Math.max(2, (n / cveMax) * 100) : 0}%"
							></span>
						</span>
						<span class="text-right font-medium tabular">{formatNumber(n)}</span>
					</li>
				{/each}
			</ul>
			<h4 class="mt-4 mb-1.5 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
				Neu gefunden im Zeitraum ({formatNumber(newCves.length)}{newCves.length >= 50 ? '+' : ''})
			</h4>
			{#if newCves.length === 0}
				<p class="text-sm text-fg-subtle">Keine neuen CVEs.</p>
			{:else}
				<ul class="divide-y divide-border rounded-md border border-border">
					{#each allCves ? newCves : newCves.slice(0, LIMIT) as c, i (c.cve + c.device + i)}
						<li class="flex items-center gap-2.5 px-3 py-1.5 text-sm">
							<SeverityBadge cvss={c.cvss} />
							<a href="/vulnerabilities/{c.cve}" class="link mono shrink-0">{c.cve}</a>
							<span class="min-w-0 flex-1 truncate text-xs text-fg-muted" title="{c.device} – {c.detail}"
								>{c.device}{#if c.detail}{' '}<span class="text-fg-subtle">· {c.detail}</span>{/if}</span
							>
						</li>
					{/each}
				</ul>
				{@render more(newCves.length, allCves, () => (allCves = !allCves))}
			{/if}
		</section>

		<div class="flex flex-col gap-6">
			<!-- certificates -->
			<section>
				{@render sectionTitle('Ablaufende Zertifikate (≤ 30 Tage nach Zeitraumende)', certs.length)}
				{#if certs.length === 0}
					<p class="text-sm text-fg-subtle">Keine ablaufenden Zertifikate.</p>
				{:else}
					<ul class="divide-y divide-border rounded-md border border-border">
						{#each certs as c, i (c.endpoint + i)}
							<li class="flex items-center gap-3 px-3 py-1.5 text-sm">
								<Badge
									tone={c.daysLeft < 0 ? 'critical' : c.daysLeft <= 7 ? 'high' : 'medium'}
									class="w-24 justify-center"
								>
									{c.daysLeft < 0 ? 'abgelaufen' : `${c.daysLeft} Tage`}
								</Badge>
								<span class="min-w-0 flex-1">
									<span class="block truncate">{c.subject || c.endpoint}</span>
									<span class="block truncate text-xs text-fg-subtle"
										>{c.device} · <span class="mono">{c.endpoint}</span> · {formatDateTime(c.notAfter)}</span
									>
								</span>
							</li>
						{/each}
					</ul>
				{/if}
			</section>

			<!-- outages -->
			<section>
				{@render sectionTitle('Ausfälle', outages.length)}
				{#if outages.length === 0}
					<p class="text-sm text-fg-subtle">Keine Ausfälle im Zeitraum.</p>
				{:else}
					<ul class="divide-y divide-border rounded-md border border-border">
						{#each outages as o, i (o.check + o.started + i)}
							<li class="flex items-center gap-3 px-3 py-1.5 text-sm">
								<Badge tone={healthTone(o.state)} class="w-24 justify-center"
									>{healthStateLabel[o.state] ?? o.state}</Badge
								>
								<span class="min-w-0 flex-1 truncate">{o.check}</span>
								<span class="shrink-0 text-right text-xs text-fg-subtle">
									{formatDateTime(o.started)} ·
									<span class="font-medium text-fg-muted tabular">{formatSeconds(o.seconds)}</span>
									{#if o.ongoing}<Badge tone="danger" dot class="ml-1">andauernd</Badge>{/if}
								</span>
							</li>
						{/each}
					</ul>
				{/if}
			</section>

			<!-- failed runs -->
			<section>
				{@render sectionTitle(
					'Fehlgeschlagene Läufe',
					failed.reduce((a, [, n]) => a + n, 0)
				)}
				{#if failed.length === 0}
					<p class="text-sm text-fg-subtle">Alle Plugin-Läufe waren erfolgreich.</p>
				{:else}
					<ul class="flex flex-wrap gap-2">
						{#each failed as [id, n] (id)}
							<li>
								<a
									href="/plugins/{id}"
									class="inline-flex items-center gap-1.5 rounded-md border border-border px-2.5 py-1 text-sm hover:bg-surface-2"
								>
									{pluginNames[id] ?? id}
									<Badge tone="danger">{formatNumber(n)}</Badge>
								</a>
							</li>
						{/each}
					</ul>
				{/if}
			</section>
		</div>
	</div>

	{#if eventTotal === 0 && newDevices.length === 0 && outages.length === 0 && newCves.length === 0}
		<EmptyState
			compact
			icon="check-circle"
			title="Ruhiger Zeitraum"
			description="Im gewählten Zeitraum gab es keine Änderungen."
		/>
	{/if}
</div>
