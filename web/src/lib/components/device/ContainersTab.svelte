<!--
	"Container" tab: containers grouped by compose project (ports, image, state, created)
	and the image list (GET /devices/{id}/containers, ?history=1 adds removed containers).
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { ContainerData, ContainerView, ImageView } from '$lib/api/types';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import Table, { type Column } from '$lib/components/ui/Table.svelte';
	import Toggle from '$lib/components/ui/Toggle.svelte';
	import { formatBytes, formatDateTime } from '$lib/utils/format';
	import { containerStateLabel, containerStateTone, LazyData, sourceName } from './util';

	interface Props {
		deviceId: number;
		version: number;
		active: boolean;
	}

	let { deviceId, version, active }: Props = $props();

	let history = $state(false);
	const data = new LazyData<ContainerData>();

	$effect(() => {
		if (!active) return;
		const h = history;
		data.ensure(`${version}|${h}`, (signal) =>
			api.get('/api/v1/devices/{id}/containers', {
				path: { id: deviceId },
				query: { history: h || undefined },
				signal
			})
		);
	});
	$effect(() => () => data.abort());

	const containers = $derived(data.data?.containers ?? []);
	const images = $derived(
		[...(data.data?.images ?? [])].sort(
			(a, b) => b.inUse - a.inUse || (a.tags?.[0] ?? '').localeCompare(b.tags?.[0] ?? '')
		)
	);
	const groups = $derived.by(() => {
		const map = new Map<string, ContainerView[]>();
		for (const c of containers) {
			const k = c.composeProject || '';
			let l = map.get(k);
			if (!l) map.set(k, (l = []));
			l.push(c);
		}
		return [...map.entries()]
			.sort(([a], [b]) => (a === '' ? 1 : b === '' ? -1 : a.localeCompare(b)))
			.map(([project, list]) => ({
				project,
				list: list.sort(
					(a, b) =>
						Number(!!a.goneAt) - Number(!!b.goneAt) ||
						(a.composeService || a.name).localeCompare(b.composeService || b.name)
				),
				running: list.filter((c) => !c.goneAt && c.state === 'running').length,
				current: list.filter((c) => !c.goneAt).length
			}));
	});

	function portText(p: NonNullable<ContainerView['ports']>[number]): string {
		const proto = p.type && p.type !== 'tcp' ? `/${p.type}` : '';
		if (p.publicPort)
			return `${p.ip && p.ip !== '0.0.0.0' ? p.ip + ':' : ''}${p.publicPort} → ${p.privatePort}${proto}`;
		return `${p.privatePort}${proto}`;
	}

	function shortId(id: string): string {
		return id.replace(/^sha256:/, '').slice(0, 12);
	}

	const columns: Column<ContainerView>[] = [
		{ key: 'name', label: 'Container', cell: nameCell },
		{ key: 'state', label: 'Zustand', cell: stateCell },
		{ key: 'image', label: 'Image', cell: imageCell },
		{ key: 'ports', label: 'Ports', hideBelow: 'md', cell: portsCell },
		{
			key: 'networks',
			label: 'Netzwerke',
			hideBelow: 'lg',
			value: (c) => (c.networks ?? []).join(', ') || '–'
		},
		{ key: 'created', label: 'Erstellt', hideBelow: 'sm', cell: createdCell }
	];

	const imageColumns: Column<ImageView>[] = [
		{ key: 'tags', label: 'Image', cell: tagsCell },
		{ key: 'id', label: 'ID', hideBelow: 'sm', cell: idCell },
		{ key: 'size', label: 'Größe', align: 'right', value: (i) => formatBytes(i.size) },
		{ key: 'created', label: 'Erstellt', hideBelow: 'md', value: (i) => formatDateTime(i.created) },
		{ key: 'inUse', label: 'Verwendet', align: 'right', cell: inUseCell }
	];
</script>

{#snippet nameCell(c: ContainerView)}
	<span class="font-medium {c.goneAt ? 'text-fg-subtle line-through' : ''}">{c.name}</span>
	{#if c.composeService && c.composeService !== c.name}
		<span class="block text-xs text-fg-subtle">Dienst {c.composeService}</span>
	{/if}
{/snippet}
{#snippet stateCell(c: ContainerView)}
	{#if c.goneAt}
		<Badge tone="neutral">entfernt</Badge>
		<span class="block text-xs whitespace-nowrap text-fg-subtle">{formatDateTime(c.goneAt)}</span>
	{:else}
		<Badge tone={containerStateTone(c.state)} dot
			>{containerStateLabel[c.state ?? ''] ?? c.state ?? '–'}</Badge
		>
		{#if c.status}<span class="block text-xs text-fg-subtle">{c.status}</span>{/if}
	{/if}
{/snippet}
{#snippet imageCell(c: ContainerView)}
	<span class="mono text-xs break-all">{c.image}</span>
{/snippet}
{#snippet portsCell(c: ContainerView)}
	{#if c.ports?.length}
		<span class="flex flex-col">
			{#each c.ports as p, i (i)}<span class="mono text-xs whitespace-nowrap">{portText(p)}</span>{/each}
		</span>
	{:else}<span class="text-fg-subtle">–</span>{/if}
{/snippet}
{#snippet createdCell(c: ContainerView)}
	<span class="whitespace-nowrap text-fg-muted">{formatDateTime(c.created)}</span>
{/snippet}
{#snippet tagsCell(i: ImageView)}
	{#each i.tags?.length ? i.tags : ['<none>'] as t (t)}<span class="mono block text-xs break-all">{t}</span
		>{/each}
{/snippet}
{#snippet idCell(i: ImageView)}<span class="mono text-xs text-fg-muted">{shortId(i.id)}</span>{/snippet}
{#snippet inUseCell(i: ImageView)}
	{#if i.inUse > 0}<Badge tone="ok">{i.inUse}×</Badge>{:else}<span class="text-xs text-fg-subtle"
			>ungenutzt</span
		>{/if}
{/snippet}

<div class="flex flex-col gap-4">
	<div class="flex flex-wrap items-center gap-3">
		<h2 class="flex-1 text-sm font-semibold">Container</h2>
		<Toggle bind:checked={history} label="Entfernte Container anzeigen" size="sm" />
	</div>

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton rows={4} />
	{:else if !containers.length && !images.length}
		<div class="rounded-lg border border-border bg-surface">
			<EmptyState
				icon="box"
				title="Keine Container"
				description="Container liefern das Docker-Plugin (Docker-Socket, auch per SSH) und das SSH-Inventar."
			/>
		</div>
	{:else}
		{#each groups as g (g.project)}
			<Card padding="none">
				{#snippet header()}
					<div class="flex min-w-0 flex-1 flex-wrap items-center gap-x-2">
						<Icon name={g.project ? 'layers' : 'box'} size={16} class="text-fg-subtle" />
						<h3 class="text-sm font-semibold">
							{g.project ? g.project : 'Ohne Compose-Projekt'}
						</h3>
						<span class="text-xs text-fg-subtle">
							{g.running}/{g.current} laufen{g.project ? ' · Compose' : ''}
						</span>
					</div>
				{/snippet}
				<Table
					{columns}
					rows={g.list}
					key={(c) => c.rowId}
					dense
					class="rounded-none border-0"
					caption="Container {g.project}"
					rowClass={(c) => (c.goneAt ? 'opacity-70' : '')}
				/>
			</Card>
		{/each}
		{#if containers.length}
			<p class="text-xs text-fg-subtle">
				Quelle: {[...new Set(containers.map((c) => sourceName(c.source)))].join(', ')} · zuletzt gesehen
				<RelativeTime
					value={containers
						.map((c) => c.lastSeen)
						.sort()
						.at(-1)}
				/>
			</p>
		{/if}

		{#if images.length}
			<section aria-labelledby="img-h" class="flex flex-col gap-2">
				<h2 id="img-h" class="text-sm font-semibold">Images ({images.length})</h2>
				<Table columns={imageColumns} rows={images} key={(i) => i.id} dense caption="Container-Images" />
			</section>
		{/if}
	{/if}
</div>
