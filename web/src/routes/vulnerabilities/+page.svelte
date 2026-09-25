<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { ApiVulnList, ApiVulnStatus, CveVulnRow } from '$lib/api/generated';
	import DevicePicker from '$lib/components/health/DevicePicker.svelte';
	import Disclaimer from '$lib/components/vulnerabilities/Disclaimer.svelte';
	import SeveritySummary from '$lib/components/vulnerabilities/SeveritySummary.svelte';
	import SyncStatusCard from '$lib/components/vulnerabilities/SyncStatusCard.svelte';
	import {
		MIN_OPTIONS,
		SORT_OPTIONS,
		cveSeverityLabel,
		matchTypeHint,
		matchTypeLabel,
		matchTypeTone
	} from '$lib/components/vulnerabilities/cve';
	import {
		Badge,
		Button,
		Checkbox,
		EmptyState,
		ErrorState,
		Input,
		PageHeader,
		Pagination,
		RelativeTime,
		Select,
		SeverityBadge,
		Table
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { siteFilter } from '$lib/stores/federation.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { formatDate, formatNumber } from '$lib/utils/format';
	import { debounce, intParam, setParams } from '$lib/utils/url';

	const PAGE_SIZES = [25, 50, 100, 250];
	const DEFAULT_SORT = '-score';

	// ---------------------------------------------------------------- URL state
	const sp = $derived(page.url.searchParams);
	const q = $derived(sp.get('q') ?? '');
	const product = $derived(sp.get('product') ?? '');
	const min = $derived(sp.get('min') ?? '');
	const device = $derived(intParam(sp, 'device', 0));
	const ignored = $derived(sp.get('ignored') === '1');
	const sort = $derived(sp.get('sort') || DEFAULT_SORT);
	const offset = $derived(Math.max(0, intParam(sp, 'offset', 0)));
	const limit = $derived(PAGE_SIZES.includes(intParam(sp, 'limit', 50)) ? intParam(sp, 'limit', 50) : 50);

	let qText = $state(untrack(() => page.url.searchParams.get('q') ?? ''));
	let productText = $state(untrack(() => page.url.searchParams.get('product') ?? ''));
	const applyQ = debounce((v: string) => setParams({ q: v.trim() || null, offset: null }), 300);
	const applyProduct = debounce((v: string) => setParams({ product: v.trim() || null, offset: null }), 300);
	$effect(() => {
		const cur = q;
		untrack(() => {
			if (cur !== qText.trim()) qText = cur;
		});
	});
	$effect(() => {
		const cur = product;
		untrack(() => {
			if (cur !== productText.trim()) productText = cur;
		});
	});

	// device filter (name for the picker)
	let deviceId = $state(0);
	let deviceName = $state('');
	$effect(() => {
		const id = device;
		untrack(() => {
			if (id === deviceId) return;
			deviceId = id;
			deviceName = '';
			if (id)
				api
					.get('/api/v1/devices/{id}', { path: { id } })
					.then((d) => {
						if (deviceId === id) deviceName = d.displayName || d.name || d.ip;
					})
					.catch(() => {});
		});
	});

	// ---------------------------------------------------------------- data
	const status = new AsyncData<ApiVulnStatus>();
	$effect(() => {
		status.run((signal) => api.get('/api/v1/vulnerabilities/status', { signal }));
	});

	const list = new AsyncData<ApiVulnList>();
	$effect(() => {
		const query = {
			q: q || null,
			product: product || null,
			min: min ? Number(min) : null,
			device: device || null,
			ignored: ignored || null,
			sort,
			limit,
			offset,
			site: siteFilter.value || null
		};
		list.run((signal) => api.get('/api/v1/vulnerabilities', { query, signal }));
	});

	const rows = $derived<CveVulnRow[]>(list.data?.items ?? []);
	const total = $derived(list.data?.total ?? 0);
	const disclaimer = $derived(status.data?.disclaimer || list.data?.disclaimer);

	const reloadAll = debounce(() => {
		status.reload();
		list.reload();
	}, 800);
	$effect(() => runs.onFinished((r) => r.pluginId === 'cve' && reloadAll()));
	$effect(() => live.onReconnect(reloadAll));
	$effect(() => () => {
		reloadAll.cancel();
		applyQ.cancel();
		applyProduct.cancel();
	});

	const hasFilter = $derived(!!(q || product || min || device || ignored));
	function resetFilters() {
		qText = '';
		productText = '';
		setParams({ q: null, product: null, min: null, device: null, ignored: null, offset: null });
	}

	// ---------------------------------------------------------------- table
	const columns: Column<CveVulnRow>[] = [
		{ key: 'score', label: 'CVSS', sortable: 'score', sortDesc: true, width: '5.5rem' },
		{ key: 'cve', label: 'CVE', sortable: 'cve', class: 'min-w-64' },
		{ key: 'devices', label: 'Geräte', sortable: 'devices', sortDesc: true, align: 'right', width: '6rem' },
		{ key: 'products', label: 'Produkte', hideBelow: 'lg', class: 'max-w-72' },
		{ key: 'match', label: 'Abgleich', hideBelow: 'md' },
		{ key: 'published', label: 'Veröffentlicht', sortable: 'published', sortDesc: true, hideBelow: 'md' },
		{ key: 'first_seen', label: 'Gefunden', sortable: 'first_seen', sortDesc: true, hideBelow: 'xl' }
	];
</script>

<PageHeader
	title="Schwachstellen"
	description="CVEs aller Geräte aus dem Abgleich gegen die lokale NVD-Kopie"
/>

<div class="flex flex-col gap-4">
	<Disclaimer text={disclaimer} compact />

	<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
		<SeveritySummary
			summary={status.data?.summary}
			activeMin={min}
			onpick={(m) => setParams({ min: m, offset: null })}
		/>
		<SyncStatusCard status={status.data} error={status.error} onretry={() => status.reload()} />
	</div>

	<!-- filters -->
	<section aria-label="Filter" class="flex flex-col gap-2">
		<div class="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-[1.4fr_1fr_1fr_1.2fr]">
			<Input
				label="Suche"
				type="search"
				icon="search"
				bind:value={qText}
				oninput={() => applyQ(qText)}
				placeholder="CVE-ID oder Beschreibung"
			/>
			<Input
				label="Produkt"
				type="search"
				icon="package"
				bind:value={productText}
				oninput={() => applyProduct(productText)}
				placeholder="z. B. openssh, cpe:/a:…"
			/>
			<Select
				label="Schweregrad"
				value={min}
				options={MIN_OPTIONS}
				placeholder="Alle"
				onchange={(e) =>
					setParams({ min: (e.currentTarget as HTMLSelectElement).value || null, offset: null })}
			/>
			<DevicePicker
				label="Gerät"
				bind:value={deviceId}
				bind:name={deviceName}
				onselect={(d) => setParams({ device: d?.id ?? null, offset: null })}
			/>
		</div>
		<div class="flex flex-wrap items-center gap-x-4 gap-y-2">
			<Checkbox
				checked={ignored}
				label="Auch als irrelevant markierte"
				onchange={(e) =>
					setParams({ ignored: (e.currentTarget as HTMLInputElement).checked ? '1' : null, offset: null })}
			/>
			<Select
				label="Sortierung"
				value={sort}
				options={SORT_OPTIONS}
				size="sm"
				class="w-64 lg:hidden"
				onchange={(e) => {
					const v = (e.currentTarget as HTMLSelectElement).value;
					setParams({ sort: v === DEFAULT_SORT ? null : v, offset: null });
				}}
			/>
			{#if hasFilter}
				<Button variant="ghost" size="sm" icon="x" onclick={resetFilters}>Filter zurücksetzen</Button>
			{/if}
			{#if list.data}
				<span class="text-xs text-fg-subtle sm:ml-auto">{formatNumber(total)} CVEs</span>
			{/if}
		</div>
	</section>

	{#if list.error && !list.data}
		<ErrorState error={list.error} onretry={() => list.reload()} />
	{:else}
		<Table
			{columns}
			{rows}
			key={(r) => r.cve}
			{sort}
			onsort={(s) => setParams({ sort: s === DEFAULT_SORT ? null : s, offset: null })}
			loading={list.loading}
			caption="Schwachstellen"
			onrowclick={(r, e) =>
				e.ctrlKey || e.metaKey
					? window.open(`/vulnerabilities/${r.cve}`, '_blank')
					: goto(`/vulnerabilities/${r.cve}`)}
			rowClass={(r) => (r.allIgnored ? 'opacity-60' : '')}
		>
			{#snippet cell(r, col)}
				{#if col.key === 'score'}
					{#if r.cvss !== undefined && r.cvss !== null}
						<SeverityBadge cvss={r.cvss} size="md" />
					{:else}
						<Badge tone="neutral" title="Keine CVSS-Bewertung">{cveSeverityLabel[r.severity] ?? '–'}</Badge>
					{/if}
				{:else if col.key === 'cve'}
					<div class="flex min-w-0 flex-col gap-0.5">
						<span class="flex flex-wrap items-center gap-1.5">
							<a href="/vulnerabilities/{r.cve}" class="link mono font-medium">{r.cve}</a>
							{#if r.allIgnored}<Badge
									tone="neutral"
									title="Für alle betroffenen Geräte als irrelevant markiert">alle ignoriert</Badge
								>{/if}
						</span>
						{#if r.description}
							<span class="line-clamp-2 max-w-3xl text-xs text-fg-muted" title={r.description}
								>{r.description}</span
							>
						{:else}
							<span class="text-xs text-fg-subtle">Keine Beschreibung (nicht in der NVD-Kopie)</span>
						{/if}
					</div>
				{:else if col.key === 'devices'}
					<span class="font-medium tabular">{formatNumber(r.devices)}</span>
					{#if r.ignoredDevices > 0 && !r.allIgnored}
						<span class="block text-[0.7rem] text-fg-subtle">{r.ignoredDevices} irrelevant</span>
					{/if}
				{:else if col.key === 'products'}
					<span class="line-clamp-2 text-xs text-fg-muted" title={r.products?.join('\n')}>
						{(r.products ?? []).slice(0, 2).join(', ')}{#if (r.products?.length ?? 0) > 2}
							<span class="text-fg-subtle"> +{r.products.length - 2}</span>{/if}
					</span>
				{:else if col.key === 'match'}
					<span class="flex flex-wrap gap-1">
						{#each r.matchTypes ?? [] as t (t)}
							<Badge tone={matchTypeTone(t)} title={matchTypeHint[t]}>{matchTypeLabel[t] ?? t}</Badge>
						{/each}
					</span>
				{:else if col.key === 'published'}
					<span class="tabular text-fg-muted">{formatDate(r.published)}</span>
				{:else if col.key === 'first_seen'}
					<RelativeTime value={r.firstSeen} class="text-fg-muted" />
				{/if}
			{/snippet}
			{#snippet empty()}
				{#if hasFilter}
					<EmptyState icon="filter" title="Keine Treffer" description="Keine CVE passt zu den Filtern.">
						{#snippet actions()}
							<Button onclick={resetFilters}>Filter zurücksetzen</Button>
						{/snippet}
					</EmptyState>
				{:else if status.data?.empty}
					<EmptyState
						icon="cloud"
						title="Noch keine CVE-Daten"
						description={auth.can('plugins.manage')
							? 'Die lokale NVD-Kopie ist leer – zuerst oben die NVD synchronisieren.'
							: 'Die lokale NVD-Kopie ist leer.'}
					/>
				{:else}
					<EmptyState
						icon="check-circle"
						title="Keine offenen Schwachstellen"
						description="Der Abgleich hat für kein Gerät eine (nicht ignorierte) CVE gefunden."
					/>
				{/if}
			{/snippet}
		</Table>

		<Pagination
			{total}
			{offset}
			{limit}
			sizes={PAGE_SIZES}
			onchange={(o, l) => setParams({ offset: o || null, limit: l === 50 ? null : l })}
		/>
	{/if}
</div>
