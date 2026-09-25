<!-- Audit log (GET /api/v1/audit) with filters in the URL, paging and a before/after diff. -->
<script lang="ts">
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { AuditEntry } from '$lib/api';
	import type { ApiAuditList } from '$lib/api/generated';
	import {
		Badge,
		Button,
		Card,
		EmptyState,
		ErrorState,
		Input,
		Pagination,
		Select,
		Table
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { formatDateTime } from '$lib/utils/format';
	import { debounce, intParam, setParams } from '$lib/utils/url';
	import AuditDiff from './AuditDiff.svelte';
	import { entityHref, entityLabel } from './system';

	const SIZES = [50, 100, 250, 500];
	const sp = $derived(page.url.searchParams);
	const entity = $derived(sp.get('entity') ?? '');
	const entityId = $derived(sp.get('id') ?? '');
	const action = $derived(sp.get('action') ?? '');
	const q = $derived(sp.get('q') ?? '');
	const offset = $derived(Math.max(0, intParam(sp, 'offset', 0)));
	const limit = $derived(SIZES.includes(intParam(sp, 'limit', 50)) ? intParam(sp, 'limit', 50) : 50);

	let qText = $state(untrack(() => page.url.searchParams.get('q') ?? ''));
	let actionText = $state(untrack(() => page.url.searchParams.get('action') ?? ''));
	let idText = $state(untrack(() => page.url.searchParams.get('id') ?? ''));
	const apply = debounce((k: string, v: string) => setParams({ [k]: v.trim() || null, offset: null }), 300);
	$effect(() => () => apply.cancel());

	const canView = $derived(auth.can('audit.view'));
	const data = new AsyncData<ApiAuditList>();
	$effect(() => {
		if (!canView) return;
		const query = {
			entity: entity || null,
			id: entityId || null,
			action: action || null,
			q: q || null,
			limit,
			offset
		};
		data.run((signal) => api.get('/api/v1/audit', { query, signal }));
	});

	let open = $state<Record<number, boolean>>({});
	const hasDiff = (e: AuditEntry) =>
		(e.before !== undefined && e.before !== null) || (e.after !== undefined && e.after !== null);

	const entityOptions = Object.entries(entityLabel)
		.map(([value, label]) => ({ value, label }))
		.sort((a, b) => a.label.localeCompare(b.label, 'de'));

	const hasFilter = $derived(!!(entity || entityId || action || q));
	function reset() {
		qText = actionText = idText = '';
		setParams({ entity: null, id: null, action: null, q: null, offset: null });
	}

	function actionTone(a: string) {
		if (a.includes('delete') || a.includes('login_failed') || a.includes('restore') || a.includes('rotate'))
			return 'danger' as const;
		if (a.includes('create') || a.includes('login')) return 'ok' as const;
		if (a.includes('update') || a.includes('settings') || a.includes('config')) return 'accent' as const;
		return 'neutral' as const;
	}

	const columns: Column<AuditEntry>[] = [
		{ key: 'ts', label: 'Zeit', width: '9.5rem' },
		{ key: 'actor', label: 'Akteur', hideBelow: 'md' },
		{ key: 'action', label: 'Aktion' },
		{ key: 'entity', label: 'Objekt', hideBelow: 'lg' },
		{ key: 'summary', label: 'Beschreibung', hideBelow: 'sm' },
		{ key: 'toggle', label: '', align: 'right', width: '3rem' }
	];
</script>

{#if !canView}
	<Card>
		<EmptyState
			icon="lock"
			title="Keine Berechtigung"
			description="Zum Anzeigen des Audit-Logs fehlt die Berechtigung „Audit-Log und Server-Protokoll einsehen“."
		/>
	</Card>
{:else}
	<Card
		title="Audit-Log"
		description="Alle manuellen Änderungen über UI und API"
		icon="history"
		padding="none"
	>
		<div class="grid grid-cols-2 gap-2 border-b border-border p-3 sm:flex sm:flex-wrap sm:items-end">
			<Select
				label="Objekttyp"
				size="sm"
				value={entity}
				options={entityOptions}
				placeholder="Alle"
				class="sm:w-44"
				onchange={(e) =>
					setParams({ entity: (e.currentTarget as HTMLSelectElement).value || null, offset: null })}
			/>
			<Input
				label="Objekt-ID"
				size="sm"
				bind:value={idText}
				oninput={() => apply('id', idText)}
				class="sm:w-28"
				placeholder="z. B. 12"
			/>
			<Input
				label="Aktion"
				size="sm"
				bind:value={actionText}
				oninput={() => apply('action', actionText)}
				class="sm:w-44"
				placeholder="z. B. device.*"
				mono
			/>
			<Input
				label="Text"
				size="sm"
				type="search"
				icon="search"
				bind:value={qText}
				oninput={() => apply('q', qText)}
				class="col-span-2 sm:w-60"
				placeholder="Beschreibung oder Akteur"
			/>
			{#if hasFilter}<Button size="sm" variant="ghost" icon="x" onclick={reset}>Zurücksetzen</Button>{/if}
		</div>
		{#if data.error && !data.data}
			<ErrorState error={data.error} onretry={() => data.reload()} />
		{:else}
			<Table
				{columns}
				rows={data.data?.items ?? []}
				key={(e) => e.id}
				loading={data.loading}
				dense
				class="rounded-none border-0"
				caption="Audit-Log"
				onrowclick={(e) => hasDiff(e) && (open[e.id] = !open[e.id])}
			>
				{#snippet cell(e, col)}
					{#if col.key === 'ts'}
						<span class="whitespace-nowrap text-fg-muted tabular">{formatDateTime(e.ts, true)}</span>
					{:else if col.key === 'actor'}
						<span class="text-fg">{e.actor || '–'}</span>
						{#if e.actorType && e.actorType !== 'user'}<Badge class="ml-1"
								>{e.actorType === 'token' ? 'Token' : e.actorType}</Badge
							>{/if}
						{#if e.ip}<span class="mono block text-xs text-fg-subtle">{e.ip}</span>{/if}
					{:else if col.key === 'action'}
						<Badge tone={actionTone(e.action)}><span class="mono">{e.action}</span></Badge>
						<span class="mt-0.5 block text-xs break-words text-fg-muted sm:hidden">{e.summary}</span>
					{:else if col.key === 'entity'}
						{@const href = entityHref(e.entityType, e.entityId)}
						<span class="text-fg-muted">{entityLabel[e.entityType] ?? e.entityType}</span>
						{#if e.entityId}
							{#if href}<a {href} class="link mono ml-1">{e.entityId}</a>{:else}<span
									class="mono ml-1 text-fg-subtle">{e.entityId}</span
								>{/if}
						{/if}
					{:else if col.key === 'summary'}
						<span class="line-clamp-2 break-words">{e.summary}</span>
					{:else if col.key === 'toggle'}
						{#if hasDiff(e)}
							<Button
								size="xs"
								variant="ghost"
								icon={open[e.id] ? 'chevron-up' : 'chevron-down'}
								label={open[e.id] ? 'Änderungen ausblenden' : 'Änderungen anzeigen'}
								aria-expanded={!!open[e.id]}
								onclick={() => (open[e.id] = !open[e.id])}
							/>
						{/if}
					{/if}
				{/snippet}
				{#snippet expanded(e)}
					{#if open[e.id]}
						<tr>
							<td colspan={columns.length} class="border-b border-border bg-surface-2 px-3 py-3">
								<AuditDiff before={e.before} after={e.after} />
							</td>
						</tr>
					{/if}
				{/snippet}
				{#snippet empty()}
					<EmptyState
						compact
						icon="history"
						title={hasFilter ? 'Keine Einträge für diese Filter' : 'Noch keine Einträge'}
					/>
				{/snippet}
			</Table>
			<div class="border-t border-border px-3 py-2">
				<Pagination
					total={data.data?.total ?? 0}
					{offset}
					{limit}
					sizes={SIZES}
					onchange={(o, l) => setParams({ offset: o || null, limit: l === 50 ? null : l })}
				/>
			</div>
		{/if}
	</Card>
{/if}
