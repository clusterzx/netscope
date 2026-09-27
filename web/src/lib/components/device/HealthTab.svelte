<!-- "Health" tab: the device's health checks with state, availability and latency. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { HealthCheck } from '$lib/api/types';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import StatusDot from '$lib/components/ui/StatusDot.svelte';
	import { locale, t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatMs, formatPercent, formatSeconds } from '$lib/utils/format';
	import { healthStateLabel, healthTone } from '$lib/utils/labels';
	import { LazyData } from './util';

	interface Props {
		deviceId: number;
		/** current IP of the device (checks without target use it) */
		deviceIp?: string;
		version: number;
		active: boolean;
		/** missing for devices of a NetScope site: their checks run at the site */
		oncreate?: () => void;
	}

	let { deviceId, deviceIp, version, active, oncreate }: Props = $props();

	const canManage = $derived(auth.can('health.manage'));

	const data = new LazyData<HealthCheck[]>();
	$effect(() => {
		if (!active) return;
		data.ensure(
			String(version),
			async (signal) =>
				((await api.get('/api/v1/devices/{id}/health', { path: { id: deviceId }, signal })) ??
					[]) as HealthCheck[]
		);
	});
	$effect(() => () => data.abort());

	const checks = $derived([...(data.data ?? [])].sort((a, b) => a.name.localeCompare(b.name, locale)));

	const typeLabel: Record<string, string> = { tcp: 'TCP', http: 'HTTP', tls: 'TLS', icmp: 'ICMP' };
	const windows = [
		['24h', t('Verfügb. 24 h')],
		['7d', t('Verfügb. 7 Tage')],
		['30d', t('Verfügb. 30 Tage')]
	] as const;

	function target(c: HealthCheck): string {
		if (c.type === 'http' && c.config?.url) return c.config.url;
		const host = c.target || deviceIp || t('(Geräte-IP)');
		return c.type === 'icmp' || !c.port ? host : `${host}:${c.port}`;
	}

	function availTone(v: number | undefined) {
		if (v === undefined) return 'text-fg-subtle';
		return v >= 99.5 ? 'text-ok' : v >= 95 ? 'text-warn' : 'text-danger';
	}

	let running = $state<number | null>(null);
	async function runNow(c: HealthCheck) {
		running = c.id;
		try {
			const res = await api.post('/api/v1/health-checks/{id}/run', { path: { id: c.id } });
			if (res.ok) toast.success(`OK in ${formatMs(res.latencyMs)}`, { title: c.name });
			else toast.error(res.error || t('Check fehlgeschlagen'), { title: c.name });
			data.invalidate();
			data.reload();
		} catch (e) {
			toast.error(e, { title: c.name });
		} finally {
			running = null;
		}
	}
</script>

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-center gap-2">
		<h2 class="flex-1 text-sm font-semibold">{t('Health-Checks')}</h2>
		<Button size="sm" href="/health" variant="ghost" iconRight="arrow-right">{t('Statusboard')}</Button>
		{#if oncreate && canManage}
			<Button size="sm" variant="primary" icon="plus" onclick={oncreate}>{t('Health-Check anlegen')}</Button>
		{/if}
	</div>

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton rows={3} />
	{:else if !checks.length}
		<div class="rounded-lg border border-border bg-surface">
			<EmptyState
				icon="activity"
				title={t('Keine Health-Checks')}
				description={oncreate
					? t(
							'Überwacht Dienste dieses Geräts per TCP, HTTP, TLS oder Ping mit Verfügbarkeit und Ausfallhistorie.'
						)
					: t(
							'Health-Checks für Geräte eines Standorts werden am Standort angelegt; ihre Ausfälle kommen als Events hierher.'
						)}
			>
				{#snippet actions()}
					{#if oncreate && canManage}
						<Button variant="primary" icon="plus" onclick={oncreate}>{t('Health-Check anlegen')}</Button>
					{/if}
				{/snippet}
			</EmptyState>
		</div>
	{:else}
		<ul class="grid grid-cols-1 gap-3 lg:grid-cols-2">
			{#each checks as c (c.id)}
				<li class="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4 shadow-sm">
					<div class="flex items-start gap-3">
						<span aria-hidden="true" class="mt-1.5 inline-flex">
							<StatusDot status={c.enabled ? c.state : 'idle'} size={10} />
						</span>
						<div class="min-w-0 flex-1">
							<h3 class="truncate text-sm font-semibold">{c.name}</h3>
							<p class="flex flex-wrap items-center gap-x-2 text-xs text-fg-muted">
								<Badge variant="outline">{typeLabel[c.type] ?? c.type}</Badge>
								<span class="mono min-w-0 truncate">{target(c)}</span>
								<span class="text-fg-subtle"
									>{t('alle {interval}', { interval: formatSeconds(c.intervalSeconds) })}</span
								>
							</p>
						</div>
						{#if c.enabled}
							<Badge tone={healthTone(c.state)} dot>{healthStateLabel[c.state] ?? c.state}</Badge>
						{:else}
							<Badge>{t('deaktiviert')}</Badge>
						{/if}
					</div>
					<dl class="grid grid-cols-3 gap-2 text-center sm:grid-cols-5">
						{#each windows as [k, label] (k)}
							<div class="rounded-md bg-surface-2 px-2 py-1.5">
								<dt class="text-[0.7rem] text-fg-subtle">{label}</dt>
								<dd class="text-sm font-medium tabular {availTone(c.availability?.[k])}">
									{formatPercent(c.availability?.[k], 2)}
								</dd>
							</div>
						{/each}
						<div class="rounded-md bg-surface-2 px-2 py-1.5">
							<dt class="text-[0.7rem] text-fg-subtle">{t('Latenz')}</dt>
							<dd class="text-sm font-medium tabular">{formatMs(c.lastLatencyMs)}</dd>
						</div>
						<div class="rounded-md bg-surface-2 px-2 py-1.5">
							<dt class="text-[0.7rem] text-fg-subtle">{t('Geprüft')}</dt>
							<dd class="text-sm"><RelativeTime value={c.lastCheckAt} /></dd>
						</div>
					</dl>
					{#if c.lastError}
						<p class="text-xs break-words text-danger">{c.lastError}</p>
					{/if}
					<div class="flex flex-wrap items-center gap-2 text-xs text-fg-subtle">
						{#if c.stateSince}<span class="flex-1"
								>{t('Zustand seit')} <RelativeTime value={c.stateSince} /></span
							>{/if}
						{#if canManage}
							<Button size="xs" icon="play" loading={running === c.id} onclick={() => runNow(c)}
								>{t('Jetzt prüfen')}</Button
							>
						{/if}
						<Button size="xs" variant="ghost" href="/health" iconRight="arrow-right">Details</Button>
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</div>
