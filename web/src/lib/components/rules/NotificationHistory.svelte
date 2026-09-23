<!--
	Notification history (GET /api/v1/notifications) with status/publisher filter, pagination
	(URL synced: status, publisher, rule, offset, limit) and live refresh.
-->
<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { NotificationView, Rule } from '$lib/api';
	import type { ApiNotificationList } from '$lib/api/generated';
	import {
		Badge,
		Button,
		EmptyState,
		ErrorState,
		Pagination,
		RelativeTime,
		Select,
		Table
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { formatDateTime } from '$lib/utils/format';
	import { debounce, intParam, setParams } from '$lib/utils/url';
	import { loadPublishers, publishers } from './publishers.svelte';
	import {
		notificationKindLabel,
		notificationStatusLabel,
		notificationStatusTone,
		priorityLabel,
		priorityTone,
		publisherName
	} from './rule';

	const SIZES = [25, 50, 100];
	const sp = $derived(page.url.searchParams);
	const status = $derived(sp.get('status') ?? '');
	const publisher = $derived(sp.get('publisher') ?? '');
	const offset = $derived(Math.max(0, intParam(sp, 'offset', 0)));
	const limit = $derived(SIZES.includes(intParam(sp, 'limit', 50)) ? intParam(sp, 'limit', 50) : 50);

	const rule = $derived(intParam(sp, 'rule', 0));

	onMount(() => loadPublishers());
	const pubs = $derived(publishers.value ?? []);

	// rules for the filter (names of rules that no longer exist stay readable via the rows)
	let rules = $state<Rule[]>([]);
	onMount(() => {
		api
			.get('/api/v1/rules')
			.then((r) => (rules = r ?? []))
			.catch(() => {});
	});

	const data = new AsyncData<ApiNotificationList>();
	$effect(() => {
		const query = {
			status: status || null,
			publisher: publisher || null,
			rule: rule || null,
			limit,
			offset
		};
		data.run((signal) => api.get('/api/v1/notifications', { query, signal }));
	});

	// created / updated (event added to a pending bundle) / sent / failed
	const refresh = debounce(() => data.reload(), 800);
	$effect(() => live.on('notification', refresh));
	$effect(() => live.onReconnect(refresh));
	$effect(() => () => refresh.cancel());

	const statusOptions = [
		{ value: '', label: 'Alle Status' },
		...['pending', 'sending', 'sent', 'failed', 'skipped'].map((s) => ({
			value: s,
			label: notificationStatusLabel[s]
		}))
	];
	const publisherOptions = $derived([
		{ value: '', label: 'Alle Publisher' },
		...pubs.map((p) => ({ value: p.id, label: p.name }))
	]);
	const ruleOptions = $derived([
		{ value: '', label: 'Alle Regeln' },
		...[...rules]
			.sort((a, b) => a.sortOrder - b.sortOrder || a.id - b.id)
			.map((r) => ({ value: String(r.id), label: r.name })),
		...(rule && !rules.some((r) => r.id === rule) ? [{ value: String(rule), label: `Regel #${rule}` }] : [])
	]);

	const columns: Column<NotificationView>[] = [
		{ key: 'createdAt', label: 'Erstellt', width: '9rem', hideBelow: 'sm' },
		{ key: 'rule', label: 'Regel / Titel' },
		{ key: 'publisher', label: 'Publisher', hideBelow: 'md' },
		{ key: 'priority', label: 'Priorität', hideBelow: 'lg' },
		{ key: 'status', label: 'Status' },
		{ key: 'delivery', label: 'Zustellung', hideBelow: 'lg' },
		{ key: 'events', label: 'Events', hideBelow: 'sm' }
	];

	const rows = $derived(data.data?.items ?? []);
	const filtered = $derived(!!status || !!publisher || !!rule);
</script>

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-end gap-2">
		<Select
			label="Status"
			size="sm"
			options={statusOptions}
			value={status}
			onchange={(e) =>
				setParams({ status: (e.currentTarget as HTMLSelectElement).value || null, offset: null })}
			class="w-44"
		/>
		<Select
			label="Publisher"
			size="sm"
			options={publisherOptions}
			value={publisher}
			onchange={(e) =>
				setParams({ publisher: (e.currentTarget as HTMLSelectElement).value || null, offset: null })}
			class="w-44"
		/>
		<Select
			label="Regel"
			size="sm"
			options={ruleOptions}
			value={rule ? String(rule) : ''}
			onchange={(e) =>
				setParams({ rule: (e.currentTarget as HTMLSelectElement).value || null, offset: null })}
			class="w-64 max-w-full"
		/>
		<div class="ml-auto">
			<Button
				size="sm"
				icon="refresh"
				label="Aktualisieren"
				loading={data.loading && !!data.data}
				onclick={() => data.reload()}
			/>
		</div>
	</div>

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else}
		<Table
			{columns}
			{rows}
			key={(n) => n.id}
			loading={data.loading && !data.data}
			dense
			caption="Benachrichtigungsverlauf"
		>
			{#snippet cell(n, col)}
				{#if col.key === 'createdAt'}
					<RelativeTime value={n.createdAt} />
				{:else if col.key === 'rule'}
					<div class="min-w-0">
						{#if n.ruleId}
							<a href="/rules/{n.ruleId}" class="link">{n.ruleName || `Regel #${n.ruleId}`}</a>
						{:else}
							<span class="text-fg-muted">{notificationKindLabel[n.kind] ?? n.kind}</span>
						{/if}
						{#if n.kind !== 'event'}
							<Badge class="ml-1">{notificationKindLabel[n.kind] ?? n.kind}</Badge>
						{/if}
						<span class="block text-xs text-fg-subtle sm:hidden"><RelativeTime value={n.createdAt} /></span>
						{#if n.title}<span class="block truncate text-xs text-fg-muted" title={n.title}>{n.title}</span
							>{/if}
						{#if n.error}
							<span
								class="block max-w-md truncate text-xs {n.status === 'failed' ? 'text-danger' : 'text-warn'}"
								title={n.error}>{n.error}</span
							>
						{/if}
					</div>
				{:else if col.key === 'publisher'}
					<a href="/plugins/{encodeURIComponent(n.publisher)}" class="hover:text-accent hover:underline"
						>{publisherName(n.publisher, pubs)}</a
					>
				{:else if col.key === 'priority'}
					<Badge tone={priorityTone(n.priority)}>{priorityLabel[n.priority] ?? n.priority}</Badge>
				{:else if col.key === 'status'}
					<span class="flex flex-col items-start gap-0.5">
						<Badge
							tone={notificationStatusTone(n.status)}
							dot={n.status === 'pending' || n.status === 'sending'}
						>
							{notificationStatusLabel[n.status] ?? n.status}
						</Badge>
						{#if n.attempts > 1 || (n.status === 'failed' && n.attempts > 0)}
							<span class="text-xs text-fg-subtle">{n.attempts} Versuche</span>
						{/if}
					</span>
				{:else if col.key === 'delivery'}
					{#if n.sentAt}
						<span title={formatDateTime(n.sentAt, true)}>gesendet <RelativeTime value={n.sentAt} /></span>
					{:else if n.status === 'pending'}
						<span title={formatDateTime(n.deliverAfter, true)}
							>geplant <RelativeTime value={n.deliverAfter} /></span
						>
					{:else}
						<span class="text-fg-subtle">–</span>
					{/if}
				{:else if col.key === 'events'}
					{@const ids = n.eventIds ?? []}
					<span class="flex flex-wrap gap-x-2 gap-y-0.5">
						{#each ids.slice(0, 3) as eid (eid)}
							<a href="/events?id={eid}" class="link mono text-xs">#{eid}</a>
						{/each}
						{#if ids.length > 3}<span class="text-xs text-fg-subtle">+{ids.length - 3}</span>{/if}
						{#if !ids.length}<span class="text-fg-subtle">–</span>{/if}
					</span>
				{/if}
			{/snippet}
			{#snippet empty()}
				{#if filtered}
					<EmptyState compact icon="filter" title="Keine Benachrichtigungen für diesen Filter">
						{#snippet actions()}
							<Button
								size="sm"
								onclick={() => setParams({ status: null, publisher: null, rule: null, offset: null })}
								>Filter zurücksetzen</Button
							>
						{/snippet}
					</EmptyState>
				{:else}
					<EmptyState
						compact
						icon="bell"
						title="Noch keine Benachrichtigungen"
						description="Sobald eine Regel greift, erscheinen die geplanten und verschickten Benachrichtigungen hier."
					/>
				{/if}
			{/snippet}
		</Table>
		<Pagination
			total={data.data?.total ?? 0}
			{offset}
			{limit}
			sizes={SIZES}
			onchange={(o, l) => setParams({ offset: o || null, limit: l === 50 ? null : l })}
		/>
	{/if}
</div>
