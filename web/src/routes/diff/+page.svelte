<script lang="ts">
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { DiffResult, PluginView, RunMessageData, RunView } from '$lib/api';
	import DiffView from '$lib/components/diff/DiffView.svelte';
	import DevicePicker from '$lib/components/events/DevicePicker.svelte';
	import { isoParam } from '$lib/components/events/filter';
	import {
		Alert,
		Badge,
		Button,
		Card,
		EmptyState,
		ErrorState,
		Icon,
		Input,
		PageHeader,
		Select,
		Skeleton,
		Tabs
	} from '$lib/components/ui';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { live } from '$lib/stores/live.svelte';
	import {
		formatDateTime,
		formatDuration,
		formatNumber,
		fromDateTimeLocal,
		toDateTimeLocal
	} from '$lib/utils/format';
	import { pluginKindLabel, runTriggerLabel, label } from '$lib/utils/labels';
	import { debounce, intParam, listParam, setParams } from '$lib/utils/url';

	// ---------------------------------------------------------------- URL state
	const sp = $derived(page.url.searchParams);
	const runA = $derived(intParam(sp, 'runA', 0) || null);
	const runB = $derived(intParam(sp, 'runB', 0) || null);
	const from = $derived(isoParam(sp.get('from')));
	const to = $derived(isoParam(sp.get('to')));
	const device = $derived(intParam(sp, 'device', 0) || null);
	const pluginParam = $derived(sp.get('plugin') ?? '');
	const kindsKey = $derived(listParam(sp, 'kind').join(','));
	const kinds = $derived(kindsKey ? kindsKey.split(',') : []);
	const change = $derived(
		['added', 'removed', 'changed'].includes(sp.get('change') ?? '') ? sp.get('change')! : ''
	);
	const textParam = $derived(sp.get('text') ?? '');

	let mode = $state<'runs' | 'time'>(
		untrack(() => (sp.get('from') || sp.get('mode') === 'time' ? 'time' : 'runs'))
	);
	// follow external navigation (links from events, device history …)
	$effect(() => {
		const m = from ? 'time' : runA || runB ? 'runs' : null;
		if (m) untrack(() => (mode = m));
	});

	// ---------------------------------------------------------------- plugins & runs
	const diff = new AsyncData<DiffResult>();

	// the plugin list is only needed for the selector (scanners and importers)
	const plugins = new AsyncData<PluginView[]>();
	$effect(() => {
		plugins.run(async (signal) => (await api.get('/api/v1/plugins', { signal })) ?? []);
	});
	const pluginOptions = $derived.by(() => {
		const opts = (plugins.data ?? [])
			.filter((p) => p.info.kind === 'scanner' || p.info.kind === 'importer')
			.sort(
				(a, b) =>
					Number(b.info.kind === 'scanner') - Number(a.info.kind === 'scanner') ||
					a.info.name.localeCompare(b.info.name, 'de')
			)
			.map((p) => ({ value: p.info.id, label: `${p.info.name} (${label(pluginKindLabel, p.info.kind)})` }));
		const side = diff.data?.b;
		if (plugin && !opts.some((o) => o.value === plugin))
			opts.push({
				value: plugin,
				label: side?.pluginId === plugin && side.pluginName ? side.pluginName : plugin
			});
		return opts;
	});

	// default plugin: the one with the most recent successful full-network scanner run
	const latest = new AsyncData<string>();
	$effect(() => {
		if (pluginParam || runA || runB || mode !== 'runs') return;
		latest.run(
			async (signal) =>
				(
					await api.get('/api/v1/runs', {
						query: { kind: 'scanner', status: 'success', scope: 'full', limit: 1 },
						signal
					})
				).items?.[0]?.pluginId ?? ''
		);
	});

	// plugin of a run given only as ?runB (links "compare with the previous run")
	const runBInfo = new AsyncData<RunView | null>();
	$effect(() => {
		const id = runB;
		if (!id || runA || pluginParam) return;
		runBInfo.run(async (signal) => {
			try {
				return await api.get('/api/v1/runs/{id}', { path: { id }, signal });
			} catch {
				return null;
			}
		});
	});

	const plugin = $derived(
		pluginParam ||
			(runA || runB
				? (diff.data?.a.pluginId ?? (runBInfo.data?.id === runB ? runBInfo.data.pluginId : ''))
				: (latest.data ?? ''))
	);

	// successful full-network runs of the plugin (partial runs only via links)
	const runList = new AsyncData<RunView[]>();
	$effect(() => {
		const p = plugin;
		if (!p || mode !== 'runs') return;
		runList.run(
			async (signal) =>
				(
					await api.get('/api/v1/runs', {
						query: { plugin: p, status: 'success', scope: 'full', limit: 100 },
						signal
					})
				).items ?? [],
			true
		);
	});

	// preselect the two latest runs when nothing is chosen
	$effect(() => {
		const list = runList.data;
		if (mode !== 'runs' || runA || runB || !list || list.length < 2 || !plugin) return;
		untrack(() => setParams({ plugin, runA: list[1].id, runB: list[0].id }));
	});

	/** run before `id` of the plugin (from the loaded list or GET /runs?before=) */
	async function previousRun(id: number, full: boolean): Promise<number | null> {
		const list = runList.data ?? [];
		const i = list.findIndex((r) => r.id === id);
		if (full && i >= 0 && list[i + 1]) return list[i + 1].id;
		if (!plugin) return null;
		try {
			const res = await api.get('/api/v1/runs', {
				query: { plugin, status: 'success', scope: full ? 'full' : null, before: id, limit: 1 }
			});
			return res.items?.[0]?.id ?? null;
		} catch {
			return null;
		}
	}

	// ?runB without runA: compare with the previous successful run
	$effect(() => {
		const b = runB;
		if (mode !== 'runs' || !b || runA || !plugin) return;
		untrack(() =>
			previousRun(b, false).then((a) => {
				if (a) setParams({ plugin, runA: a });
			})
		);
	});

	// new successful runs of the plugin appear in the lists
	$effect(() =>
		live.on<RunMessageData>('run', (m) => {
			if (m.type !== 'finished' || m.data.discarded) return;
			const r = m.data.run;
			if (r && r.pluginId === untrack(() => plugin) && r.status === 'success') runList.reload();
		})
	);

	// a merged device lives on under another id
	$effect(() =>
		live.on<{ id: number; mergedInto?: number }>('device', (m) => {
			if (m.type === 'deleted' && m.data.mergedInto && m.data.id === untrack(() => device))
				setParams({ device: m.data.mergedInto });
		})
	);

	function statsText(r: RunView): string {
		const s = r.stats ?? {};
		for (const k of ['hosts', 'devices', 'observations']) {
			const v = s[k];
			if (typeof v === 'number')
				return `${formatNumber(v)} ${k === 'hosts' ? 'Hosts' : k === 'devices' ? 'Geräte' : 'Beob.'}`;
		}
		return '';
	}
	function runLabel(r: RunView): string {
		const st = statsText(r);
		return `#${r.id} · ${formatDateTime(r.startedAt ?? r.createdAt)}${st ? ` · ${st}` : ''}`;
	}
	const runOptions = $derived.by(() => {
		const list = runList.data ?? [];
		const opts = list.map((r) => ({ value: String(r.id), label: runLabel(r) }));
		// runs from links that are not in the list (older or partial runs)
		for (const side of [diff.data?.a, diff.data?.b]) {
			const id = side?.runId;
			if (side && id && (id === runA || id === runB) && !opts.some((o) => o.value === String(id)))
				opts.push({
					value: String(id),
					label: `#${id} · ${formatDateTime(side.time)}${side.partial ? ' · Teilscan' : ''}`
				});
		}
		for (const id of [runA, runB])
			if (id && !opts.some((o) => o.value === String(id))) opts.push({ value: String(id), label: `#${id}` });
		return opts;
	});

	function setPlugin(p: string) {
		setParams({ plugin: p || null, runA: null, runB: null });
	}
	function setRunA(v: string) {
		setParams({ plugin: plugin || null, runA: v ? Number(v) : null });
	}
	/** choosing run B also picks the previous run as A unless A is already older */
	async function setRunB(v: string) {
		const b = v ? Number(v) : null;
		let a = runA;
		if (b && (!a || a >= b)) a = await previousRun(b, true);
		setParams({ plugin: plugin || null, runB: b, runA: a });
	}
	function swapRuns() {
		setParams({ runA: runB, runB: runA });
	}

	// ---------------------------------------------------------------- time mode
	let fromLocal = $state('');
	let toLocal = $state('');
	let timeError = $state<string | null>(null);
	$effect(() => {
		const f = from;
		const t = to;
		untrack(() => {
			fromLocal = f ? toDateTimeLocal(f) : fromLocal || toDateTimeLocal(Date.now() - 86400e3);
			toLocal = toDateTimeLocal(t);
		});
	});

	const PRESETS = [
		{ label: 'Letzte 24 h', ms: 86400e3 },
		{ label: 'Letzte 7 Tage', ms: 7 * 86400e3 },
		{ label: 'Letzte 30 Tage', ms: 30 * 86400e3 }
	];

	function applyPreset(ms: number) {
		timeError = null;
		setParams({
			mode: null,
			runA: null,
			runB: null,
			plugin: null,
			from: new Date(Date.now() - ms).toISOString(),
			to: null
		});
	}

	function applyTime() {
		timeError = null;
		const f = fromDateTimeLocal(fromLocal);
		const t = fromDateTimeLocal(toLocal);
		if (!f) {
			timeError = 'Startzeitpunkt angeben';
			return;
		}
		if (t && new Date(f) >= new Date(t)) {
			timeError = '„Von“ muss vor „Bis“ liegen';
			return;
		}
		if (new Date(f).getTime() > Date.now()) {
			timeError = '„Von“ liegt in der Zukunft';
			return;
		}
		setParams({ mode: null, runA: null, runB: null, plugin: null, from: f, to: t || null });
	}

	function setMode(m: string) {
		mode = m === 'time' ? 'time' : 'runs';
		if (mode === 'time') setParams({ mode: from ? null : 'time', runA: null, runB: null, plugin: null });
		else setParams({ mode: null, from: null, to: null });
	}

	// ---------------------------------------------------------------- diff
	const sameRun = $derived(mode === 'runs' && !!runA && runA === runB);
	const request = $derived.by(() => {
		if (mode === 'runs' && runA && runB && runA !== runB) return { runA, runB, device };
		if (mode === 'time' && from) return { from, to: to || null, device };
		return null;
	});
	const requestKey = $derived(JSON.stringify(request));
	$effect(() => {
		const q = JSON.parse(requestKey) as typeof request;
		if (!q) {
			diff.abort();
			return;
		}
		diff.run((signal) => api.get('/api/v1/diff', { query: q, signal }), true);
	});

	const result = $derived(request ? diff.data : undefined);
	const differentPlugins = $derived(
		!!result && result.a.kind === 'run' && !!result.a.pluginId && result.a.pluginId !== result.b.pluginId
	);
	const partialSide = $derived(
		result?.a.kind === 'run' ? [result.a, result.b].find((x) => x.partial) : undefined
	);

	// ---------------------------------------------------------------- result filters (URL)
	let textInput = $state(untrack(() => sp.get('text') ?? ''));
	$effect(() => {
		const t = textParam;
		untrack(() => {
			if (t !== textInput.trim()) textInput = t;
		});
	});
	const applyText = debounce((t: string) => setParams({ text: t.trim() || null }), 300);
	$effect(() => () => applyText.cancel());

	const selectedRunB = $derived(runList.data?.find((r) => r.id === runB));
	const olderFirst = $derived(!result || result.a.kind !== 'run' || result.a.time <= result.b.time);
</script>

<PageHeader title="Diff" description="Zwei Läufe oder zwei Zeitpunkte gegeneinander vergleichen" />

<div class="flex flex-col gap-4">
	<Card padding="none">
		<Tabs
			items={[
				{ id: 'runs', label: 'Läufe vergleichen', icon: 'play' },
				{ id: 'time', label: 'Zeitpunkte vergleichen', icon: 'clock' }
			]}
			active={mode}
			onchange={setMode}
			label="Vergleichsart"
			idPrefix="diff-"
			class="px-4 pt-1"
		/>
		<div class="p-4" role="tabpanel" id="diff-panel-{mode}" aria-labelledby="diff-tab-{mode}">
			{#if mode === 'runs'}
				<div
					class="grid grid-cols-1 items-start gap-3 md:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)_auto_minmax(0,1.2fr)]"
				>
					<Select
						label="Plugin"
						options={pluginOptions}
						value={plugin}
						placeholder={plugins.loading && !plugins.data ? 'Lädt …' : 'Plugin wählen …'}
						onchange={(e) => setPlugin(e.currentTarget.value)}
						hint="Scanner und Importer"
					/>
					<Select
						label="Lauf A (älter)"
						options={runOptions}
						value={runA ? String(runA) : ''}
						placeholder={runList.loading ? 'Lädt …' : 'Lauf wählen …'}
						onchange={(e) => setRunA(e.currentTarget.value)}
						disabled={!plugin}
					/>
					<Button
						icon="swap"
						label="Läufe tauschen"
						class="hidden xl:mt-6 xl:inline-flex"
						disabled={!runA || !runB}
						onclick={swapRuns}
					/>
					<Select
						label="Lauf B (neuer)"
						options={runOptions}
						value={runB ? String(runB) : ''}
						placeholder={runList.loading ? 'Lädt …' : 'Lauf wählen …'}
						onchange={(e) => setRunB(e.currentTarget.value)}
						disabled={!plugin}
						error={sameRun ? 'Zwei verschiedene Läufe wählen' : null}
					/>
				</div>
				<div class="mt-3 flex flex-wrap items-center gap-2">
					<Button size="sm" icon="swap" class="xl:hidden" disabled={!runA || !runB} onclick={swapRuns}
						>Tauschen</Button
					>
					<DevicePicker value={device} size="sm" onchange={(id) => setParams({ device: id })} />
					{#if !olderFirst}
						<span class="flex items-center gap-1 text-xs text-warn">
							<Icon name="alert" size={13} /> Lauf A ist neuer als Lauf B – „neu“ und „entfernt“ sind vertauscht.
						</span>
					{/if}
				</div>
				{#if runList.error}
					<ErrorState error={runList.error} compact class="mt-3" onretry={() => runList.reload()} />
				{:else if plugin && runList.data && runList.data.length < 2 && !runA}
					<p class="mt-3 text-sm text-fg-muted">
						Für dieses Plugin gibt es noch keine zwei erfolgreichen Läufe.
					</p>
				{/if}
			{:else}
				<form
					class="flex flex-wrap items-start gap-3"
					onsubmit={(e) => {
						e.preventDefault();
						applyTime();
					}}
				>
					<Input type="datetime-local" label="Von" bind:value={fromLocal} required class="w-full sm:w-56" />
					<Input
						type="datetime-local"
						label="Bis"
						bind:value={toLocal}
						hint="leer = jetzt"
						class="w-full sm:w-56"
					/>
					<Button type="submit" variant="primary" icon="diff" class="sm:mt-6">Vergleichen</Button>
					<div class="flex flex-wrap gap-1.5 sm:mt-6">
						{#each PRESETS as p (p.label)}
							<Button size="sm" variant="subtle" onclick={() => applyPreset(p.ms)}>{p.label}</Button>
						{/each}
					</div>
				</form>
				{#if timeError}<p class="mt-2 text-xs text-danger" role="alert">{timeError}</p>{/if}
				<div class="mt-3 flex flex-wrap items-center gap-2">
					<DevicePicker value={device} size="sm" onchange={(id) => setParams({ device: id })} />
				</div>
			{/if}
		</div>
	</Card>

	{#if !request}
		{#if !(mode === 'runs' && (latest.loading || runList.loading))}
			<EmptyState
				icon="diff"
				title={mode === 'runs' ? 'Zwei Läufe wählen' : 'Zeitraum wählen'}
				description={mode === 'runs'
					? 'Vergleicht die Beobachtungen zweier erfolgreicher Läufe eines Plugins.'
					: 'Vergleicht den Inventarzustand zu zwei Zeitpunkten (Geräte, IPs, Ports, Zertifikate, Pakete, Container …).'}
			/>
		{:else}
			<Skeleton rows={4} />
		{/if}
	{:else if diff.error}
		<ErrorState error={diff.error} onretry={() => diff.reload()} />
	{:else if !result}
		<Skeleton rows={6} />
	{:else}
		<!-- compared sides -->
		<div class="grid grid-cols-1 items-stretch gap-2 sm:grid-cols-[1fr_auto_1fr]">
			{#each [result.a, result.b] as side, i (i)}
				{#if i === 1}
					<div class="hidden items-center justify-center text-fg-subtle sm:flex" aria-hidden="true">
						<Icon name="arrow-right" size={18} />
					</div>
				{/if}
				<div class="rounded-lg border border-border bg-surface px-4 py-2.5 shadow-sm">
					<div class="text-xs font-semibold tracking-wide text-fg-subtle uppercase">
						{i === 0 ? 'A' : 'B'}
					</div>
					{#if side.kind === 'run'}
						<div class="font-medium text-fg">
							{#if side.pluginId}
								<a
									class="hover:text-accent hover:underline"
									href="/plugins/{side.pluginId}/runs/{side.runId}"
								>
									{side.pluginName || side.pluginId} · Lauf #{side.runId}
								</a>
							{:else}
								Lauf #{side.runId}
							{/if}
						</div>
						<div class="text-sm text-fg-muted">
							{formatDateTime(side.time, true)}
							{#if side.partial}
								<Badge tone="warn" class="ml-1" title="Der Lauf umfasste nur ausgewählte Geräte"
									>Teilscan</Badge
								>
							{/if}
							{#if side.finished}
								<span class="text-fg-subtle">
									· {formatDuration(new Date(side.finished).getTime() - new Date(side.time).getTime())}</span
								>
							{/if}
						</div>
					{:else}
						<div class="font-medium text-fg">{formatDateTime(side.time, true)}</div>
						<div class="text-sm text-fg-muted">Inventarzustand</div>
					{/if}
				</div>
			{/each}
		</div>

		{#if partialSide}
			<Alert tone="warn">
				Lauf #{partialSide.runId} umfasste nur ausgewählte Geräte – Geräte außerhalb dieses Laufs erscheinen als
				„neu“ bzw. „entfernt“.{#if !device}{' '}Mit einem Gerätefilter lässt sich der Vergleich auf ein
					erfasstes Gerät beschränken.{/if}
			</Alert>
		{/if}
		{#if differentPlugins}
			<Alert tone="warn">
				Die Läufe stammen von verschiedenen Plugins – Unterschiede können aus den unterschiedlichen
				Datenquellen kommen.
			</Alert>
		{/if}
		{#if mode === 'runs' && selectedRunB}
			<p class="-mt-2 text-xs text-fg-subtle">
				Lauf B: {label(runTriggerLabel, selectedRunB.trigger)}{selectedRunB.requestedBy &&
				selectedRunB.requestedBy !== label(runTriggerLabel, selectedRunB.trigger)
					? ` · ${selectedRunB.requestedBy}`
					: ''} · „Entfernt“ bedeutet: im neueren Lauf nicht mehr beobachtet.
			</p>
		{/if}

		<DiffView
			{result}
			{kinds}
			{change}
			bind:text={textInput}
			onkinds={(k) => setParams({ kind: k })}
			onchange={(c) => setParams({ change: c || null })}
			ontext={(t) => applyText(t)}
			onreset={() => {
				applyText.cancel();
				setParams({ kind: null, change: null, text: null });
			}}
		/>
	{/if}
</div>
