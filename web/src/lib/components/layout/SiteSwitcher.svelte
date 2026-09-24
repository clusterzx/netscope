<!-- Site selector of a central instance: restricts devices, topology, events, vulnerabilities,
     the dashboard and exports to one NetScope site (hidden without sites). -->
<script lang="ts">
	import Icon from '$lib/components/ui/Icon.svelte';
	import { federation, siteFilter } from '$lib/stores/federation.svelte';

	const disconnected = $derived(federation.sites.filter((s) => !s.connected && s.contacted).length);
</script>

{#if federation.isCentral}
	<label
		class="relative inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-surface-2 pr-1 pl-2 text-xs text-fg-muted
			focus-within:ring-2 focus-within:ring-accent/40 hover:border-border-strong"
		title={disconnected
			? `${disconnected} Standort(e) melden sich zurzeit nicht`
			: 'Ansicht auf einen Standort einschränken'}
	>
		<Icon name="globe" size={15} class={disconnected ? 'text-warn' : 'text-fg-subtle'} />
		<span class="sr-only">Standort</span>
		<select
			bind:value={siteFilter.value}
			class="max-w-[9rem] cursor-pointer appearance-none bg-transparent pr-4 font-medium text-fg focus:outline-none sm:max-w-[12rem]"
		>
			<option value="">Alle Standorte</option>
			<option value="local">{federation.localName}</option>
			{#each federation.sites as s (s.id)}
				<option value={s.slug}>{s.name}{!s.connected && s.contacted ? ' (getrennt)' : ''}</option>
			{/each}
		</select>
		<Icon name="chevron-down" size={13} class="pointer-events-none absolute right-1.5 text-fg-subtle" />
	</label>
{/if}
