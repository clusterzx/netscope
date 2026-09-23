<!--
	"Ports & Dienste" tab: GET /devices/{id}/ports (?history=1 adds closed ports with their
	gone time) and the HTTP endpoints with detected web apps (GET /devices/{id}/http).
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { HTTPView, PortView } from '$lib/api/types';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import Table, { type Column } from '$lib/components/ui/Table.svelte';
	import Toggle from '$lib/components/ui/Toggle.svelte';
	import { formatDateTime } from '$lib/utils/format';
	import { httpStatusTone, LazyData, portStateLabel, sourceName } from './util';

	interface Props {
		deviceId: number;
		version: number;
		active: boolean;
	}

	let { deviceId, version, active }: Props = $props();

	let history = $state(false);
	let sort = $state('port');

	const ports = new LazyData<PortView[]>();
	const http = new LazyData<HTTPView[]>();

	$effect(() => {
		if (!active) return;
		const h = history;
		ports.ensure(
			`${version}|${h}`,
			async (signal) =>
				((await api.get('/api/v1/devices/{id}/ports', {
					path: { id: deviceId },
					query: { history: h || undefined },
					signal
				})) ?? []) as PortView[]
		);
		http.ensure(
			String(version),
			async (signal) =>
				((await api.get('/api/v1/devices/{id}/http', { path: { id: deviceId }, signal })) ?? []) as HTTPView[]
		);
	});
	$effect(() => () => {
		ports.abort();
		http.abort();
	});

	const list = $derived(ports.data ?? []);
	const multiIp = $derived(new Set(list.map((p) => p.ip)).size > 1);

	const sorted = $derived.by(() => {
		const desc = sort.startsWith('-');
		const f = desc ? sort.slice(1) : sort;
		const val = (p: PortView): string | number => {
			switch (f) {
				case 'port':
					return p.port * 10 + (p.proto === 'udp' ? 1 : 0);
				case 'firstSeen':
					return new Date(p.firstSeen).getTime();
				default:
					return String((p as unknown as Record<string, unknown>)[f] ?? '').toLowerCase();
			}
		};
		return [...list].sort((a, b) => {
			const va = val(a);
			const vb = val(b);
			const c = va < vb ? -1 : va > vb ? 1 : 0;
			// open ports before gone ones, then the chosen order
			return Number(!!a.goneAt) - Number(!!b.goneAt) || (desc ? -c : c);
		});
	});

	function stateTone(p: PortView) {
		if (p.goneAt) return 'neutral' as const;
		if (p.state === 'open') return 'ok' as const;
		if (p.state.includes('filtered')) return 'warn' as const;
		return 'neutral' as const;
	}

	const columns = $derived<Column<PortView>[]>([
		{ key: 'port', label: 'Port', sortable: true, width: '6.5rem', cell: portCell },
		...(multiIp ? [{ key: 'ip', label: 'IP', sortable: true, cell: ipCell } as Column<PortView>] : []),
		{ key: 'state', label: 'Status', cell: stateCell },
		{ key: 'service', label: 'Dienst', sortable: true, cell: serviceCell },
		{ key: 'product', label: 'Produkt / Version', sortable: true, cell: productCell },
		{ key: 'cpes', label: 'CPE', hideBelow: 'lg', cell: cpeCell },
		{ key: 'source', label: 'Quelle', hideBelow: 'md', value: (p) => sourceName(p.source) },
		{
			key: 'firstSeen',
			label: history ? 'Erstsichtung / weg' : 'Erstsichtung',
			sortable: true,
			sortDesc: true,
			hideBelow: 'sm',
			cell: seenCell
		}
	]);

	const httpList = $derived(
		[...(http.data ?? [])].sort((a, b) => a.port - b.port || a.url.localeCompare(b.url))
	);
</script>

{#snippet portCell(p: PortView)}
	<span class="mono whitespace-nowrap {p.goneAt ? 'text-fg-subtle line-through' : 'font-medium'}"
		>{p.port}<span class="text-fg-subtle">/{p.proto}</span></span
	>
{/snippet}
{#snippet ipCell(p: PortView)}<span class="mono text-fg-muted">{p.ip}</span>{/snippet}
{#snippet stateCell(p: PortView)}
	{#if p.goneAt}
		<Badge tone="neutral">geschlossen</Badge>
	{:else}
		<Badge tone={stateTone(p)}>{portStateLabel[p.state] ?? p.state}</Badge>
	{/if}
{/snippet}
{#snippet serviceCell(p: PortView)}
	<span class="whitespace-nowrap">{p.service || '–'}</span>
	{#if p.tunnel}<Badge variant="outline" title="Tunnel">{p.tunnel}</Badge>{/if}
{/snippet}
{#snippet productCell(p: PortView)}
	{#if p.product || p.version}
		<span class="text-fg">{p.product}</span>
		{#if p.version}<span class="mono text-fg-muted">{p.version}</span>{/if}
	{:else}<span class="text-fg-subtle">–</span>{/if}
	{#if p.extraInfo}<span class="block text-xs text-fg-subtle">{p.extraInfo}</span>{/if}
{/snippet}
{#snippet cpeCell(p: PortView)}
	{#if p.cpes?.length}
		<span class="flex flex-col">
			{#each p.cpes as c (c)}<span class="mono text-xs break-all text-fg-muted">{c}</span>{/each}
		</span>
	{:else}<span class="text-fg-subtle">–</span>{/if}
{/snippet}
{#snippet seenCell(p: PortView)}
	<span class="whitespace-nowrap text-fg-muted" title="Zuletzt gesehen {formatDateTime(p.lastSeen)}"
		>{formatDateTime(p.firstSeen)}</span
	>
	{#if p.goneAt}<span class="block text-xs whitespace-nowrap text-danger"
			>weg seit {formatDateTime(p.goneAt)}</span
		>{/if}
{/snippet}

<div class="flex flex-col gap-4">
	<section aria-labelledby="ports-h" class="flex flex-col gap-2">
		<div class="flex flex-wrap items-center gap-3">
			<h2 id="ports-h" class="flex-1 text-sm font-semibold">Ports & Dienste</h2>
			<Toggle bind:checked={history} label="Geschlossene Ports anzeigen" size="sm" />
		</div>
		{#if ports.error && !ports.data}
			<ErrorState error={ports.error} onretry={() => ports.reload()} />
		{:else}
			<Table
				{columns}
				rows={sorted}
				key={(p) => p.id}
				{sort}
				onsort={(s) => (sort = s)}
				loading={ports.loading}
				dense
				caption="Ports und Dienste"
				rowClass={(p) => (p.goneAt ? 'opacity-70' : '')}
			>
				{#snippet empty()}
					<EmptyState
						compact
						icon="network"
						title={history ? 'Keine Ports bekannt' : 'Keine offenen Ports'}
						description="Offene Ports ermittelt der Nmap-Scanner (Scan jetzt → Nmap)."
					/>
				{/snippet}
			</Table>
		{/if}
	</section>

	<section aria-labelledby="web-h" class="flex flex-col gap-2">
		<h2 id="web-h" class="text-sm font-semibold">Web-Endpunkte</h2>
		{#if http.error && !http.data}
			<ErrorState error={http.error} onretry={() => http.reload()} />
		{:else if !http.data}
			<Skeleton rows={3} />
		{:else if !httpList.length}
			<div class="rounded-lg border border-border bg-surface">
				<EmptyState
					compact
					icon="globe"
					title="Keine HTTP-Endpunkte"
					description="Der HTTP-Scanner prüft alle offenen Web-Ports auf Titel, Server und bekannte Web-Apps."
				/>
			</div>
		{:else}
			<div class="grid grid-cols-1 gap-3 lg:grid-cols-2">
				{#each httpList as h (h.id)}
					<Card padding="sm">
						{#snippet header()}
							<div class="flex min-w-0 flex-1 items-center gap-2">
								<Badge tone={httpStatusTone(h.statusCode)} title="HTTP-Status">{h.statusCode || '–'}</Badge>
								<a
									href={h.url}
									target="_blank"
									rel="noopener noreferrer"
									class="link mono min-w-0 truncate text-sm"
									title="{h.url} (öffnet in neuem Tab)"
								>
									{h.url}
								</a>
								<Icon name="external" size={13} class="shrink-0 text-fg-subtle" />
							</div>
						{/snippet}
						<div class="flex flex-col gap-2 text-sm">
							{#if h.title}<p class="font-medium break-words">{h.title}</p>{/if}
							{#if h.apps?.length}
								<div class="flex flex-wrap gap-1.5" aria-label="Erkannte Web-Apps">
									{#each h.apps as app (app.name)}
										<Badge
											tone="accent"
											title="Konfidenz: {app.confidence}{app.evidence
												? ` · Hinweis: ${app.evidence}`
												: ''}{app.cpe ? ` · ${app.cpe}` : ''}"
										>
											{app.name}{#if app.version}&nbsp;<span class="mono">{app.version}</span>{/if}
										</Badge>
									{/each}
								</div>
							{/if}
							<dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
								{#if h.server}
									<dt class="text-fg-subtle">Server</dt>
									<dd class="mono break-all">{h.server}</dd>
								{/if}
								{#if h.redirects?.length || (h.finalUrl && h.finalUrl !== h.url)}
									<dt class="text-fg-subtle">Weiterleitung</dt>
									<dd class="mono break-all">
										{[...(h.redirects ?? []), h.finalUrl].filter(Boolean).join(' → ')}
									</dd>
								{/if}
								{#if h.contentType}
									<dt class="text-fg-subtle">Content-Type</dt>
									<dd class="mono break-all">{h.contentType}</dd>
								{/if}
								{#if h.faviconHash}
									<dt class="text-fg-subtle">Favicon</dt>
									<dd class="mono break-all">
										mmh3 {h.faviconHash}{#if h.faviconMd5}<span class="text-fg-subtle">
												· md5 {h.faviconMd5}</span
											>{/if}
									</dd>
								{/if}
								<dt class="text-fg-subtle">Gesehen</dt>
								<dd>
									seit {formatDateTime(h.firstSeen)} · zuletzt <RelativeTime value={h.lastSeen} />
								</dd>
							</dl>
							{#if h.headers && Object.keys(h.headers).length}
								<details>
									<summary class="cursor-pointer text-xs text-fg-subtle hover:text-fg">
										Antwort-Header ({Object.keys(h.headers).length})
									</summary>
									<dl class="mt-1 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
										{#each Object.entries(h.headers) as [k, v] (k)}
											<dt class="mono text-fg-subtle">{k}</dt>
											<dd class="mono break-all">{v}</dd>
										{/each}
									</dl>
								</details>
							{/if}
						</div>
					</Card>
				{/each}
			</div>
		{/if}
	</section>
</div>
