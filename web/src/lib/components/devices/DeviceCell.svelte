<!-- One cell of the device list, rendered by column key. -->
<script lang="ts">
	import type { DeviceRow } from '$lib/api/types';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import SeverityBadge from '$lib/components/ui/SeverityBadge.svelte';
	import StatusDot from '$lib/components/ui/StatusDot.svelte';
	import type { DeviceColumnDef } from './columns';
	import {
		criticalityLabel,
		criticalityTone,
		deviceTypeName,
		healthStateLabel,
		healthTone,
		stateLabel,
		stateTone
	} from '$lib/utils/labels';
	import { formatDate } from '$lib/utils/format';

	interface Props {
		row: DeviceRow;
		col: DeviceColumnDef | undefined;
		colKey: string;
	}

	let { row: d, col, colKey }: Props = $props();

	const certDays = $derived(
		d.certExpiry ? Math.floor((new Date(d.certExpiry).getTime() - Date.now()) / 86400000) : null
	);
	const title = $derived(d.name || d.ip || d.mac || `Gerät ${d.id}`);
</script>

{#if colKey === 'status'}
	<span title={d.online ? 'online' : 'offline'} class="inline-flex px-1">
		<StatusDot status={d.online ? 'online' : 'offline'} size={9} pulse={false} />
	</span>
{:else if colKey === 'name'}
	<div class="flex min-w-0 max-w-[22rem] items-center gap-1.5">
		<a
			href="/devices/{d.id}"
			class="min-w-0 truncate font-medium text-fg hover:text-accent hover:underline"
			{title}
		>
			{title}
		</a>
		{#if d.displayName && d.hostname && d.displayName !== d.hostname}
			<span class="hidden truncate text-xs text-fg-subtle xl:inline">{d.hostname}</span>
		{/if}
	</div>
{:else if colKey === 'ip'}
	<span class="mono whitespace-nowrap">{d.ip || '–'}</span>
	{#if (d.ips?.length ?? 0) > 1}<span class="text-xs text-fg-subtle"> +{d.ips.length - 1}</span>{/if}
{:else if colKey === 'mac'}
	<span class="mono whitespace-nowrap text-fg-muted">{d.mac || '–'}</span>
{:else if colKey === 'ips' || colKey === 'macs'}
	<span class="mono text-xs text-fg-muted">{col?.text?.(d) || '–'}</span>
{:else if colKey === 'vendor'}
	<span class="block max-w-[14rem] truncate text-fg-muted" title={d.vendor}>{d.vendor || '–'}</span>
{:else if colKey === 'type'}
	<span class="whitespace-nowrap">{deviceTypeName(d.type)}</span>
{:else if colKey === 'os'}
	<span class="block max-w-[14rem] truncate text-fg-muted" title={d.os}>{d.os || '–'}</span>
{:else if colKey === 'parent'}
	{#if d.parentId}
		<a href="/devices/{d.parentId}" class="link whitespace-nowrap">{d.parentName || `#${d.parentId}`}</a>
	{:else}<span class="text-fg-subtle">–</span>{/if}
{:else if colKey === 'ports'}
	{#if d.portCount === 0}
		<span class="text-fg-subtle">–</span>
	{:else if d.ports?.length}
		<span class="flex items-center gap-1 whitespace-nowrap" title={d.ports.join(', ')}>
			{#each d.ports.slice(0, 4) as p (p)}
				<span class="mono rounded bg-surface-3 px-1 text-[0.72rem] text-fg-muted"
					>{p.replace('/tcp', '')}</span
				>
			{/each}
			{#if d.ports.length > 4}<span class="text-xs text-fg-subtle">+{d.ports.length - 4}</span>{/if}
		</span>
	{:else}
		<span class="tabular">{d.portCount}</span>
	{/if}
{:else if colKey === 'cve'}
	{#if d.cveCount > 0}
		<span class="inline-flex items-center gap-1.5 whitespace-nowrap">
			<SeverityBadge cvss={d.maxCvss ?? 0} />
			<span class="text-xs text-fg-subtle">{d.cveCount}</span>
		</span>
	{:else}<span class="text-fg-subtle">–</span>{/if}
{:else if colKey === 'certExpiry'}
	{#if d.certExpiry && certDays !== null}
		<Badge tone={certDays < 0 ? 'critical' : certDays <= 14 ? 'high' : certDays <= 30 ? 'medium' : 'neutral'}>
			{certDays < 0 ? 'abgelaufen' : formatDate(d.certExpiry)}
		</Badge>
	{:else}<span class="text-fg-subtle">–</span>{/if}
{:else if colKey === 'healthState'}
	{#if d.healthState}
		<Badge tone={healthTone(d.healthState)} dot>{healthStateLabel[d.healthState] ?? d.healthState}</Badge>
	{:else}<span class="text-fg-subtle">–</span>{/if}
{:else if colKey === 'state'}
	<Badge tone={stateTone(d.state)}>{stateLabel[d.state] ?? d.state}</Badge>
{:else if colKey === 'criticality'}
	<Badge tone={criticalityTone(d.criticality)}>{criticalityLabel[d.criticality] ?? d.criticality}</Badge>
{:else if colKey === 'tags'}
	<span class="flex max-w-[14rem] flex-wrap gap-1">
		{#each (d.tags ?? []).slice(0, 4) as t (t)}
			<a
				href="/devices?q={encodeURIComponent('tag:' + t)}"
				class="rounded bg-accent-soft px-1.5 text-[0.72rem] text-accent hover:underline">{t}</a
			>
		{:else}<span class="text-fg-subtle">–</span>{/each}
		{#if (d.tags?.length ?? 0) > 4}<span class="text-xs text-fg-subtle">+{d.tags.length - 4}</span>{/if}
	</span>
{:else if colKey === 'groups'}
	<span class="flex max-w-[14rem] flex-wrap gap-1">
		{#each d.groups ?? [] as g (g.id)}
			<Badge variant="outline">{g.name}</Badge>
		{:else}<span class="text-fg-subtle">–</span>{/each}
	</span>
{:else if colKey === 'notes'}
	{#if d.hasNotes}<Icon name="note" size={15} label="Hat Notizen" class="text-fg-muted" />{/if}
{:else if colKey === 'firstSeen'}
	<RelativeTime value={d.firstSeen} class="text-fg-muted" />
{:else if colKey === 'lastSeen'}
	<RelativeTime value={d.lastSeen} class="text-fg-muted" />
{:else if colKey === 'onlineChangedAt'}
	<RelativeTime value={d.onlineChangedAt} class="text-fg-muted" />
{:else if colKey === 'id'}
	<span class="mono text-fg-subtle">{d.id}</span>
{:else}
	{@const v = col?.text?.(d) ?? ''}
	{#if v && col?.cfType === 'url' && /^https?:\/\//i.test(v)}
		<a href={v} target="_blank" rel="noopener noreferrer" class="link block max-w-[16rem] truncate" title={v}
			>{v}</a
		>
	{:else if v}<span class="block max-w-[16rem] truncate" title={v}>{v}</span>{:else}<span
			class="text-fg-subtle">–</span
		>{/if}
{/if}
