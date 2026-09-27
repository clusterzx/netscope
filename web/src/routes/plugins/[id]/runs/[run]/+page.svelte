<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, errorMessage } from '$lib/api';
	import type { RunMessageData, RunView } from '$lib/api';
	import { t } from '$lib/i18n';
	import {
		Alert,
		Button,
		Card,
		DescItem,
		DescList,
		EmptyState,
		ErrorState,
		JsonView,
		PageHeader,
		RelativeTime,
		Skeleton
	} from '$lib/components/ui';
	import RunLogViewer from '$lib/components/plugins/RunLogViewer.svelte';
	import RunProgress from '$lib/components/plugins/RunProgress.svelte';
	import RunStatusBadge from '$lib/components/plugins/RunStatusBadge.svelte';
	import {
		complexStats,
		diffable,
		diffUrl,
		isActive,
		previousSuccessfulRun,
		scalarStats,
		scopeSummary,
		statLabel,
		statValue
	} from '$lib/components/plugins/plugin';
	import { auth } from '$lib/stores/auth.svelte';
	import { groups } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDateTime, formatDuration } from '$lib/utils/format';
	import { pluginKindLabel, runTriggerLabel } from '$lib/utils/labels';
	import { debounce } from '$lib/utils/url';

	const pluginId = $derived(page.params.id ?? '');
	const runId = $derived(Number(page.params.run));

	const data = new AsyncData<RunView>();
	$effect(() => {
		const id = runId;
		if (!Number.isInteger(id) || id <= 0) return;
		data.run((signal) => api.get('/api/v1/runs/{id}', { path: { id }, signal }), true);
	});
	$effect(() => {
		groups.load().catch(() => {});
	});

	// the URL plugin id is only cosmetic – correct it when it does not match the run
	$effect(() => {
		const r = data.data;
		if (r && r.pluginId && r.pluginId !== pluginId)
			goto(`/plugins/${encodeURIComponent(r.pluginId)}/runs/${r.id}`, { replaceState: true });
	});

	/** routine scheduled run without changes – deleted after it finished (GET would be 404) */
	let discarded = $state(false);
	$effect(() => {
		void runId;
		discarded = false;
	});

	const refresh = debounce(() => {
		if (!discarded) data.reload();
	}, 400);
	// queued/started/finished carry the current RunView – no refetch needed
	$effect(() =>
		live.on<RunMessageData>('run', (m) => {
			const d = m.data;
			if (d?.id !== runId || m.type === 'progress') return;
			// ids of discarded runs are reused by the next run: a new queued/started resets the flag
			discarded = !!d.discarded;
			if (d.run) data.set(d.run);
			else if (!d.discarded) refresh();
		})
	);
	$effect(() => live.onReconnect(refresh));
	$effect(() => () => refresh.cancel());

	/** stored run with the live progress of the runs store */
	const run = $derived.by(() => {
		const r = data.data;
		if (!r) return undefined;
		const l = isActive(r.status) ? runs.get(r.id) : undefined;
		return l ? { ...r, status: l.status, progress: l.progress, durationMs: l.durationMs } : r;
	});
	const active = $derived(isActive(run?.status));
	const result = $derived((run?.stats?.result ?? null) as { message?: string; data?: unknown } | null);
	const scalars = $derived(scalarStats(run?.stats));
	const complex = $derived(complexStats(run?.stats));
	const params = $derived(Object.entries(run?.params ?? {}).filter(([k]) => !k.startsWith('_')));
	const groupName = (id: number) => groups.value?.find((g) => g.id === id)?.name ?? `#${id}`;
	const notFound = $derived(!data.data && data.error instanceof ApiError && data.error.isNotFound);

	let cancelling = $state(false);
	async function cancel() {
		if (!run) return;
		const ok = await confirm({
			title: t('Lauf #{id} abbrechen?', { id: run.id }),
			message: t('Bereits gespeicherte Ergebnisse bleiben erhalten.'),
			confirmLabel: t('Abbrechen'),
			cancelLabel: t('Weiterlaufen lassen'),
			danger: true
		});
		if (!ok) return;
		cancelling = true;
		try {
			await api.post('/api/v1/runs/{id}/cancel', { path: { id: run.id } });
			toast.info(t('Lauf #{id} wird abgebrochen', { id: run.id }));
			refresh();
		} catch (e) {
			toast.error(errorMessage(e), { title: t('Abbrechen fehlgeschlagen') });
		} finally {
			cancelling = false;
		}
	}

	let diffing = $state(false);
	async function openDiff() {
		if (!run) return;
		diffing = true;
		try {
			const prev = await previousSuccessfulRun(run);
			if (!prev) toast.info(t('Es gibt keinen früheren erfolgreichen Lauf dieses Plugins zum Vergleichen.'));
			else await goto(diffUrl(prev, run));
		} catch (e) {
			toast.error(e);
		} finally {
			diffing = false;
		}
	}

	const pluginHref = $derived(`/plugins/${encodeURIComponent(run?.pluginId ?? pluginId)}`);
	// "beendet vor 5 Minuten": the relative time is a component, the sentence is split at its placeholder
	const finishedText = t('beendet {time}').split('{time}');
</script>

<PageHeader
	title={t('Lauf #{id}', { id: run?.id ?? page.params.run ?? '' })}
	description={run ? `${run.pluginName} · ${pluginKindLabel[run.kind] ?? run.kind}` : undefined}
>
	{#snippet breadcrumb()}
		<a href="/plugins" class="hover:text-fg">Plugins</a>
		<span aria-hidden="true">›</span>
		<a href={pluginHref} class="hover:text-fg">{run?.pluginName ?? pluginId}</a>
		<span aria-hidden="true">›</span>
		<a href="{pluginHref}?tab=runs" class="hover:text-fg">{t('Läufe')}</a>
	{/snippet}
	{#snippet meta()}
		{#if run}
			<RunStatusBadge status={run.status} />
			<span>{runTriggerLabel[run.trigger] ?? run.trigger}</span>
			{#if run.attempt > 1}<span>· {t('Versuch {n}', { n: run.attempt })}</span>{/if}
			{#if run.finishedAt}<span
					>· {finishedText[0]}<RelativeTime value={run.finishedAt} />{finishedText[1]}</span
				>{/if}
		{/if}
	{/snippet}
	{#snippet actions()}
		{#if run && active && auth.can('devices.scan')}
			<Button variant="danger" icon="stop" loading={cancelling} onclick={cancel}>{t('Abbrechen')}</Button>
		{/if}
		{#if run && !discarded && diffable(run.kind) && run.status === 'success' && run.trigger !== 'action'}
			<Button icon="diff" loading={diffing} onclick={openDiff}>{t('Diff zum vorherigen Lauf')}</Button>
		{/if}
		<Button variant="ghost" icon="history" href="{pluginHref}?tab=runs">{t('Alle Läufe')}</Button>
	{/snippet}
</PageHeader>

{#if notFound}
	<EmptyState
		icon="history"
		title={t('Lauf nicht gefunden')}
		description={t(
			'Der Lauf existiert nicht (mehr). Routineläufe nach Zeitplan ohne Änderungen werden nicht in der Historie behalten.'
		)}
	>
		{#snippet actions()}
			<Button href="{pluginHref}?tab=runs">{t('Zur Laufhistorie')}</Button>
		{/snippet}
	</EmptyState>
{:else if data.error && !data.data}
	<ErrorState error={data.error} onretry={() => data.reload()} />
{:else if !run}
	<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
		<Card><Skeleton lines={6} /></Card>
		<Card><Skeleton lines={6} /></Card>
	</div>
{:else}
	<div class="flex flex-col gap-4">
		{#if discarded}
			<Alert tone="info" title={t('Lauf wurde nicht gespeichert')}>
				{t(
					'Routinelauf nach Zeitplan ohne Änderungen – er wird nicht in der Laufhistorie behalten. Die Angaben hier bleiben nur bis zum Verlassen der Seite sichtbar.'
				)}
			</Alert>
		{/if}
		{#if active}
			<Card padding="sm">
				<div class="flex flex-col gap-1.5">
					<span class="text-sm font-medium"
						>{run.status === 'queued' ? t('Wartet auf Ausführung') : t('Läuft')}</span
					>
					<RunProgress {run} />
				</div>
			</Card>
		{/if}
		{#if run.error}
			<Alert
				tone={run.status === 'cancelled' ? 'warn' : 'danger'}
				title={run.status === 'cancelled' ? t('Abgebrochen') : t('Fehler')}
			>
				<span class="break-words whitespace-pre-wrap">{run.error}</span>
			</Alert>
		{/if}
		{#if result?.message || result?.data !== undefined}
			<Card title={t('Ergebnis der Aktion')} icon="check-circle">
				{#if result?.message}<p class="text-sm">{result.message}</p>{/if}
				{#if result?.data !== undefined && result?.data !== null}
					<JsonView value={result.data} class="mt-3" />
				{/if}
			</Card>
		{/if}

		<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
			<Card title={t('Überblick')} icon="info">
				<DescList cols={2}>
					<DescItem label="Status"><RunStatusBadge status={run.status} /></DescItem>
					<DescItem label={t('Auslöser')} value={runTriggerLabel[run.trigger] ?? run.trigger} />
					<DescItem label={t('Angefordert von')} value={run.requestedBy} />
					<DescItem label={t('Versuch')} value={run.attempt} />
					<DescItem label={t('Erstellt')}>{formatDateTime(run.createdAt, true)}</DescItem>
					<DescItem label={t('Gestartet')}>{formatDateTime(run.startedAt, true)}</DescItem>
					<DescItem label={t('Beendet')}>{formatDateTime(run.finishedAt, true)}</DescItem>
					<DescItem
						label={t('Dauer')}
						value={active
							? run.status === 'queued'
								? t('wartet')
								: t('läuft noch')
							: run.startedAt
								? formatDuration(run.durationMs)
								: '–'}
					/>
					{#if typeof run.params?._action === 'string'}
						<DescItem label={t('Aktion')} value={run.params._action} mono />
					{/if}
					{#if run.notBefore}
						<DescItem label={t('Frühestens ab')}>{formatDateTime(run.notBefore, true)}</DescItem>
					{/if}
					{#if run.parentRunId}
						<DescItem label={t('Vorheriger Versuch')}>
							<a class="link mono" href="{pluginHref}/runs/{run.parentRunId}">#{run.parentRunId}</a>
						</DescItem>
					{/if}
					<DescItem label={t('Bereich')} class="sm:col-span-2" value={scopeSummary(run.scope, groupName)} />
				</DescList>
				{#if params.length}
					<h3 class="mt-4 mb-1.5 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
						{t('Parameter')}
					</h3>
					<JsonView value={Object.fromEntries(params)} openDepth={1} maxHeight="16rem" />
				{/if}
			</Card>

			<Card title={t('Statistik')} icon="activity">
				{#if !scalars.length && !complex.length}
					<p class="text-sm text-fg-subtle">
						{active
							? t('Statistiken liegen nach Abschluss des Laufs vor.')
							: t('Keine Statistiken vorhanden.')}
					</p>
				{:else}
					{#if scalars.length}
						<dl class="grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2">
							{#each scalars as [k, v] (k)}
								<div class="flex items-baseline justify-between gap-3 border-b border-border/60 pb-1 text-sm">
									<dt class="min-w-0 truncate text-fg-muted" title={k}>{statLabel(k)}</dt>
									<dd class="shrink-0 font-medium tabular">{statValue(k, v)}</dd>
								</div>
							{/each}
						</dl>
					{/if}
					{#each complex as [k, v] (k)}
						<h3 class="mt-4 mb-1.5 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
							{statLabel(k)}
						</h3>
						<JsonView value={v} openDepth={1} maxHeight="16rem" />
					{/each}
				{/if}
			</Card>
		</div>

		<Card title={t('Laufprotokoll')} icon="terminal">
			<RunLogViewer runId={run.id} {active} frozen={discarded} />
		</Card>
	</div>
{/if}
