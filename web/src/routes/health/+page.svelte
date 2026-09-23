<script lang="ts">
	import { page } from '$app/state';
	import { onMount, untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { HealthBoard, HealthCheck } from '$lib/api';
	import CheckDrawer from '$lib/components/health/CheckDrawer.svelte';
	import CheckFormModal from '$lib/components/health/CheckFormModal.svelte';
	import CheckTile from '$lib/components/health/CheckTile.svelte';
	import OutageList from '$lib/components/health/OutageList.svelte';
	import {
		CHECK_TYPES,
		checkTypeLabel,
		sectionOf,
		stateSectionLabel,
		type CheckForm,
		type CheckType
	} from '$lib/components/health/health';
	import {
		Button,
		Card,
		EmptyState,
		ErrorState,
		Input,
		PageHeader,
		Select,
		Skeleton,
		StatusDot
	} from '$lib/components/ui';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { formatNumber, formatPercent, formatTime } from '$lib/utils/format';
	import { debounce, intParam, setParams } from '$lib/utils/url';

	// ---------------------------------------------------------------- URL state
	const sp = $derived(page.url.searchParams);
	const fState = $derived(sp.get('state') ?? '');
	const fType = $derived(sp.get('type') ?? '');
	const fq = $derived(sp.get('q') ?? '');
	const openId = $derived(sp.get('check') ? Number(sp.get('check')) : null);
	// ?device=<id> shows the checks of one device (links from the device page)
	const fDevice = $derived(sp.get('new') === '1' ? 0 : intParam(sp, 'device', 0));

	let text = $state(untrack(() => page.url.searchParams.get('q') ?? ''));
	const applyText = debounce((v: string) => setParams({ q: v.trim() || null }), 250);
	$effect(() => {
		const cur = fq;
		untrack(() => {
			if (cur !== text.trim()) text = cur;
		});
	});

	// ---------------------------------------------------------------- data
	const board = new AsyncData<HealthBoard>();
	$effect(() => {
		board.run((signal) => api.get('/api/v1/health/board', { signal }));
	});

	let loadedAt = $state<number | null>(null);
	$effect(() => {
		if (board.data) untrack(() => (loadedAt = Date.now()));
	});

	const checks = $derived(board.data?.checks ?? []);
	const allOutages = $derived(board.data?.outages ?? []);
	const summary = $derived(board.data?.summary ?? {});
	const enabledCount = $derived(checks.filter((c) => c.enabled).length);
	const avg24 = $derived.by(() => {
		const list = checks.filter((c) => c.enabled && c.availability?.['24h'] !== undefined);
		if (!list.length) return null;
		return list.reduce((a, c) => a + (c.availability?.['24h'] ?? 0), 0) / list.length;
	});

	const filtered = $derived.by(() => {
		const q = fq.trim().toLowerCase();
		return checks.filter((c) => {
			if (fState === 'disabled' ? c.enabled : fState && (!c.enabled || sectionOf(c) !== fState)) return false;
			if (fType && c.type !== fType) return false;
			if (fDevice && c.deviceId !== fDevice) return false;
			if (q) {
				const hay =
					`${c.name} ${c.deviceName ?? ''} ${c.target} ${c.port || ''} ${c.config?.url ?? ''}`.toLowerCase();
				if (!hay.includes(q)) return false;
			}
			return true;
		});
	});

	// outages follow the filters (only checks that are shown)
	const outages = $derived.by(() => {
		if (!(fState || fType || fq || fDevice)) return allOutages;
		const ids = new Set(filtered.map((c) => c.id));
		return allOutages.filter((o) => ids.has(o.checkId));
	});

	const sections = $derived.by(() => {
		const order = ['down', 'degraded', 'unknown', 'up', 'disabled'];
		return order
			.map((s) => ({ state: s, items: filtered.filter((c) => sectionOf(c) === s) }))
			.filter((g) => g.items.length);
	});

	// live refresh: topic "health" – "state" on every state change, "round" after every check
	// round (new latency samples, availability); bursts are coalesced
	const refresh = debounce(() => board.reload(), 1500);
	$effect(() =>
		live.on('health', (m) => {
			if (m.type === 'state' || m.type === 'round') refresh();
		})
	);
	$effect(() => live.onReconnect(refresh));
	$effect(() => () => {
		refresh.cancel();
		applyText.cancel();
	});

	// ---------------------------------------------------------------- drawer & form
	let drawerOpen = $state(false);
	$effect(() => {
		const id = openId;
		untrack(() => (drawerOpen = id !== null));
	});
	const drawerCheck = $derived(openId ? (checks.find((c) => c.id === openId) ?? null) : null);

	function openCheck(id: number) {
		setParams({ check: id }, { push: true });
	}
	function closeDrawer() {
		setParams({ check: null });
	}

	let formOpen = $state(false);
	let editing = $state<HealthCheck | null>(null);
	let preset = $state<Partial<CheckForm> | undefined>(undefined);

	function openCreate(p?: Partial<CheckForm>) {
		editing = null;
		preset = p;
		formOpen = true;
	}
	function openEdit(c: HealthCheck) {
		editing = c;
		preset = undefined;
		formOpen = true;
	}

	// /health?new=1&device=12[&type=tcp&port=22&target=…&name=…] (links from the device page)
	onMount(async () => {
		const q = page.url.searchParams;
		if (q.get('new') !== '1') return;
		const p: Partial<CheckForm> = {};
		const type = q.get('type');
		if (type && (CHECK_TYPES as readonly string[]).includes(type)) p.type = type as CheckType;
		const port = Number(q.get('port'));
		if (port > 0 && port < 65536) p.port = port;
		if (q.get('target')) p.target = q.get('target') ?? '';
		if (q.get('name')) p.name = q.get('name') ?? '';
		const dev = Number(q.get('device'));
		if (dev > 0) {
			p.deviceId = dev;
			try {
				const d = await api.get('/api/v1/devices/{id}', { path: { id: dev } });
				p.deviceName = d.displayName || d.name || d.ip || `Gerät #${dev}`;
			} catch {
				p.deviceName = `Gerät #${dev}`;
			}
		}
		setParams({ new: null, device: null, type: null, port: null, target: null, name: null });
		openCreate(p);
	});

	function onSaved(c: HealthCheck) {
		board.reload();
		if (!editing) openCheck(c.id);
	}

	const stateOptions = [
		{ value: 'down', label: 'Down' },
		{ value: 'degraded', label: 'Beeinträchtigt' },
		{ value: 'unknown', label: 'Unbekannt' },
		{ value: 'up', label: 'Up' },
		{ value: 'disabled', label: 'Deaktiviert' }
	];
	const typeOptions = CHECK_TYPES.map((t) => ({ value: t, label: checkTypeLabel[t] }));
	const hasFilter = $derived(!!(fState || fType || fq || fDevice));
	const deviceLabel = $derived(
		fDevice ? (checks.find((c) => c.deviceId === fDevice)?.deviceName ?? `Gerät #${fDevice}`) : ''
	);
	const resetFilters = () => {
		text = '';
		setParams({ state: null, type: null, q: null, device: null });
	};

	const summaryTiles = [
		{ state: 'down', label: 'Down', dot: 'down' },
		{ state: 'degraded', label: 'Beeinträchtigt', dot: 'degraded' },
		{ state: 'up', label: 'Up', dot: 'up' },
		{ state: 'unknown', label: 'Unbekannt', dot: 'unknown' }
	];
</script>

<PageHeader title="Health" description="Statusboard aller Checks mit Verfügbarkeit und Ausfallhistorie">
	{#snippet actions()}
		{#if loadedAt}
			<span class="text-xs text-fg-subtle" title="Aktualisiert sich nach jedem Health-Check-Lauf automatisch"
				>Stand {formatTime(loadedAt, true)}</span
			>
		{/if}
		<Button
			icon="refresh"
			label="Aktualisieren"
			loading={board.loading && !!board.data}
			onclick={() => board.reload()}
		/>
		<Button
			variant="primary"
			icon="plus"
			onclick={() => openCreate(fDevice ? { deviceId: fDevice, deviceName: deviceLabel } : undefined)}
			>Check anlegen</Button
		>
	{/snippet}
</PageHeader>

{#if board.error && !board.data}
	<ErrorState error={board.error} onretry={() => board.reload()} />
{:else}
	<!-- summary -->
	<section aria-label="Zusammenfassung" class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
		{#each summaryTiles as t (t.state)}
			{@const n = summary[t.state] ?? 0}
			<button
				type="button"
				aria-pressed={fState === t.state}
				onclick={() => setParams({ state: fState === t.state ? null : t.state })}
				class="flex flex-col gap-1 rounded-lg border bg-surface p-3.5 text-left shadow-sm transition-colors hover:border-border-strong hover:bg-surface-2
					{fState === t.state ? 'border-accent ring-1 ring-accent' : 'border-border'}"
			>
				<span class="flex items-center gap-1.5 text-[0.8125rem] font-medium text-fg-muted">
					<StatusDot status={t.dot} pulse={t.state === 'down' && n > 0} />
					{t.label}
				</span>
				<span
					class="text-2xl font-semibold tracking-tight tabular
						{n > 0 && t.state === 'down' ? 'text-danger' : n > 0 && t.state === 'degraded' ? 'text-warn' : 'text-fg'}"
				>
					{#if board.data}{formatNumber(n)}{:else}<span class="ns-skeleton inline-block h-7 w-10 align-middle"
						></span>{/if}
				</span>
			</button>
		{/each}
		<div
			class="col-span-2 flex flex-col gap-1 rounded-lg border border-border bg-surface p-3.5 shadow-sm sm:col-span-1"
		>
			<span class="text-[0.8125rem] font-medium text-fg-muted">Ø Verfügbarkeit 24 h</span>
			<span class="text-2xl font-semibold tracking-tight tabular">
				{#if board.data}{avg24 === null ? '–' : formatPercent(avg24, 2)}{:else}<span
						class="ns-skeleton inline-block h-7 w-16 align-middle"
					></span>{/if}
			</span>
			<span class="text-xs text-fg-subtle"
				>{formatNumber(enabledCount)} aktive von {formatNumber(checks.length)} Checks</span
			>
		</div>
	</section>

	<!-- filters -->
	<div
		class:hidden={board.data && checks.length === 0}
		class="mt-5 flex flex-col gap-2 sm:flex-row sm:items-end"
	>
		<Input
			icon="search"
			label="Suche"
			bind:value={text}
			oninput={() => applyText(text)}
			placeholder="Name, Gerät, Ziel …"
			class="sm:w-72"
			type="search"
		/>
		<div class="grid grid-cols-2 gap-2 sm:flex">
			<Select
				label="Zustand"
				value={fState}
				options={stateOptions}
				placeholder="Alle Zustände"
				class="sm:w-44"
				onchange={(e) => setParams({ state: (e.currentTarget as HTMLSelectElement).value || null })}
			/>
			<Select
				label="Typ"
				value={fType}
				options={typeOptions}
				placeholder="Alle Typen"
				class="sm:w-36"
				onchange={(e) => setParams({ type: (e.currentTarget as HTMLSelectElement).value || null })}
			/>
		</div>
		{#if fDevice}
			<span
				class="inline-flex h-8.5 items-center gap-1 self-start rounded-md border border-accent/40 bg-accent-soft pr-1 pl-2.5 text-sm text-accent sm:self-auto"
			>
				Gerät: <a href="/devices/{fDevice}" class="font-medium hover:underline">{deviceLabel}</a>
				<Button
					variant="ghost"
					size="xs"
					icon="x"
					label="Gerätefilter entfernen"
					onclick={() => setParams({ device: null })}
				/>
			</span>
		{/if}
		{#if hasFilter}
			<Button variant="ghost" icon="x" onclick={resetFilters}>Filter zurücksetzen</Button>
		{/if}
		{#if board.data}
			<span class="text-xs text-fg-subtle sm:ml-auto sm:pb-2">
				{formatNumber(filtered.length)} von {formatNumber(checks.length)} Checks
			</span>
		{/if}
	</div>

	<!-- board -->
	<div class="mt-4 flex flex-col gap-6">
		{#if !board.data}
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
				{#each Array(6) as _, i (i)}
					<div class="rounded-lg border border-border bg-surface p-4"><Skeleton lines={4} /></div>
				{/each}
			</div>
		{:else if checks.length === 0}
			<Card>
				<EmptyState
					icon="health"
					title="Noch keine Health-Checks"
					description="Checks prüfen Dienste regelmäßig per TCP, HTTP, TLS oder Ping, messen die Latenz und melden Ausfälle als Event."
				>
					{#snippet actions()}
						<Button variant="primary" icon="plus" onclick={() => openCreate()}>Ersten Check anlegen</Button>
					{/snippet}
				</EmptyState>
			</Card>
		{:else if filtered.length === 0}
			<Card>
				<EmptyState icon="filter" title="Keine Treffer" description="Kein Check passt zu den Filtern.">
					{#snippet actions()}
						<Button onclick={resetFilters}>Filter zurücksetzen</Button>
					{/snippet}
				</EmptyState>
			</Card>
		{:else}
			{#each sections as g (g.state)}
				<section aria-labelledby="sec-{g.state}">
					<h2 id="sec-{g.state}" class="mb-2 flex items-center gap-2 text-sm font-semibold text-fg">
						<StatusDot status={g.state === 'disabled' ? 'idle' : g.state} pulse={false} />
						{stateSectionLabel[g.state]}
						<span class="rounded bg-surface-3 px-1.5 text-xs font-medium text-fg-subtle tabular"
							>{g.items.length}</span
						>
					</h2>
					<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
						{#each g.items as c (c.id)}
							<CheckTile check={c} points={c.latency24h ?? []} onopen={(x) => openCheck(x.id)} />
						{/each}
					</div>
				</section>
			{/each}
		{/if}

		<!-- outage history -->
		{#if board.data && checks.length > 0}
			<Card title="Ausfallhistorie" description="Letzte 50 Ausfälle aller Checks" icon="history">
				{#snippet actions()}
					{#if outages.some((o) => !o.endedAt)}
						<span class="text-xs text-danger">{outages.filter((o) => !o.endedAt).length} andauernd</span>
					{/if}
				{/snippet}
				{#if outages.length === 0}
					<EmptyState
						compact
						icon="check-circle"
						title="Keine Ausfälle"
						description="Bisher wurde kein Ausfall aufgezeichnet."
					/>
				{:else}
					<OutageList {outages} showCheck onopen={openCheck} class="-my-2.5" />
				{/if}
			</Card>
		{/if}
	</div>
{/if}

<CheckDrawer
	bind:open={drawerOpen}
	checkId={openId}
	initial={drawerCheck}
	onclose={closeDrawer}
	onedit={openEdit}
	onchanged={() => board.reload()}
	ondeleted={() => {
		closeDrawer();
		board.reload();
	}}
/>

<CheckFormModal bind:open={formOpen} check={editing} {preset} onsaved={onSaved} />
