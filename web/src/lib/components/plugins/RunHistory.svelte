<!--
	Run history of one plugin ("Läufe" tab): status filter, pagination (URL synced),
	live progress of active runs, cancel, diff to the previous successful run.
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, errorMessage } from '$lib/api';
	import type { RunList, RunMessageData, RunView } from '$lib/api';
	import { t } from '$lib/i18n';
	import {
		Button,
		EmptyState,
		ErrorState,
		Pagination,
		RelativeTime,
		Select,
		Table
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { formatDuration } from '$lib/utils/format';
	import { runStatusLabel, runTriggerLabel } from '$lib/utils/labels';
	import { debounce, intParam, setParams } from '$lib/utils/url';
	import RunProgress from './RunProgress.svelte';
	import RunStatusBadge from './RunStatusBadge.svelte';
	import {
		diffable,
		diffUrl,
		isActive,
		previousSuccessfulRun,
		scalarStats,
		statLabel,
		statValue
	} from './plugin';

	interface Props {
		pluginId: string;
		kind: string;
	}

	let { pluginId, kind }: Props = $props();

	const SIZES = [25, 50, 100];
	const sp = $derived(page.url.searchParams);
	const status = $derived(sp.get('status') ?? '');
	const offset = $derived(Math.max(0, intParam(sp, 'offset', 0)));
	const limit = $derived(SIZES.includes(intParam(sp, 'limit', 25)) ? intParam(sp, 'limit', 25) : 25);

	const data = new AsyncData<RunList>();
	$effect(() => {
		const query = { plugin: pluginId, status: status || null, limit, offset };
		data.run((signal) => api.get('/api/v1/runs', { query, signal }));
	});

	const refresh = debounce(() => data.reload(), 600);
	$effect(() =>
		live.on<RunMessageData>('run', (m) => {
			const d = m.data;
			if (!d || m.type === 'progress') return; // progress comes from the runs store
			const list = data.data;
			const known = !!list?.items.some((r) => r.id === d.id);
			if ((d.pluginId ?? d.run?.pluginId) !== pluginId && !known) return;
			if (m.type === 'finished' && d.discarded) {
				// routine scheduled run without changes: not kept in the history (no refetch, it is gone)
				if (list && known)
					data.set({ total: Math.max(0, list.total - 1), items: list.items.filter((r) => r.id !== d.id) });
				return;
			}
			if (m.type === 'finished' && d.run && list && known) {
				const run = d.run;
				data.set({ ...list, items: list.items.map((r) => (r.id === run.id ? run : r)) });
				return;
			}
			refresh(); // a new run (queued/started) – reload the page of the list
		})
	);
	$effect(() => live.onReconnect(refresh));
	$effect(() => () => refresh.cancel());

	/** list rows with live state of active runs */
	const rows = $derived(
		(data.data?.items ?? []).map((r) => (isActive(r.status) ? (runs.get(r.id) ?? r) : r))
	);

	let cancelling = $state<number | null>(null);
	async function cancel(r: RunView) {
		const ok = await confirm({
			title: t('Lauf #{id} abbrechen?', { id: r.id }),
			message:
				r.status === 'running'
					? t('Der laufende Vorgang wird abgebrochen; bereits gespeicherte Ergebnisse bleiben erhalten.')
					: t('Der wartende Lauf wird aus der Warteschlange entfernt.'),
			confirmLabel: t('Abbrechen'),
			cancelLabel: t('Weiterlaufen lassen'),
			danger: true
		});
		if (!ok) return;
		cancelling = r.id;
		try {
			await api.post('/api/v1/runs/{id}/cancel', { path: { id: r.id } });
			toast.info(t('Lauf #{id} wird abgebrochen', { id: r.id }));
			refresh();
		} catch (e) {
			toast.error(errorMessage(e), { title: t('Abbrechen fehlgeschlagen') });
		} finally {
			cancelling = null;
		}
	}

	let diffing = $state<number | null>(null);
	async function openDiff(r: RunView) {
		diffing = r.id;
		try {
			const prev = await previousSuccessfulRun(r);
			if (!prev) {
				toast.info(t('Es gibt keinen früheren erfolgreichen Lauf dieses Plugins zum Vergleichen.'));
				return;
			}
			await goto(diffUrl(prev, r));
		} catch (e) {
			toast.error(e);
		} finally {
			diffing = null;
		}
	}

	const detailHref = (r: RunView) => `/plugins/${encodeURIComponent(pluginId)}/runs/${r.id}`;

	const statusOptions = [
		{ value: '', label: t('Alle Status') },
		...['running,queued', 'success', 'failed,timeout', 'cancelled'].map((v) => ({
			value: v,
			label: v
				.split(',')
				.map((s) => runStatusLabel[s])
				.join(' / ')
		}))
	];

	const columns: Column<RunView>[] = [
		{ key: 'id', label: t('Lauf'), width: '5.5rem' },
		{ key: 'status', label: 'Status', width: '13rem' },
		{ key: 'trigger', label: t('Auslöser'), hideBelow: 'md' },
		{ key: 'started', label: t('Gestartet') },
		{ key: 'duration', label: t('Dauer'), align: 'right', hideBelow: 'sm' },
		{ key: 'stats', label: t('Ergebnis'), hideBelow: 'lg' },
		{ key: 'actions', label: t('Aktionen'), align: 'right' }
	];
</script>

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-end gap-2">
		<Select
			label="Status"
			size="sm"
			options={statusOptions}
			value={status}
			onchange={(e) =>
				setParams({ status: (e.currentTarget as HTMLSelectElement).value || null, offset: null })}
			class="w-48"
		/>
		<div class="ml-auto">
			<Button
				size="sm"
				icon="refresh"
				label={t('Aktualisieren')}
				loading={data.loading && !!data.data}
				onclick={() => data.reload()}
			/>
		</div>
	</div>

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else}
		<Table
			{columns}
			{rows}
			key={(r) => r.id}
			loading={data.loading && !data.data}
			dense
			caption={t('Laufhistorie')}
		>
			{#snippet cell(r, col)}
				{#if col.key === 'id'}
					<a href={detailHref(r)} class="link mono tabular">#{r.id}</a>
				{:else if col.key === 'status'}
					<div class="flex min-w-0 flex-col gap-1">
						<span class="flex items-center gap-1.5">
							<RunStatusBadge status={r.status} />
							{#if r.attempt > 1}<span class="text-xs text-fg-subtle" title={t('Versuch')}
									>{t('Versuch {n}', { n: r.attempt })}</span
								>{/if}
						</span>
						{#if isActive(r.status)}
							<RunProgress run={r} />
						{:else if r.error}
							<span class="max-w-72 truncate text-xs text-danger" title={r.error}>{r.error}</span>
						{/if}
					</div>
				{:else if col.key === 'trigger'}
					<span class="text-sm">{runTriggerLabel[r.trigger] ?? r.trigger}</span>
					{#if r.requestedBy && r.requestedBy !== (runTriggerLabel[r.trigger] ?? r.trigger)}
						<span class="block text-xs text-fg-subtle">{r.requestedBy}</span>
					{/if}
				{:else if col.key === 'started'}
					<RelativeTime value={r.startedAt ?? r.createdAt} />
					{#if r.status === 'queued' && r.notBefore}
						<span class="block text-xs text-fg-subtle"
							>{t('frühestens')} <RelativeTime value={r.notBefore} /></span
						>
					{/if}
				{:else if col.key === 'duration'}
					<span class="tabular">{r.startedAt ? formatDuration(r.durationMs) : '–'}</span>
				{:else if col.key === 'stats'}
					{@const st = scalarStats(r.stats).slice(0, 3)}
					{#if st.length}
						<span class="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-fg-muted">
							{#each st as [k, v] (k)}
								<span
									><span class="text-fg-subtle">{statLabel(k)}</span>
									<span class="tabular text-fg">{statValue(k, v)}</span></span
								>
							{/each}
						</span>
					{:else}
						<span class="text-fg-subtle">–</span>
					{/if}
				{:else if col.key === 'actions'}
					<span class="inline-flex items-center justify-end gap-1">
						{#if isActive(r.status) && auth.can('devices.scan')}
							<Button
								size="xs"
								variant="ghost"
								icon="stop"
								label={t('Lauf #{id} abbrechen', { id: r.id })}
								loading={cancelling === r.id}
								onclick={() => cancel(r)}
							/>
						{/if}
						{#if diffable(kind) && r.status === 'success' && r.trigger !== 'action'}
							<Button
								size="xs"
								variant="ghost"
								icon="diff"
								label={t('Diff zum vorherigen erfolgreichen Lauf')}
								loading={diffing === r.id}
								onclick={() => openDiff(r)}
							/>
						{/if}
						<Button
							size="xs"
							variant="ghost"
							icon="terminal"
							label={t('Details und Protokoll')}
							href={detailHref(r)}
						/>
					</span>
				{/if}
			{/snippet}
			{#snippet empty()}
				{#if status}
					<EmptyState compact icon="filter" title={t('Keine Läufe mit diesem Status')}>
						{#snippet actions()}
							<Button size="sm" onclick={() => setParams({ status: null, offset: null })}
								>{t('Filter zurücksetzen')}</Button
							>
						{/snippet}
					</EmptyState>
				{:else}
					<EmptyState
						compact
						icon="history"
						title={t('Noch keine Läufe')}
						description={t('Sobald das Plugin nach Zeitplan oder manuell läuft, erscheinen die Läufe hier.')}
					/>
				{/if}
			{/snippet}
		</Table>
		<Pagination
			total={data.data?.total ?? 0}
			{offset}
			{limit}
			sizes={SIZES}
			onchange={(o, l) => setParams({ offset: o || null, limit: l === 25 ? null : l })}
		/>
	{/if}
</div>
