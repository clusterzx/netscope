<!--
	"Historie" tab: the device timeline (GET /devices/{id}/timeline: IP/port/cert/container/
	package/fact changes and events) merged with GET /devices/{id}/events, newest first,
	grouped by day, filterable by kind, with links to events and diffs.
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import type { Event, EventList, TimelineEntry } from '$lib/api/types';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import type { IconName } from '$lib/components/ui/icons';
	import SeverityBadge from '$lib/components/ui/SeverityBadge.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDate, formatNumber, formatTime } from '$lib/utils/format';
	import { LazyData } from './util';

	interface Props {
		deviceId: number;
		version: number;
		active: boolean;
	}

	let { deviceId, version, active }: Props = $props();

	type Entry = TimelineEntry & { event?: Event };
	type Data = { timeline: TimelineEntry[]; events: EventList };

	let limit = $state(300);
	const data = new LazyData<Data>();
	$effect(() => {
		if (!active) return;
		const l = limit;
		data.ensure(`${version}|${l}`, async (signal) => {
			const [timeline, events] = await Promise.all([
				api.get('/api/v1/devices/{id}/timeline', { path: { id: deviceId }, query: { limit: l }, signal }),
				api.get('/api/v1/devices/{id}/events', { path: { id: deviceId }, query: { limit: 200 }, signal })
			]);
			return { timeline: timeline ?? [], events: events ?? { total: 0, items: [] } };
		});
	});
	$effect(() => () => data.abort());

	const KINDS: { id: string; label: string; icon: IconName }[] = [
		{ id: 'event', label: 'Events', icon: 'events' },
		{ id: 'ip', label: 'IP-Adressen', icon: 'network' },
		{ id: 'port', label: 'Ports', icon: 'radar' },
		{ id: 'cert', label: 'Zertifikate', icon: 'lock' },
		{ id: 'container', label: 'Container', icon: 'box' },
		{ id: 'package', label: 'Pakete', icon: 'package' },
		{ id: 'fact', label: 'Merkmale', icon: 'tag' }
	];
	const kindMeta = (k: string) =>
		KINDS.find((x) => x.id === k) ?? { id: k, label: k, icon: 'info' as IconName };

	let kind = $state('');

	const entries = $derived.by<Entry[]>(() => {
		if (!data.data) return [];
		const evById = new Map((data.data.events.items ?? []).map((e) => [e.id, e]));
		const seen = new Set<number>();
		const out: Entry[] = [];
		for (const t of data.data.timeline) {
			if (t.kind === 'event' && t.eventId) {
				seen.add(t.eventId);
				out.push({ ...t, event: evById.get(t.eventId) });
			} else out.push(t);
		}
		// events outside the timeline limit
		for (const e of data.data.events.items ?? [])
			if (!seen.has(e.id))
				out.push({
					ts: e.ts,
					kind: 'event',
					change: 'event',
					text: e.title,
					eventId: e.id,
					severity: e.severity,
					runId: e.runId,
					event: e
				});
		return out.sort((a, b) => new Date(b.ts).getTime() - new Date(a.ts).getTime());
	});

	const counts = $derived.by(() => {
		const c: Record<string, number> = {};
		for (const e of entries) c[e.kind] = (c[e.kind] ?? 0) + 1;
		return c;
	});
	const shown = $derived(kind ? entries.filter((e) => e.kind === kind) : entries);
	const days = $derived.by(() => {
		const out: { day: string; items: Entry[] }[] = [];
		for (const e of shown) {
			const day = formatDate(e.ts);
			const last = out[out.length - 1];
			if (last && last.day === day) last.items.push(e);
			else out.push({ day, items: [e] });
		}
		return out;
	});
	const truncated = $derived((data.data?.timeline.length ?? 0) >= limit);

	function changeTone(c: string): string {
		return c === 'added' ? 'text-ok' : c === 'removed' ? 'text-danger' : c === 'changed' ? 'text-accent' : '';
	}
	const changeLabel: Record<string, string> = { added: 'neu', removed: 'entfernt', changed: 'geändert' };

	/** time-based diff around a change (the diff page compares the state before and after) */
	function timeDiffHref(ts: string): string {
		const t = new Date(ts).getTime();
		const from = new Date(t - 1000).toISOString();
		const to = new Date(t + 1000).toISOString();
		return `/diff?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&device=${deviceId}`;
	}

	let opening = $state<number | null>(null);
	/** event diff: previous run of the same plugin → this run */
	async function openEventDiff(e: Entry) {
		if (!e.eventId) return;
		opening = e.eventId;
		try {
			const d = await api.get('/api/v1/events/{id}', { path: { id: e.eventId } });
			if (d.prevRunId && d.runId) goto(`/diff?runA=${d.prevRunId}&runB=${d.runId}&device=${deviceId}`);
			else goto(timeDiffHref(e.ts));
		} catch (err) {
			toast.error(err);
		} finally {
			opening = null;
		}
	}
</script>

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-center gap-2">
		<h2 class="mr-2 text-sm font-semibold">Historie</h2>
		<div
			class="flex flex-wrap gap-1 {data.data ? '' : 'invisible'}"
			role="group"
			aria-label="Nach Art filtern"
		>
			<button
				type="button"
				class="rounded-full border px-2.5 py-0.5 text-xs {kind === ''
					? 'border-accent bg-accent-soft text-accent'
					: 'border-border text-fg-muted hover:text-fg'}"
				aria-pressed={kind === ''}
				onclick={() => (kind = '')}
			>
				Alle <span class="tabular">{formatNumber(entries.length)}</span>
			</button>
			{#each KINDS.filter((k) => counts[k.id]) as k (k.id)}
				<button
					type="button"
					class="inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-xs {kind === k.id
						? 'border-accent bg-accent-soft text-accent'
						: 'border-border text-fg-muted hover:text-fg'}"
					aria-pressed={kind === k.id}
					onclick={() => (kind = k.id)}
				>
					<Icon name={k.icon} size={12} />
					{k.label} <span class="tabular">{formatNumber(counts[k.id])}</span>
				</button>
			{/each}
		</div>
		<span class="flex-1"></span>
		<Button size="sm" variant="ghost" href="/events?device={deviceId}" iconRight="arrow-right"
			>Alle Events</Button
		>
	</div>

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton lines={8} />
	{:else if !shown.length}
		<div class="rounded-lg border border-border bg-surface">
			<EmptyState
				compact
				icon="history"
				title="Keine Einträge"
				description="Für dieses Gerät ist noch nichts passiert."
			/>
		</div>
	{:else}
		<div class="rounded-lg border border-border bg-surface shadow-sm">
			{#each days as g (g.day)}
				<h3
					class="sticky top-0 z-[1] border-b border-border bg-surface-2 px-4 py-1.5 text-xs font-semibold text-fg-muted first:rounded-t-lg"
				>
					{g.day}
				</h3>
				<ol class="divide-y divide-border">
					{#each g.items as e, i (e.kind + e.ts + (e.eventId ?? '') + i)}
						{@const km = kindMeta(e.kind)}
						<li class="flex items-start gap-3 px-4 py-2 text-sm">
							<span class="w-11 shrink-0 pt-0.5 text-xs text-fg-subtle tabular">{formatTime(e.ts)}</span>
							<span
								class="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-surface-3 {changeTone(
									e.change
								)}"
								title={km.label}
							>
								<Icon name={km.icon} size={12} label={km.label} />
							</span>
							<div class="min-w-0 flex-1">
								<p class="break-words">
									{#if e.kind === 'event' && e.eventId}
										<a
											href="/events?device={deviceId}&event={e.eventId}"
											class="hover:text-accent hover:underline">{e.text}</a
										>
									{:else}
										{e.text}
									{/if}
								</p>
								{#if e.event?.message}<p class="text-xs break-words text-fg-muted">{e.event.message}</p>{/if}
								<p class="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-subtle">
									{#if e.kind === 'event'}
										{#if e.event?.label}<span>{e.event.label}</span>{/if}
										{#if e.event?.ackedAt}<Badge tone="ok">quittiert</Badge>{:else if e.event}<Badge
												>offen</Badge
											>{/if}
									{:else if changeLabel[e.change]}
										<span class={changeTone(e.change)}>{changeLabel[e.change]}</span>
									{/if}
									{#if e.kind === 'event' && e.runId}
										<button
											type="button"
											class="inline-flex items-center gap-1 text-accent hover:underline disabled:opacity-50"
											disabled={opening === e.eventId}
											onclick={() => openEventDiff(e)}
										>
											<Icon name="diff" size={12} /> Diff
										</button>
									{:else if e.kind !== 'event' && e.kind !== 'package'}
										<a
											href={timeDiffHref(e.ts)}
											class="inline-flex items-center gap-1 text-accent hover:underline"
										>
											<Icon name="diff" size={12} /> Diff
										</a>
									{/if}
								</p>
							</div>
							{#if e.severity}<SeverityBadge severity={e.severity} class="shrink-0" />{/if}
						</li>
					{/each}
				</ol>
			{/each}
		</div>
		{#if truncated}
			<div class="flex justify-center">
				<Button size="sm" loading={data.loading} onclick={() => (limit = Math.min(2000, limit + 500))}
					>Ältere Einträge laden</Button
				>
			</div>
		{/if}
	{/if}
</div>
