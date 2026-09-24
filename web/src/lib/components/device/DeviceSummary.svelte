<!-- Key facts of a device below the page header (addresses, identity, location, times). -->
<script lang="ts">
	import type { DeviceDetail } from '$lib/api/types';
	import CopyButton from '$lib/components/ui/CopyButton.svelte';
	import DescItem from '$lib/components/ui/DescItem.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import { deviceTypeName } from '$lib/utils/labels';
	import { sourceName } from './util';

	interface Props {
		device: DeviceDetail;
		class?: string;
	}

	let { device: d, class: klass = '' }: Props = $props();

	/** winning source per fact kind (hostname, vendor, model, type, os) */
	const win = $derived(d.effectiveSources ?? {});
	const randomized = $derived(new Set((d.macList ?? []).filter((m) => m.randomized).map((m) => m.mac)));
	const ips = $derived(d.ips?.length ? d.ips : d.ip ? [d.ip] : []);
	const macs = $derived(d.macs?.length ? d.macs : d.mac ? [d.mac] : []);

	function hint(kind: string): string | undefined {
		return win[kind] ? `(${sourceName(win[kind])})` : undefined;
	}
</script>

<section
	class="rounded-lg border border-border bg-surface px-4 py-3 shadow-sm {klass}"
	aria-label="Kerndaten"
>
	<dl class="grid grid-cols-2 gap-x-4 gap-y-3 sm:gap-x-6 lg:grid-cols-3 2xl:grid-cols-4">
		<DescItem label={ips.length > 1 ? `IP-Adressen (${ips.length})` : 'IP-Adresse'}>
			{#if ips.length}
				<span class="flex flex-wrap items-center gap-x-2">
					{#each ips.slice(0, 4) as ip, i (ip)}
						<span class="mono {i === 0 ? 'text-fg' : 'text-fg-muted'}">{ip}</span>
					{/each}
					{#if ips.length > 4}<span class="text-xs text-fg-subtle">+{ips.length - 4}</span>{/if}
					<CopyButton text={ips[0]} label="IP kopieren" />
				</span>
			{:else}<span class="text-fg-subtle">–</span>{/if}
		</DescItem>
		<DescItem label={macs.length > 1 ? `MAC-Adressen (${macs.length})` : 'MAC-Adresse'}>
			{#if macs.length}
				<span class="flex flex-wrap items-center gap-x-2">
					{#each macs.slice(0, 3) as mac, i (mac)}
						<span class="mono {i === 0 ? 'text-fg' : 'text-fg-muted'}">{mac}</span>
						{#if randomized.has(mac)}
							<span class="text-xs text-warn" title="Zufällige (private) MAC-Adresse">zufällig</span>
						{/if}
					{/each}
					{#if macs.length > 3}<span class="text-xs text-fg-subtle">+{macs.length - 3}</span>{/if}
					<CopyButton text={macs[0]} label="MAC kopieren" />
				</span>
			{:else}<span class="text-fg-subtle">–</span>{/if}
		</DescItem>
		<DescItem label="Hostname" value={d.hostname} hint={d.hostname ? hint('hostname') : undefined} />
		<DescItem label="Hersteller" value={d.vendor} hint={d.vendor ? hint('vendor') : undefined} />
		<DescItem label="Modell" value={d.model} hint={d.model ? hint('model') : undefined} />
		<DescItem
			label="Typ"
			value={d.type ? deviceTypeName(d.type) : ''}
			hint={d.type ? hint('type') : undefined}
		/>
		<DescItem label="Betriebssystem" value={d.os} hint={d.os ? hint('os') : undefined} />
		<DescItem label="Aufstellort" value={d.location} />
		<DescItem label="Besitzer" value={d.owner} />
		<DescItem label="Eltern-Gerät">
			{#if d.parentId}
				<a href="/devices/{d.parentId}" class="link">{d.parentName || `#${d.parentId}`}</a>
			{:else}<span class="text-fg-subtle">–</span>{/if}
		</DescItem>
		<DescItem label="Erstsichtung">
			<RelativeTime value={d.firstSeen} absolute />
			{#if d.createdSource}<span class="ml-1 text-xs text-fg-subtle">({sourceName(d.createdSource)})</span
				>{/if}
		</DescItem>
		<DescItem label="Zuletzt gesehen"><RelativeTime value={d.lastSeen} /></DescItem>
	</dl>
</section>
