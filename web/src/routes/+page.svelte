<script lang="ts">
	import { api } from '$lib/api';
	import type { Dashboard, DashboardPluginStatus, RunView } from '$lib/api';
	import {
		Badge,
		Button,
		Card,
		EmptyState,
		ErrorState,
		Icon,
		PageHeader,
		ProgressBar,
		RelativeTime,
		SeverityBadge,
		Skeleton,
		StatCard,
		StatusDot
	} from '$lib/components/ui';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { debounce } from '$lib/utils/url';
	import { formatDuration, formatNumber, formatPercent, formatDateTime } from '$lib/utils/format';
	import {
		pluginKindLabel,
		runStatusLabel,
		runStatusTone,
		severityLabel,
		runTriggerLabel
	} from '$lib/utils/labels';

	const data = new AsyncData<Dashboard>();
	$effect(() => {
		data.run((signal) => api.get('/api/v1/dashboard', { signal }));
	});

	// live refresh: coalesce bursts of messages into one reload
	const refresh = debounce(() => data.reload(), 1500);
	$effect(() => live.on('device', refresh));
	$effect(() => live.on('event', refresh));
	$effect(() => live.on('plugin', refresh));
	$effect(() => live.on('health', refresh));
	$effect(() => live.onReconnect(refresh));
	$effect(() => runs.onFinished(refresh));
	$effect(() => () => refresh.cancel());

	const d = $derived(data.data);
	const sevOrder = ['critical', 'high', 'medium', 'low', 'info'] as const;
	const openTotal = $derived(d ? Object.values(d.openEvents ?? {}).reduce((a, b) => a + b, 0) : 0);
	const openMax = $derived(d ? Math.max(1, ...Object.values(d.openEvents ?? {})) : 1);
	const cveTotal = $derived(d ? Object.values(d.cves ?? {}).reduce((a, b) => a + b, 0) : 0);
	const cveMax = $derived(d ? Math.max(1, ...Object.values(d.cves ?? {})) : 1);
	const healthTotal = $derived(d ? Object.values(d.health ?? {}).reduce((a, b) => a + b, 0) : 0);

	const kindOrder = ['scanner', 'importer', 'processor', 'publisher'];
	const pluginGroups = $derived.by(() => {
		const list = [...(d?.plugins ?? [])];
		return kindOrder
			.map((k) => ({
				kind: k,
				items: list
					.filter((p) => p.kind === k)
					.sort((a, b) => Number(b.enabled) - Number(a.enabled) || a.name.localeCompare(b.name, 'de'))
			}))
			.filter((g) => g.items.length);
	});

	function runningOf(p: DashboardPluginStatus): RunView | undefined {
		return runs.active.find((r) => r.pluginId === p.id && r.status === 'running');
	}

	function pluginState(p: DashboardPluginStatus): { status: string; text: string } {
		const r = runningOf(p);
		if (r || p.running) return { status: 'running', text: 'läuft' };
		if (!p.enabled) return { status: 'idle', text: 'inaktiv' };
		if (p.lastStatus === 'failed' || p.lastStatus === 'timeout')
			return { status: 'danger', text: runStatusLabel[p.lastStatus] };
		if (p.lastStatus === 'success') return { status: 'ok', text: 'OK' };
		return {
			status: 'idle',
			text: p.lastStatus ? (runStatusLabel[p.lastStatus] ?? p.lastStatus) : 'noch nicht gelaufen'
		};
	}

	const sevBar: Record<string, string> = {
		critical: 'bg-sev-critical',
		high: 'bg-sev-high',
		medium: 'bg-sev-medium',
		low: 'bg-sev-low',
		info: 'bg-sev-info'
	};
</script>

<PageHeader title="Dashboard" description="Zustand des Netzwerks auf einen Blick">
	{#snippet actions()}
		{#if d}
			<span class="text-xs text-fg-subtle">Stand <RelativeTime value={d.generatedAt} /></span>
		{/if}
		<Button
			size="sm"
			icon="refresh"
			label="Aktualisieren"
			loading={data.loading && !!d}
			onclick={() => data.reload()}
		/>
	{/snippet}
</PageHeader>

{#if data.error && !d}
	<ErrorState error={data.error} onretry={() => data.reload()} />
{:else}
	<!-- device counters -->
	<section aria-label="Geräte" class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5">
		<StatCard
			label="Geräte"
			value={d?.devices.total}
			icon="devices"
			tone="accent"
			href="/devices"
			loading={!d}
		>
			{#if d?.devices.ignored}{formatNumber(d.devices.ignored)} ignoriert{:else}im Inventar{/if}
		</StatCard>
		<StatCard
			label="Online"
			value={d?.devices.online}
			icon="wifi"
			tone="ok"
			href="/devices?q=is:online"
			loading={!d}
		>
			{#if d && d.devices.total}{formatPercent(
					(d.devices.online / Math.max(1, d.devices.total - d.devices.ignored)) * 100,
					0
				)} erreichbar{/if}
		</StatCard>
		<StatCard
			label="Offline"
			value={d?.devices.offline}
			icon="x-circle"
			tone={d?.devices.offline ? 'warn' : 'neutral'}
			href="/devices?q=is:offline"
			loading={!d}
		>
			nicht mehr gesehen
		</StatCard>
		<StatCard
			label="Neu (24 h)"
			value={d?.devices.new24h}
			icon="plus"
			tone="accent"
			href="/devices?q=first<24h"
			loading={!d}
		>
			erstmals entdeckt
		</StatCard>
		<StatCard
			label="Unbekannt"
			value={d?.devices.unknown}
			icon="info"
			tone={d?.devices.unknown ? 'unknown' : 'neutral'}
			href="/devices?q=state:unknown"
			loading={!d}
		>
			noch nicht als bekannt markiert
		</StatCard>
	</section>

	<div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2 2xl:grid-cols-3">
		<!-- events -->
		<Card title="Offene Events" icon="events">
			{#snippet actions()}
				<Button size="xs" variant="ghost" href="/events?acked=0" iconRight="arrow-right">Alle</Button>
			{/snippet}
			{#if !d}
				<Skeleton lines={5} />
			{:else}
				<div class="flex items-baseline gap-2">
					<span class="text-3xl font-semibold tracking-tight">{formatNumber(openTotal)}</span>
					<span class="text-sm text-fg-muted">nicht quittiert</span>
				</div>
				<ul class="mt-3 flex flex-col gap-1.5">
					{#each sevOrder as s (s)}
						{@const n = d.openEvents?.[s] ?? 0}
						<li>
							<a
								href="/events?severity={s}&acked=0"
								class="group grid grid-cols-[5.5rem_1fr_3rem] items-center gap-3 rounded text-sm"
							>
								<span class="text-fg-muted group-hover:text-fg">{severityLabel[s]}</span>
								<span class="h-2 overflow-hidden rounded-full bg-surface-3">
									<span
										class="block h-full rounded-full {sevBar[s]}"
										style="width:{n ? Math.max(2, (n / openMax) * 100) : 0}%"
									></span>
								</span>
								<span class="text-right font-medium tabular">{formatNumber(n)}</span>
							</a>
						</li>
					{/each}
				</ul>
				<h3 class="mt-5 mb-2 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
					Neueste kritische & hohe
				</h3>
				{#if (d.criticalEvents ?? []).length === 0}
					<p class="flex items-center gap-2 text-sm text-fg-muted">
						<Icon name="check-circle" size={16} class="text-ok" /> Keine offenen kritischen Events
					</p>
				{:else}
					<ul class="-mx-2 flex flex-col">
						{#each d.criticalEvents.slice(0, 6) as ev (ev.id)}
							<li>
								<a
									href="/events?id={ev.id}"
									class="flex items-start gap-2.5 rounded px-2 py-1.5 hover:bg-surface-2"
								>
									<SeverityBadge severity={ev.severity} class="mt-0.5" />
									<span class="min-w-0 flex-1">
										<span class="block truncate text-sm text-fg">{ev.title}</span>
										<span class="block truncate text-xs text-fg-subtle">
											{ev.deviceName || ev.label} · <RelativeTime value={ev.ts} />
										</span>
									</span>
								</a>
							</li>
						{/each}
					</ul>
				{/if}
			{/if}
		</Card>

		<!-- health -->
		<Card title="Health" icon="health">
			{#snippet actions()}
				<Button size="xs" variant="ghost" href="/health" iconRight="arrow-right">Statusboard</Button>
			{/snippet}
			{#if !d}
				<Skeleton lines={4} />
			{:else if healthTotal === 0}
				<EmptyState
					compact
					icon="health"
					title="Keine Health-Checks"
					description="Checks werden in der Gerätedetailansicht oder unter Health angelegt."
				/>
			{:else}
				<div class="flex items-baseline gap-2">
					<span class="text-3xl font-semibold tracking-tight">
						{d.availability24h !== undefined && d.availability24h !== null
							? formatPercent(d.availability24h, 2)
							: '–'}
					</span>
					<span class="text-sm text-fg-muted">Ø Verfügbarkeit 24 h</span>
				</div>
				<div class="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
					{#each [['up', 'Up', 'ok'], ['degraded', 'Beeinträchtigt', 'warn'], ['down', 'Down', 'danger'], ['unknown', 'Unbekannt', 'neutral']] as [k, lbl, tone] (k)}
						<a href="/health" class="rounded-md border border-border px-3 py-2 hover:bg-surface-2">
							<span class="flex items-center gap-1.5 text-xs text-fg-muted">
								<StatusDot
									status={tone === 'ok'
										? 'up'
										: tone === 'warn'
											? 'degraded'
											: tone === 'danger'
												? 'down'
												: 'idle'}
									pulse={false}
								/>
								{lbl}
							</span>
							<span class="mt-0.5 block text-xl font-semibold tabular"
								>{formatNumber(d.health?.[k] ?? 0)}</span
							>
						</a>
					{/each}
				</div>
			{/if}
		</Card>

		<!-- CVEs -->
		<Card title="Schwachstellen" icon="shield">
			{#snippet actions()}
				<Button size="xs" variant="ghost" href="/vulnerabilities" iconRight="arrow-right">Alle</Button>
			{/snippet}
			{#if !d}
				<Skeleton lines={5} />
			{:else if cveTotal === 0}
				<EmptyState
					compact
					icon="shield"
					title="Keine offenen CVEs"
					description="Es sind keine (nicht ignorierten) Schwachstellen bekannt."
				/>
			{:else}
				<ul class="flex flex-col gap-1.5">
					{#each ['critical', 'high', 'medium', 'low'] as s (s)}
						{@const n = d.cves?.[s] ?? 0}
						<li class="grid grid-cols-[5.5rem_1fr_3rem] items-center gap-3 text-sm">
							<span class="text-fg-muted">{severityLabel[s]}</span>
							<span class="h-2 overflow-hidden rounded-full bg-surface-3">
								<span
									class="block h-full rounded-full {sevBar[s]}"
									style="width:{n ? Math.max(2, (n / cveMax) * 100) : 0}%"
								></span>
							</span>
							<span class="text-right font-medium tabular" title="Betroffene Gerät/CVE-Paare"
								>{formatNumber(n)}</span
							>
						</li>
					{/each}
				</ul>
				{#if (d.topCves ?? []).length}
					<h3 class="mt-5 mb-1.5 text-xs font-semibold tracking-wide text-fg-subtle uppercase">Top-CVEs</h3>
					<ul class="-mx-2">
						{#each d.topCves.slice(0, 6) as c (c.cve)}
							<li>
								<a
									href="/vulnerabilities?q={encodeURIComponent(c.cve)}"
									class="flex items-center gap-2.5 rounded px-2 py-1 hover:bg-surface-2"
								>
									<SeverityBadge cvss={c.cvss} />
									<span class="mono flex-1 truncate text-sm">{c.cve}</span>
									<span class="text-xs text-fg-subtle"
										>{c.devices} {c.devices === 1 ? 'Gerät' : 'Geräte'}</span
									>
								</a>
							</li>
						{/each}
					</ul>
				{/if}
			{/if}
		</Card>

		<!-- running scans -->
		<Card title="Laufende Scans" icon="radar">
			{#snippet actions()}
				{#if runs.active.length}<Badge tone="accent" dot>{runs.active.length} aktiv</Badge>{/if}
			{/snippet}
			{#if runs.active.length === 0}
				<p class="flex items-center gap-2 text-sm text-fg-muted">
					<Icon name="check" size={16} /> Zurzeit läuft kein Scan.
				</p>
				{#if runs.recent.length}
					<h3 class="mt-4 mb-1.5 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
						Zuletzt beendet
					</h3>
					<ul class="flex flex-col gap-1">
						{#each runs.recent.slice(0, 5) as r (r.id)}
							<li class="flex items-center gap-2 text-sm">
								<span class="min-w-0 flex-1 truncate">{r.pluginName}</span>
								<Badge tone={runStatusTone(r.status)}>{runStatusLabel[r.status] ?? r.status}</Badge>
								<span class="w-20 text-right text-xs text-fg-subtle tabular"
									>{formatDuration(r.durationMs)}</span
								>
							</li>
						{/each}
					</ul>
				{/if}
			{:else}
				<ul class="flex flex-col gap-3">
					{#each runs.active as r (r.id)}
						<li>
							<div class="flex items-center gap-2 text-sm">
								<StatusDot status={r.status === 'running' ? 'running' : 'queued'} />
								<span class="min-w-0 flex-1 truncate font-medium">{r.pluginName}</span>
								<span class="text-xs text-fg-subtle">{runTriggerLabel[r.trigger] ?? r.trigger}</span>
								<Badge tone={runStatusTone(r.status)}>{runStatusLabel[r.status] ?? r.status}</Badge>
							</div>
							{#if r.status === 'running'}
								<div class="mt-1.5 flex items-center gap-2 text-xs text-fg-subtle">
									<ProgressBar done={r.progress?.done} total={r.progress?.total} tone="live" class="flex-1" />
									<span class="w-24 text-right tabular">
										{#if r.progress?.total}{r.progress.done} / {r.progress.total}{:else}{formatDuration(
												r.durationMs
											)}{/if}
									</span>
								</div>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</Card>

		<!-- certificates -->
		<Card title="Zertifikate (≤ 30 Tage)" icon="lock">
			{#if !d}
				<Skeleton lines={4} />
			{:else if (d.certificates ?? []).length === 0}
				<EmptyState
					compact
					icon="lock"
					title="Keine ablaufenden Zertifikate"
					description="Kein aktives Zertifikat läuft in den nächsten 30 Tagen ab."
				/>
			{:else}
				<ul class="-mx-2">
					{#each d.certificates as c (c.id)}
						<li>
							<a
								href="/devices/{c.deviceId}?tab=certs"
								class="flex items-center gap-3 rounded px-2 py-1.5 hover:bg-surface-2"
							>
								<Badge
									tone={c.daysLeft < 0 ? 'critical' : c.daysLeft <= 7 ? 'high' : 'medium'}
									class="w-20 justify-center"
								>
									{c.daysLeft < 0 ? 'abgelaufen' : `${c.daysLeft} Tage`}
								</Badge>
								<span class="min-w-0 flex-1">
									<span class="block truncate text-sm">{c.subjectCn || c.serverName || c.ip}</span>
									<span class="block truncate text-xs text-fg-subtle">
										{c.deviceName} · <span class="mono">{c.ip}:{c.port}</span> · {formatDateTime(c.notAfter)}
									</span>
								</span>
							</a>
						</li>
					{/each}
				</ul>
			{/if}
		</Card>

		<!-- subnets -->
		<Card title="Subnetze" icon="network">
			{#snippet actions()}
				<Button size="xs" variant="ghost" href="/system" iconRight="arrow-right">Verwalten</Button>
			{/snippet}
			{#if !d}
				<Skeleton lines={3} />
			{:else if (d.subnets ?? []).length === 0}
				<EmptyState
					compact
					icon="network"
					title="Keine Subnetze"
					description="Subnetze werden unter System gepflegt."
				/>
			{:else}
				<ul class="-mx-2">
					{#each d.subnets as s (s.id)}
						<li>
							<a
								href="/devices?q={encodeURIComponent('subnet:' + s.cidr)}"
								class="flex items-center gap-3 rounded px-2 py-1.5 hover:bg-surface-2"
							>
								<span class="mono text-sm">{s.cidr}</span>
								<span class="min-w-0 flex-1 truncate text-sm text-fg-muted"
									>{s.name}{s.vlan ? ` · VLAN ${s.vlan}` : ''}</span
								>
								{#if !s.enabled}<Badge>inaktiv</Badge>{/if}
								<span class="text-sm tabular">{formatNumber(s.deviceCount)}</span>
								<span class="text-xs text-fg-subtle">Geräte</span>
							</a>
						</li>
					{/each}
				</ul>
			{/if}
		</Card>
	</div>

	<!-- plugins -->
	<Card title="Plugins" icon="plugins" class="mt-4">
		{#snippet actions()}
			{#if d?.pluginsFailed}
				<Badge tone="danger" dot>{d.pluginsFailed} fehlgeschlagen</Badge>
			{/if}
			<Button size="xs" variant="ghost" href="/plugins" iconRight="arrow-right">Verwalten</Button>
		{/snippet}
		{#if !d}
			<Skeleton lines={6} />
		{:else}
			<div class="flex flex-col gap-5">
				{#each pluginGroups as g (g.kind)}
					<div>
						<h3 class="mb-2 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
							{pluginKindLabel[g.kind] ?? g.kind}
						</h3>
						<ul class="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
							{#each g.items as p (p.id)}
								{@const st = pluginState(p)}
								{@const r = runningOf(p)}
								<li>
									<a
										href="/plugins/{p.id}"
										class="flex h-full flex-col gap-1 rounded-md border border-border px-3 py-2 transition-colors hover:border-border-strong hover:bg-surface-2
											{p.enabled ? '' : 'opacity-60'}"
									>
										<span class="flex items-center gap-2">
											<StatusDot status={st.status} />
											<span class="min-w-0 flex-1 truncate text-sm font-medium">{p.name}</span>
											<span class="text-xs {st.status === 'danger' ? 'text-danger' : 'text-fg-subtle'}"
												>{st.text}</span
											>
										</span>
										{#if r}
											<span class="flex items-center gap-2 text-xs text-fg-subtle">
												<ProgressBar
													done={r.progress?.done}
													total={r.progress?.total}
													tone="live"
													class="flex-1"
												/>
												{#if r.progress?.total}<span class="tabular"
														>{r.progress.done}/{r.progress.total}</span
													>{/if}
											</span>
										{:else}
											<span class="truncate text-xs text-fg-subtle">
												{#if p.lastRunAt}zuletzt <RelativeTime value={p.lastRunAt} />{:else}noch kein Lauf{/if}
												{#if p.enabled && p.nextRun}
													· nächster <RelativeTime value={p.nextRun} />{/if}
											</span>
										{/if}
										{#if p.lastError && st.status === 'danger'}
											<span class="truncate text-xs text-danger" title={p.lastError}>{p.lastError}</span>
										{/if}
									</a>
								</li>
							{/each}
						</ul>
					</div>
				{/each}
			</div>
		{/if}
	</Card>
{/if}
