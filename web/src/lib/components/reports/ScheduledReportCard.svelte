<!--
	Status of the scheduled report plugin ("report") with a link to its configuration.
	<ScheduledReportCard />
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { PluginView } from '$lib/api';
	import { Badge, Button, Card, ErrorState, RelativeTime, Skeleton } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { runStatusLabel, runStatusTone } from '$lib/utils/labels';

	interface Props {
		class?: string;
	}
	let { class: klass = '' }: Props = $props();

	const plugin = new AsyncData<PluginView>();
	$effect(() => {
		plugin.run((signal) => api.get('/api/v1/plugins/{id}', { path: { id: 'report' }, signal }));
	});
	$effect(() => live.on<{ id?: string }>('plugin', (m) => m.data?.id === 'report' && plugin.reload()));

	const p = $derived(plugin.data);
	const settings = $derived((p?.config?.settings ?? {}) as Record<string, unknown>);
	const pubs = $derived(Array.isArray(settings.publishers) ? (settings.publishers as string[]) : []);
</script>

{#snippet configure()}
	<Button size="sm" href="/plugins/report" icon="system">Bericht konfigurieren</Button>
{/snippet}

<Card
	title="Geplanter Bericht"
	description="Änderungsbericht automatisch per Publisher"
	icon="calendar"
	class={klass}
	footer={auth.can('plugins.manage') ? configure : undefined}
>
	{#if plugin.error && !p}
		<ErrorState compact error={plugin.error} onretry={() => plugin.reload()} />
	{:else if !p}
		<Skeleton lines={4} />
	{:else}
		<div class="flex flex-col gap-3 text-sm">
			<div class="flex flex-wrap items-center gap-2">
				<Badge tone={p.config?.enabled ? 'ok' : 'neutral'} dot
					>{p.config?.enabled ? 'Aktiv' : 'Inaktiv'}</Badge
				>
				<span class="text-fg-muted">{p.config?.scheduleText || p.config?.schedule || 'ohne Zeitplan'}</span>
			</div>
			<dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
				<dt class="text-fg-subtle">Zeitraum</dt>
				<dd>{typeof settings.period_days === 'number' ? `${settings.period_days} Tage` : '–'}</dd>
				<dt class="text-fg-subtle">Publisher</dt>
				<dd>
					{#if pubs.length}{pubs.join(', ')}{:else}<span class="text-warn">keiner gewählt</span>{/if}
				</dd>
				{#if p.config?.enabled}
					<dt class="text-fg-subtle">Nächster Versand</dt>
					<dd><RelativeTime value={p.nextRun} absolute fallback="–" /></dd>
				{/if}
				<dt class="text-fg-subtle">Letzter Lauf</dt>
				<dd class="flex flex-wrap items-center gap-1.5">
					{#if p.lastRun}
						<RelativeTime value={p.lastRun.finishedAt ?? p.lastRun.createdAt} />
						<Badge tone={runStatusTone(p.lastRun.status)}
							>{runStatusLabel[p.lastRun.status] ?? p.lastRun.status}</Badge
						>
					{:else}
						<span class="text-fg-subtle">noch nie</span>
					{/if}
				</dd>
			</dl>
			{#if !p.config?.enabled || !pubs.length}
				<p class="text-xs text-fg-subtle">
					Zum automatischen Versand das Plugin aktivieren und mindestens einen konfigurierten Publisher
					wählen.
				</p>
			{/if}
		</div>
	{/if}
</Card>
