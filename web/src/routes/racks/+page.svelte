<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import type { RackSummary } from '$lib/api';
	import RackFormModal from '$lib/components/racks/RackFormModal.svelte';
	import {
		Badge,
		Button,
		EmptyState,
		ErrorState,
		Icon,
		PageHeader,
		ProgressBar,
		Skeleton
	} from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';

	const racks = new AsyncData<RackSummary[]>();
	$effect(() => {
		racks.run((signal) => api.get('/api/v1/racks', { signal }));
	});

	let createOpen = $state(false);
	const canEdit = $derived(auth.can('devices.edit'));
</script>

<PageHeader
	title={t('Racks')}
	description={t(
		'Wo Geräte eingebaut sind und was an welchem Port steckt – erkannt oder von Hand eingetragen.'
	)}
>
	{#snippet actions()}
		{#if canEdit}
			<Button variant="primary" icon="plus" onclick={() => (createOpen = true)}>{t('Rack anlegen')}</Button>
		{/if}
	{/snippet}
</PageHeader>

{#if racks.error && !racks.data}
	<ErrorState error={racks.error} onretry={() => racks.reload()} />
{:else if !racks.data}
	<Skeleton rows={4} />
{:else if racks.data.length === 0}
	<EmptyState
		icon="rack"
		title={t('Noch kein Rack')}
		description={t(
			'Lege ein Rack an und baue Geräte aus dem Inventar ein. Ports von Switches kommen aus SNMP und den Importen; was daran steckt, erkennt NetScope oder du trägst es mit einem Klick ein.'
		)}
	>
		{#snippet actions()}
			{#if canEdit}
				<Button variant="primary" icon="plus" onclick={() => (createOpen = true)}>{t('Rack anlegen')}</Button>
			{/if}
		{/snippet}
	</EmptyState>
{:else}
	<ul class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
		{#each racks.data as r (r.id)}
			<li>
				<a
					href="/racks/{r.id}"
					class="flex h-full flex-col gap-3 rounded-lg border border-border bg-surface p-4 shadow-sm transition-colors hover:border-accent focus-visible:ring-2 focus-visible:ring-focus focus-visible:outline-none"
				>
					<div class="flex items-start gap-3">
						<span class="rounded-md bg-surface-2 p-2 text-fg-muted"><Icon name="rack" size={20} /></span>
						<div class="min-w-0 flex-1">
							<h2 class="truncate font-semibold text-fg">{r.name}</h2>
							<p class="truncate text-sm text-fg-muted">
								{[r.location, `${r.width}" · ${r.height} ${t('HE')}`].filter(Boolean).join(' · ')}
							</p>
						</div>
						{#if r.offline > 0}
							<Badge tone="danger" dot>{t('{n} offline', { n: r.offline })}</Badge>
						{/if}
					</div>
					<ProgressBar done={r.usedUnits} total={r.height} label={t('Belegte Höheneinheiten')} />
					<p class="text-xs text-fg-muted">
						{t('{used} von {total} HE belegt · {devices} Geräte · {items} Elemente', {
							used: r.usedUnits,
							total: r.height,
							devices: r.devices,
							items: r.items
						})}
					</p>
				</a>
			</li>
		{/each}
	</ul>
{/if}

<RackFormModal bind:open={createOpen} onsaved={(r) => goto(`/racks/${r.id}`)} />
