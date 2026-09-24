<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount, untrack } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import type { DeviceList, DeviceRow, SavedView } from '$lib/api';
	import QueryInput from '$lib/components/QueryInput.svelte';
	import BulkBar from '$lib/components/devices/BulkBar.svelte';
	import ColumnChooser from '$lib/components/devices/ColumnChooser.svelte';
	import CreateDeviceModal from '$lib/components/devices/CreateDeviceModal.svelte';
	import DeviceCell from '$lib/components/devices/DeviceCell.svelte';
	import SavedViews from '$lib/components/devices/SavedViews.svelte';
	import { DEFAULT_COLUMNS, allColumns, tableColumns } from '$lib/components/devices/columns';
	import { Button, EmptyState, ErrorState, Menu, PageHeader, Pagination, Table } from '$lib/components/ui';
	import type { RowKey } from '$lib/components/ui';
	import { customFields } from '$lib/stores/catalog.svelte';
	import { siteFilter } from '$lib/stores/federation.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { formatNumber } from '$lib/utils/format';
	import { debounce, intParam, loadPref, savePref, setParams } from '$lib/utils/url';

	const PAGE_SIZES = [50, 100, 250, 500];

	// ---------------------------------------------------------------- URL state
	const sp = $derived(page.url.searchParams);
	const q = $derived(sp.get('q') ?? '');
	const sort = $derived(sp.get('sort') ?? 'name');
	const offset = $derived(Math.max(0, intParam(sp, 'offset', 0)));
	const limit = $derived(PAGE_SIZES.includes(intParam(sp, 'limit', 100)) ? intParam(sp, 'limit', 100) : 100);
	const viewId = $derived(sp.get('view') ? Number(sp.get('view')) : null);

	let queryText = $state(untrack(() => page.url.searchParams.get('q') ?? ''));
	$effect(() => {
		// external navigation (global search, links) → input
		const current = q;
		untrack(() => {
			if (current !== queryText.trim()) queryText = current;
		});
	});

	// ---------------------------------------------------------------- columns
	let columns = $state<string[]>(loadPref('devices.columns', DEFAULT_COLUMNS));
	onMount(() => {
		customFields.load().catch(() => {});
	});
	const catalogue = $derived(allColumns(customFields.value ?? []));
	const tableCols = $derived(tableColumns(columns, catalogue));
	const needPorts = $derived(columns.includes('ports'));

	function setColumns(keys: string[]) {
		columns = keys.includes('name') ? keys : ['name', ...keys];
		savePref('devices.columns', columns);
	}

	// ---------------------------------------------------------------- data
	const data = new AsyncData<DeviceList>();
	let queryError = $state<string | null>(null);
	let lastGood = $state<DeviceList | null>(null);

	$effect(() => {
		const query = { q, sort, limit, offset, ports: needPorts, site: siteFilter.value };
		data.run(async (signal) => {
			try {
				const res = await api.get('/api/v1/devices', { query, signal });
				queryError = null;
				lastGood = res;
				return res;
			} catch (e) {
				if (e instanceof ApiError && e.status === 400) {
					queryError = e.message;
					return lastGood ?? { total: 0, items: [], tookMs: 0 };
				}
				throw e;
			}
		});
	});

	const refresh = debounce(() => data.reload(), 2000);
	$effect(() => live.on('device', refresh));
	$effect(() => live.onReconnect(refresh));
	$effect(() => () => refresh.cancel());

	const rows = $derived<DeviceRow[]>(data.data?.items ?? []);
	const total = $derived(data.data?.total ?? 0);

	// ---------------------------------------------------------------- selection
	let selected = $state<RowKey[]>([]);
	let lastQ = untrack(() => q);
	$effect(() => {
		if (q !== lastQ) {
			lastQ = q;
			untrack(() => (selected = []));
		}
	});
	const selectedIds = $derived(selected.map(Number));

	// ---------------------------------------------------------------- actions
	function applyQuery(text: string) {
		setParams({ q: text || null, offset: null });
	}
	function applyView(v: SavedView | null) {
		if (!v) {
			setParams({ view: null, q: null, sort: null, offset: null });
			return;
		}
		if (v.columns?.length) setColumns(v.columns);
		setParams({ view: v.id, q: v.query || null, sort: v.sort || null, offset: null });
	}

	let createOpen = $state(false);
	const exportQuery = $derived(
		(q ? `&q=${encodeURIComponent(q)}` : '') +
			(siteFilter.value ? `&site=${encodeURIComponent(siteFilter.value)}` : '')
	);
</script>

<PageHeader title="Geräte" description="Inventar aller entdeckten und manuell angelegten Geräte">
	{#snippet actions()}
		<Menu
			text="Export"
			label="Export"
			icon="download"
			variant="secondary"
			size="md"
			items={[
				{
					label: 'CSV',
					icon: 'download',
					href: `/api/v1/reports/inventory?format=csv${exportQuery}`,
					hint: 'Filter'
				},
				{
					label: 'JSON',
					icon: 'download',
					href: `/api/v1/reports/inventory?format=json${exportQuery}`,
					hint: 'Filter'
				},
				{
					label: 'PDF',
					icon: 'download',
					href: `/api/v1/reports/inventory?format=pdf${exportQuery}`,
					hint: 'Filter'
				}
			]}
		/>
		<Button variant="primary" icon="plus" onclick={() => (createOpen = true)}>Gerät anlegen</Button>
	{/snippet}
</PageHeader>

<div class="flex flex-col gap-3">
	<div class="flex flex-col gap-2 md:flex-row md:items-start">
		<QueryInput bind:value={queryText} onsubmit={applyQuery} error={queryError} class="flex-1" />
		<div class="flex shrink-0 items-center gap-2">
			<SavedViews activeId={viewId} query={q} {columns} {sort} onapply={applyView} />
			<ColumnChooser {catalogue} value={columns} onchange={setColumns} />
		</div>
	</div>

	{#if selected.length}
		<BulkBar ids={selectedIds} {rows} onclear={() => (selected = [])} ondone={() => data.reload()} />
	{/if}

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else}
		<Table
			columns={tableCols}
			{rows}
			key={(r) => r.id}
			{sort}
			onsort={(s) => setParams({ sort: s === 'name' ? null : s, offset: null })}
			selectable
			bind:selected
			loading={data.loading}
			dense
			maxHeight="calc(100dvh - 15rem)"
			caption="Geräteliste"
			onrowclick={(r, e) =>
				e.ctrlKey || e.metaKey ? window.open(`/devices/${r.id}`, '_blank') : goto(`/devices/${r.id}`)}
			rowClass={(r) => (r.state === 'ignored' ? 'opacity-60' : '')}
		>
			{#snippet cell(row, col)}
				<DeviceCell {row} colKey={col.key} col={catalogue.find((c) => c.key === col.key)} />
			{/snippet}
			{#snippet empty()}
				{#if q && queryError}
					<EmptyState icon="filter" title="Filter ungültig" description={queryError}>
						{#snippet actions()}
							<Button onclick={() => applyQuery('')}>Filter zurücksetzen</Button>
						{/snippet}
					</EmptyState>
				{:else if q}
					<EmptyState icon="filter" title="Keine Treffer" description="Kein Gerät passt zum Filter „{q}“.">
						{#snippet actions()}
							<Button onclick={() => applyQuery('')}>Filter zurücksetzen</Button>
						{/snippet}
					</EmptyState>
				{:else}
					<EmptyState
						icon="devices"
						title="Noch keine Geräte"
						description="Sobald ein Scanner läuft, erscheinen die gefundenen Geräte hier."
					>
						{#snippet actions()}
							<Button href="/plugins" icon="plugins">Scanner konfigurieren</Button>
							<Button variant="primary" icon="plus" onclick={() => (createOpen = true)}>Gerät anlegen</Button>
						{/snippet}
					</EmptyState>
				{/if}
			{/snippet}
		</Table>

		<div class="flex flex-wrap items-center justify-between gap-2">
			<Pagination
				{total}
				{offset}
				{limit}
				sizes={PAGE_SIZES}
				class="flex-1"
				onchange={(o, l) => setParams({ offset: o || null, limit: l === 100 ? null : l })}
			/>
			{#if data.data && !queryError}
				<span class="text-xs text-fg-subtle tabular" title="Antwortzeit der Geräteabfrage im Backend">
					{formatNumber(data.data.tookMs)} ms
				</span>
			{/if}
		</div>
	{/if}
</div>

<CreateDeviceModal bind:open={createOpen} />
