<!--
	Step 6 (optional): systems NetScope reads with credentials, in tabs by area. Each tab
	shows how many systems are set up. New importers bring their tab along (plugin category).
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { PluginView } from '$lib/api';
	import { Alert, ErrorState, Skeleton, Tabs } from '$lib/components/ui';
	import { intlLocale, t } from '$lib/i18n';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import SourceCard from './SourceCard.svelte';
	import { wizard } from './wizard.svelte';

	const data = new AsyncData<{ plugins: PluginView[] }>();
	$effect(() => {
		data.run(async (signal) => ({ plugins: (await api.get('/api/v1/plugins', { signal })) ?? [] }));
	});

	const categories = $derived(wizard.options?.categories ?? []);
	let active = $state('');
	$effect(() => {
		if (!active && categories.length) active = categories[0].id;
	});

	function pluginsOf(id: string): PluginView[] {
		const ids = categories.find((c) => c.id === id)?.plugins ?? [];
		return (data.data?.plugins ?? [])
			.filter((p) => ids.includes(p.info.id))
			.sort((a, b) => a.info.name.localeCompare(b.info.name, intlLocale));
	}

	const tabs = $derived(
		categories.map((c) => ({
			id: c.id,
			label: c.label,
			count: pluginsOf(c.id).filter((p) => p.config?.enabled).length || null
		}))
	);

	function saved(next: PluginView) {
		const list = data.data?.plugins ?? [];
		const i = list.findIndex((p) => p.info.id === next.info.id);
		if (i >= 0) list[i] = next;
	}
</script>

<div class="flex flex-col gap-4">
	<p class="text-sm text-fg-muted">
		{t(
			'Router, Controller und Server kennen Namen, Adressen und Verbindungen, die ein Scan nicht sieht. Dieser Schritt ist optional – alles lässt sich später unter Plugins einrichten.'
		)}
	</p>
	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton rows={4} />
	{:else}
		<Tabs items={tabs} bind:active idPrefix="src-" label={t('Bereiche')} />
		{#each categories as c (c.id)}
			{#if active === c.id}
				<div
					role="tabpanel"
					id="src-panel-{c.id}"
					aria-labelledby="src-tab-{c.id}"
					class="flex flex-col gap-3"
				>
					{#each pluginsOf(c.id) as p (p.info.id)}
						<SourceCard plugin={p} onsaved={saved} />
					{/each}
					{#if c.hints?.includes('windows_dhcp')}
						<Alert tone="info" title={t('Windows-DHCP')}>
							{t(
								'Leases und Reservierungen eines Windows-DHCP-Servers liefert der NetScope-Agent für Windows. Ihn installierst du nach der Einrichtung unter Agents auf dem DHCP-Server.'
							)}
						</Alert>
					{/if}
				</div>
			{/if}
		{/each}
	{/if}
</div>
