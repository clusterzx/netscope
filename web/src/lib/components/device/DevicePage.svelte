<!--
	Device detail (/devices/[id]): header with actions, summary and lazily loaded tabs
	(?tab=…). Rendered inside {#key id} by the route, so every device gets fresh state.
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { api, ApiError } from '$lib/api';
	import type { DeviceAction, DeviceDetail, DeviceMessageData, EventMessageData, RunView } from '$lib/api';
	import ActionParamsDialog from '$lib/components/devices/ActionParamsDialog.svelte';
	import { outcomeToast } from '$lib/components/devices/actions';
	import {
		Alert,
		Badge,
		Button,
		EmptyState,
		ErrorState,
		Menu,
		PageHeader,
		RelativeTime,
		SeverityBadge,
		Skeleton,
		StatusDot,
		Tabs
	} from '$lib/components/ui';
	import type { MenuItem, TabItem } from '$lib/components/ui';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { customFields, meta as metaStore } from '$lib/stores/catalog.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatNumber } from '$lib/utils/format';
	import {
		criticalityLabel,
		criticalityTone,
		deviceTypeName,
		healthStateLabel,
		healthTone,
		stateLabel,
		stateTone
	} from '$lib/utils/labels';
	import { debounce, setParams } from '$lib/utils/url';
	import CertificatesTab from './CertificatesTab.svelte';
	import ContainersTab from './ContainersTab.svelte';
	import CvesTab from './CvesTab.svelte';
	import DeviceSummary from './DeviceSummary.svelte';
	import EditDeviceModal from './EditDeviceModal.svelte';
	import HealthCheckModal from './HealthCheckModal.svelte';
	import HealthTab from './HealthTab.svelte';
	import HistoryTab from './HistoryTab.svelte';
	import OverviewTab from './OverviewTab.svelte';
	import PortsTab from './PortsTab.svelte';
	import RawTab from './RawTab.svelte';
	import RelationsTab from './RelationsTab.svelte';
	import ScanModal, { type StartedScan } from './ScanModal.svelte';
	import ScanStrip from './ScanStrip.svelte';
	import SoftwareTab from './SoftwareTab.svelte';
	import SplitModal from './SplitModal.svelte';
	import { devicesHref, groupQuery, isDeviceTab, type DeviceTab } from './util';

	interface Props {
		id: number;
	}

	let { id }: Props = $props();

	const validId = $derived(Number.isInteger(id) && id > 0);

	onMount(() => {
		metaStore.load().catch(() => {});
		customFields.load().catch(() => {});
	});

	// ---------------------------------------------------------------- device
	const dev = new AsyncData<DeviceDetail>();
	$effect(() => {
		if (!validId) return;
		dev.run((signal) => api.get('/api/v1/devices/{id}', { path: { id }, signal }));
		return () => dev.abort();
	});

	const d = $derived(dev.data);
	const notFound = $derived(!validId || (dev.error instanceof ApiError && dev.error.status === 404));
	const title = $derived(d ? d.name || d.ip || d.mac || `Gerät ${d.id}` : 'Gerät');

	/** bumped on live updates; tabs refetch their data when it changes */
	let version = $state(0);
	let deleting = false;
	let gone = $state<{ target: { id: number; name: string } | null } | null>(null);

	const refresh = debounce(() => {
		if (gone) return;
		dev.reload();
		version++;
	}, 1200);

	/** deleted (SSE); after a merge the message names the target device */
	async function onDeleted(mergedInto?: number) {
		if (deleting || gone) return;
		refresh.cancel();
		if (!mergedInto) {
			gone = { target: null };
			return;
		}
		gone = { target: { id: mergedInto, name: `Gerät #${mergedInto}` } };
		try {
			const res = await api.get('/api/v1/devices', { query: { q: `id:${mergedInto}`, limit: 1 } });
			const t = res.items?.[0];
			if (t && gone?.target?.id === t.id) gone = { target: { id: t.id, name: t.name || t.ip || t.mac } };
		} catch {
			// keep the id as name
		}
	}

	$effect(() =>
		live.on<DeviceMessageData & { mergedInto?: number }>('device', (m) => {
			if (m.data?.id !== id) return;
			if (m.type === 'deleted') onDeleted(m.data.mergedInto);
			else refresh();
		})
	);
	$effect(() =>
		live.on<EventMessageData>('event', (m) => {
			if (m.type === 'created' && 'deviceId' in m.data && m.data.deviceId === id) refresh();
		})
	);
	$effect(() => live.onReconnect(() => refresh()));
	$effect(() => () => refresh.cancel());

	// ---------------------------------------------------------------- tabs
	const tab = $derived<DeviceTab>(
		isDeviceTab(page.url.searchParams.get('tab'))
			? (page.url.searchParams.get('tab') as DeviceTab)
			: 'overview'
	);
	const visited = new SvelteSet<DeviceTab>();
	$effect(() => {
		visited.add(tab);
	});

	function selectTab(t: string) {
		setParams({ tab: t === 'overview' ? null : t });
	}

	const counts = $derived(d?.counts ?? {});
	const tabs = $derived<TabItem[]>([
		{ id: 'overview', label: 'Überblick' },
		{ id: 'ports', label: 'Ports & Dienste', count: counts.ports ?? null },
		{ id: 'software', label: 'Software', count: counts.packages || null },
		{ id: 'containers', label: 'Container', count: counts.containers ?? null },
		{ id: 'certificates', label: 'Zertifikate', count: counts.certificates ?? null },
		{ id: 'cves', label: 'CVEs', count: counts.cves ?? null },
		{ id: 'health', label: 'Health', count: counts.health ?? null },
		{ id: 'history', label: 'Historie', count: counts.events ?? null },
		{ id: 'relations', label: 'Beziehungen', count: counts.relations ?? null },
		{ id: 'raw', label: 'Rohdaten' }
	]);

	// ---------------------------------------------------------------- scans
	let scanOpen = $state(false);
	let scans = $state<StartedScan[]>([]);

	/** final RunViews of runs that finished recently (a fast run can finish before the scan POST returns) */
	const finishedRuns = new Map<number, RunView>();

	function applyFinal(s: StartedScan, run: RunView) {
		s.status = run.status;
		s.error = run.error;
		s.durationMs = run.durationMs;
	}

	function onScanStarted(list: StartedScan[]) {
		const ids = new Set(list.map((s) => s.runId));
		scans = [...scans.filter((s) => !ids.has(s.runId)), ...list];
		let done = false;
		for (const s of scans) {
			const run = finishedRuns.get(s.runId);
			if (run && !s.status) {
				applyFinal(s, run);
				done = true;
			}
		}
		if (done) refresh();
	}

	// the runs store passes the RunView carried by the SSE "finished" message (no refetch)
	$effect(() =>
		runs.onFinished((run) => {
			finishedRuns.set(run.id, run);
			if (finishedRuns.size > 50) finishedRuns.delete(finishedRuns.keys().next().value as number);
			const s = scans.find((x) => x.runId === run.id);
			if (!s) return;
			applyFinal(s, run);
			refresh();
		})
	);

	// ---------------------------------------------------------------- actions
	let editOpen = $state(false);
	let healthOpen = $state(false);
	let splitOpen = $state(false);
	let notesEditing = $state(false);
	let paramsOpen = $state(false);
	let paramsAction = $state<DeviceAction | null>(null);
	let actionBusy = $state(false);

	const deviceActions = $derived(metaStore.value?.deviceActions ?? []);
	const wol = $derived(deviceActions.find((a) => a.plugin === 'wol'));

	async function runAction(a: DeviceAction, params: Record<string, unknown> = {}) {
		actionBusy = true;
		try {
			const out = await api.post('/api/v1/devices/{id}/actions/{plugin}/{action}', {
				path: { id, plugin: a.plugin, action: a.name },
				body: { params }
			});
			outcomeToast(a, out);
		} finally {
			actionBusy = false;
		}
	}

	async function startAction(a: DeviceAction) {
		if (a.params?.length) {
			paramsAction = a;
			paramsOpen = true;
			return;
		}
		if (a.confirm && !(await confirm({ title: a.label, message: a.confirm, confirmLabel: 'Ausführen' })))
			return;
		try {
			await runAction(a);
		} catch (e) {
			toast.error(e, { title: a.label });
		}
	}

	function editNotes() {
		selectTab('overview');
		notesEditing = true;
	}

	async function remove() {
		const ok = await confirm({
			title: `„${title}“ löschen?`,
			message:
				'Alle Daten des Geräts (Ports, Zertifikate, Pakete, Historie …) werden gelöscht. Events bleiben erhalten. Ist das Gerät weiter im Netz aktiv, legt der nächste Scan es neu an.',
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		deleting = true;
		try {
			await api.delete('/api/v1/devices/{id}', { path: { id } });
			toast.success(`„${title}“ gelöscht`);
			goto('/devices');
		} catch (e) {
			deleting = false;
			toast.error(e);
		}
	}

	const menuItems = $derived<MenuItem[]>([
		...(deviceActions.length
			? [
					{ separator: true as const, label: 'Plugin-Aktionen' },
					...deviceActions.map((a): MenuItem => ({
						label: a.label + (a.params?.length ? ' …' : ''),
						icon: a.plugin === 'wol' ? 'zap' : 'play',
						hint: a.pluginName,
						disabled: actionBusy,
						onclick: () => startAction(a)
					}))
				]
			: []),
		{ separator: true, label: 'Gerät' },
		{ label: 'Health-Check anlegen …', icon: 'activity', onclick: () => (healthOpen = true) },
		{ label: 'Notiz bearbeiten', icon: 'note', onclick: editNotes },
		{
			label: 'MAC-Adressen abspalten …',
			icon: 'split',
			disabled: (d?.macs?.length ?? 0) < 2,
			hint: (d?.macs?.length ?? 0) < 2 ? 'nur eine MAC' : undefined,
			onclick: () => (splitOpen = true)
		},
		{ label: 'Löschen …', icon: 'trash', danger: true, onclick: remove }
	]);

	function onChanged(next?: DeviceDetail) {
		if (next) dev.set(next);
		else dev.reload();
		version++;
	}
</script>

{#if notFound}
	<PageHeader title="Gerät nicht gefunden">
		{#snippet breadcrumb()}<a href="/devices" class="hover:text-fg hover:underline">Geräte</a>{/snippet}
	</PageHeader>
	<EmptyState
		icon="devices"
		title="Dieses Gerät gibt es nicht (mehr)"
		description="Es wurde gelöscht, mit einem anderen Gerät zusammengeführt oder die Adresse ist falsch."
	>
		{#snippet actions()}
			<Button href="/devices" icon="arrow-left">Zur Geräteliste</Button>
		{/snippet}
	</EmptyState>
{:else if !d && dev.error}
	<PageHeader title="Gerät">
		{#snippet breadcrumb()}<a href="/devices" class="hover:text-fg hover:underline">Geräte</a>{/snippet}
	</PageHeader>
	<ErrorState error={dev.error} onretry={() => dev.reload()} />
{:else if !d}
	<div class="mb-4 flex flex-col gap-2" aria-busy="true">
		<Skeleton class="h-3 w-24" />
		<Skeleton class="h-7 w-72 max-w-full" />
		<Skeleton class="h-4 w-96 max-w-full" />
	</div>
	<Skeleton lines={6} />
{:else}
	<PageHeader {title}>
		{#snippet breadcrumb()}
			<a href="/devices" class="hover:text-fg hover:underline">Geräte</a>
			<span aria-hidden="true"> / </span><span class="mono">#{d.id}</span>
		{/snippet}
		{#snippet meta()}
			<span class="inline-flex items-center gap-1.5">
				<span aria-hidden="true" class="inline-flex"
					><StatusDot status={d.online ? 'online' : 'offline'} pulse={false} /></span
				>
				<span class="text-fg">{d.online ? 'Online' : 'Offline'}</span>
				{#if d.onlineChangedAt}
					<span class="text-fg-subtle">seit <RelativeTime value={d.onlineChangedAt} /></span>
				{/if}
			</span>
			<Badge tone={stateTone(d.state)} title="Zustand">{stateLabel[d.state] ?? d.state}</Badge>
			<Badge tone={criticalityTone(d.criticality)} title="Kritikalität">
				Kritikalität: {criticalityLabel[d.criticality] ?? d.criticality}
			</Badge>
			{#if d.type}<Badge variant="outline" title="Gerätetyp">{deviceTypeName(d.type)}</Badge>{/if}
			{#if d.healthState}
				<Badge tone={healthTone(d.healthState)} dot title="Health-Checks">
					Health: {healthStateLabel[d.healthState] ?? d.healthState}
				</Badge>
			{/if}
			{#if d.cveCount > 0}
				<button
					type="button"
					class="inline-flex items-center gap-1 rounded hover:underline"
					onclick={() => selectTab('cves')}
					title="CVEs anzeigen"
				>
					<SeverityBadge cvss={d.maxCvss ?? 0} />
					<span class="text-xs">{formatNumber(d.cveCount)} CVEs</span>
				</button>
			{/if}
			{#each d.tags ?? [] as t (t)}
				<a
					href={devicesHref('tag:' + t)}
					class="rounded bg-accent-soft px-1.5 text-xs leading-5 text-accent hover:underline">#{t}</a
				>
			{/each}
			{#each d.groups ?? [] as g (g.id)}
				<a href={devicesHref(groupQuery(g.name))} class="hover:underline" title="Gruppe">
					<Badge variant="outline">{g.name}</Badge>
				</a>
			{/each}
		{/snippet}
		{#snippet actions()}
			{#if !gone}
				<div class="flex items-center gap-2">
					{#if wol && !d.online}
						<Button icon="zap" label={wol.label} loading={actionBusy} onclick={() => startAction(wol)}
							><span class="hidden sm:inline">Wake-on-LAN</span></Button
						>
					{/if}
					<Button icon="edit" label="Manuelle Daten bearbeiten" onclick={() => (editOpen = true)}
						><span class="hidden sm:inline">Bearbeiten</span></Button
					>
					<Button variant="primary" icon="radar" onclick={() => (scanOpen = true)}>Scan jetzt</Button>
					<Menu
						label="Weitere Aktionen"
						icon="more-vertical"
						variant="secondary"
						size="md"
						items={menuItems}
					/>
				</div>
			{/if}
		{/snippet}
	</PageHeader>

	{#if gone}
		<Alert tone="danger" title="Dieses Gerät existiert nicht mehr" class="mb-4">
			{#if gone.target}
				Es wurde mit
				<a href="/devices/{gone.target.id}" class="link font-medium">{gone.target.name}</a>
				zusammengeführt. Die angezeigten Daten sind nicht mehr aktuell.
			{:else}
				Es wurde gelöscht. Die angezeigten Daten sind nicht mehr aktuell.
			{/if}
			{#snippet actions()}
				<Button size="sm" href="/devices">Zur Geräteliste</Button>
			{/snippet}
		</Alert>
	{/if}

	{#if scans.length}
		<ScanStrip bind:scans class="mb-4" />
	{/if}

	<div class={gone ? 'pointer-events-none opacity-60' : ''} inert={!!gone}>
		<DeviceSummary device={d} class="mb-4" />

		<Tabs items={tabs} active={tab} onchange={selectTab} label="Gerätedetails" class="mb-4" />

		{#each tabs as t (t.id)}
			{#if visited.has(t.id as DeviceTab)}
				<div role="tabpanel" id="panel-{t.id}" aria-labelledby="tab-{t.id}" hidden={tab !== t.id}>
					{#if t.id === 'overview'}
						<OverviewTab
							device={d}
							{version}
							active={tab === 'overview'}
							bind:notesEditing
							onchanged={onChanged}
							onshowrelations={() => selectTab('relations')}
							onedit={() => (editOpen = true)}
						/>
					{:else if t.id === 'ports'}
						<PortsTab deviceId={id} {version} active={tab === 'ports'} />
					{:else if t.id === 'software'}
						<SoftwareTab deviceId={id} {version} active={tab === 'software'} />
					{:else if t.id === 'containers'}
						<ContainersTab deviceId={id} {version} active={tab === 'containers'} />
					{:else if t.id === 'certificates'}
						<CertificatesTab deviceId={id} {version} active={tab === 'certificates'} />
					{:else if t.id === 'cves'}
						<CvesTab deviceId={id} {version} active={tab === 'cves'} onchanged={() => onChanged()} />
					{:else if t.id === 'health'}
						<HealthTab
							deviceId={id}
							deviceIp={d.ip}
							{version}
							active={tab === 'health'}
							oncreate={() => (healthOpen = true)}
						/>
					{:else if t.id === 'history'}
						<HistoryTab deviceId={id} {version} active={tab === 'history'} />
					{:else if t.id === 'relations'}
						<RelationsTab device={d} {version} active={tab === 'relations'} onchanged={() => onChanged()} />
					{:else if t.id === 'raw'}
						<RawTab deviceId={id} {version} active={tab === 'raw'} />
					{/if}
				</div>
			{/if}
		{/each}
	</div>

	<EditDeviceModal bind:open={editOpen} device={d} onsaved={(next) => onChanged(next)} />
	<ScanModal bind:open={scanOpen} deviceId={id} deviceName={title} onstarted={onScanStarted} />
	<HealthCheckModal
		bind:open={healthOpen}
		device={d}
		oncreated={() => {
			onChanged();
			selectTab('health');
		}}
	/>
	<SplitModal bind:open={splitOpen} device={d} onsplit={() => onChanged()} />
	<ActionParamsDialog
		bind:open={paramsOpen}
		action={paramsAction}
		target={title}
		onrun={(p) => (paramsAction ? runAction(paramsAction, p) : Promise.resolve())}
	/>
{/if}
