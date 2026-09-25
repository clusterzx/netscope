<script lang="ts">
	import { goto, replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount, untrack } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import type { Graph, GraphEdge, GraphNode, RunMessageData } from '$lib/api';
	import QueryInput from '$lib/components/QueryInput.svelte';
	import EdgeModal from '$lib/components/topology/EdgeModal.svelte';
	import Legend from '$lib/components/topology/Legend.svelte';
	import NodeList from '$lib/components/topology/NodeList.svelte';
	import SelectionPanel from '$lib/components/topology/SelectionPanel.svelte';
	import TopologyCanvas from '$lib/components/topology/TopologyCanvas.svelte';
	import type { LayoutMode, Pins } from '$lib/components/topology/graph';
	import {
		Alert,
		Button,
		EmptyState,
		ErrorState,
		PageHeader,
		Select,
		Spinner,
		Toggle
	} from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { subnets, tags } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { siteFilter } from '$lib/stores/federation.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatNumber, plural } from '$lib/utils/format';
	import { deviceTypeName, label, relationKindLabel } from '$lib/utils/labels';
	import { debounce, loadPref, savePref, setParams } from '$lib/utils/url';

	// ---------------------------------------------------------------- URL state
	const sp = $derived(page.url.searchParams);
	const subnet = $derived(sp.get('subnet') ?? '');
	const tag = $derived(sp.get('tag') ?? '');
	const q = $derived(sp.get('q') ?? '');
	const containers = $derived(sp.get('containers') === '1');
	const ignored = $derived(sp.get('ignored') === '1');
	const filtered = $derived(!!(subnet || tag || q));

	let queryText = $state(untrack(() => page.url.searchParams.get('q') ?? ''));
	$effect(() => {
		const current = q;
		untrack(() => {
			if (current !== queryText.trim()) queryText = current;
		});
	});

	onMount(() => {
		subnets.load().catch(() => {});
		tags.load().catch(() => {});
	});

	const subnetOptions = $derived.by(() => {
		const list = (subnets.value ?? []).map((s) => ({
			value: s.cidr,
			label: s.name && s.name !== s.cidr ? `${s.cidr} (${s.name})` : s.cidr
		}));
		if (subnet && !list.some((o) => o.value === subnet)) list.push({ value: subnet, label: subnet });
		return list;
	});
	const tagOptions = $derived.by(() => {
		const list = (tags.value ?? []).map((t) => ({
			value: t.tag,
			label: `${t.tag} (${formatNumber(t.count)})`
		}));
		if (tag && !list.some((o) => o.value === tag)) list.push({ value: tag, label: tag });
		return list;
	});

	// ---------------------------------------------------------------- data
	const data = new AsyncData<Graph>();
	let queryError = $state<string | null>(null);
	let lastGood: Graph | null = null;

	$effect(() => {
		const query = {
			subnet: subnet || null,
			tag: tag || null,
			q: q || null,
			containers: containers || null,
			ignored: ignored || null,
			site: siteFilter.value || null
		};
		data.run(async (signal) => {
			try {
				const g = await api.get('/api/v1/topology', { query, signal });
				queryError = null;
				lastGood = g;
				return g;
			} catch (e) {
				if (e instanceof ApiError && e.status === 400 && query.q) {
					queryError = e.message;
					return lastGood ?? { nodes: [], edges: [], tookMs: 0 };
				}
				throw e;
			}
		});
	});

	// live: device changes (online/offline, new devices, manual edges) and finished topology runs
	const refresh = debounce(() => data.reload(), 2500);
	const GRAPH_PLUGINS = new Set(['topology', 'docker', 'proxmox', 'snmp', 'openwrt']);
	$effect(() =>
		live.on<{ id: number; mergedInto?: number }>('device', (m) => {
			// a merged device lives on under another id – keep it selected
			if (m.type === 'deleted' && m.data.mergedInto && untrack(() => selected) === `d${m.data.id}`)
				select(`d${m.data.mergedInto}`);
			refresh();
		})
	);
	$effect(() => live.onReconnect(refresh));
	$effect(() =>
		live.on<RunMessageData>('run', (m) => {
			// discarded = routine run without changes → nothing to reload
			if (m.type !== 'finished' || m.data.discarded) return;
			const plugin = m.data.run?.pluginId ?? m.data.pluginId;
			if (plugin && GRAPH_PLUGINS.has(plugin)) refresh();
		})
	);
	$effect(() => () => refresh.cancel());

	const nodes = $derived<GraphNode[]>(data.data?.nodes ?? []);
	const edges = $derived<GraphEdge[]>(data.data?.edges ?? []);
	const nodesById = $derived(new Map(nodes.map((n) => [n.id, n])));
	const deviceNodes = $derived(nodes.filter((n) => n.kind === 'device' && n.deviceId));
	const containerCount = $derived(nodes.length - deviceNodes.length);
	const sortedNodes = $derived(
		[...nodes].sort(
			(a, b) =>
				Number(a.kind === 'container') - Number(b.kind === 'container') ||
				a.label.localeCompare(b.label, 'de', { numeric: true })
		)
	);
	const presentKinds = $derived([...new Set(edges.map((e) => e.kind))]);
	const presentTypes = $derived([
		...new Set(nodes.filter((n) => n.kind === 'device' && n.type).map((n) => n.type!))
	]);

	// ---------------------------------------------------------------- search (highlight + list)
	let search = $state('');
	function haystack(n: GraphNode): string {
		return [n.label, n.ip, n.vendor, n.type ? deviceTypeName(n.type) : '', n.image, ...(n.tags ?? [])]
			.filter(Boolean)
			.join(' ')
			.toLowerCase();
	}
	const listNodes = $derived.by(() => {
		const s = search.trim().toLowerCase();
		if (!s) return sortedNodes;
		const terms = s.split(/\s+/);
		return sortedNodes.filter((n) => {
			const h = haystack(n);
			return terms.every((t) => h.includes(t));
		});
	});
	const matches = $derived(search.trim() ? new Set(listNodes.map((n) => n.id)) : null);

	// ---------------------------------------------------------------- layout (persisted per browser)
	let layout = $state<LayoutMode>(
		loadPref<LayoutMode>('topology.layout', 'force') === 'radial' ? 'radial' : 'force'
	);
	function setLayout(v: LayoutMode) {
		layout = v;
		savePref('topology.layout', v);
	}

	// ---------------------------------------------------------------- pins (persisted per browser)
	const initialPins = loadPref<Pins>('topology.pins', {});
	let pins = $state.raw<Pins>(initialPins);
	let pinned = $state(new Set(Object.keys(initialPins)));
	function onPinsChange(current: Pins) {
		// keep pins of nodes that are hidden by the current filter
		const next: Pins = {};
		for (const [id, p] of Object.entries(pins)) if (!nodesById.has(id)) next[id] = p;
		Object.assign(next, current);
		pins = next;
		pinned = new Set(Object.keys(next));
		savePref('topology.pins', next);
	}

	// ---------------------------------------------------------------- selection
	let view: TopologyCanvas | null = $state(null);
	let selected = $state<string | null>(untrack(() => page.url.searchParams.get('node')));
	let selectedEdge = $state<string | null>(null);
	let pendingCenter = untrack(() => selected);
	const selectedNode = $derived(selected ? (nodesById.get(selected) ?? null) : null);
	const selectedEdgeObj = $derived(selectedEdge ? (edges.find((e) => e.id === selectedEdge) ?? null) : null);

	// center a node given in the URL (?node=d12) once the graph is there
	$effect(() => {
		if (!pendingCenter || !nodes.length || !view) return;
		const id = pendingCenter;
		pendingCenter = null;
		if (nodesById.has(id)) requestAnimationFrame(() => view?.center(id));
	});

	function syncNodeParam(id: string | null) {
		const url = new URL(page.url);
		if (id) url.searchParams.set('node', id);
		else url.searchParams.delete('node');
		if (url.search !== page.url.search) replaceState(url, {});
	}

	function select(id: string | null, centerIt = false) {
		selected = id;
		selectedEdge = null;
		syncNodeParam(id);
		if (id && centerIt) view?.center(id);
	}

	function selectEdge(id: string) {
		selectedEdge = id;
		selected = null;
		syncNodeParam(null);
	}

	let openTimer: ReturnType<typeof setTimeout> | null = null;
	function cancelOpen() {
		if (openTimer) clearTimeout(openTimer);
		openTimer = null;
	}
	$effect(() => cancelOpen);

	function onNodeClick(n: GraphNode, e: MouseEvent) {
		if (connect) return pickEndpoint(n);
		if (e.detail > 1) return; // second click of a double click
		const target = n.deviceId;
		if ((e.ctrlKey || e.metaKey) && target) {
			window.open(`/devices/${target}`, '_blank', 'noopener');
			return;
		}
		if (n.id === selected && target) {
			// second click on the selected node opens the device (unless it becomes a double click)
			cancelOpen();
			openTimer = setTimeout(() => goto(`/devices/${target}`), 300);
			return;
		}
		select(n.id);
	}

	function onNodeDblClick(n: GraphNode) {
		cancelOpen();
		if (connect) return;
		if (view?.isPinned(n.id)) view.unpin(n.id);
		else view?.center(n.id);
	}

	function onEdgeClick(e: GraphEdge) {
		if (connect) return;
		selectEdge(e.id);
	}

	function onListPick(n: GraphNode) {
		if (connect) return pickEndpoint(n);
		select(n.id, true);
	}

	// ---------------------------------------------------------------- manual edges
	let connect = $state<{ from: string | null } | null>(null);
	let edgeOpen = $state(false);
	let edgeParent = $state('');
	let edgeChild = $state('');

	function startConnect(from: string | null = null) {
		if (from && !nodesById.get(from)?.deviceId) from = null;
		connect = { from };
		selectedEdge = null;
	}

	function pickEndpoint(n: GraphNode) {
		if (!connect) return;
		if (!n.deviceId) {
			toast.warning('Container können nicht manuell verbunden werden – bitte ein Gerät wählen.');
			return;
		}
		if (!connect.from) {
			connect = { from: n.id };
			return;
		}
		if (connect.from === n.id) return;
		edgeParent = connect.from;
		edgeChild = n.id;
		connect = null;
		edgeOpen = true;
	}

	function openEdgeDialog() {
		// keyboard/manual path: open the dialog directly with the current selection as parent
		edgeParent = selectedNode?.deviceId ? selectedNode.id : '';
		edgeChild = '';
		connect = null;
		edgeOpen = true;
	}

	async function deleteEdge(e: GraphEdge) {
		if (!e.relationId) return;
		const from = nodesById.get(e.source)?.label ?? e.source;
		const to = nodesById.get(e.target)?.label ?? e.target;
		const ok = await confirm({
			title: 'Verbindung löschen?',
			message: `Die manuelle Verbindung „${from} → ${to}“ (${label(relationKindLabel, e.kind)}) wird entfernt.`,
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/topology/edges/{id}', { path: { id: e.relationId } });
			toast.success('Verbindung gelöscht');
			if (selectedEdge === e.id) selectedEdge = null;
			await data.reload();
		} catch (err) {
			toast.error(err);
		}
	}

	function onWindowKey(e: KeyboardEvent) {
		if (e.key !== 'Escape' || e.defaultPrevented || document.querySelector('dialog[open]')) return;
		const t = e.target as HTMLElement | null;
		if (t?.closest('input, textarea, select, [role="listbox"], [role="menu"]')) return;
		if (connect) {
			connect = null;
			e.preventDefault();
		} else if (selected || selectedEdge) {
			select(null);
			e.preventDefault();
		}
	}

	const graphLabel = $derived(
		`Topologie-Graph mit ${plural(deviceNodes.length, 'Gerät', 'Geräten')} und ${plural(edges.length, 'Verbindung', 'Verbindungen')}. Pfeiltasten verschieben, Plus/Minus zoomen, 0 zeigt alles.`
	);
	// legend: open by default on large screens only; the choice is remembered once toggled
	let legendOpen = $state(
		loadPref<boolean | null>('topology.legend', null) ?? window.matchMedia('(min-width: 1024px)').matches
	);
	let legendInit = true;
	$effect(() => {
		const v = legendOpen;
		if (legendInit) legendInit = false;
		else savePref('topology.legend', v);
	});

	function resetFilters() {
		queryText = '';
		setParams({ subnet: null, tag: null, q: null });
	}
</script>

<svelte:window onkeydown={onWindowKey} />

<PageHeader
	title="Topologie"
	description="Verbindungen zwischen den Geräten – automatisch erkannt oder manuell gepflegt"
>
	{#snippet meta()}
		{#if data.data}
			<span>{plural(deviceNodes.length, 'Gerät', 'Geräte')}</span>
			{#if containerCount}<span class="text-fg-subtle">·</span>
				<span>{plural(containerCount, 'Container', 'Container')}</span>{/if}
			<span class="text-fg-subtle">·</span>
			<span>{plural(edges.length, 'Verbindung', 'Verbindungen')}</span>
		{/if}
	{/snippet}
	{#snippet actions()}
		{#if auth.canWrite}
			<Button
				icon="link"
				active={!!connect}
				onclick={() => (connect ? (connect = null) : startConnect(selected))}
				disabled={deviceNodes.length < 2}
			>
				{connect ? 'Abbrechen' : 'Verbindung anlegen'}
			</Button>
		{/if}
		<Button
			icon="refresh"
			label="Aktualisieren"
			loading={data.loading && !!data.data}
			onclick={() => data.reload()}
		/>
	{/snippet}
</PageHeader>

<div class="mb-3 flex flex-col gap-2 xl:flex-row xl:items-start">
	<QueryInput
		bind:value={queryText}
		onsubmit={(v) => setParams({ q: v || null })}
		error={queryError}
		class="flex-1"
	/>
	<div class="flex flex-wrap items-center gap-2">
		<Select
			aria-label="Subnetz"
			options={subnetOptions}
			placeholder="Alle Subnetze"
			value={subnet}
			onchange={(e) => setParams({ subnet: e.currentTarget.value || null })}
			class="w-44"
		/>
		<Select
			aria-label="Tag"
			options={tagOptions}
			placeholder="Alle Tags"
			value={tag}
			onchange={(e) => setParams({ tag: e.currentTarget.value || null })}
			class="w-40"
		/>
		<Toggle
			label="Container"
			size="sm"
			checked={containers}
			onchange={(v) => setParams({ containers: v ? '1' : null })}
		/>
		<Toggle
			label="Ignorierte"
			size="sm"
			checked={ignored}
			onchange={(v) => setParams({ ignored: v ? '1' : null })}
		/>
	</div>
</div>

{#if data.error && !data.data}
	<ErrorState error={data.error} onretry={() => data.reload()} />
{:else}
	<div class="grid grid-cols-1 gap-3 lg:grid-cols-[16rem_minmax(0,1fr)] xl:grid-cols-[18rem_minmax(0,1fr)]">
		<aside
			class="order-2 flex max-h-72 min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-sm lg:order-1 lg:h-[calc(100dvh-14.5rem)] lg:max-h-none lg:min-h-[26rem]"
			aria-label="Geräteliste der Topologie"
		>
			<NodeList
				nodes={listNodes}
				total={nodes.length}
				bind:search
				{selected}
				connectMode={!!connect}
				onpick={onListPick}
				class="h-full"
			/>
		</aside>

		<div
			class="relative order-1 h-[62dvh] min-h-[22rem] overflow-hidden rounded-lg border border-border bg-surface shadow-sm lg:order-2 lg:h-[calc(100dvh-14.5rem)] lg:min-h-[26rem]"
		>
			<TopologyCanvas
				bind:this={view}
				{nodes}
				{edges}
				{pins}
				{layout}
				{selected}
				{selectedEdge}
				{matches}
				connectMode={!!connect}
				connectFrom={connect?.from ?? null}
				label={graphLabel}
				onnodeclick={onNodeClick}
				onnodedblclick={onNodeDblClick}
				onedgeclick={onEdgeClick}
				onemptyclick={() => {
					cancelOpen();
					if (!connect) select(null);
				}}
				onpinschange={onPinsChange}
			/>

			<!-- view controls -->
			<div
				class="absolute top-2 left-2 flex flex-col gap-0.5 rounded-lg border border-border bg-surface/95 p-0.5 shadow-md backdrop-blur-sm"
				role="toolbar"
				aria-label="Ansicht"
				aria-orientation="vertical"
			>
				<Button variant="ghost" size="sm" icon="zoom-in" label="Vergrößern" onclick={() => view?.zoomIn()} />
				<Button
					variant="ghost"
					size="sm"
					icon="zoom-out"
					label="Verkleinern"
					onclick={() => view?.zoomOut()}
				/>
				<Button
					variant="ghost"
					size="sm"
					icon="maximize"
					label="Alles anzeigen"
					onclick={() => view?.fit()}
				/>
				<Button
					variant="ghost"
					size="sm"
					icon="refresh"
					label="Layout neu berechnen"
					onclick={() => view?.relayout()}
				/>
				<span class="mx-1 my-0.5 h-px bg-border" aria-hidden="true"></span>
				<Button
					variant="ghost"
					size="sm"
					icon="topology"
					label="Anordnung: frei (Kräfte, Knoten lassen sich fixieren)"
					active={layout === 'force'}
					aria-pressed={layout === 'force'}
					onclick={() => setLayout('force')}
				/>
				<Button
					variant="ghost"
					size="sm"
					icon="radar"
					label="Anordnung: Kreise (Kinder rund um ihr Gerät, Container rund um ihren Host)"
					active={layout === 'radial'}
					aria-pressed={layout === 'radial'}
					onclick={() => setLayout('radial')}
				/>
				{#if pinned.size && layout === 'force'}
					<Button
						variant="ghost"
						size="sm"
						icon="pin"
						label="Alle Fixierungen lösen"
						onclick={() => view?.unpinAll()}
					/>
				{/if}
			</div>

			{#if connect}
				<div
					class="absolute top-2 left-1/2 z-10 flex w-[min(30rem,calc(100%-6rem))] -translate-x-1/2 items-center gap-2 rounded-lg border border-accent/40 bg-surface px-3 py-2 text-sm shadow-md"
					role="status"
				>
					<span class="min-w-0 flex-1">
						{#if connect.from}
							<span class="text-fg-muted">Von</span>
							<strong class="font-medium">{nodesById.get(connect.from)?.label}</strong> –
							<span class="text-fg-muted">jetzt das zweite Gerät wählen</span>
						{:else}
							<span class="text-fg-muted">Erstes Gerät im Graph oder in der Liste wählen</span>
						{/if}
					</span>
					<Button size="xs" variant="ghost" onclick={openEdgeDialog}>Formular</Button>
					<Button size="xs" onclick={() => (connect = null)}>Abbrechen</Button>
				</div>
			{/if}

			{#if data.loading && !data.data}
				<div class="absolute inset-0 flex items-center justify-center gap-2 text-sm text-fg-muted">
					<Spinner /> Topologie wird geladen …
				</div>
			{:else if data.data && nodes.length === 0}
				<div class="absolute inset-0 flex items-center justify-center p-4">
					{#if queryError}
						<EmptyState icon="alert" title="Filter ungültig" description={queryError}>
							{#snippet actions()}
								<Button onclick={resetFilters}>Filter zurücksetzen</Button>
							{/snippet}
						</EmptyState>
					{:else if filtered}
						<EmptyState
							icon="filter"
							title="Keine Geräte für diesen Filter"
							description="Kein Gerät passt zu Subnetz, Tag oder Filter."
						>
							{#snippet actions()}
								<Button onclick={resetFilters}>Filter zurücksetzen</Button>
							{/snippet}
						</EmptyState>
					{:else}
						<EmptyState
							icon="topology"
							title="Noch keine Geräte"
							description="Sobald Scanner Geräte gefunden haben, erscheinen sie hier als Graph."
						>
							{#snippet actions()}
								<Button href="/plugins" icon="plugins">Scanner konfigurieren</Button>
							{/snippet}
						</EmptyState>
					{/if}
				</div>
			{:else if data.data && edges.length === 0 && !connect}
				<div class="absolute top-2 right-2 left-14 z-[5] sm:left-auto sm:w-96">
					<Alert tone="info" title="Noch keine Verbindungen">
						Das Topologie-Plugin leitet Verbindungen aus ARP/LLDP/SNMP ab. Verbindungen lassen sich auch
						manuell anlegen.
					</Alert>
				</div>
			{/if}

			<Legend
				kinds={presentKinds}
				types={presentTypes}
				containers={containerCount > 0}
				bind:open={legendOpen}
				class="absolute bottom-2 left-2 w-56 max-w-[calc(100%-1rem)]"
			/>

			{#if data.data}
				<span
					class="pointer-events-none absolute right-2 bottom-1.5 text-[0.6875rem] text-fg-subtle tabular"
					title="Antwortzeit der Topologie-Abfrage im Backend"
				>
					{formatNumber(data.data.tookMs)} ms
				</span>
			{/if}

			{#if selectedNode || selectedEdgeObj}
				<SelectionPanel
					node={selectedNode}
					edge={selectedEdgeObj}
					{edges}
					{nodesById}
					pinned={!!selectedNode && pinned.has(selectedNode.id)}
					canWrite={auth.canWrite}
					class="absolute inset-x-2 bottom-2 z-20 max-h-[55%] sm:inset-x-auto sm:top-2 sm:right-2 sm:bottom-auto sm:max-h-[calc(100%-1rem)] sm:w-80"
					onclose={() => select(null)}
					onselectnode={(id) => select(id, true)}
					onselectedge={selectEdge}
					oncenter={(id) => view?.center(id)}
					onunpin={(id) => view?.unpin(id)}
					onconnect={(id) => startConnect(id)}
					ondeleteedge={deleteEdge}
				/>
			{/if}
		</div>
	</div>
{/if}

<EdgeModal
	bind:open={edgeOpen}
	devices={deviceNodes}
	parent={edgeParent}
	child={edgeChild}
	onsaved={() => data.reload()}
/>
