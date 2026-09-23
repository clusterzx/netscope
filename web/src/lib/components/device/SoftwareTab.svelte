<!--
	"Software" tab: structured inventory per source (GET /devices/{id}/inventory; SSH as
	dedicated view, everything else generic) and the package list with search and paging
	(GET /devices/{id}/packages).
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { PackageList, PackageView } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import JsonView from '$lib/components/ui/JsonView.svelte';
	import Pagination from '$lib/components/ui/Pagination.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import Table, { type Column } from '$lib/components/ui/Table.svelte';
	import { formatDateTime, formatNumber } from '$lib/utils/format';
	import { debounce } from '$lib/utils/url';
	import GenericData from './GenericData.svelte';
	import SshInventory from './SshInventory.svelte';
	import { LazyData, sourceName } from './util';

	interface Props {
		deviceId: number;
		version: number;
		active: boolean;
	}

	let { deviceId, version, active }: Props = $props();

	type InvEntry = { collectedAt?: string; data?: unknown };
	const inv = new LazyData<Record<string, InvEntry>>();
	const pkgs = new LazyData<PackageList>();

	let search = $state('');
	let q = $state('');
	let offset = $state(0);
	let limit = $state(100);
	const applySearch = debounce((v: string) => {
		q = v.trim();
		offset = 0;
	}, 300);

	$effect(() => {
		if (!active) return;
		inv.ensure(String(version), async (signal) => {
			const r = await api.get('/api/v1/devices/{id}/inventory', { path: { id: deviceId }, signal });
			return (r ?? {}) as Record<string, InvEntry>;
		});
		const query = { q: q || undefined, limit, offset };
		pkgs.ensure(`${version}|${q}|${limit}|${offset}`, (signal) =>
			api.get('/api/v1/devices/{id}/packages', { path: { id: deviceId }, query, signal })
		);
	});
	$effect(() => () => {
		inv.abort();
		pkgs.abort();
		applySearch.cancel();
	});

	const ORDER = ['ssh', 'snmp', 'proxmox', 'openwrt', 'upnp', 'docker', 'nmap', 'mdns', 'netbios', 'oui'];
	const sources = $derived(
		Object.entries(inv.data ?? {}).sort(([a], [b]) => {
			const ra = ORDER.indexOf(a) < 0 ? 99 : ORDER.indexOf(a);
			const rb = ORDER.indexOf(b) < 0 ? 99 : ORDER.indexOf(b);
			return ra - rb || a.localeCompare(b);
		})
	);
	let rawView = $state<Record<string, boolean>>({});
	/** open/closed per source (default: the first two sources are open) */
	let expanded = $state<Record<string, boolean>>({});

	const items = $derived(pkgs.data?.items ?? []);
	const total = $derived(pkgs.data?.total ?? 0);
	const multiManager = $derived(new Set(items.map((p) => p.manager)).size > 1);

	const columns = $derived<Column<PackageView>[]>([
		{ key: 'name', label: 'Paket', cell: nameCell },
		{ key: 'version', label: 'Version', cell: versionCell },
		{ key: 'arch', label: 'Architektur', hideBelow: 'md', value: (p) => p.arch || '–' },
		...(multiManager ? [{ key: 'manager', label: 'Manager', hideBelow: 'sm' } as Column<PackageView>] : []),
		{ key: 'firstSeen', label: 'Installiert/geändert', hideBelow: 'sm', cell: seenCell }
	]);
</script>

{#snippet nameCell(p: PackageView)}<span class="mono">{p.name}</span>{/snippet}
{#snippet versionCell(p: PackageView)}<span class="mono break-all text-fg-muted">{p.version}</span>{/snippet}
{#snippet seenCell(p: PackageView)}
	<span class="whitespace-nowrap text-fg-muted" title="Zuletzt gesehen {formatDateTime(p.lastSeen)}"
		>{formatDateTime(p.firstSeen)}</span
	>
{/snippet}

<div class="flex flex-col gap-4">
	<section aria-labelledby="inv-h" class="flex flex-col gap-2">
		<h2 id="inv-h" class="text-sm font-semibold">System-Inventar</h2>
		{#if inv.error && !inv.data}
			<ErrorState error={inv.error} onretry={() => inv.reload()} />
		{:else if !inv.data}
			<Skeleton lines={5} />
		{:else if !sources.length}
			<div class="rounded-lg border border-border bg-surface">
				<EmptyState
					compact
					icon="server"
					title="Kein strukturiertes Inventar"
					description="Details wie CPU, RAM, Datenträger und Dienste liefert das SSH-Inventar (Zugangsdaten unter Credentials) oder SNMP."
				/>
			</div>
		{:else}
			{#each sources as [src, entry], i (src)}
				{@const isOpen = expanded[src] ?? i < 2}
				<Card padding={isOpen ? 'md' : 'none'}>
					{#snippet header()}
						<h3 class="min-w-0 flex-1 text-sm font-semibold">
							<button
								type="button"
								class="flex w-full flex-wrap items-center gap-x-2 text-left"
								aria-expanded={isOpen}
								onclick={() => (expanded[src] = !isOpen)}
							>
								<Icon name={isOpen ? 'chevron-down' : 'chevron-right'} size={14} class="text-fg-subtle" />
								{sourceName(src)}
								{#if entry.collectedAt}
									<span class="text-xs font-normal text-fg-subtle"
										>erfasst <RelativeTime value={entry.collectedAt} /></span
									>
								{/if}
							</button>
						</h3>
						{#if isOpen}
							<Button
								size="xs"
								variant="ghost"
								icon={rawView[src] ? 'eye' : 'file'}
								active={rawView[src]}
								onclick={() => (rawView[src] = !rawView[src])}
							>
								{rawView[src] ? 'Ansicht' : 'JSON'}
							</Button>
						{/if}
					{/snippet}
					{#if isOpen}
						{#if rawView[src]}
							<JsonView value={entry.data} openDepth={2} />
						{:else if src === 'ssh' && entry.data && typeof entry.data === 'object'}
							<SshInventory data={entry.data} />
						{:else}
							<GenericData value={entry.data} />
						{/if}
					{/if}
				</Card>
			{/each}
		{/if}
	</section>

	<section aria-labelledby="pkg-h" class="flex flex-col gap-2">
		<div class="flex flex-wrap items-center gap-2">
			<h2 id="pkg-h" class="flex-1 text-sm font-semibold">
				Installierte Pakete
				{#if pkgs.data}<span class="font-normal text-fg-subtle"
						>({formatNumber(total)}{q ? ' Treffer' : ''})</span
					>{/if}
			</h2>
			<Input
				size="sm"
				icon="search"
				type="search"
				bind:value={search}
				oninput={() => applySearch(search)}
				placeholder="Paket oder Version suchen"
				aria-label="Pakete durchsuchen"
				class="w-full sm:w-72"
			/>
		</div>
		{#if pkgs.error && !pkgs.data}
			<ErrorState error={pkgs.error} onretry={() => pkgs.reload()} />
		{:else}
			<Table
				{columns}
				rows={items}
				key={(p) => `${p.manager}/${p.name}/${p.arch}`}
				loading={pkgs.loading}
				dense
				maxHeight="36rem"
				caption="Installierte Pakete"
			>
				{#snippet empty()}
					<EmptyState
						compact
						icon="package"
						title={q ? 'Keine Treffer' : 'Keine Paketliste'}
						description={q
							? `Kein Paket passt zu „${q}“.`
							: 'Paketlisten (dpkg, rpm, apk) liefert das SSH-Inventar.'}
					/>
				{/snippet}
			</Table>
			{#if total > limit}
				<Pagination {total} bind:offset bind:limit sizes={[50, 100, 250, 500]} />
			{/if}
		{/if}
	</section>
</div>
