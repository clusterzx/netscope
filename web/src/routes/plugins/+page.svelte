<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import type { PluginView, RunMessageData } from '$lib/api';
	import {
		Badge,
		Button,
		EmptyState,
		ErrorState,
		Icon,
		Input,
		PageHeader,
		RelativeTime,
		Select,
		Skeleton,
		Toggle
	} from '$lib/components/ui';
	import RunProgress from '$lib/components/plugins/RunProgress.svelte';
	import RunStatusBadge from '$lib/components/plugins/RunStatusBadge.svelte';
	import { KIND_ORDER, canRun, isPublisher, kindDescription } from '$lib/components/plugins/plugin';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDuration, formatNumber } from '$lib/utils/format';
	import { pluginKindLabel } from '$lib/utils/labels';
	import { debounce, setParams } from '$lib/utils/url';

	// ---------------------------------------------------------------- data
	const data = new AsyncData<PluginView[]>();
	$effect(() => {
		data.run(async (signal) => (await api.get('/api/v1/plugins', { signal })) ?? []);
	});

	const refresh = debounce(() => data.reload(), 1000);
	$effect(() => live.on('plugin', refresh));
	$effect(() =>
		live.on<RunMessageData>('run', (m) => {
			if (m.type === 'queued' || m.type === 'started') refresh();
		})
	);
	$effect(() => runs.onFinished(refresh));
	$effect(() => live.onReconnect(refresh));
	$effect(() => () => refresh.cancel());

	// ---------------------------------------------------------------- filter (URL synced)
	const sp = $derived(page.url.searchParams);
	const q = $derived(sp.get('q') ?? '');
	const status = $derived(sp.get('status') ?? '');
	let text = $state(untrack(() => page.url.searchParams.get('q') ?? ''));
	const applyText = debounce((v: string) => setParams({ q: v.trim() || null }), 250);
	$effect(() => () => applyText.cancel());

	const statusOptions = [
		{ value: '', label: 'Alle Plugins' },
		{ value: 'enabled', label: 'Aktiv' },
		{ value: 'disabled', label: 'Inaktiv' },
		{ value: 'running', label: 'Läuft gerade' },
		{ value: 'failed', label: 'Letzter Lauf fehlgeschlagen' },
		{ value: 'problems', label: 'Programme fehlen' }
	];

	function activeRunOf(p: PluginView) {
		return runs.forPlugin(p.info.id)[0] ?? p.running;
	}

	function matches(p: PluginView): boolean {
		const needle = q.toLowerCase();
		if (
			needle &&
			![p.info.name, p.info.id, p.info.description, pluginKindLabel[p.info.kind] ?? ''].some((s) =>
				s.toLowerCase().includes(needle)
			)
		)
			return false;
		switch (status) {
			case 'enabled':
				return !!p.config?.enabled;
			case 'disabled':
				return !p.config?.enabled;
			case 'running':
				return !!activeRunOf(p);
			case 'failed':
				return p.lastRun?.status === 'failed' || p.lastRun?.status === 'timeout';
			case 'problems':
				return (p.missingBinaries?.length ?? 0) > 0;
		}
		return true;
	}

	const all = $derived(data.data ?? []);
	const groups = $derived(
		KIND_ORDER.map((kind) => ({
			kind,
			total: all.filter((p) => p.info.kind === kind).length,
			items: all
				.filter((p) => p.info.kind === kind && matches(p))
				.sort((a, b) => a.info.name.localeCompare(b.info.name, 'de'))
		})).filter((g) => g.total > 0)
	);
	const shown = $derived(groups.reduce((n, g) => n + g.items.length, 0));
	const counts = $derived({
		enabled: all.filter((p) => p.config?.enabled).length,
		failed: all.filter((p) => p.lastRun?.status === 'failed' || p.lastRun?.status === 'timeout').length,
		missing: all.filter((p) => (p.missingBinaries?.length ?? 0) > 0).length
	});

	// ---------------------------------------------------------------- actions
	let busy = $state<Record<string, boolean>>({});

	async function setEnabled(p: PluginView, v: boolean) {
		if (!p.config) return;
		const prev = p.config.enabled;
		p.config.enabled = v;
		busy[p.info.id] = true;
		try {
			const next = await api.put('/api/v1/plugins/{id}/config', {
				path: { id: p.info.id },
				body: { enabled: v }
			});
			const list = data.data ?? [];
			const i = list.findIndex((x) => x.info.id === next.info.id);
			if (i >= 0) list[i] = next;
			toast.success(`${next.info.name} ${v ? 'aktiviert' : 'deaktiviert'}`);
		} catch (e) {
			if (p.config) p.config.enabled = prev;
			toast.error(errorMessage(e), { title: `${p.info.name}: Umschalten fehlgeschlagen` });
		} finally {
			busy[p.info.id] = false;
		}
	}

	let starting = $state<Record<string, boolean>>({});
	async function runNow(p: PluginView) {
		starting[p.info.id] = true;
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
			starting[p.info.id] = false;
		}
	}

	const href = (p: PluginView) => `/plugins/${encodeURIComponent(p.info.id)}`;
</script>

<PageHeader
	title="Plugins"
	description="Scanner, Importer, Processor und Publisher – aktivieren, planen, ausführen und überwachen"
>
	{#snippet meta()}
		{#if data.data}
			<span>{formatNumber(all.length)} Plugins · {formatNumber(counts.enabled)} aktiv</span>
			{#if counts.failed}
				<button type="button" onclick={() => setParams({ status: 'failed' })} class="cursor-pointer">
					<Badge tone="danger" dot>{counts.failed} fehlgeschlagen</Badge>
				</button>
			{/if}
			{#if counts.missing}
				<button type="button" onclick={() => setParams({ status: 'problems' })} class="cursor-pointer">
					<Badge tone="warn" dot>{counts.missing} mit fehlenden Programmen</Badge>
				</button>
			{/if}
		{/if}
	{/snippet}
</PageHeader>

<div class="mb-4 flex flex-col gap-2 sm:flex-row sm:items-end">
	<Input
		label="Suche"
		icon="search"
		type="search"
		placeholder="Name, ID oder Beschreibung …"
		bind:value={text}
		oninput={() => applyText(text)}
		class="sm:w-80"
	/>
	<Select
		label="Status"
		options={statusOptions}
		value={status}
		onchange={(e) => setParams({ status: (e.currentTarget as HTMLSelectElement).value || null })}
		class="sm:w-60"
	/>
	<nav aria-label="Plugin-Typen" class="flex flex-wrap gap-1 sm:ml-auto">
		{#each groups as g (g.kind)}
			<a
				href="#kind-{g.kind}"
				class="rounded-md px-2 py-1 text-sm text-fg-muted hover:bg-surface-3 hover:text-fg"
			>
				{pluginKindLabel[g.kind]} <span class="text-fg-subtle tabular">{g.items.length}</span>
			</a>
		{/each}
	</nav>
</div>

{#if data.error && !data.data}
	<ErrorState error={data.error} onretry={() => data.reload()} />
{:else if !data.data}
	<div class="flex flex-col gap-6">
		{#each [0, 1] as i (i)}
			<div class="rounded-lg border border-border bg-surface p-4"><Skeleton rows={5} /></div>
		{/each}
	</div>
{:else if all.length === 0}
	<EmptyState
		icon="plugins"
		title="Keine Plugins"
		description="Der Server meldet keine installierten Plugins."
	/>
{:else if shown === 0}
	<EmptyState icon="filter" title="Keine Treffer" description="Kein Plugin passt zu Suche und Statusfilter.">
		{#snippet actions()}
			<Button
				onclick={() => {
					text = '';
					setParams({ q: null, status: null });
				}}>Filter zurücksetzen</Button
			>
		{/snippet}
	</EmptyState>
{:else}
	<div class="flex flex-col gap-6">
		{#each groups as g (g.kind)}
			{#if g.items.length}
				<section id="kind-{g.kind}" aria-labelledby="kind-{g.kind}-h" class="scroll-mt-20">
					<div class="mb-2 flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
						<h2 id="kind-{g.kind}-h" class="text-base font-semibold">
							{pluginKindLabel[g.kind]}
							<span class="ml-1 text-sm font-normal text-fg-subtle tabular">{g.items.length}</span>
						</h2>
						<p class="text-sm text-fg-muted">{kindDescription[g.kind]}</p>
					</div>
					<ul
						class="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface shadow-sm"
					>
						{#each g.items as p (p.info.id)}
							{@const run = activeRunOf(p)}
							{@const enabled = !!p.config?.enabled}
							<li
								class="grid grid-cols-[auto_1fr] items-start gap-x-3 gap-y-2 px-4 py-3 transition-colors hover:bg-surface-2
									lg:grid-cols-[auto_minmax(0,1.6fr)_minmax(0,1.1fr)_minmax(0,1fr)_auto] lg:items-center"
							>
								<div class="pt-0.5 lg:pt-0">
									<Toggle
										checked={enabled}
										onchange={(v) => setEnabled(p, v)}
										disabled={busy[p.info.id]}
										label="{p.info.name} aktiv"
										hideLabel
										size="sm"
									/>
								</div>

								<!-- name, description, warnings -->
								<div class="min-w-0">
									<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
										<a href={href(p)} class="font-medium text-fg hover:text-accent hover:underline"
											>{p.info.name}</a
										>
										<span class="mono text-xs text-fg-subtle">{p.info.id}</span>
										{#if !enabled}<Badge>inaktiv</Badge>{/if}
										{#if p.missingBinaries?.length}
											<Badge tone="warn" title="Fehlende Programme: {p.missingBinaries.join(', ')}">
												<Icon name="alert" size={12} class="-mt-px inline align-middle" /> fehlt: {p.missingBinaries.join(
													', '
												)}
											</Badge>
										{/if}
										{#if p.backlog > 0}
											<Badge tone="warn" title="Ausstehende Änderungen in der Warteschlange">
												{formatNumber(p.backlog)} ausstehend
											</Badge>
										{/if}
									</div>
									<p
										class="mt-0.5 line-clamp-2 text-sm text-fg-muted lg:line-clamp-1"
										title={p.info.description}
									>
										{p.info.description}
									</p>
								</div>

								<!-- last / current run -->
								<div class="col-start-2 min-w-0 lg:col-start-auto">
									{#if run}
										<div class="flex flex-col gap-1">
											<span class="flex items-center gap-2 text-sm">
												<RunStatusBadge status={run.status} />
												<a class="link mono text-xs" href="{href(p)}/runs/{run.id}">#{run.id}</a>
											</span>
											<RunProgress {run} class="max-w-64" />
										</div>
									{:else if p.lastRun}
										<div class="flex flex-col gap-0.5 text-sm">
											<span class="flex flex-wrap items-center gap-x-2 gap-y-1">
												<RunStatusBadge status={p.lastRun.status} />
												<span class="text-xs text-fg-muted">
													<RelativeTime value={p.lastRun.finishedAt ?? p.lastRun.createdAt} />
													· {formatDuration(p.lastRun.durationMs)}
												</span>
											</span>
											{#if p.lastRun.error}
												<a
													href="{href(p)}/runs/{p.lastRun.id}"
													class="truncate text-xs text-danger hover:underline"
													title={p.lastRun.error}>{p.lastRun.error}</a
												>
											{/if}
										</div>
									{:else}
										<span class="text-xs text-fg-subtle">
											{isPublisher(p)
												? 'Versand über Regeln'
												: canRun(p)
													? 'Noch nicht gelaufen'
													: 'Nur Aktionen'}
										</span>
									{/if}
								</div>

								<!-- schedule -->
								<div class="col-start-2 min-w-0 text-sm lg:col-start-auto">
									{#if canRun(p)}
										{#if p.config?.schedule}
											<span class="flex items-center gap-1.5" title={p.config.schedule}>
												<Icon name="clock" size={14} class="shrink-0 text-fg-subtle" />
												<span class="truncate">{p.config.scheduleText || p.config.schedule}</span>
											</span>
											{#if enabled && p.nextRun}
												<span class="block text-xs text-fg-subtle"
													>nächster Lauf <RelativeTime value={p.nextRun} /></span
												>
											{:else if !enabled}
												<span class="block text-xs text-fg-subtle">pausiert (inaktiv)</span>
											{/if}
										{:else}
											<span class="text-fg-muted">Nur manuell</span>
										{/if}
									{:else}
										<span class="text-xs text-fg-subtle">kein Zeitplan</span>
									{/if}
								</div>

								<!-- actions -->
								<div class="col-start-2 flex items-center gap-1 lg:col-start-auto lg:justify-end">
									{#if canRun(p)}
										<Button
											size="sm"
											icon="play"
											loading={starting[p.info.id]}
											onclick={() => runNow(p)}
											disabled={!!run && run.status === 'queued'}
											label="{p.info.name} jetzt ausführen"
										>
											<span class="lg:hidden xl:inline">Jetzt ausführen</span>
										</Button>
									{/if}
									<Button
										size="sm"
										variant="ghost"
										icon="system"
										label="{p.info.name} konfigurieren"
										href={href(p)}
									/>
								</div>
							</li>
						{/each}
					</ul>
				</section>
			{/if}
		{/each}
	</div>
{/if}
