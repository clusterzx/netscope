<script lang="ts">
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { api, ApiError, errorMessage } from '$lib/api';
	import type { ChangeReport } from '$lib/api';
	import ChangeReportView from '$lib/components/reports/ChangeReportView.svelte';
	import InventoryExport from '$lib/components/reports/InventoryExport.svelte';
	import ScheduledReportCard from '$lib/components/reports/ScheduledReportCard.svelte';
	import SendReportModal from '$lib/components/reports/SendReportModal.svelte';
	import { RANGE_PRESETS } from '$lib/components/reports/reports';
	import { Button, Card, ErrorState, Input, PageHeader, Skeleton } from '$lib/components/ui';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDateTime, fromDateTimeLocal, toDateTimeLocal } from '$lib/utils/format';
	import { setParams } from '$lib/utils/url';

	// ---------------------------------------------------------------- period (URL: ?range=24h|7d|30d or ?from=&to=)
	const sp = $derived(page.url.searchParams);
	const pFrom = $derived(sp.get('from') ?? '');
	const pTo = $derived(sp.get('to') ?? '');
	const custom = $derived(!!pFrom);
	const preset = $derived(custom ? '' : (RANGE_PRESETS.find((r) => r.id === sp.get('range'))?.id ?? '7d'));

	let fromText = $state('');
	let toText = $state('');
	let rangeErrors = $state<{ from?: string; to?: string }>({});

	// query for the API: presets are relative to "now" of the server (no `to`)
	const query = $derived.by(() => {
		if (custom) return { from: fromDateTimeLocal(pFrom), to: pTo ? fromDateTimeLocal(pTo) : '' };
		const ms = RANGE_PRESETS.find((r) => r.id === preset)?.ms ?? 7 * 86400_000;
		return { from: new Date(Date.now() - ms).toISOString(), to: '' };
	});

	// inputs follow the URL
	$effect(() => {
		const q = query;
		untrack(() => {
			fromText = toDateTimeLocal(q.from);
			toText = q.to ? toDateTimeLocal(q.to) : toDateTimeLocal(Date.now());
			rangeErrors = {};
		});
	});

	function pickPreset(id: string) {
		setParams({ range: id === '7d' ? null : id, from: null, to: null });
		if (id === preset && !custom) report.reload();
	}

	function applyCustom() {
		const e: { from?: string; to?: string } = {};
		const f = fromText ? new Date(fromText) : null;
		const t = toText ? new Date(toText) : null;
		if (!f || isNaN(f.getTime())) e.from = 'Startzeitpunkt angeben';
		if (toText && (!t || isNaN(t.getTime()))) e.to = 'Ungültiges Datum';
		if (f && t && !e.from && !e.to && f >= t) e.to = 'Ende muss nach dem Start liegen';
		rangeErrors = e;
		if (Object.keys(e).length) return;
		setParams({ range: null, from: fromText, to: toText || null });
		if (custom && fromText === pFrom && toText === pTo) report.reload();
	}

	// ---------------------------------------------------------------- data
	const report = new AsyncData<ChangeReport>();
	$effect(() => {
		const q = query;
		report.run((signal) =>
			api.get('/api/v1/reports/changes', { query: { from: q.from, to: q.to || null }, signal })
		);
	});

	let pluginNames = $state<Record<string, string>>({});
	$effect(() => {
		const failed = Object.keys(report.data?.failedRuns ?? {});
		if (!failed.length || failed.every((id) => id in untrack(() => pluginNames))) return;
		api
			.get('/api/v1/plugins')
			.then((list) => {
				pluginNames = Object.fromEntries((list ?? []).map((p) => [p.info.id, p.info.name]));
			})
			.catch(() => {});
	});

	const rangeText = $derived(
		report.data
			? `${formatDateTime(report.data.from)} – ${formatDateTime(report.data.to)}`
			: `${formatDateTime(query.from)} – ${query.to ? formatDateTime(query.to) : 'jetzt'}`
	);

	let downloading = $state<string | null>(null);
	async function download(format: 'md' | 'pdf') {
		downloading = format;
		try {
			await api.download(
				'/api/v1/reports/changes',
				{ from: query.from, to: query.to || null, format },
				`netscope-aenderungen.${format}`
			);
		} catch (e) {
			toast.error(e instanceof ApiError ? e.message : errorMessage(e), { title: 'Download fehlgeschlagen' });
		} finally {
			downloading = null;
		}
	}

	let sendOpen = $state(false);
</script>

<PageHeader title="Reports" description="Inventar-Export, Änderungsberichte und geplanter Versand" />

<div class="flex flex-col gap-4">
	<div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
		<InventoryExport class="lg:col-span-2" />
		<ScheduledReportCard />
	</div>

	<Card
		title="Änderungsbericht"
		description="Was sich im gewählten Zeitraum im Netzwerk geändert hat"
		icon="reports"
	>
		{#snippet actions()}
			<Button
				size="sm"
				variant="ghost"
				icon="refresh"
				label="Neu erstellen"
				loading={report.loading && !!report.data}
				onclick={() => report.reload()}
			/>
		{/snippet}

		<div class="flex flex-col gap-4">
			<!-- period -->
			<form
				class="flex flex-col gap-3 rounded-lg border border-border bg-surface-2 p-3 lg:flex-row lg:items-end"
				onsubmit={(e) => {
					e.preventDefault();
					applyCustom();
				}}
				novalidate
			>
				<fieldset class="flex flex-col gap-1">
					<legend class="mb-1 text-[0.8125rem] font-medium text-fg">Zeitraum</legend>
					<div class="flex w-fit items-center gap-0.5 rounded-md bg-surface-3 p-0.5">
						{#each RANGE_PRESETS as r (r.id)}
							<button
								type="button"
								aria-pressed={!custom && preset === r.id}
								onclick={() => pickPreset(r.id)}
								class="h-7.5 rounded px-3 text-sm {!custom && preset === r.id
									? 'bg-surface font-medium text-fg shadow-sm'
									: 'text-fg-muted hover:text-fg'}">{r.label}</button
							>
						{/each}
					</div>
				</fieldset>
				<div class="grid flex-1 grid-cols-1 gap-3 sm:grid-cols-[1fr_1fr_auto] sm:items-start lg:max-w-2xl">
					<Input label="Von" type="datetime-local" bind:value={fromText} error={rangeErrors.from} required />
					<Input label="Bis" type="datetime-local" bind:value={toText} error={rangeErrors.to} />
					<Button type="submit" class="sm:mt-6" active={custom}>Übernehmen</Button>
				</div>
			</form>

			<!-- actions -->
			<div class="flex flex-wrap items-center gap-2">
				<span class="mr-auto text-sm text-fg-muted">
					{#if report.data}Zeitraum <span class="font-medium text-fg">{rangeText}</span>{/if}
				</span>
				<Button
					icon="download"
					loading={downloading === 'md'}
					disabled={!!downloading}
					onclick={() => download('md')}>Markdown</Button
				>
				<Button
					icon="download"
					loading={downloading === 'pdf'}
					disabled={!!downloading}
					onclick={() => download('pdf')}>PDF</Button
				>
				<Button variant="primary" icon="send" onclick={() => (sendOpen = true)}>Jetzt versenden</Button>
			</div>

			{#if report.error && !report.data}
				<ErrorState error={report.error} onretry={() => report.reload()} />
			{:else if !report.data}
				<Skeleton lines={8} />
			{:else}
				{#if report.error}
					<ErrorState compact error={report.error} onretry={() => report.reload()} />
				{/if}
				<div class={report.loading ? 'opacity-60 transition-opacity' : ''}>
					<ChangeReportView report={report.data} {pluginNames} />
				</div>
				<p class="text-xs text-fg-subtle">Erstellt {formatDateTime(report.data.generatedAt, true)}</p>
			{/if}
		</div>
	</Card>
</div>

<SendReportModal bind:open={sendOpen} from={query.from} to={query.to || undefined} {rangeText} />
