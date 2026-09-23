<script lang="ts">
	import { beforeNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, errorMessage } from '$lib/api';
	import type { PluginView, RunMessageData } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		Card,
		EmptyState,
		ErrorState,
		PageHeader,
		RelativeTime,
		Skeleton,
		Tabs,
		Toggle
	} from '$lib/components/ui';
	import type { TabItem } from '$lib/components/ui';
	import PluginActions from '$lib/components/plugins/PluginActions.svelte';
	import PluginSettings from '$lib/components/plugins/PluginSettings.svelte';
	import RunHistory from '$lib/components/plugins/RunHistory.svelte';
	import RunProgress from '$lib/components/plugins/RunProgress.svelte';
	import RunStatusBadge from '$lib/components/plugins/RunStatusBadge.svelte';
	import {
		canRun,
		capabilityHint,
		capabilityLabel,
		hasRuns,
		isPublisher,
		kindDescription,
		scopeSummary,
		targetsLabel
	} from '$lib/components/plugins/plugin';
	import { groups } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDuration, formatNumber } from '$lib/utils/format';
	import { pluginKindLabel } from '$lib/utils/labels';
	import { debounce, setParams } from '$lib/utils/url';

	const id = $derived(page.params.id ?? '');

	const data = new AsyncData<PluginView>();
	$effect(() => {
		const pid = id;
		data.run((signal) => api.get('/api/v1/plugins/{id}', { path: { id: pid }, signal }), true);
	});
	$effect(() => {
		groups.load().catch(() => {});
	});

	const refresh = debounce(() => data.reload(), 500);
	$effect(() =>
		live.on<{ id: string }>('plugin', (m) => {
			if (m.data?.id === id) refresh();
		})
	);
	$effect(() =>
		live.on<RunMessageData>('run', (m) => {
			if (m.data?.pluginId === id && (m.type === 'queued' || m.type === 'started')) refresh();
		})
	);
	$effect(() =>
		runs.onFinished((r) => {
			if (r.pluginId === id) refresh();
		})
	);
	$effect(() => live.onReconnect(refresh));
	$effect(() => () => refresh.cancel());

	const p = $derived(data.data);
	const notFound = $derived(data.error instanceof ApiError && data.error.isNotFound);
	const activeRun = $derived(p ? (runs.forPlugin(p.info.id)[0] ?? p.running) : undefined);
	const groupName = (gid: number) => groups.value?.find((g) => g.id === gid)?.name ?? `#${gid}`;

	// ---------------------------------------------------------------- tabs (URL synced)
	const tabs = $derived.by((): TabItem[] => {
		if (!p) return [];
		const out: TabItem[] = [{ id: 'settings', label: 'Einstellungen', icon: 'system' }];
		if ((p.actions ?? []).length || isPublisher(p))
			out.push({ id: 'actions', label: 'Aktionen', icon: 'zap', count: (p.actions ?? []).length || null });
		if (hasRuns(p)) out.push({ id: 'runs', label: 'Läufe', icon: 'history' });
		return out;
	});
	const tab = $derived.by(() => {
		const t = page.url.searchParams.get('tab') ?? 'settings';
		return tabs.some((x) => x.id === t) ? t : 'settings';
	});

	// local tab state (the Tabs bar must snap back when the unsaved-changes guard cancels)
	let activeTab = $state('settings');
	$effect(() => {
		activeTab = tab;
	});

	function selectTab(t: string) {
		setParams({ tab: t === 'settings' ? null : t, status: null, offset: null, limit: null }, { push: true });
	}

	// ---------------------------------------------------------------- unsaved changes guard
	let dirty = $state(false);
	let bypass = false;
	beforeNavigate((nav) => {
		if (!dirty || bypass) return;
		// query-only changes on this page (tabs) keep the form mounted only for the settings tab
		const to = nav.to?.url;
		if (to && to.pathname === page.url.pathname && (to.searchParams.get('tab') ?? 'settings') === 'settings')
			return;
		if (nav.type === 'leave') {
			nav.cancel();
			return;
		}
		nav.cancel();
		activeTab = tab;
		confirm({
			title: 'Ungespeicherte Änderungen verwerfen?',
			message: 'Die Einstellungen dieses Plugins wurden geändert, aber noch nicht gespeichert.',
			confirmLabel: 'Verwerfen',
			cancelLabel: 'Weiter bearbeiten',
			danger: true
		}).then((ok) => {
			if (!ok || !to) return;
			bypass = true;
			goto(to.pathname + to.search + to.hash).finally(() => (bypass = false));
		});
	});

	// ---------------------------------------------------------------- enable / run
	let enabledBusy = $state(false);
	async function setEnabled(v: boolean) {
		if (!p?.config) return;
		const prev = p.config.enabled;
		p.config.enabled = v; // optimistic
		enabledBusy = true;
		try {
			const next = await api.put('/api/v1/plugins/{id}/config', {
				path: { id: p.info.id },
				body: { enabled: v }
			});
			data.set(next);
			toast.success(`${next.info.name} ${v ? 'aktiviert' : 'deaktiviert'}`);
		} catch (e) {
			if (p.config) p.config.enabled = prev;
			toast.error(errorMessage(e), { title: `${p.info.name}: Umschalten fehlgeschlagen` });
		} finally {
			enabledBusy = false;
		}
	}

	let starting = $state(false);
	async function runNow() {
		if (!p) return;
		starting = true;
		try {
			const res = await api.post('/api/v1/plugins/{id}/run', { path: { id: p.info.id }, body: {} });
			const href = `/plugins/${encodeURIComponent(p.info.id)}/runs/${res.id}`;
			toast.success(`Lauf #${res.id} wurde eingeplant.`, {
				title: p.info.name,
				action: { label: 'Protokoll', onClick: () => goto(href) }
			});
			refresh();
		} catch (e) {
			toast.error(errorMessage(e), { title: `${p.info.name}: Start fehlgeschlagen` });
		} finally {
			starting = false;
		}
	}
</script>

<PageHeader title={p?.info.name ?? 'Plugin'} description={p?.info.description}>
	{#snippet breadcrumb()}
		<a href="/plugins" class="hover:text-fg">Plugins</a>
		{#if p}
			<span aria-hidden="true">›</span>
			<a href="/plugins#kind-{p.info.kind}" class="hover:text-fg"
				>{pluginKindLabel[p.info.kind] ?? p.info.kind}</a
			>
		{/if}
	{/snippet}
	{#snippet meta()}
		{#if p}
			<Badge tone="accent" title={kindDescription[p.info.kind]}
				>{pluginKindLabel[p.info.kind] ?? p.info.kind}</Badge
			>
			<span class="mono text-xs">v{p.info.version}</span>
			{#each p.capabilities ?? [] as c (c)}
				<Badge variant="outline" title={capabilityHint[c]}>{capabilityLabel[c] ?? c}</Badge>
			{/each}
			{#if p.info.targets}<span class="text-xs">{targetsLabel[p.info.targets] ?? p.info.targets}</span>{/if}
		{/if}
	{/snippet}
	{#snippet actions()}
		{#if p?.config}
			<Toggle checked={p.config.enabled} onchange={setEnabled} disabled={enabledBusy} label="Aktiv" />
			{#if canRun(p)}
				<Button variant="primary" icon="play" loading={starting} onclick={runNow}>Jetzt ausführen</Button>
			{/if}
		{/if}
	{/snippet}
</PageHeader>

{#if notFound}
	<EmptyState
		icon="plugins"
		title="Plugin nicht gefunden"
		description="Ein Plugin mit der ID „{id}“ gibt es nicht."
	>
		{#snippet actions()}
			<Button href="/plugins">Zur Pluginliste</Button>
		{/snippet}
	</EmptyState>
{:else if data.error && !p}
	<ErrorState error={data.error} onretry={() => data.reload()} />
{:else if !p}
	<Card><Skeleton lines={4} /></Card>
	<Card class="mt-4"><Skeleton lines={10} /></Card>
{:else}
	<div class="flex flex-col gap-4">
		{#if p.missingBinaries?.length}
			<Alert tone="warn" title="Programme fehlen">
				Für dieses Plugin fehlen auf dem Server:
				{#each p.missingBinaries as b, i (b)}<span class="mono">{b}</span>{i < p.missingBinaries.length - 1
						? ', '
						: ''}{/each}. Läufe schlagen fehl, bis die Programme installiert sind.
			</Alert>
		{/if}

		<!-- status strip -->
		{#if isPublisher(p) && !hasRuns(p)}
			<section
				aria-label="Status"
				class="grid grid-cols-1 gap-px overflow-hidden rounded-lg border border-border bg-border shadow-sm sm:grid-cols-2"
			>
				<div class="flex flex-col gap-1 bg-surface px-4 py-3">
					<span class="text-xs text-fg-subtle">Zustellung</span>
					{#if p.config?.enabled}
						<span class="text-sm">Aktiv – stellt Benachrichtigungen aus Regeln zu</span>
					{:else}
						<span class="text-sm text-warn"
							>Inaktiv – Benachrichtigungen an diesen Publisher werden übersprungen</span
						>
					{/if}
				</div>
				<div class="flex flex-col gap-1 bg-surface px-4 py-3">
					<span class="text-xs text-fg-subtle">Benachrichtigungen</span>
					<span class="flex flex-wrap gap-x-4 gap-y-1 text-sm">
						<a class="link" href="/rules?tab=notifications&publisher={encodeURIComponent(p.info.id)}"
							>Verlauf</a
						>
						<a class="link" href="/rules">Regeln</a>
					</span>
				</div>
			</section>
		{:else}
			<section
				aria-label="Status"
				class="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border shadow-sm xl:grid-cols-4"
			>
				<div class="flex flex-col gap-1 bg-surface px-4 py-3">
					<span class="text-xs text-fg-subtle">{activeRun ? 'Aktueller Lauf' : 'Letzter Lauf'}</span>
					{#if activeRun}
						<span class="flex items-center gap-2 text-sm">
							<RunStatusBadge status={activeRun.status} />
							<a class="link mono text-xs" href="/plugins/{encodeURIComponent(p.info.id)}/runs/{activeRun.id}"
								>#{activeRun.id}</a
							>
						</span>
						<RunProgress run={activeRun} />
					{:else if p.lastRun}
						<span class="flex flex-wrap items-center gap-2 text-sm">
							<RunStatusBadge status={p.lastRun.status} />
							<a class="link mono text-xs" href="/plugins/{encodeURIComponent(p.info.id)}/runs/{p.lastRun.id}"
								>#{p.lastRun.id}</a
							>
							<span class="text-xs text-fg-muted">
								<RelativeTime value={p.lastRun.finishedAt ?? p.lastRun.createdAt} /> · {formatDuration(
									p.lastRun.durationMs
								)}
							</span>
						</span>
						{#if p.lastRun.error}
							<span class="truncate text-xs text-danger" title={p.lastRun.error}>{p.lastRun.error}</span>
						{/if}
					{:else}
						<span class="text-sm text-fg-muted"
							>{isPublisher(p) ? 'Publisher haben keine Läufe' : 'Noch nicht gelaufen'}</span
						>
					{/if}
				</div>
				<div class="flex flex-col gap-1 bg-surface px-4 py-3">
					<span class="text-xs text-fg-subtle">Zeitplan</span>
					{#if !canRun(p)}
						<span class="text-sm text-fg-muted"
							>{isPublisher(p) ? 'Wird von Regeln ausgelöst' : 'Nur über Aktionen/Hooks'}</span
						>
					{:else if p.config?.schedule}
						<span class="text-sm">{p.config.scheduleText || p.config.schedule}</span>
						<span class="mono text-xs text-fg-subtle">{p.config.schedule}</span>
					{:else}
						<span class="text-sm text-fg-muted">Nur manuell</span>
					{/if}
				</div>
				<div class="flex flex-col gap-1 bg-surface px-4 py-3">
					<span class="text-xs text-fg-subtle">Nächster Lauf</span>
					{#if !canRun(p)}
						<span class="text-sm text-fg-muted">–</span>
					{:else if !p.config?.enabled}
						<span class="text-sm text-fg-muted">Plugin inaktiv</span>
					{:else if p.nextRun}
						<span class="text-sm"><RelativeTime value={p.nextRun} /></span>
						<span class="text-xs text-fg-subtle"><RelativeTime value={p.nextRun} absolute /></span>
					{:else}
						<span class="text-sm text-fg-muted">nicht geplant</span>
					{/if}
				</div>
				<div class="flex flex-col gap-1 bg-surface px-4 py-3">
					{#if p.info.targets}
						<span class="text-xs text-fg-subtle">Bereich</span>
						<span class="text-sm">{scopeSummary(p.config?.scope, groupName)}</span>
					{:else}
						<span class="text-xs text-fg-subtle">Warteschlange</span>
						<span class="text-sm">
							{#if p.backlog > 0}
								<Badge tone="warn">{formatNumber(p.backlog)} ausstehende Änderungen</Badge>
							{:else}
								<span class="text-fg-muted">
									{(p.capabilities ?? []).includes('changes') ? 'keine ausstehenden Änderungen' : '–'}
								</span>
							{/if}
						</span>
					{/if}
					{#if p.info.targets && p.backlog > 0}
						<Badge tone="warn">{formatNumber(p.backlog)} ausstehende Änderungen</Badge>
					{/if}
				</div>
			</section>
		{/if}

		<Tabs items={tabs} bind:active={activeTab} onchange={selectTab} label="Plugin-Bereiche" />

		{#if tab === 'settings'}
			<div role="tabpanel" id="panel-settings" aria-labelledby="tab-settings">
				<PluginSettings
					plugin={p}
					bind:dirty
					onsaved={(next) => data.set(next)}
					onenabled={setEnabled}
					{enabledBusy}
				/>
			</div>
		{:else if tab === 'actions'}
			<div role="tabpanel" id="panel-actions" aria-labelledby="tab-actions">
				<PluginActions plugin={p} />
			</div>
		{:else if tab === 'runs'}
			<div role="tabpanel" id="panel-runs" aria-labelledby="tab-runs">
				<RunHistory pluginId={p.info.id} kind={p.info.kind} />
			</div>
		{/if}
	</div>
{/if}
