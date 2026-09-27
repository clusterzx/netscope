<!--
	Detail drawer of a health check: state, availability, latency chart with range presets,
	outage history, "Jetzt prüfen", edit/delete.
	<CheckDrawer bind:open checkId={id} initial={check} onedit={(c) => …} onchanged={() => …} ondeleted={() => …} />
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import type { CheckRunResult, HealthCheck, Outage, SeriesResponse } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		DescItem,
		DescList,
		Drawer,
		ErrorState,
		Skeleton,
		TimeSeriesChart,
		Toggle
	} from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDateTime, formatMs, formatPercent } from '$lib/utils/format';
	import { healthStateLabel, healthTone } from '$lib/utils/labels';
	import { debounce } from '$lib/utils/url';
	import { t } from '$lib/i18n';
	import OutageList from './OutageList.svelte';
	import {
		availabilityText,
		availabilityTone,
		checkTypeLabel,
		intervalText,
		payloadFromForm,
		formFromCheck,
		targetText,
		type HealthRoundMessage,
		type HealthStateMessage
	} from './health';

	interface Props {
		open?: boolean;
		checkId: number | null;
		initial?: HealthCheck | null;
		onedit?: (c: HealthCheck) => void;
		onchanged?: (c: HealthCheck) => void;
		ondeleted?: (id: number) => void;
		onclose?: () => void;
	}

	let {
		open = $bindable(false),
		checkId,
		initial = null,
		onedit,
		onchanged,
		ondeleted,
		onclose
	}: Props = $props();

	const RANGES = [
		{ id: '1h', label: '1 h', ms: 3600_000 },
		{ id: '6h', label: '6 h', ms: 6 * 3600_000 },
		{ id: '24h', label: '24 h', ms: 24 * 3600_000 },
		{ id: '7d', label: t('7 Tage'), ms: 7 * 86400_000 },
		{ id: '30d', label: t('30 Tage'), ms: 30 * 86400_000 }
	];
	let range = $state('24h');
	let rangeTo = $state(Date.now());
	const rangeMs = $derived(RANGES.find((r) => r.id === range)?.ms ?? 86400_000);

	const detail = new AsyncData<HealthCheck>();
	const outages = new AsyncData<Outage[]>();
	const latency = new AsyncData<SeriesResponse>();
	const latencyPts = $derived(latency.data?.points ?? []);
	const chartTo = $derived(
		Math.max(rangeTo, ...latencyPts.map((p) => new Date(p.t).getTime()).filter((ms) => isFinite(ms)))
	);

	let running = $state(false);
	let result = $state<CheckRunResult | null>(null);
	let toggling = $state(false);

	const c = $derived(detail.data && detail.data.id === checkId ? detail.data : initial);

	$effect(() => {
		const id = checkId;
		if (!open || !id) return;
		untrack(() => {
			result = null;
			rangeTo = Date.now();
		});
		detail.run((signal) => api.get('/api/v1/health-checks/{id}', { path: { id }, signal }), true);
		outages.run(
			async (signal) =>
				(await api.get('/api/v1/health-checks/{id}/outages', {
					path: { id },
					query: { limit: 100 },
					signal
				})) ?? [],
			true
		);
	});

	$effect(() => {
		const id = checkId;
		const to = rangeTo;
		const from = to - rangeMs;
		if (!open || !id) return;
		latency.run((signal) =>
			api.get('/api/v1/health-checks/{id}/latency', {
				path: { id },
				// no "to": the server's "now" (the browser clock may lag behind the server)
				query: { from: new Date(from).toISOString(), points: 300 },
				signal
			})
		);
	});

	function reloadAll() {
		detail.reload();
		outages.reload();
		rangeTo = Date.now();
	}

	// live (topic "health"): "state" of this check, "round" = new samples/availability
	const refresh = debounce(() => {
		if (open && checkId) reloadAll();
	}, 1500);
	$effect(() =>
		live.on<HealthStateMessage | HealthRoundMessage>('health', (m) => {
			if (m.type === 'round') refresh();
			else if (m.type === 'state' && (m.data as HealthStateMessage).checkId === checkId) refresh();
		})
	);
	$effect(() => () => refresh.cancel());

	async function runNow() {
		if (!checkId) return;
		running = true;
		result = null;
		try {
			const res = await api.post('/api/v1/health-checks/{id}/run', { path: { id: checkId } });
			result = res;
			if (res.check) {
				detail.set(res.check);
				onchanged?.(res.check);
			}
			outages.reload();
			rangeTo = Date.now();
		} catch (e) {
			result = { ok: false, error: errorMessage(e), latencyMs: 0 };
		} finally {
			running = false;
		}
	}

	async function setEnabled(v: boolean) {
		if (!c) return;
		toggling = true;
		try {
			const body = { ...payloadFromForm(formFromCheck(c)), enabled: v } as HealthCheck;
			const saved = await api.put('/api/v1/health-checks/{id}', { path: { id: c.id }, body });
			detail.set(saved);
			onchanged?.(saved);
			toast.success(v ? t('Check aktiviert') : t('Check deaktiviert'));
		} catch (e) {
			toast.error(e);
			detail.reload();
		} finally {
			toggling = false;
		}
	}

	async function remove() {
		if (!c) return;
		const ok = await confirm({
			title: t('Health-Check löschen?'),
			message: t('„{name}“ wird mit Ausfallhistorie und Latenz-Zeitreihe gelöscht.', { name: c.name }),
			confirmLabel: t('Löschen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/health-checks/{id}', { path: { id: c.id } });
			toast.success(t('Health-Check gelöscht'));
			open = false;
			ondeleted?.(c.id);
		} catch (e) {
			toast.error(e);
		}
	}

	const windows = [
		['24h', t('24 Stunden')],
		['7d', t('7 Tage')],
		['30d', t('30 Tage')]
	] as const;
</script>

<Drawer bind:open title={c?.name ?? t('Health-Check')} size="xl" {onclose}>
	{#snippet headerExtra()}
		{#if c}
			<Badge tone={c.enabled ? healthTone(c.state) : 'neutral'} size="md" dot>
				{c.enabled ? (healthStateLabel[c.state] ?? c.state) : t('Deaktiviert')}
			</Badge>
		{/if}
	{/snippet}

	{#if !c && detail.loading}
		<Skeleton lines={8} />
	{:else if !c && detail.error}
		<ErrorState error={detail.error} onretry={() => detail.reload()} />
	{:else if c}
		<div class="flex flex-col gap-5">
			<!-- summary -->
			<section class="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
				<div>
					<div class="text-xs text-fg-subtle">{t('Ziel')}</div>
					<div class="mono">{targetText(c)}</div>
				</div>
				{#if c.deviceId}
					<div>
						<div class="text-xs text-fg-subtle">{t('Gerät')}</div>
						<a href="/devices/{c.deviceId}" class="link">{c.deviceName || `#${c.deviceId}`}</a>
					</div>
				{/if}
				<div>
					<div class="text-xs text-fg-subtle">{t('Letzte Latenz')}</div>
					<div class="tabular">{c.lastLatencyMs !== undefined ? formatMs(c.lastLatencyMs) : '–'}</div>
				</div>
				<div>
					<div class="text-xs text-fg-subtle">{t('Zuletzt geprüft')}</div>
					<div>{formatDateTime(c.lastCheckAt, true)}</div>
				</div>
				{#if c.stateSince}
					<div>
						<div class="text-xs text-fg-subtle">{t('Zustand seit')}</div>
						<div>{formatDateTime(c.stateSince)}</div>
					</div>
				{/if}
			</section>

			{#if c.lastError}
				<Alert tone={c.state === 'down' ? 'danger' : 'warn'} title={t('Letzter Fehler')}>{c.lastError}</Alert>
			{/if}

			<!-- actions -->
			{#if auth.can('health.manage')}
				<section class="flex flex-wrap items-center gap-2">
					<Button variant="primary" icon="play" loading={running} onclick={runNow}>{t('Jetzt prüfen')}</Button
					>
					<Button icon="edit" onclick={() => onedit?.(c)}>{t('Bearbeiten')}</Button>
					<Button variant="ghost" icon="trash" class="text-danger" onclick={remove}>{t('Löschen')}</Button>
					<Toggle
						class="ml-auto"
						checked={c.enabled}
						disabled={toggling}
						label={t('Aktiv')}
						onchange={(v) => setEnabled(v)}
					/>
				</section>
			{/if}

			{#if result}
				<div aria-live="polite">
					<Alert
						tone={result.ok ? 'ok' : 'danger'}
						title={result.ok ? t('Prüfung erfolgreich') : t('Prüfung fehlgeschlagen')}
					>
						{#if result.ok}
							{t('Antwortzeit {latency}.', { latency: formatMs(result.latencyMs) })}
						{:else}
							{result.error || t('Unbekannter Fehler')}{#if result.latencyMs}
								· {formatMs(result.latencyMs)}{/if}
						{/if}
						{#if result.check}
							{t('Zustand: {state} ({fail} Fehler / {ok} OK in Folge).', {
								state: healthStateLabel[result.check.state] ?? result.check.state,
								fail: result.check.consecutiveFail,
								ok: result.check.consecutiveOk
							})}
						{/if}
					</Alert>
				</div>
			{/if}

			<!-- availability -->
			<section aria-labelledby="av-h">
				<h3 id="av-h" class="mb-2 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
					{t('Verfügbarkeit')}
				</h3>
				<dl class="grid grid-cols-3 gap-2">
					{#each windows as [k, lbl] (k)}
						{@const v = c.availability?.[k]}
						<div class="rounded-md border border-border px-3 py-2">
							<dt class="text-xs text-fg-subtle">{lbl}</dt>
							<dd class="text-lg font-semibold tabular {availabilityText[availabilityTone(v)]}">
								{v === undefined ? '–' : formatPercent(v, 3)}
							</dd>
						</div>
					{/each}
				</dl>
			</section>

			<!-- latency -->
			<section aria-labelledby="lat-h">
				<div class="mb-2 flex flex-wrap items-center justify-between gap-2">
					<h3 id="lat-h" class="text-xs font-semibold tracking-wide text-fg-subtle uppercase">
						{t('Latenz')}
					</h3>
					<div
						class="flex items-center gap-0.5 rounded-md bg-surface-3 p-0.5"
						role="group"
						aria-label={t('Zeitraum')}
					>
						{#each RANGES as r (r.id)}
							<button
								type="button"
								aria-pressed={range === r.id}
								onclick={() => {
									range = r.id;
									rangeTo = Date.now();
								}}
								class="rounded px-2 py-0.5 text-xs {range === r.id
									? 'bg-surface text-fg shadow-sm'
									: 'text-fg-muted hover:text-fg'}">{r.label}</button
							>
						{/each}
					</div>
				</div>
				{#if latency.error}
					<ErrorState compact error={latency.error} onretry={() => latency.reload()} />
				{:else if !latency.data && latency.loading}
					<Skeleton class="h-44 w-full" />
				{:else}
					<TimeSeriesChart
						points={latency.data?.points ?? []}
						label={t('Latenz {name}', { name: c.name })}
						format={formatMs}
						from={rangeTo - rangeMs}
						to={chartTo}
						emptyText={!c.lastCheckAt
							? t('Noch keine Messwerte')
							: c.lastOk === false
								? t('Keine Latenzwerte – fehlgeschlagene Prüfungen liefern keine Latenz')
								: t('Keine Messwerte im Zeitraum')}
					/>
					{#if latency.data?.resolution}
						<p class="mt-1 text-[11px] text-fg-subtle">
							{t('Auflösung: {resolution}', { resolution: latency.data.resolution })}
						</p>
					{/if}
				{/if}
			</section>

			<!-- configuration -->
			<section aria-labelledby="cfg-h">
				<h3 id="cfg-h" class="mb-2 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
					{t('Konfiguration')}
				</h3>
				<DescList cols={2}>
					<DescItem label={t('Typ')} value={checkTypeLabel[c.type] ?? c.type} />
					<DescItem
						label={t('Intervall / Timeout')}
						value="{intervalText(c.intervalSeconds)} / {c.timeoutSeconds} s"
					/>
					<DescItem
						label={t('Flap-Dämpfung')}
						value={t('Down nach {fail}, Up nach {ok} Prüfungen in Folge', {
							fail: c.failThreshold,
							ok: c.recoverThreshold
						})}
					/>
					<DescItem
						label={t('Beeinträchtigt ab')}
						value={c.config?.degradedMs ? `${c.config.degradedMs} ms` : t('aus')}
					/>
					{#if c.type === 'http'}
						<DescItem
							label={t('Methode / Status')}
							mono
							value="{c.config?.method || 'GET'} · {c.config?.expectStatus || '200-399'}"
						/>
						<DescItem label={t('Body-Regex')} mono value={c.config?.bodyMatch} />
						<DescItem
							label={t('TLS / Weiterleitungen')}
							value={`${c.config?.verifyTls ? t('Zertifikat prüfen') : t('Zertifikat nicht prüfen')} · ${
								c.config?.followRedirects ? t('folgen') : t('nicht folgen')
							}`}
						/>
					{:else if c.type === 'tls'}
						<DescItem label={t('Servername (SNI)')} mono value={c.config?.serverName} />
						<DescItem
							label={t('Zertifikat')}
							value={`${c.config?.verifyTls ? t('Kette prüfen') : t('Kette nicht prüfen')}${
								c.config?.minDays ? ` · ${t('mind. {n} Tage gültig', { n: c.config.minDays })}` : ''
							}`}
						/>
					{:else if c.type === 'icmp'}
						<DescItem label={t('Pings je Prüfung')} value={c.config?.count ?? 3} />
					{/if}
					<DescItem label={t('Angelegt')} value={formatDateTime(c.createdAt)} />
				</DescList>
			</section>

			<!-- outages -->
			<section aria-labelledby="out-h">
				<h3 id="out-h" class="mb-1 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
					{t('Ausfallhistorie')}
				</h3>
				{#if outages.error}
					<ErrorState compact error={outages.error} onretry={() => outages.reload()} />
				{:else if !outages.data}
					<Skeleton lines={3} />
				{:else if outages.data.length === 0}
					<p class="py-2 text-sm text-fg-muted">{t('Keine Ausfälle aufgezeichnet.')}</p>
				{:else}
					<OutageList outages={outages.data} />
				{/if}
			</section>
		</div>
	{/if}
</Drawer>
