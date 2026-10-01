<!--
	A rack: front and rear drawn to scale, a panel for the selected element or port, and the
	dialogs to mount, edit and connect. Selection lives in the URL (?item=&port=) so links from
	the device page open the right spot.
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import type { RackItem, RackPort, RackSummary, RackView } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		Card,
		EmptyState,
		ErrorState,
		PageHeader,
		Skeleton
	} from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { debounce, setParams } from '$lib/utils/url';
	import CableModal from './CableModal.svelte';
	import ItemFormModal from './ItemFormModal.svelte';
	import ItemPanel from './ItemPanel.svelte';
	import PortPanel from './PortPanel.svelte';
	import PortSocket from './PortSocket.svelte';
	import RackDrawing from './RackDrawing.svelte';
	import RackFormModal from './RackFormModal.svelte';
	import { CABLE_COLORS, itemName, type Face, type Slot } from './rack';

	let { id }: { id: number } = $props();

	const data = new AsyncData<RackView>();
	$effect(() => {
		data.run((signal) => api.get('/api/v1/racks/{id}', { path: { id }, signal }));
	});
	const rack = $derived(data.data);
	let racks = $state<RackSummary[]>([]);
	function loadRacks() {
		api
			.get('/api/v1/racks')
			.then((r) => (racks = r ?? []))
			.catch(() => {});
	}
	$effect(loadRacks);

	// device status changes (online/offline, names) show without reload
	const refresh = debounce(() => data.reload(), 1500);
	$effect(() =>
		live.on('device', (m) => {
			const dev = (m.data as { id?: number } | undefined)?.id;
			if (!rack || !dev) return;
			const shown = rack.items.some(
				(i) =>
					i.deviceId === dev ||
					i.ports.some((p) => p.device?.id === dev || p.detected.some((d) => d.device.id === dev))
			);
			if (shown) refresh();
		})
	);
	$effect(() => live.onReconnect(() => data.reload()));
	$effect(() => () => refresh.cancel());

	const canEdit = $derived(auth.can('devices.edit'));

	// ---------------------------------------------------------------- selection (URL)
	const sp = $derived(page.url.searchParams);
	const selItemId = $derived(sp.get('item') ? Number(sp.get('item')) : null);
	const selPortName = $derived(sp.get('port'));
	const view = $derived<'both' | Face>((sp.get('view') as 'both' | Face | null) ?? 'front');
	const selItem = $derived(rack?.items.find((i) => i.id === selItemId) ?? null);
	const selPort = $derived(
		selItem && selPortName ? (selItem.ports.find((p) => p.name === selPortName) ?? null) : null
	);

	function selectItem(it: RackItem | null) {
		connectFrom = null;
		setParams({ item: it?.id ?? null, port: null });
	}
	function selectPort(it: RackItem, p: RackPort) {
		if (connectFrom) {
			if (connectFrom.itemId === it.id && connectFrom.port === p.name) {
				connectFrom = null;
				return;
			}
			cableFrom = connectFrom;
			cableTo = { itemId: it.id, port: p.name };
			connectFrom = null;
			cableOpen = true;
			return;
		}
		setParams({ item: it.id, port: p.name });
	}

	// ---------------------------------------------------------------- dialogs
	let rackOpen = $state(false);
	let itemOpen = $state(false);
	let editItem = $state<RackItem | null>(null);
	let slot = $state<Slot | null>(null);
	let cableOpen = $state(false);
	let cableFrom = $state<{ itemId: number; port: string } | null>(null);
	let cableTo = $state<{ itemId: number; port: string } | null>(null);
	let connectFrom = $state<{ itemId: number; port: string } | null>(null);
	let busy = $state(false);
	let areaW = $state(0);

	function mountAt(s: Slot | null) {
		editItem = null;
		slot = s;
		itemOpen = true;
	}
	function edit(it: RackItem) {
		editItem = it;
		slot = null;
		itemOpen = true;
	}

	async function move(it: RackItem, place: { position: number; col?: number }) {
		busy = true;
		try {
			await api.put('/api/v1/rack-items/{id}', {
				path: { id: it.id },
				body: {
					kind: it.kind,
					deviceId: it.deviceId,
					label: it.label,
					position: place.position,
					height: it.height,
					face: it.face,
					fullDepth: it.fullDepth,
					col: place.col ?? it.col,
					cols: it.cols,
					portCount: it.portCount,
					portPrefix: it.portPrefix
				}
			});
			await data.reload();
		} catch (e) {
			toast.error(e);
		} finally {
			busy = false;
		}
	}

	async function remove(it: RackItem) {
		const ok = await confirm({
			title: t('{name} ausbauen?', { name: itemName(it) }),
			message: t('Portdaten und Patchkabel dieses Elements werden gelöscht, das Gerät bleibt im Inventar.'),
			confirmLabel: t('Ausbauen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/rack-items/{id}', { path: { id: it.id } });
			toast.success(t('Ausgebaut'));
			selectItem(null);
			await data.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	async function adopt(it: RackItem) {
		busy = true;
		try {
			const res = await api.post('/api/v1/rack-items/{id}/adopt', { path: { id: it.id } });
			toast.success(t('{n} Verbindungen übernommen', { n: res.ports }));
			await data.reload();
		} catch (e) {
			toast.error(e);
		} finally {
			busy = false;
		}
	}

	async function deleteRack() {
		if (!rack) return;
		const ok = await confirm({
			title: t('Rack {name} löschen?', { name: rack.name }),
			message: t(
				'Alle eingebauten Elemente, Portdaten und Patchkabel des Racks werden gelöscht. Die Geräte bleiben im Inventar.'
			),
			confirmLabel: t('Löschen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/racks/{id}', { path: { id: rack.id } });
			toast.success(t('Rack gelöscht'));
			goto('/racks');
		} catch (e) {
			toast.error(e);
		}
	}

	function changed() {
		data.reload();
		loadRacks();
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Escape' && connectFrom) {
			connectFrom = null;
			e.preventDefault();
		}
	}
</script>

<svelte:window onkeydown={onKey} />

{#if data.error && !rack}
	<ErrorState error={data.error} onretry={() => data.reload()} />
{:else if !rack}
	<Skeleton rows={8} />
{:else}
	<PageHeader
		title={rack.name}
		description={[rack.location, `${rack.width}" · ${rack.height} ${t('HE')}`].filter(Boolean).join(' · ')}
	>
		{#snippet breadcrumb()}<a href="/racks" class="hover:underline">{t('Racks')}</a>{/snippet}
		{#snippet actions()}
			<div class="flex flex-wrap items-center gap-2">
				<div class="inline-flex rounded-md border border-border p-0.5" role="group" aria-label={t('Ansicht')}>
					{#each [['front', t('Vorderseite')], ['rear', t('Rückseite')], ['both', t('Beide Seiten')]] as [v, l] (v)}
						<Button
							size="sm"
							variant={view === v ? 'secondary' : 'ghost'}
							active={view === v}
							onclick={() => setParams({ view: v === 'front' ? null : v })}
						>
							{l}
						</Button>
					{/each}
				</div>
				{#if canEdit}
					<Button icon="plus" variant="primary" onclick={() => mountAt(null)}>{t('Einbauen')}</Button>
					<Button icon="edit" label={t('Rack bearbeiten')} onclick={() => (rackOpen = true)} />
					<Button icon="trash" variant="danger" label={t('Rack löschen')} onclick={deleteRack} />
				{/if}
			</div>
		{/snippet}
	</PageHeader>

	{#if connectFrom}
		<Alert tone="info" class="mb-4">
			{t('Kabel von Port {port}: jetzt den Ziel-Port anklicken (Esc bricht ab).', { port: connectFrom.port })}
			<Button size="xs" variant="ghost" class="ml-2" onclick={() => (connectFrom = null)}
				>{t('Abbrechen')}</Button
			>
		</Alert>
	{/if}

	<div class="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,max-content)_minmax(320px,440px)]">
		<div class="min-w-0">
			<div class="flex flex-wrap gap-6 overflow-x-auto pb-2" bind:clientWidth={areaW}>
				{#each view === 'both' ? (['front', 'rear'] as Face[]) : [view] as face (face)}
					<RackDrawing
						{rack}
						{face}
						{canEdit}
						selectedItem={selItemId}
						selectedPort={selItemId && selPortName ? { item: selItemId, port: selPortName } : null}
						connecting={!!connectFrom}
						onselect={(it) => selectItem(it)}
						onport={selectPort}
						onslot={(s) => mountAt(s)}
						onmove={(it, place) => move(it, place)}
						fit={areaW}
					/>
				{/each}
			</div>
			<ul
				class="mt-3 flex w-0 min-w-full flex-wrap gap-x-4 gap-y-1.5 text-xs text-fg-muted"
				aria-label={t('Legende')}
			>
				{#each [{ state: 'device', text: t('Gerät von Hand eingetragen') }, { state: 'cable', text: t('Patchkabel (in seiner Farbe)'), color: CABLE_COLORS[4].css }, { state: 'detected', text: t('Gerät erkannt') }, { state: 'up', text: t('Link aktiv (SNMP)') }, { state: 'free', text: t('frei') }] as l (l.state)}
					<li class="flex items-center gap-1.5">
						<span class="legend-chip"
							><PortSocket state={l.state as 'device'} color={l.color} width={15} /></span
						>{l.text}
					</li>
				{/each}
			</ul>
		</div>

		<Card class="h-fit lg:sticky lg:top-4">
			{#if selItem && selPort}
				<PortPanel
					item={selItem}
					port={selPort}
					rackId={rack.id}
					{canEdit}
					connecting={!!connectFrom}
					onchanged={changed}
					onconnect={() => (connectFrom = connectFrom ? null : { itemId: selItem.id, port: selPort.name })}
					oncablemodal={() => {
						cableFrom = { itemId: selItem.id, port: selPort.name };
						cableTo = null;
						cableOpen = true;
					}}
					onback={() => setParams({ port: null })}
				/>
			{:else if selItem}
				<ItemPanel
					{rack}
					item={selItem}
					{canEdit}
					{busy}
					selectedPort={selPortName}
					onport={(p) => selectPort(selItem, p)}
					onedit={() => edit(selItem)}
					onremove={() => remove(selItem)}
					onadopt={() => adopt(selItem)}
					onmove={(position) => move(selItem, { position })}
					onclose={() => selectItem(null)}
				/>
			{:else if rack.items.length === 0}
				<EmptyState
					compact
					icon="rack"
					title={t('Das Rack ist leer')}
					description={canEdit
						? t(
								'Klicke auf eine freie Höheneinheit oder auf „Einbauen“, um ein Gerät oder ein Patchfeld einzubauen.'
							)
						: undefined}
				/>
			{:else}
				<div class="flex flex-col gap-3 text-sm text-fg-muted">
					<p>{t('Wähle ein Element oder einen Port im Rack.')}</p>
					{#if canEdit}
						<p>{t('Elemente lassen sich mit der Maus auf eine andere Höheneinheit ziehen.')}</p>
					{/if}
					{#if rack.notes}
						<p class="whitespace-pre-line text-fg">{rack.notes}</p>
					{/if}
					<p class="flex flex-wrap gap-1.5">
						<Badge>{t('{n} Elemente', { n: rack.items.length })}</Badge>
						<Badge>{t('{n} Geräte', { n: rack.items.filter((i) => i.kind === 'device').length })}</Badge>
					</p>
				</div>
			{/if}
		</Card>
	</div>

	<RackFormModal bind:open={rackOpen} {rack} onsaved={changed} />
	<ItemFormModal bind:open={itemOpen} {rack} item={editItem} {slot} {racks} onsaved={changed} />
	<CableModal bind:open={cableOpen} {rack} from={cableFrom} to={cableTo} {racks} onsaved={changed} />
{/if}

<style>
	.legend-chip {
		display: inline-flex;
		padding: 3px 4px;
		border-radius: 3px;
		background: #23272c;
	}
</style>
