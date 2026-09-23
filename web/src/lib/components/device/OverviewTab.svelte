<!--
	"Überblick" tab: ping charts, facts per source (and which source won), IP history,
	MACs, notes, custom fields, presence per plugin, parent/children and external refs.
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { DeviceDetail, Relation } from '$lib/api/types';
	import { formatCustom } from '$lib/components/devices/columns';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import JsonView from '$lib/components/ui/JsonView.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import StatusDot from '$lib/components/ui/StatusDot.svelte';
	import { customFields } from '$lib/stores/catalog.svelte';
	import { formatDateTime, formatNumber } from '$lib/utils/format';
	import { deviceTypeName, factKindLabel, relationKindLabel } from '$lib/utils/labels';
	import NotesCard from './NotesCard.svelte';
	import PingCharts from './PingCharts.svelte';
	import { groupFacts, LazyData, sourceName } from './util';

	interface Props {
		device: DeviceDetail;
		version: number;
		active: boolean;
		notesEditing?: boolean;
		onchanged: (next?: DeviceDetail) => void;
		onshowrelations: () => void;
		/** opens the edit dialog (custom fields) */
		onedit: () => void;
	}

	let {
		device: d,
		version,
		active,
		notesEditing = $bindable(false),
		onchanged,
		onshowrelations,
		onedit
	}: Props = $props();

	/** winning source per fact kind, computed by the backend from the configured priorities */
	const win = $derived(d.effectiveSources ?? {});
	const factGroups = $derived(groupFacts(d.facts ?? []));
	const defs = $derived(customFields.value ?? []);
	const filledDefs = $derived(
		defs.map((def) => ({ def, v: formatCustom(d.custom?.[def.key], def.type) })).filter((x) => x.v)
	);

	// parent/children from the relations (small request, shared shape with the Beziehungen tab)
	const rel = new LazyData<Relation[]>();
	$effect(() => {
		if (!active) return;
		rel.ensure(
			String(version),
			async (signal) =>
				((await api.get('/api/v1/devices/{id}/relations', { path: { id: d.id }, signal })) ??
					[]) as Relation[]
		);
	});
	$effect(() => () => rel.abort());
	const parents = $derived((rel.data ?? []).filter((r) => r.childId === d.id));
	const children = $derived((rel.data ?? []).filter((r) => r.parentId === d.id));

	function factValue(kind: string, value: string): string {
		return kind === 'type' ? deviceTypeName(value) : value;
	}

	function accuracy(extra: unknown): number | null {
		if (extra && typeof extra === 'object' && 'accuracy' in extra) {
			const a = (extra as { accuracy: unknown }).accuracy;
			return typeof a === 'number' ? a : null;
		}
		return null;
	}
</script>

<div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
	<div class="flex min-w-0 flex-col gap-4 xl:col-span-2">
		<PingCharts deviceId={d.id} {version} {active} />

		<Card
			title="Eigenschaften nach Quelle"
			description="Welche Quelle was meldet – markiert ist der verwendete Wert"
			icon="layers"
			padding="none"
		>
			{#if factGroups.length}
				<div class="relative overflow-x-auto">
					<table class="w-full border-separate border-spacing-0 text-sm">
						<caption class="sr-only">Eigenschaften nach Quelle</caption>
						<thead>
							<tr class="text-left text-xs text-fg-muted">
								<th scope="col" class="border-b border-border px-4 py-2 font-semibold">Merkmal</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">Wert</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">Quelle</th>
								<th
									scope="col"
									class="hidden border-b border-border px-3 py-2 font-semibold whitespace-nowrap sm:table-cell"
									>Zuletzt gemeldet</th
								>
							</tr>
						</thead>
						<tbody>
							{#each factGroups as g (g.kind)}
								{#each g.items as f, i (f.source)}
									{@const used = win[f.kind] === f.source}
									{@const acc = accuracy(f.extra)}
									<tr class={used ? 'bg-accent-soft/60' : ''}>
										{#if i === 0}
											<th
												scope="rowgroup"
												rowspan={g.items.length}
												class="border-b border-border px-4 py-1.5 text-left align-top text-xs font-medium whitespace-nowrap text-fg-muted"
											>
												{factKindLabel[g.kind] ?? g.kind}
											</th>
										{/if}
										<td class="min-w-36 border-b border-border px-3 py-1.5 break-words">
											<span class={used ? 'font-medium text-fg' : 'text-fg-muted'}
												>{factValue(f.kind, f.value)}</span
											>
											{#if acc !== null}<span class="ml-1 text-xs text-fg-subtle">({acc} %)</span>{/if}
										</td>
										<td class="border-b border-border px-3 py-1.5">
											<span class="inline-flex flex-wrap items-center gap-x-1.5 gap-y-0.5 whitespace-nowrap">
												{sourceName(f.source)}
												{#if used}
													<Badge tone="accent" title="Dieser Wert wird verwendet">verwendet</Badge>
												{/if}
											</span>
										</td>
										<td class="hidden border-b border-border px-3 py-1.5 text-fg-muted sm:table-cell">
											<RelativeTime value={f.lastSeen} />
										</td>
									</tr>
								{/each}
							{/each}
						</tbody>
					</table>
				</div>
				<p class="px-4 py-2 text-xs text-fg-subtle">
					Manuelle Werte haben immer Vorrang. Die Reihenfolge der übrigen Quellen für den Hostnamen lässt sich
					unter <a href="/system" class="link">System</a> einstellen.
				</p>
			{:else}
				<p class="px-4 py-3 text-sm text-fg-subtle">Noch keine Eigenschaften gemeldet.</p>
			{/if}
		</Card>

		<Card title="IP-Adressen" icon="network" padding="none" description="Aktuelle und frühere Zuordnungen">
			{#if d.ipHistory?.length}
				<div class="relative overflow-x-auto">
					<table class="w-full border-separate border-spacing-0 text-sm">
						<caption class="sr-only">IP-Historie</caption>
						<thead>
							<tr class="text-left text-xs text-fg-muted">
								<th scope="col" class="border-b border-border px-4 py-2 font-semibold">IP</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">MAC</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">Quelle</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">Erstsichtung</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">Status</th>
							</tr>
						</thead>
						<tbody>
							{#each d.ipHistory as ip (ip.ip + ip.mac + ip.firstSeen)}
								<tr class={ip.goneAt ? 'text-fg-subtle' : ''}>
									<td class="mono border-b border-border px-4 py-1.5 whitespace-nowrap">{ip.ip}</td>
									<td class="mono border-b border-border px-3 py-1.5 whitespace-nowrap text-fg-muted">
										{ip.mac || '–'}
									</td>
									<td class="border-b border-border px-3 py-1.5 whitespace-nowrap">{sourceName(ip.source)}</td
									>
									<td class="border-b border-border px-3 py-1.5 whitespace-nowrap">
										{formatDateTime(ip.firstSeen)}
									</td>
									<td class="border-b border-border px-3 py-1.5 whitespace-nowrap">
										{#if ip.goneAt}
											nicht mehr seit {formatDateTime(ip.goneAt)}
										{:else}
											<span class="inline-flex items-center gap-1.5">
												<span aria-hidden="true" class="inline-flex"
													><StatusDot status="ok" pulse={false} /></span
												>
												aktuell · gesehen <RelativeTime value={ip.lastSeen} />
											</span>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else}
				<p class="px-4 py-3 text-sm text-fg-subtle">Keine IP-Adresse bekannt.</p>
			{/if}
		</Card>

		<Card title="MAC-Adressen" icon="link" padding="none">
			{#if d.macList?.length}
				<div class="relative overflow-x-auto">
					<table class="w-full border-separate border-spacing-0 text-sm">
						<caption class="sr-only">MAC-Adressen</caption>
						<thead>
							<tr class="text-left text-xs text-fg-muted">
								<th scope="col" class="border-b border-border px-4 py-2 font-semibold">MAC</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">Hersteller (OUI)</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">Quelle</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">Erstsichtung</th>
								<th scope="col" class="border-b border-border px-3 py-2 font-semibold">Zuletzt</th>
							</tr>
						</thead>
						<tbody>
							{#each d.macList as m (m.mac)}
								<tr>
									<td class="border-b border-border px-4 py-1.5 whitespace-nowrap">
										<span class="mono">{m.mac}</span>
										{#if m.randomized}
											<Badge tone="warn" title="Lokal verwaltete, zufällige MAC (z. B. Privatsphäre-Modus)"
												>zufällig</Badge
											>
										{/if}
									</td>
									<td class="border-b border-border px-3 py-1.5">{m.vendor || '–'}</td>
									<td class="border-b border-border px-3 py-1.5 whitespace-nowrap">{sourceName(m.source)}</td>
									<td class="border-b border-border px-3 py-1.5 whitespace-nowrap">
										{formatDateTime(m.firstSeen)}
									</td>
									<td class="border-b border-border px-3 py-1.5 whitespace-nowrap text-fg-muted">
										<RelativeTime value={m.lastSeen} />
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else}
				<p class="px-4 py-3 text-sm text-fg-subtle">Keine MAC-Adresse bekannt.</p>
			{/if}
		</Card>
	</div>

	<div class="flex min-w-0 flex-col gap-4">
		<NotesCard device={d} bind:editing={notesEditing} onsaved={(next) => onchanged(next)} />

		{#if defs.length}
			<Card title="Custom Fields" icon="tag" padding="md">
				{#snippet actions()}
					<Button size="xs" variant="ghost" icon="edit" onclick={onedit}>Bearbeiten</Button>
				{/snippet}
				{#if filledDefs.length}
					<dl class="grid grid-cols-1 gap-2.5">
						{#each filledDefs as { def, v } (def.id)}
							<div class="min-w-0">
								<dt class="text-xs text-fg-subtle" title={def.description || undefined}>{def.label}</dt>
								<dd class="mt-0.5 text-sm break-words">
									{#if def.type === 'url'}
										<a href={v} class="link" target="_blank" rel="noopener noreferrer">{v}</a>
									{:else}
										{v}
									{/if}
								</dd>
							</div>
						{/each}
					</dl>
				{/if}
				{#if filledDefs.length < defs.length}
					<p class="text-xs text-fg-subtle {filledDefs.length ? 'mt-2.5' : ''}">
						{filledDefs.length ? 'Ohne Wert' : 'Noch keine Werte'}:
						{defs
							.filter((x) => !filledDefs.some((f) => f.def.id === x.id))
							.map((x) => x.label)
							.join(', ')}
					</p>
				{/if}
			</Card>
		{/if}

		<Card title="Anwesenheit je Plugin" icon="radar" padding="none" description="Verpasste Läufe in Folge">
			{#if d.presence?.length}
				<ul class="divide-y divide-border">
					{#each d.presence as p (p.plugin)}
						<li class="flex items-center gap-3 px-4 py-2 text-sm">
							<span aria-hidden="true" class="inline-flex">
								<StatusDot status={p.missed > 0 ? 'warn' : 'ok'} pulse={false} />
							</span>
							<span class="min-w-0 flex-1 truncate">{sourceName(p.plugin)}</span>
							{#if p.missed > 0}
								<Badge tone="warn" title="Läufe ohne Antwort seit der letzten Sichtung">
									{formatNumber(p.missed)}× verpasst
								</Badge>
							{/if}
							<RelativeTime value={p.lastSeen} class="text-xs text-fg-subtle" />
						</li>
					{/each}
				</ul>
			{:else}
				<p class="px-4 py-3 text-sm text-fg-subtle">Noch von keinem Scanner gesehen.</p>
			{/if}
		</Card>

		<Card title="Eltern & Kinder" icon="topology" padding="none">
			{#snippet actions()}
				{#if (rel.data?.length ?? 0) > 0}
					<Button size="xs" variant="ghost" iconRight="arrow-right" onclick={onshowrelations}>Alle</Button>
				{/if}
			{/snippet}
			{#if !rel.data}
				<p class="px-4 py-3 text-sm text-fg-subtle">{rel.error ? 'Nicht verfügbar' : 'Wird geladen …'}</p>
			{:else if !parents.length && !children.length}
				<p class="px-4 py-3 text-sm text-fg-subtle">Keine Beziehungen bekannt.</p>
			{:else}
				{#each [{ label: 'Eltern', list: parents, parent: true }, { label: 'Kinder', list: children, parent: false }] as grp (grp.label)}
					{#if grp.list.length}
						<h3 class="px-4 pt-2.5 text-[0.7rem] font-semibold tracking-wider text-fg-subtle uppercase">
							{grp.label} ({grp.list.length})
						</h3>
						<ul class="pb-1.5">
							{#each grp.list.slice(0, 6) as r (r.id)}
								{@const otherId = grp.parent ? r.parentId : r.childId}
								{@const otherName = grp.parent ? r.parentName : r.childName}
								<li class="flex items-center gap-2 px-4 py-1 text-sm">
									<a href="/devices/{otherId}" class="link min-w-0 flex-1 truncate"
										>{otherName || `#${otherId}`}</a
									>
									<span class="text-xs text-fg-subtle">{relationKindLabel[r.kind] ?? r.kind}</span>
								</li>
							{/each}
							{#if grp.list.length > 6}
								<li class="px-4 py-1">
									<button type="button" class="text-xs text-accent hover:underline" onclick={onshowrelations}>
										+ {grp.list.length - 6} weitere
									</button>
								</li>
							{/if}
						</ul>
					{/if}
				{/each}
			{/if}
		</Card>

		{#if d.refs?.length}
			<Card title="Externe Referenzen" icon="external" padding="none">
				<ul class="divide-y divide-border">
					{#each d.refs as r (r.source + r.ref)}
						<li class="px-4 py-2 text-sm">
							<div class="flex items-center gap-2">
								<span class="font-medium">{sourceName(r.source)}</span>
								<span class="mono min-w-0 flex-1 truncate text-fg-muted" title={r.ref}>{r.ref}</span>
								<RelativeTime value={r.lastSeen} class="text-xs text-fg-subtle" />
							</div>
							{#if r.data && typeof r.data === 'object' && Object.keys(r.data).length}
								<details class="mt-1">
									<summary class="cursor-pointer text-xs text-fg-subtle hover:text-fg">Daten</summary>
									<JsonView value={r.data} openDepth={1} maxHeight="16rem" class="mt-1" />
								</details>
							{/if}
						</li>
					{/each}
				</ul>
			</Card>
		{/if}
	</div>
</div>
