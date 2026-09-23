<!--
	NVD mirror status with sync/match actions and live run progress.
	<SyncStatusCard status={status.data} error={status.error} onretry={…} />
	The page reloads its data on finished cve runs itself.
-->
<script lang="ts">
	import { api, errorMessage } from '$lib/api';
	import type { RunView } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		Card,
		Checkbox,
		ErrorState,
		Modal,
		ProgressBar,
		RelativeTime,
		Skeleton
	} from '$lib/components/ui';
	import type { ApiVulnStatus } from '$lib/api/generated';
	import { runs } from '$lib/stores/runs.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatBytes, formatDateTime, formatNumber } from '$lib/utils/format';
	import { runStatusLabel } from '$lib/utils/labels';
	import { feedStatusLabel, feedStatusTone, syncModeLabel } from './cve';

	interface Props {
		status: ApiVulnStatus | undefined;
		error?: unknown;
		onretry?: () => void;
		class?: string;
	}

	let { status, error, onretry, class: klass = '' }: Props = $props();

	const uid = $props.id();
	let showFeeds = $state(false);
	let syncOpen = $state(false);
	let fullSync = $state(false);
	let pending = $state<'sync' | 'match' | null>(null);
	let tracked = new Set<number>();
	let starting = $state<number | null>(null);

	const active = $derived(runs.forPlugin('cve'));

	function actionOf(r: RunView): string {
		const a = (r.params as Record<string, unknown> | undefined)?._action;
		if (a === 'sync') return 'NVD-Synchronisation';
		if (a === 'match') return 'Abgleich';
		return 'Synchronisation & Abgleich';
	}

	$effect(() =>
		runs.onFinished((r) => {
			if (!tracked.has(r.id)) return;
			tracked.delete(r.id);
			if (starting === r.id) starting = null;
			const msg = (r.stats?.result as { message?: string } | undefined)?.message;
			if (r.status === 'success')
				toast.success(msg || 'Lauf abgeschlossen', { title: actionOf(r), timeout: 9000 });
			else toast.error(r.error || runStatusLabel[r.status] || r.status, { title: actionOf(r) });
		})
	);

	async function runAction(action: 'sync' | 'match', params?: Record<string, unknown>) {
		pending = action;
		try {
			// wait=0: returns at once with {runId, status:"queued"}; progress arrives via the runs store
			const out = await api.post('/api/v1/plugins/{id}/actions/{action}', {
				path: { id: 'cve', action },
				query: { wait: 0 },
				body: params ? { params } : {}
			});
			tracked.add(out.runId);
			// keep the buttons disabled until the run shows up in the runs store
			starting = out.runId;
			setTimeout(() => starting === out.runId && (starting = null), 15_000);
			toast.info(`Lauf #${out.runId} gestartet – der Fortschritt erscheint hier.`, {
				title: action === 'sync' ? 'NVD-Synchronisation' : 'Abgleich'
			});
		} catch (e) {
			toast.error(errorMessage(e), {
				title: action === 'sync' ? 'Synchronisation nicht gestartet' : 'Abgleich nicht gestartet'
			});
		} finally {
			pending = null;
		}
	}

	function startSync() {
		syncOpen = false;
		runAction('sync', { full: fullSync });
	}

	const busy = $derived(pending !== null || starting !== null || active.length > 0);
	$effect(() => {
		if (starting !== null && active.some((r) => r.id === starting)) starting = null;
	});
</script>

<Card
	title="NVD-Spiegel"
	description="Lokale Kopie der NVD-Datenbank und letzter Abgleich"
	icon="cloud"
	class={klass}
>
	{#snippet actions()}
		<Button size="sm" variant="ghost" href="/plugins/cve" iconRight="arrow-right">Plugin</Button>
	{/snippet}
	{#if error && !status}
		<ErrorState compact {error} {onretry} />
	{:else if !status}
		<Skeleton lines={5} />
	{:else}
		<div class="flex flex-col gap-4">
			{#if status.empty}
				<Alert tone="info" title="Die lokale NVD-Kopie ist leer">
					Ohne Synchronisation kann der Abgleich keine CVEs finden. Der erste Import lädt alle Jahres-Feeds
					der NVD herunter und dauert einige Minuten.
				</Alert>
			{/if}
			{#if status.syncStatus === 'error' && status.syncError}
				<Alert tone="danger" title="Letzte Synchronisation fehlgeschlagen">{status.syncError}</Alert>
			{/if}
			{#if status.lastMatch?.status === 'error' && status.lastMatch.error}
				<Alert tone="danger" title="Letzter Abgleich fehlgeschlagen">{status.lastMatch.error}</Alert>
			{/if}

			<dl class="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
				<div>
					<dt class="text-xs text-fg-subtle">CVEs im Spiegel</dt>
					<dd class="text-lg font-semibold tabular">{formatNumber(status.cveCount)}</dd>
				</div>
				<div>
					<dt class="text-xs text-fg-subtle">CPE-Einträge</dt>
					<dd class="text-lg font-semibold tabular">{formatNumber(status.cpeMatchCount)}</dd>
				</div>
				<div>
					<dt class="text-xs text-fg-subtle">Letzte Synchronisation</dt>
					<dd class="flex flex-wrap items-center gap-1.5">
						<RelativeTime value={status.lastSync} fallback="noch nie" />
						{#if status.syncMode}<Badge>{syncModeLabel[status.syncMode] ?? status.syncMode}</Badge>{/if}
						{#if status.syncStatus}
							<Badge tone={feedStatusTone(status.syncStatus)}
								>{feedStatusLabel[status.syncStatus] ?? status.syncStatus}</Badge
							>
						{/if}
					</dd>
				</div>
				<div>
					<dt class="text-xs text-fg-subtle">Letzter Abgleich</dt>
					<dd class="flex flex-wrap items-center gap-1.5">
						<RelativeTime value={status.lastMatch?.at} fallback="noch nie" />
						{#if status.lastMatch}
							<Badge tone={status.lastMatch.status === 'ok' ? 'ok' : 'danger'}
								>{status.lastMatch.status === 'ok' ? 'OK' : 'Fehler'}</Badge
							>
						{/if}
					</dd>
					{#if status.lastMatch}
						<dd class="text-xs text-fg-subtle">
							{formatNumber(status.lastMatch.devices)} Geräte · {formatNumber(status.lastMatch.activeCves)} aktive
							Treffer
						</dd>
					{/if}
				</div>
			</dl>

			{#if active.length}
				<ul
					class="flex flex-col gap-2 rounded-md border border-border bg-surface-2 px-3 py-2.5"
					aria-live="polite"
				>
					{#each active as r (r.id)}
						<li class="flex flex-col gap-1.5">
							<div class="flex items-center gap-2 text-sm">
								<span class="font-medium">{actionOf(r)}</span>
								<Badge tone="accent" dot>{runStatusLabel[r.status] ?? r.status}</Badge>
								<span class="ml-auto text-xs text-fg-subtle tabular">
									{#if r.progress?.total}{formatNumber(r.progress.done)} / {formatNumber(
											r.progress.total
										)}{:else if r.startedAt}gestartet
										<RelativeTime value={r.startedAt} />{/if}
								</span>
							</div>
							<ProgressBar
								done={r.progress?.done}
								total={r.progress?.total}
								tone="live"
								label="Fortschritt {actionOf(r)}"
							/>
						</li>
					{/each}
				</ul>
			{/if}

			<div class="flex flex-wrap gap-2">
				<Button
					variant={status.empty ? 'primary' : 'secondary'}
					icon="download"
					loading={pending === 'sync'}
					disabled={busy}
					onclick={() => {
						fullSync = false;
						syncOpen = true;
					}}>NVD jetzt synchronisieren</Button
				>
				<Button
					icon="refresh"
					loading={pending === 'match'}
					disabled={busy || status.empty}
					onclick={() => runAction('match')}>Abgleich jetzt ausführen</Button
				>
			</div>

			<div>
				<button
					type="button"
					class="inline-flex items-center gap-1 text-xs font-medium text-fg-muted hover:text-fg"
					aria-expanded={showFeeds}
					aria-controls="{uid}-feeds"
					onclick={() => (showFeeds = !showFeeds)}
				>
					{showFeeds ? 'Feeds ausblenden' : `Feeds anzeigen (${status.feeds?.length ?? 0})`}
				</button>
				<div
					id="{uid}-feeds"
					hidden={!showFeeds}
					class="mt-2 max-h-80 overflow-auto rounded-md border border-border"
				>
					<table class="w-full text-xs">
						<caption class="sr-only">NVD-Feeds</caption>
						<thead class="sticky top-0 bg-surface-2 text-fg-muted">
							<tr>
								<th scope="col" class="px-2 py-1.5 text-left font-medium">Feed</th>
								<th scope="col" class="px-2 py-1.5 text-left font-medium">Status</th>
								<th scope="col" class="px-2 py-1.5 text-right font-medium">CVEs</th>
								<th scope="col" class="hidden px-2 py-1.5 text-right font-medium sm:table-cell">Größe</th>
								<th scope="col" class="px-2 py-1.5 text-left font-medium">Geprüft</th>
							</tr>
						</thead>
						<tbody>
							{#each status.feeds ?? [] as f (f.name)}
								<tr class="border-t border-border align-top">
									<td class="px-2 py-1 font-medium">{f.name === 'modified' ? 'Änderungen' : f.name}</td>
									<td class="px-2 py-1">
										<Badge tone={feedStatusTone(f.status)}>{feedStatusLabel[f.status] ?? f.status}</Badge>
										{#if f.error}<p class="mt-0.5 break-words text-danger">{f.error}</p>{/if}
									</td>
									<td class="px-2 py-1 text-right tabular">{formatNumber(f.cveCount)}</td>
									<td class="hidden px-2 py-1 text-right tabular sm:table-cell">{formatBytes(f.size)}</td>
									<td class="px-2 py-1 whitespace-nowrap" title="Stand NVD: {f.lastModified || '–'}"
										>{formatDateTime(f.syncedAt)}</td
									>
								</tr>
							{:else}
								<tr
									><td colspan="5" class="px-2 py-3 text-center text-fg-subtle">Noch keine Feeds geladen</td
									></tr
								>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		</div>
	{/if}
</Card>

<Modal bind:open={syncOpen} title="NVD synchronisieren" size="sm" as="form" onsubmit={startSync}>
	<div class="flex flex-col gap-3 text-sm">
		<p class="text-fg-muted">
			Lädt geänderte NVD-Feeds herunter und gleicht danach alle Geräte ab. Der Lauf erscheint mit Fortschritt
			auf dieser Seite.
		</p>
		<Checkbox
			bind:checked={fullSync}
			label="Alle Feeds neu laden"
			description="Alle Jahres-Feeds unabhängig von Prüfsummen neu herunterladen und importieren (dauert mehrere Minuten)."
		/>
		{#if status?.lastSync}
			<p class="text-xs text-fg-subtle">
				Letzte Synchronisation {formatDateTime(status.lastSync)}
			</p>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (syncOpen = false)}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="download">Synchronisieren</Button>
	{/snippet}
</Modal>
