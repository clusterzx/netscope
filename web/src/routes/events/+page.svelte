<script lang="ts">
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { Event, EventList } from '$lib/api';
	import AckModal from '$lib/components/events/AckModal.svelte';
	import DevicePicker from '$lib/components/events/DevicePicker.svelte';
	import EventDrawer from '$lib/components/events/EventDrawer.svelte';
	import {
		RANGES,
		ackFilter,
		apiQuery,
		filterFromParams,
		hasFilter,
		matchesEvent,
		type EventFilter
	} from '$lib/components/events/filter';
	import {
		Badge,
		Button,
		EmptyState,
		ErrorState,
		Input,
		MultiSelect,
		PageHeader,
		Pagination,
		RelativeTime,
		Select,
		SeverityBadge,
		Table
	} from '$lib/components/ui';
	import type { Column, MultiOption, RowKey } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { eventTypes } from '$lib/stores/catalog.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import {
		formatDateTime,
		formatNumber,
		fromDateTimeLocal,
		plural,
		toDateTimeLocal
	} from '$lib/utils/format';
	import { eventCategoryLabel, label, severityLabel } from '$lib/utils/labels';
	import { debounce, intParam, setParams } from '$lib/utils/url';

	const PAGE_SIZES = [25, 50, 100, 250];

	// ---------------------------------------------------------------- URL state
	const sp = $derived(page.url.searchParams);
	// a string key keeps the filter object stable when only ?id (drawer) changes
	const filterKey = $derived(JSON.stringify(filterFromParams(sp)));
	const filter = $derived(JSON.parse(filterKey) as EventFilter);
	const offset = $derived(Math.max(0, intParam(sp, 'offset', 0)));
	const limit = $derived(PAGE_SIZES.includes(intParam(sp, 'limit', 50)) ? intParam(sp, 'limit', 50) : 50);
	const openId = $derived(intParam(sp, 'id', 0) || intParam(sp, 'event', 0) || null);

	eventTypes.load().catch(() => {});

	// ---------------------------------------------------------------- data
	const data = new AsyncData<EventList>();
	$effect(() => {
		const query = apiQuery(filter, offset, limit);
		data.run((signal) => api.get('/api/v1/events', { query, signal }));
	});
	const rows = $derived<Event[]>(data.data?.items ?? []);
	const total = $derived(data.data?.total ?? 0);

	// ---------------------------------------------------------------- live updates
	let newCount = $state(0);
	let flash = $state(new Set<number>());
	let flashIds: number[] = [];
	let flashTimer: ReturnType<typeof setTimeout> | null = null;

	const reloadLive = debounce(async () => {
		await data.reload();
		if (flashIds.length) {
			flash = new Set(flashIds);
			flashIds = [];
			if (flashTimer) clearTimeout(flashTimer);
			flashTimer = setTimeout(() => (flash = new Set()), 3000);
		}
	}, 500);

	$effect(() =>
		live.on<Event | { ids?: number[]; count?: number }>('event', (m) => {
			const f = untrack(() => filter);
			if (m.type === 'created') {
				const ev = m.data as Event;
				if (!matchesEvent(ev, f)) return;
				if (untrack(() => offset) > 0) newCount++;
				else {
					flashIds.push(ev.id);
					reloadLive();
				}
			} else if (m.type === 'acked') {
				const d = m.data as { ids?: number[]; count?: number };
				const current = untrack(() => data.data);
				if (d.ids && f.acked === 'all' && current) {
					// mark in place – the rows stay where they are
					const ids = new Set(d.ids);
					data.set({
						...current,
						items: current.items.map((e) => (ids.has(e.id) && !e.ackedAt ? { ...e, ackedAt: m.at } : e))
					});
				} else reloadLive();
			}
		})
	);
	// a merged device lives on under another id – follow it with the device filter
	$effect(() =>
		live.on<{ id: number; mergedInto?: number }>('device', (m) => {
			if (m.type === 'deleted' && m.data.mergedInto && m.data.id === untrack(() => filter.device))
				setParams({ device: m.data.mergedInto, offset: null });
		})
	);
	$effect(() => live.onReconnect(() => reloadLive()));
	$effect(() => () => {
		reloadLive.cancel();
		if (flashTimer) clearTimeout(flashTimer);
	});

	// reset the "new events" hint and the selection when the filter changes
	let lastKey = untrack(() => filterKey);
	$effect(() => {
		if (filterKey !== lastKey) {
			lastKey = filterKey;
			untrack(() => {
				newCount = 0;
				selected = [];
			});
		}
	});
	$effect(() => {
		if (offset === 0) untrack(() => (newCount = 0));
	});

	function showNew() {
		newCount = 0;
		if (offset > 0) setParams({ offset: null });
		else data.reload();
	}

	// ---------------------------------------------------------------- filter controls
	let qText = $state(untrack(() => filter.q));
	$effect(() => {
		const current = filter.q;
		untrack(() => {
			if (current !== qText.trim()) qText = current;
		});
	});
	const applyQ = debounce((v: string) => setParams({ q: v.trim() || null, offset: null }), 350);
	$effect(() => () => applyQ.cancel());

	const typeOptions = $derived.by<MultiOption[]>(() => {
		const list = [...(eventTypes.value ?? [])].sort(
			(a, b) =>
				label(eventCategoryLabel, a.category).localeCompare(label(eventCategoryLabel, b.category), 'de') ||
				a.label.localeCompare(b.label, 'de')
		);
		const opts: MultiOption[] = list.map((t) => ({
			value: t.type,
			label: t.label,
			description: t.type,
			group: label(eventCategoryLabel, t.category)
		}));
		// patterns / unknown types from the URL (e.g. type=port.*)
		for (const t of filter.types)
			if (!opts.some((o) => typeof o !== 'string' && o.value === t))
				opts.unshift({ value: t, label: t, description: 'Muster', group: 'Aus der Adresse' });
		return opts;
	});

	const sevOptions = [
		{ value: '', label: 'Alle Schweregrade' },
		{ value: 'low', label: `ab ${severityLabel.low}` },
		{ value: 'medium', label: `ab ${severityLabel.medium}` },
		{ value: 'high', label: `ab ${severityLabel.high}` },
		{ value: 'critical', label: severityLabel.critical }
	];
	const ackOptions = [
		{ value: 'open', label: 'Offen' },
		{ value: 'acked', label: 'Quittiert' },
		{ value: 'all', label: 'Alle' }
	];
	const rangeOptions = [
		{ value: '', label: 'Beliebiger Zeitraum' },
		...RANGES.map((r) => ({ value: r.value, label: r.label })),
		{ value: 'custom', label: 'Benutzerdefiniert …' }
	];

	let customRange = $state(false);
	const rangeValue = $derived(customRange || filter.from || filter.to ? 'custom' : filter.range);
	let fromLocal = $state('');
	let toLocal = $state('');
	let rangeError = $state<string | null>(null);
	$effect(() => {
		const f = filter.from;
		const t = filter.to;
		untrack(() => {
			fromLocal = toDateTimeLocal(f);
			toLocal = toDateTimeLocal(t);
		});
	});

	function setRange(v: string) {
		rangeError = null;
		if (v === 'custom') {
			customRange = true;
			if (!filter.from) fromLocal = toDateTimeLocal(Date.now() - 86400e3);
			return;
		}
		customRange = false;
		setParams({ range: v || null, from: null, to: null, offset: null });
	}

	function applyCustom() {
		rangeError = null;
		const from = fromDateTimeLocal(fromLocal);
		const to = fromDateTimeLocal(toLocal);
		if (!from && !to) {
			rangeError = 'Mindestens einen Zeitpunkt angeben';
			return;
		}
		if (from && to && new Date(from) >= new Date(to)) {
			rangeError = '„Von“ muss vor „Bis“ liegen';
			return;
		}
		setParams({ range: null, from: from || null, to: to || null, offset: null });
	}

	function resetFilters() {
		customRange = false;
		qText = '';
		setParams({
			type: null,
			category: null,
			severity: null,
			acked: null,
			device: null,
			run: null,
			q: null,
			range: null,
			from: null,
			to: null,
			offset: null
		});
	}

	const deviceName = $derived(
		filter.device ? (rows.find((r) => r.deviceId === filter.device)?.deviceName ?? null) : null
	);

	// ---------------------------------------------------------------- selection & ack
	let selected = $state<RowKey[]>([]);
	const selectedIds = $derived(selected.map(Number));
	const openSelected = $derived(
		rows.filter((r) => selectedIds.includes(r.id) && !r.ackedAt).map((r) => r.id)
	);

	let ackOpen = $state(false);
	let ackIds = $state<number[]>([]);
	let ackByFilter = $state(false);

	function ackSelection() {
		ackIds = openSelected.length ? openSelected : selectedIds;
		ackByFilter = false;
		ackOpen = true;
	}

	function ackAll() {
		ackIds = [];
		ackByFilter = true;
		ackOpen = true;
	}

	async function quickAck(ev: Event) {
		try {
			await api.post('/api/v1/events/ack', { body: { ids: [ev.id] } });
			toast.success('Event quittiert');
			await data.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	function afterAck() {
		selected = [];
		data.reload();
	}

	const canAckAll = $derived(filter.acked !== 'acked' && total > 0);
	const filterSummary = $derived.by(() => {
		const parts: string[] = [];
		if (filter.types.length) parts.push(`${plural(filter.types.length, 'Typ', 'Typen')}`);
		if (filter.categories.length)
			parts.push(filter.categories.map((c) => label(eventCategoryLabel, c)).join(', '));
		if (filter.severity) parts.push(`Schweregrad ab ${label(severityLabel, filter.severity)}`);
		if (filter.device) parts.push(`Gerät ${deviceName ?? '#' + filter.device}`);
		if (filter.run) parts.push(`Lauf #${filter.run}`);
		if (filter.range) parts.push(RANGES.find((r) => r.value === filter.range)?.label ?? filter.range);
		else if (filter.from || filter.to)
			parts.push(
				`${filter.from ? formatDateTime(filter.from) : '…'} – ${filter.to ? formatDateTime(filter.to) : 'jetzt'}`
			);
		if (filter.q) parts.push(`Text „${filter.q}“`);
		return parts.length ? parts.join(' · ') : 'alle offenen Events';
	});

	// ---------------------------------------------------------------- drawer
	const openIndex = $derived(openId ? rows.findIndex((r) => r.id === openId) : -1);
	function openEvent(id: number | null) {
		setParams({ id, event: null });
	}

	const columns: Column<Event>[] = [
		{ key: 'severity', label: 'Schweregrad', width: '6.5rem' },
		// max-w-0 lets the column take the remaining width and truncate long titles
		{ key: 'title', label: 'Event', class: 'max-w-0' },
		{ key: 'device', label: 'Gerät', hideBelow: 'md', width: '14rem' },
		{ key: 'ts', label: 'Zeit', width: '9rem', hideBelow: 'sm' },
		{ key: 'status', label: 'Status', width: '8.5rem', hideBelow: 'lg' }
	];
</script>

<PageHeader
	title="Events"
	description="Was im Netzwerk passiert ist – neue Geräte, Ports, Zertifikate, Ausfälle"
>
	{#snippet actions()}
		{#if auth.canWrite}
			<Button
				icon="check"
				disabled={!canAckAll}
				title={filter.acked === 'acked' ? 'Es werden nur quittierte Events angezeigt' : undefined}
				onclick={ackAll}
			>
				Alle passenden quittieren
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

<div class="flex flex-col gap-3">
	<!-- filters -->
	<div class="flex flex-wrap items-start gap-2" role="search" aria-label="Events filtern">
		<Input
			type="search"
			icon="search"
			bind:value={qText}
			oninput={() => applyQ(qText)}
			onkeydown={(e) => {
				if (e.key === 'Enter') {
					applyQ.cancel();
					setParams({ q: qText.trim() || null, offset: null });
				}
			}}
			placeholder="Titel, Meldung, Details …"
			aria-label="Events durchsuchen"
			class="w-full sm:w-auto sm:min-w-60 sm:flex-1 lg:max-w-sm"
		/>
		<MultiSelect
			options={typeOptions}
			value={filter.types}
			placeholder="Alle Typen"
			onchange={(v) => setParams({ type: v, offset: null })}
			class="w-full sm:w-52"
		/>
		<Select
			aria-label="Mindest-Schweregrad"
			options={sevOptions}
			value={filter.severity}
			onchange={(e) => setParams({ severity: e.currentTarget.value || null, offset: null })}
			class="w-[calc(50%-0.25rem)] sm:w-44"
		/>
		<Select
			aria-label="Quittierstatus"
			options={ackOptions}
			value={filter.acked}
			onchange={(e) => {
				const v = e.currentTarget.value;
				setParams({ acked: v === 'open' ? null : v === 'acked' ? '1' : 'all', offset: null });
			}}
			class="w-[calc(50%-0.25rem)] sm:w-32"
		/>
		<Select
			aria-label="Zeitraum"
			options={rangeOptions}
			value={rangeValue}
			onchange={(e) => setRange(e.currentTarget.value)}
			class="w-full sm:w-52"
		/>
		<DevicePicker
			value={filter.device}
			name={deviceName}
			onchange={(id) => setParams({ device: id, offset: null })}
			class="max-w-full sm:max-w-64"
		/>
		{#if filter.run}
			<Badge tone="accent" size="md" class="h-8.5 gap-1.5 pr-1">
				Lauf #{filter.run}
				<Button
					variant="ghost"
					size="xs"
					icon="x"
					label="Lauf-Filter entfernen"
					onclick={() => setParams({ run: null, offset: null })}
				/>
			</Badge>
		{/if}
		{#if filter.categories.length}
			<Badge tone="accent" size="md" class="h-8.5 gap-1.5 pr-1">
				{filter.categories.map((c) => label(eventCategoryLabel, c)).join(', ')}
				<Button
					variant="ghost"
					size="xs"
					icon="x"
					label="Kategorie-Filter entfernen"
					onclick={() => setParams({ category: null, offset: null })}
				/>
			</Badge>
		{/if}
		{#if hasFilter(filter)}
			<Button variant="ghost" icon="x" onclick={resetFilters}>Zurücksetzen</Button>
		{/if}
	</div>

	{#if rangeValue === 'custom'}
		<form
			class="flex flex-wrap items-start gap-2 rounded-lg border border-border bg-surface-2 p-3"
			onsubmit={(e) => {
				e.preventDefault();
				applyCustom();
			}}
		>
			<Input type="datetime-local" label="Von" bind:value={fromLocal} class="w-full sm:w-56" />
			<Input
				type="datetime-local"
				label="Bis"
				bind:value={toLocal}
				hint="leer = jetzt"
				class="w-full sm:w-56"
			/>
			<Button type="submit" variant="primary" class="sm:mt-6">Anwenden</Button>
			{#if rangeError}<p class="w-full text-xs text-danger" role="alert">{rangeError}</p>{/if}
		</form>
	{/if}

	<!-- live / bulk bar -->
	{#if newCount > 0}
		<button
			type="button"
			class="flex items-center justify-center gap-2 rounded-lg border border-accent/40 bg-accent-soft px-3 py-2 text-sm font-medium text-accent hover:bg-accent/15"
			onclick={showNew}
		>
			{plural(newCount, 'neues Event', 'neue Events')} – anzeigen
		</button>
	{/if}

	{#if selected.length}
		<div
			class="sticky top-16 z-20 flex flex-wrap items-center gap-2 rounded-lg border border-accent/40 bg-surface px-3 py-2 text-sm shadow-md"
			role="region"
			aria-label="Auswahl"
		>
			<span class="font-medium text-fg">{plural(selected.length, 'Event', 'Events')} ausgewählt</span>
			{#if openSelected.length !== selected.length}
				<span class="text-fg-muted">({formatNumber(openSelected.length)} offen)</span>
			{/if}
			<span class="flex-1"></span>
			{#if auth.canWrite}
				<Button
					size="sm"
					variant="primary"
					icon="check"
					disabled={!openSelected.length}
					onclick={ackSelection}
				>
					Quittieren …
				</Button>
			{/if}
			<Button size="sm" variant="ghost" onclick={() => (selected = [])}>Auswahl aufheben</Button>
		</div>
	{/if}

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else}
		<Table
			{columns}
			{rows}
			key={(r) => r.id}
			selectable={auth.canWrite}
			bind:selected
			loading={data.loading}
			dense
			caption="Events"
			onrowclick={(r) => openEvent(r.id)}
			rowClass={(r) =>
				`cursor-pointer ${r.id === openId ? 'bg-accent-soft' : ''} ${flash.has(r.id) ? 'animate-[ns-flash_2.5s_ease-out]' : ''}`}
		>
			{#snippet cell(ev, col)}
				{#if col.key === 'severity'}
					<SeverityBadge severity={ev.severity} />
				{:else if col.key === 'title'}
					<div class="min-w-0">
						<button
							type="button"
							class="block max-w-full truncate text-left font-medium text-fg hover:text-accent {ev.ackedAt
								? 'font-normal text-fg-muted'
								: ''}"
							onclick={() => openEvent(ev.id)}
						>
							{ev.title}
						</button>
						<div class="truncate text-xs text-fg-subtle">
							{ev.label || ev.type}{#if ev.message}{' · '}{ev.message}{/if}
						</div>
						<div class="truncate text-xs text-fg-subtle sm:hidden">
							<RelativeTime value={ev.ts} />{#if ev.deviceName}{' · '}{ev.deviceName}{/if}
						</div>
					</div>
				{:else if col.key === 'device'}
					{#if ev.deviceId}
						<a class="link block truncate" href="/devices/{ev.deviceId}"
							>{ev.deviceName || `Gerät #${ev.deviceId}`}</a
						>
					{:else}
						<span class="text-fg-subtle">–</span>
					{/if}
				{:else if col.key === 'ts'}
					<RelativeTime value={ev.ts} class="text-fg-muted" />
				{:else if col.key === 'status'}
					{#if ev.ackedAt}
						<span
							class="inline-flex items-center gap-1 text-xs text-ok"
							title="Quittiert {ev.ackedBy ? `von ${ev.ackedBy} ` : ''}am {formatDateTime(
								ev.ackedAt
							)}{ev.ackNote ? ` – ${ev.ackNote}` : ''}"
						>
							<span class="inline-block size-1.5 rounded-full bg-ok"></span> Quittiert
						</span>
					{:else if auth.canWrite}
						<Button size="xs" variant="ghost" icon="check" onclick={() => quickAck(ev)}>Quittieren</Button>
					{:else}
						<span class="text-xs text-warn">Offen</span>
					{/if}
				{/if}
			{/snippet}
			{#snippet empty()}
				{#if hasFilter(filter) && filter.acked !== 'open'}
					<EmptyState icon="filter" title="Keine Events" description="Kein Event passt zu den Filtern.">
						{#snippet actions()}
							<Button onclick={resetFilters}>Filter zurücksetzen</Button>
						{/snippet}
					</EmptyState>
				{:else if hasFilter(filter)}
					<EmptyState
						icon="filter"
						title="Keine offenen Events"
						description="Kein offenes Event passt zu den Filtern."
					>
						{#snippet actions()}
							<Button onclick={() => setParams({ acked: 'all', offset: null })}>Auch quittierte zeigen</Button
							>
							<Button variant="ghost" onclick={resetFilters}>Filter zurücksetzen</Button>
						{/snippet}
					</EmptyState>
				{:else}
					<EmptyState
						icon="check-circle"
						title="Alles erledigt"
						description="Es gibt keine offenen Events. Neue Events erscheinen hier live."
					>
						{#snippet actions()}
							<Button onclick={() => setParams({ acked: 'all', offset: null })}>Alle Events zeigen</Button>
						{/snippet}
					</EmptyState>
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

<EventDrawer
	id={openId}
	prevId={openIndex > 0 ? rows[openIndex - 1].id : null}
	nextId={openIndex >= 0 && openIndex < rows.length - 1 ? rows[openIndex + 1].id : null}
	canWrite={auth.canWrite}
	onclose={() => openEvent(null)}
	onnavigate={(id) => openEvent(id)}
	onacked={() => data.reload()}
/>

<AckModal
	bind:open={ackOpen}
	ids={ackIds}
	filter={ackByFilter ? ackFilter(filter) : null}
	count={ackByFilter ? (filter.acked === 'open' ? total : -1) : ackIds.length}
	summary={ackByFilter ? filterSummary : ''}
	ondone={afterAck}
/>
