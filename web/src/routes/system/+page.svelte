<script lang="ts">
	import { page } from '$app/state';
	import AccountTab from '$lib/components/system/AccountTab.svelte';
	import AuditTab from '$lib/components/system/AuditTab.svelte';
	import BackupsTab from '$lib/components/system/BackupsTab.svelte';
	import CustomFieldsTab from '$lib/components/system/CustomFieldsTab.svelte';
	import FederationTab from '$lib/components/system/FederationTab.svelte';
	import GroupsTab from '$lib/components/system/GroupsTab.svelte';
	import LogsTab from '$lib/components/system/LogsTab.svelte';
	import OverviewTab from '$lib/components/system/OverviewTab.svelte';
	import SettingsTab from '$lib/components/system/SettingsTab.svelte';
	import SubnetsTab from '$lib/components/system/SubnetsTab.svelte';
	import TokensTab from '$lib/components/system/TokensTab.svelte';
	import VaultTab from '$lib/components/system/VaultTab.svelte';
	import { SYSTEM_TABS } from '$lib/components/system/system';
	import { Icon, PageHeader } from '$lib/components/ui';

	const tabId = $derived(page.url.searchParams.get('tab') ?? 'overview');
	const tab = $derived(SYSTEM_TABS.find((t) => t.id === tabId) ?? SYSTEM_TABS[0]);

	// keep the active entry visible in the horizontally scrolling nav (phones)
	let navList: HTMLUListElement | null = $state(null);
	$effect(() => {
		const id = tab.id;
		const el = navList?.querySelector<HTMLElement>(`[data-tab="${id}"]`);
		if (el && navList && navList.scrollWidth > navList.clientWidth)
			navList.scrollTo({ left: el.offsetLeft - navList.clientWidth / 2 + el.offsetWidth / 2 });
	});
</script>

<PageHeader
	title="System"
	docTitle="{tab.label} · System"
	description="Einstellungen, Zugänge, Datenpflege und Protokolle"
/>

<div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:gap-6">
	<nav aria-label="Systembereiche" class="-mx-4 shrink-0 sm:-mx-6 lg:sticky lg:top-4 lg:mx-0 lg:w-52">
		<ul
			bind:this={navList}
			class="flex gap-1 overflow-x-auto border-b border-border px-4 [scrollbar-width:none] sm:px-6 lg:flex-col lg:gap-0.5 lg:overflow-visible lg:border-0 lg:px-0"
		>
			{#each SYSTEM_TABS as t (t.id)}
				{@const active = t.id === tab.id}
				<li class="shrink-0">
					<a
						href="?tab={t.id}"
						data-tab={t.id}
						aria-current={active ? 'page' : undefined}
						data-sveltekit-noscroll
						class="flex items-center gap-2 border-b-2 px-2.5 py-2 text-sm whitespace-nowrap transition-colors lg:rounded-md lg:border-b-0 lg:py-1.5
							{active
							? 'border-accent font-medium text-fg lg:bg-accent-soft lg:text-accent'
							: 'border-transparent text-fg-muted hover:text-fg lg:hover:bg-surface-3'}"
					>
						<Icon name={t.icon} size={16} class={active ? 'text-accent' : 'text-fg-subtle'} />
						{t.label}
					</a>
				</li>
			{/each}
		</ul>
	</nav>

	<section class="min-w-0 flex-1" aria-labelledby="sys-tab-title">
		<div class="mb-3">
			<h2 id="sys-tab-title" class="text-base font-semibold text-fg">{tab.label}</h2>
			<p class="text-sm text-fg-muted">{tab.description}</p>
		</div>
		{#key tab.id}
			{#if tab.id === 'overview'}
				<OverviewTab />
			{:else if tab.id === 'settings'}
				<SettingsTab />
			{:else if tab.id === 'federation'}
				<FederationTab />
			{:else if tab.id === 'account'}
				<AccountTab />
			{:else if tab.id === 'tokens'}
				<TokensTab />
			{:else if tab.id === 'subnets'}
				<SubnetsTab />
			{:else if tab.id === 'groups'}
				<GroupsTab />
			{:else if tab.id === 'fields'}
				<CustomFieldsTab />
			{:else if tab.id === 'backups'}
				<BackupsTab />
			{:else if tab.id === 'vault'}
				<VaultTab />
			{:else if tab.id === 'logs'}
				<LogsTab />
			{:else if tab.id === 'audit'}
				<AuditTab />
			{/if}
		{/key}
	</section>
</div>
