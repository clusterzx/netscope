<!--
	"CVEs" tab: GET /devices/{id}/cves (heuristic NVD match) with severity filter, search,
	sort, ignored toggle, detail rows and "Als irrelevant markieren" (POST /vulnerabilities/ignore).
-->
<script lang="ts">
	import { api, errorMessage } from '$lib/api';
	import type { Response } from '$lib/api';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Checkbox from '$lib/components/ui/Checkbox.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import Pagination from '$lib/components/ui/Pagination.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import SeverityBadge from '$lib/components/ui/SeverityBadge.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import Table, { type Column } from '$lib/components/ui/Table.svelte';
	import Textarea from '$lib/components/ui/Textarea.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDate, formatDateTime, formatNumber } from '$lib/utils/format';
	import { severityFromCvss, severityRank } from '$lib/utils/labels';
	import { LazyData, matchTypeLabel, matchTypeTone, sourceName } from './util';

	type CVEList = Response<'/api/v1/devices/{id}/cves'>;
	type Item = CVEList['items'][number];

	interface Props {
		deviceId: number;
		version: number;
		active: boolean;
		onchanged: () => void;
	}

	let { deviceId, version, active, onchanged }: Props = $props();

	let showIgnored = $state(false);
	let minSev = $state('');
	let text = $state('');
	let sort = $state('-cvss');
	let offset = $state(0);
	let limit = $state(100);

	const data = new LazyData<CVEList>();
	$effect(() => {
		if (!active) return;
		const ign = showIgnored;
		data.ensure(`${version}|${ign}`, (signal) =>
			api.get('/api/v1/devices/{id}/cves', {
				path: { id: deviceId },
				query: { ignored: ign || undefined },
				signal
			})
		);
	});
	$effect(() => () => data.abort());

	const all = $derived(data.data?.items ?? []);
	const sevOf = (c: Item) => c.severity || severityFromCvss(c.cvss);

	const filtered = $derived.by(() => {
		const t = text.trim().toLowerCase();
		const min = minSev ? severityRank[minSev] : -1;
		const list = all.filter(
			(c) =>
				(severityRank[sevOf(c)] ?? 0) >= min &&
				(!t ||
					c.cve.toLowerCase().includes(t) ||
					c.product.toLowerCase().includes(t) ||
					(c.description ?? '').toLowerCase().includes(t))
		);
		const desc = sort.startsWith('-');
		const f = desc ? sort.slice(1) : sort;
		const val = (c: Item): number | string => {
			switch (f) {
				case 'cvss':
					return c.cvss ?? -1;
				case 'published':
					return c.published ? new Date(c.published).getTime() : 0;
				case 'firstSeen':
					return new Date(c.firstSeen).getTime();
				case 'product':
					return `${c.product} ${c.version}`.toLowerCase();
				default:
					return c.cve;
			}
		};
		return list.sort((a, b) => {
			const va = val(a);
			const vb = val(b);
			const r = va < vb ? -1 : va > vb ? 1 : 0;
			return (desc ? -r : r) || (b.cvss ?? 0) - (a.cvss ?? 0) || a.cve.localeCompare(b.cve);
		});
	});

	// back to page 1 whenever the filter changes
	$effect(() => {
		void [minSev, text, sort, showIgnored];
		offset = 0;
	});

	const pageRows = $derived(filtered.slice(offset, offset + limit));
	const summary = $derived.by(() => {
		const out: Record<string, number> = {};
		for (const c of all) if (!c.ignored) out[sevOf(c)] = (out[sevOf(c)] ?? 0) + 1;
		return out;
	});
	const ignoredCount = $derived(all.filter((c) => c.ignored).length);

	// ---------------------------------------------------------------- expand
	let open = $state<Record<number, boolean>>({});

	// ---------------------------------------------------------------- ignore
	let ignoreOpen = $state(false);
	let ignoreItem = $state<Item | null>(null);
	let note = $state('');
	let busy = $state(false);
	let ignoreError = $state('');

	function askIgnore(c: Item) {
		ignoreItem = c;
		note = '';
		ignoreError = '';
		ignoreOpen = true;
	}

	async function setIgnored(c: Item, ignored: boolean, n = '') {
		busy = true;
		try {
			await api.post('/api/v1/vulnerabilities/ignore', {
				body: { deviceId, cve: c.cve, ignored, note: n.trim() || undefined }
			});
			toast.success(ignored ? `${c.cve} als irrelevant markiert` : `${c.cve} wieder als relevant markiert`);
			ignoreOpen = false;
			data.invalidate();
			await data.reload();
			onchanged();
		} catch (e) {
			if (ignoreOpen) ignoreError = errorMessage(e);
			else toast.error(e);
		} finally {
			busy = false;
		}
	}

	const canManage = $derived(auth.can('vulns.manage'));
	const allColumns: Column<Item>[] = [
		{ key: 'expand', label: '', width: '2.25rem', cell: expandCell },
		{ key: 'cve', label: 'CVE', sortable: true, cell: cveCell },
		{ key: 'cvss', label: 'CVSS', sortable: true, sortDesc: true, cell: cvssCell },
		{ key: 'product', label: 'Produkt', sortable: true, cell: productCell },
		{ key: 'matchType', label: 'Abgleich', hideBelow: 'md', cell: matchCell },
		{ key: 'source', label: 'Quelle', hideBelow: 'lg', value: (c) => sourceName(c.source) },
		{
			key: 'firstSeen',
			label: 'Erstsichtung',
			sortable: true,
			sortDesc: true,
			hideBelow: 'lg',
			cell: seenCell
		},
		{ key: 'actions', label: 'Aktion', align: 'right', cell: actionCell }
	];
	const columns = $derived(canManage ? allColumns : allColumns.filter((c) => c.key !== 'actions'));

	const sevOptions = [
		{ value: '', label: 'Alle Schweregrade' },
		{ value: 'low', label: 'ab Niedrig' },
		{ value: 'medium', label: 'ab Mittel' },
		{ value: 'high', label: 'ab Hoch' },
		{ value: 'critical', label: 'nur Kritisch' }
	];
	const sortOptions = [
		{ value: '-cvss', label: 'CVSS absteigend' },
		{ value: 'cvss', label: 'CVSS aufsteigend' },
		{ value: '-published', label: 'Neueste CVEs zuerst' },
		{ value: '-firstSeen', label: 'Zuletzt erkannt zuerst' },
		{ value: 'product', label: 'Produkt' },
		{ value: 'cve', label: 'CVE-ID' }
	];
</script>

{#snippet expandCell(c: Item)}
	<Button
		size="xs"
		variant="ghost"
		icon={open[c.id] ? 'chevron-down' : 'chevron-right'}
		label={open[c.id] ? 'Details ausblenden' : 'Details anzeigen'}
		aria-expanded={!!open[c.id]}
		onclick={() => (open[c.id] = !open[c.id])}
	/>
{/snippet}
{#snippet cveCell(c: Item)}
	<a href="/vulnerabilities/{c.cve}" class="link mono whitespace-nowrap {c.ignored ? 'line-through' : ''}"
		>{c.cve}</a
	>
	{#if c.published}<span class="hidden text-xs text-fg-subtle sm:block"
			>veröffentlicht {formatDate(c.published)}</span
		>{/if}
{/snippet}
{#snippet cvssCell(c: Item)}
	{#if c.cvss}<SeverityBadge cvss={c.cvss} />{:else}<SeverityBadge severity={sevOf(c)} />{/if}
{/snippet}
{#snippet productCell(c: Item)}
	<span>{c.product || '–'}</span>
	{#if c.version}<span class="mono text-fg-muted"> {c.version}</span>{/if}
{/snippet}
{#snippet matchCell(c: Item)}
	<Badge tone={matchTypeTone(c.matchType)}>{matchTypeLabel[c.matchType] ?? c.matchType}</Badge>
{/snippet}
{#snippet seenCell(c: Item)}
	<span class="whitespace-nowrap text-fg-muted">{formatDate(c.firstSeen)}</span>
{/snippet}
{#snippet actionCell(c: Item)}
	{#if c.ignored}
		<Button
			size="xs"
			icon="eye"
			label="Wieder als relevant markieren"
			disabled={busy}
			onclick={() => setIgnored(c, false)}><span class="hidden sm:inline">Wieder relevant</span></Button
		>
	{:else}
		<Button
			size="xs"
			variant="ghost"
			icon="eye-off"
			label="Als irrelevant markieren"
			disabled={busy}
			onclick={() => askIgnore(c)}><span class="hidden sm:inline">Irrelevant</span></Button
		>
	{/if}
{/snippet}

<div class="flex flex-col gap-3">
	{#if data.data?.disclaimer}
		<Alert tone="warn" title="Heuristischer Abgleich – Treffer prüfen">
			<p class="text-sm">{data.data.disclaimer}</p>
		</Alert>
	{/if}

	<div class="flex flex-col gap-2 lg:flex-row lg:items-end">
		<Input
			size="sm"
			icon="search"
			type="search"
			bind:value={text}
			placeholder="CVE, Produkt oder Beschreibung"
			aria-label="CVEs durchsuchen"
			class="lg:w-72"
		/>
		<Select
			size="sm"
			bind:value={minSev}
			options={sevOptions}
			aria-label="Mindest-Schweregrad"
			class="lg:w-44"
		/>
		<Select size="sm" bind:value={sort} options={sortOptions} aria-label="Sortierung" class="lg:w-52" />
		<Checkbox
			bind:checked={showIgnored}
			label="Als irrelevant markierte anzeigen"
			class="lg:mb-1.5 lg:ml-2"
		/>
	</div>

	{#if data.data && all.length}
		<p class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-fg-muted">
			{#each ['critical', 'high', 'medium', 'low', 'info'] as s (s)}
				{#if summary[s]}
					<span class="inline-flex items-center gap-1"
						><SeverityBadge severity={s} /> {formatNumber(summary[s])}</span
					>
				{/if}
			{/each}
			{#if ignoredCount}<span>· {formatNumber(ignoredCount)} als irrelevant markiert</span>{/if}
			{#if filtered.length !== all.length}<span>· {formatNumber(filtered.length)} angezeigt</span>{/if}
		</p>
	{/if}

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton rows={6} />
	{:else}
		<Table
			{columns}
			rows={pageRows}
			key={(c) => c.id}
			{sort}
			onsort={(s) => (sort = s)}
			loading={data.loading}
			dense
			caption="CVEs des Geräts"
			rowClass={(c) => (c.ignored ? 'opacity-60' : '')}
		>
			{#snippet expanded(c)}
				{#if open[c.id]}
					<tr class="bg-surface-2">
						<td colspan={columns.length} class="border-b border-border px-4 py-3">
							<div class="flex max-w-4xl flex-col gap-2 text-sm">
								<p class="break-words whitespace-pre-line">{c.description || 'Keine Beschreibung.'}</p>
								<dl class="grid grid-cols-[9rem_1fr] gap-x-3 gap-y-1 text-xs">
									{#if c.vector}
										<dt class="text-fg-subtle">Vektor</dt>
										<dd class="mono break-all">
											{c.vector}{c.cvssVersion ? ` (CVSS ${c.cvssVersion})` : ''}
										</dd>
									{/if}
									{#if c.cwes?.length}
										<dt class="text-fg-subtle">Schwäche (CWE)</dt>
										<dd class="mono">{c.cwes.join(', ')}</dd>
									{/if}
									<dt class="text-fg-subtle">Abgleich</dt>
									<dd>
										{matchTypeLabel[c.matchType] ?? c.matchType} über
										<span class="mono break-all">{c.cpe || '–'}</span> ({sourceName(c.source)})
									</dd>
									<dt class="text-fg-subtle">Erkannt</dt>
									<dd>seit {formatDateTime(c.firstSeen)} · zuletzt {formatDateTime(c.lastSeen)}</dd>
									{#if c.ignored}
										<dt class="text-fg-subtle">Irrelevant</dt>
										<dd>
											{c.ignoredBy ? `von ${c.ignoredBy}` : ''}
											{c.ignoredAt ? `am ${formatDateTime(c.ignoredAt)}` : ''}
											{#if c.ignoreNote}<span class="block text-fg">„{c.ignoreNote}“</span>{/if}
										</dd>
									{/if}
								</dl>
								{#if c.refs?.length}
									<details>
										<summary class="cursor-pointer text-xs text-fg-subtle hover:text-fg">
											Referenzen ({c.refs.length})
										</summary>
										<ul class="mt-1 flex flex-col gap-0.5 text-xs">
											{#each c.refs as r (r.url)}
												<li class="break-all">
													<a href={r.url} class="link" target="_blank" rel="noopener noreferrer">{r.url}</a>
													{#if r.tags?.length}<span class="text-fg-subtle"> · {r.tags.join(', ')}</span>{/if}
												</li>
											{/each}
										</ul>
									</details>
								{/if}
								<a href="/vulnerabilities/{c.cve}" class="link text-xs">Alle betroffenen Geräte anzeigen →</a>
							</div>
						</td>
					</tr>
				{/if}
			{/snippet}
			{#snippet empty()}
				{#if all.length}
					<EmptyState
						compact
						icon="filter"
						title="Keine Treffer"
						description="Keine CVE passt zu den Filtern."
					/>
				{:else}
					<EmptyState
						compact
						icon="shield"
						title="Keine CVEs gefunden"
						description="Für die erkannten Produkte und Versionen dieses Geräts liegen keine passenden Einträge in der lokalen NVD-Kopie vor."
					/>
				{/if}
			{/snippet}
		</Table>
		{#if filtered.length > limit}
			<Pagination total={filtered.length} bind:offset bind:limit sizes={[50, 100, 250]} />
		{/if}
	{/if}
</div>

<Modal
	bind:open={ignoreOpen}
	title="Als irrelevant markieren"
	description={ignoreItem ? `${ignoreItem.cve} · ${ignoreItem.product} ${ignoreItem.version}` : ''}
	size="sm"
	as="form"
	onsubmit={() => ignoreItem && setIgnored(ignoreItem, true, note)}
	{busy}
>
	<div class="flex flex-col gap-3">
		<p class="text-sm text-fg-muted">
			Die CVE zählt für dieses Gerät nicht mehr in Übersichten und löst keine Events mehr aus. Die Markierung
			lässt sich jederzeit zurücknehmen.
		</p>
		{#if ignoreError}<Alert tone="danger">{ignoreError}</Alert>{/if}
		<Textarea
			label="Begründung (optional)"
			bind:value={note}
			rows={3}
			maxlength={500}
			placeholder="z. B. Backport im Distributionspaket, Modul nicht aktiv"
		/>
	</div>
	{#snippet footer()}
		<Button onclick={() => (ignoreOpen = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="eye-off" loading={busy}>Als irrelevant markieren</Button>
	{/snippet}
</Modal>
