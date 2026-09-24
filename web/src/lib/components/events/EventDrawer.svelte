<!--
  Event detail drawer (GET /api/v1/events/{id}): message, device, plugin/run, acknowledgement,
  payload (readable key/values + raw JSON), notifications and a link to the diff of the run.
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { EventDetail, NotificationView, RunView } from '$lib/api';
	import {
		Badge,
		Button,
		DescItem,
		DescList,
		Drawer,
		ErrorState,
		JsonView,
		RelativeTime,
		SeverityBadge,
		Skeleton,
		Textarea
	} from '$lib/components/ui';
	import { eventTypes } from '$lib/stores/catalog.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDateTime } from '$lib/utils/format';
	import type { Tone } from '$lib/utils/labels';
	import { eventCategoryLabel, label, runStatusLabel, runStatusTone, stateLabel } from '$lib/utils/labels';
	import { debounce } from '$lib/utils/url';

	interface Props {
		/** event to show (null = closed) */
		id: number | null;
		prevId?: number | null;
		nextId?: number | null;
		canWrite?: boolean;
		onclose: () => void;
		onnavigate?: (id: number) => void;
		onacked?: (id: number) => void;
	}

	let { id, prevId = null, nextId = null, canWrite = true, onclose, onnavigate, onacked }: Props = $props();

	let open = $state(false);
	$effect(() => {
		open = id !== null;
	});

	const detail = new AsyncData<EventDetail>();
	const run = new AsyncData<RunView | null>();
	$effect(() => {
		const eid = id;
		if (eid === null) return;
		detail.run((signal) => api.get('/api/v1/events/{id}', { path: { id: eid }, signal }), true);
	});
	const ev = $derived(detail.data && detail.data.id === id ? detail.data : undefined);

	$effect(() => {
		const rid = ev?.runId;
		if (!rid) return;
		run.run(async (signal) => {
			try {
				return await api.get('/api/v1/runs/{id}', { path: { id: rid }, signal });
			} catch {
				return null; // run removed by retention – the id is still shown
			}
		}, true);
	});
	const runInfo = $derived(run.data && ev?.runId && run.data.id === ev.runId ? run.data : null);

	// live: acknowledgements and notification results change the detail
	const reload = debounce(() => {
		if (id !== null) detail.reload();
	}, 600);
	$effect(() =>
		live.on<{ ids?: number[]; count?: number }>('event', (m) => {
			if (m.type !== 'acked' || id === null) return;
			if (m.data.count || m.data.ids?.includes(id)) reload();
		})
	);
	$effect(() => live.on('notification', () => ev?.notifications?.length && reload()));
	$effect(() => () => reload.cancel());

	eventTypes.load().catch(() => {});
	const spec = $derived(ev ? eventTypes.value?.find((t) => t.type === ev.type) : undefined);

	// ---------------------------------------------------------------- payload
	const ISO = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/;
	// labels for common keys whose catalog description is not a usable label
	const KEY_LABEL: Record<string, string> = {
		device_state: 'Gerätezustand',
		source: 'Quelle',
		proto: 'Protokoll'
	};
	function fmt(v: unknown): string {
		if (v === null || v === undefined || v === '') return '–';
		if (typeof v === 'boolean') return v ? 'Ja' : 'Nein';
		if (typeof v === 'string') return ISO.test(v) ? formatDateTime(v, true) : v;
		if (typeof v === 'number') return String(v);
		if (Array.isArray(v) && v.every((x) => typeof x !== 'object' || x === null))
			return v.length ? v.join(', ') : '–';
		const s = JSON.stringify(v);
		return s.length > 160 ? s.slice(0, 159) + '…' : s;
	}
	const payloadRows = $derived.by(() => {
		const p = ev?.payload ?? {};
		const fields = spec?.payload ?? [];
		const known = fields.filter((f) => f.key in p).map((f) => f.key);
		const rest = Object.keys(p)
			.filter((k) => !known.includes(k))
			.sort();
		return [...known, ...rest].map((k) => {
			const f = fields.find((x) => x.key === k);
			const desc = f?.description ?? '';
			const useDesc = desc && desc.length <= 32 && !desc.includes('|');
			const value = k === 'device_state' ? label(stateLabel, String(p[k] ?? '')) : fmt(p[k]);
			return { key: k, label: KEY_LABEL[k] ?? (useDesc ? desc : k), hint: useDesc ? k : desc || k, value };
		});
	});

	// ---------------------------------------------------------------- notifications
	const notifStatus: Record<string, { label: string; tone: Tone }> = {
		pending: { label: 'Wartend', tone: 'accent' },
		sending: { label: 'Wird gesendet', tone: 'accent' },
		sent: { label: 'Gesendet', tone: 'ok' },
		failed: { label: 'Fehlgeschlagen', tone: 'danger' },
		skipped: { label: 'Übersprungen', tone: 'neutral' }
	};
	const notifKind: Record<string, string> = {
		event: 'Sofort',
		digest: 'Sammelmeldung',
		escalation: 'Eskalation'
	};
	const notifications = $derived<NotificationView[]>(ev?.notifications ?? []);

	// ---------------------------------------------------------------- acknowledge
	let note = $state('');
	let acking = $state(false);
	$effect(() => {
		void id;
		note = '';
	});

	async function ack() {
		if (!ev) return;
		acking = true;
		try {
			const res = await api.post('/api/v1/events/ack', { body: { ids: [ev.id], note: note.trim() } });
			toast.success(res.acknowledged ? 'Event quittiert' : 'Event war bereits quittiert');
			onacked?.(ev.id);
			await detail.reload();
		} catch (e) {
			toast.error(e);
		} finally {
			acking = false;
		}
	}

	const diffHref = $derived(
		ev?.runId && ev.prevRunId
			? `/diff?runA=${ev.prevRunId}&runB=${ev.runId}${ev.deviceId ? `&device=${ev.deviceId}` : ''}`
			: null
	);
</script>

<Drawer
	bind:open
	title={ev?.title ?? (detail.error ? 'Event' : 'Event wird geladen …')}
	description={ev ? `${ev.label || ev.type} · ${label(eventCategoryLabel, ev.category)}` : undefined}
	size="lg"
	{onclose}
>
	{#snippet headerExtra()}
		{#if onnavigate}
			<Button
				variant="ghost"
				size="sm"
				icon="chevron-up"
				label="Vorheriges Event"
				disabled={prevId === null}
				onclick={() => prevId !== null && onnavigate(prevId)}
			/>
			<Button
				variant="ghost"
				size="sm"
				icon="chevron-down"
				label="Nächstes Event"
				disabled={nextId === null}
				onclick={() => nextId !== null && onnavigate(nextId)}
			/>
		{/if}
	{/snippet}

	{#if detail.error && !ev}
		<ErrorState error={detail.error} onretry={() => detail.reload()} />
	{:else if !ev}
		<Skeleton lines={8} />
	{:else}
		<div class="flex flex-col gap-5">
			<div class="flex flex-wrap items-center gap-2">
				<SeverityBadge severity={ev.severity} size="md" />
				<Badge tone="neutral" size="md">{ev.label || ev.type}</Badge>
				{#if ev.ackedAt}
					<Badge tone="ok" size="md" dot>Quittiert</Badge>
				{:else}
					<Badge tone="warn" size="md" dot>Offen</Badge>
				{/if}
				{#if diffHref}
					<Button size="sm" icon="diff" href={diffHref} class="ml-auto">Diff anzeigen</Button>
				{/if}
			</div>

			{#if ev.message}
				<p class="text-sm whitespace-pre-wrap text-fg">{ev.message}</p>
			{/if}

			<DescList>
				<DescItem label="Zeitpunkt">
					{formatDateTime(ev.ts, true)}
					<span class="text-fg-subtle">(<RelativeTime value={ev.ts} />)</span>
				</DescItem>
				<DescItem label="Gerät">
					{#if ev.deviceId}
						<a class="link" href="/devices/{ev.deviceId}">{ev.deviceName || `Gerät #${ev.deviceId}`}</a>
						<a
							class="ml-2 text-xs text-fg-subtle hover:text-accent"
							href="/events?device={ev.deviceId}&acked=all">alle Events</a
						>
					{:else}
						<span class="text-fg-subtle">–</span>
					{/if}
				</DescItem>
				{#if ev.site}
					<DescItem label="Standort" hint="Event des NetScope-Standorts" value={ev.site} />
					<DescItem label="Erzeugt von" hint="am Standort" value={ev.pluginId} mono />
				{:else}
					<DescItem label="Erzeugt von">
						<a class="link" href="/plugins/{ev.pluginId}">{ev.pluginId}</a>
					</DescItem>
				{/if}
				{#if ev.runId}
					<DescItem label="Lauf">
						{#if runInfo}
							<a class="link" href="/plugins/{runInfo.pluginId}/runs/{runInfo.id}">
								{runInfo.pluginName || runInfo.pluginId} · #{runInfo.id}
							</a>
							<Badge tone={runStatusTone(runInfo.status)} class="ml-1.5"
								>{label(runStatusLabel, runInfo.status)}</Badge
							>
						{:else}
							<span class="mono">#{ev.runId}</span>
						{/if}
						{#if ev.prevRunId}
							<span class="block text-xs text-fg-subtle">Vorheriger Lauf: #{ev.prevRunId}</span>
						{/if}
					</DescItem>
				{/if}
				{#if ev.dedupKey}
					<DescItem label="Dedup-Schlüssel" mono value={ev.dedupKey} />
				{/if}
			</DescList>

			<!-- acknowledgement -->
			<section aria-labelledby="ev-ack-h">
				<h3 id="ev-ack-h" class="mb-2 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
					Quittierung
				</h3>
				{#if ev.ackedAt}
					<div class="rounded-md border border-border bg-surface-2 px-3 py-2.5 text-sm">
						<div>
							Quittiert von <strong class="font-medium">{ev.ackedBy || 'unbekannt'}</strong> am
							{formatDateTime(ev.ackedAt)}
						</div>
						{#if ev.ackNote}
							<p class="mt-1 whitespace-pre-wrap text-fg-muted">{ev.ackNote}</p>
						{/if}
					</div>
				{:else if canWrite}
					<form
						class="flex flex-col gap-2"
						onsubmit={(e) => {
							e.preventDefault();
							ack();
						}}
					>
						<Textarea label="Notiz (optional)" bind:value={note} rows={2} maxlength={1000} />
						<div>
							<Button type="submit" variant="primary" size="sm" icon="check" loading={acking}
								>Quittieren</Button
							>
						</div>
					</form>
				{:else}
					<p class="text-sm text-fg-muted">Noch nicht quittiert.</p>
				{/if}
			</section>

			<!-- payload -->
			<section aria-labelledby="ev-payload-h">
				<h3 id="ev-payload-h" class="mb-2 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
					Details
				</h3>
				{#if payloadRows.length === 0}
					<p class="text-sm text-fg-muted">Keine weiteren Daten.</p>
				{:else}
					<dl class="grid grid-cols-[minmax(7rem,auto)_1fr] gap-x-4 gap-y-1.5 text-sm">
						{#each payloadRows as r (r.key)}
							<dt class="text-fg-subtle" title={r.hint || undefined}>{r.label}</dt>
							<dd class="min-w-0 break-words text-fg">{r.value}</dd>
						{/each}
					</dl>
					<details class="mt-3">
						<summary class="cursor-pointer text-sm text-fg-muted select-none hover:text-fg"
							>Rohdaten (JSON)</summary
						>
						<JsonView value={ev.payload} class="mt-2" maxHeight="20rem" />
					</details>
				{/if}
			</section>

			<!-- notifications -->
			<section aria-labelledby="ev-notif-h">
				<div class="mb-2 flex items-center justify-between gap-2">
					<h3 id="ev-notif-h" class="text-xs font-semibold tracking-wide text-fg-subtle uppercase">
						Benachrichtigungen ({notifications.length})
					</h3>
					<a href="/rules?tab=notifications" class="text-xs text-fg-subtle hover:text-accent"
						>Verlauf öffnen</a
					>
				</div>
				{#if notifications.length === 0}
					<p class="text-sm text-fg-muted">
						Keine Regel hat für dieses Event eine Benachrichtigung ausgelöst.
					</p>
				{:else}
					<ul class="flex flex-col divide-y divide-border rounded-md border border-border">
						{#each notifications as n (n.id)}
							{@const st = notifStatus[n.status] ?? { label: n.status, tone: 'neutral' as Tone }}
							<li class="flex flex-col gap-1 px-3 py-2 text-sm">
								<div class="flex flex-wrap items-center gap-2">
									<span class="font-medium text-fg">{n.publisher}</span>
									<Badge tone={st.tone}>{st.label}</Badge>
									<span class="text-xs text-fg-subtle">{notifKind[n.kind] ?? n.kind}</span>
									<span class="ml-auto text-xs text-fg-subtle">
										<RelativeTime value={n.sentAt ?? n.createdAt} />
									</span>
								</div>
								{#if n.ruleName}
									<div class="text-xs text-fg-muted">
										Regel:
										{#if n.ruleId}
											<a class="hover:text-accent hover:underline" href="/rules/{n.ruleId}">{n.ruleName}</a>
										{:else}
											{n.ruleName}
										{/if}
										{#if n.attempts > 1}· {n.attempts} Versuche{/if}
									</div>
								{/if}
								{#if n.error}<div class="text-xs break-words text-danger">{n.error}</div>{/if}
							</li>
						{/each}
					</ul>
				{/if}
			</section>
		</div>
	{/if}
</Drawer>
