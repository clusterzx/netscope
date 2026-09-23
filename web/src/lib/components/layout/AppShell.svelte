<!-- Application frame: sidebar navigation, top bar and content area. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onMount } from 'svelte';
	import { afterNavigate } from '$app/navigation';
	import Sidebar from './Sidebar.svelte';
	import Topbar from './Topbar.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { eventCounts } from '$lib/stores/eventCounts.svelte';
	import { meta, wireCatalogRefresh } from '$lib/stores/catalog.svelte';

	let { children }: { children: Snippet } = $props();
	let mobileOpen = $state(false);
	let main: HTMLElement | null = $state(null);

	onMount(() => {
		live.connect();
		runs.init();
		eventCounts.init();
		wireCatalogRefresh();
		meta.load().catch(() => {});
		return () => live.disconnect();
	});

	afterNavigate(({ type, from, to }) => {
		mobileOpen = false;
		// move focus to the content only for real page changes – not for query updates
		// (filters synced via setParams), which would steal the focus from inputs
		if (type === 'enter' || type === 'popstate') return;
		if (from?.url.pathname === to?.url.pathname) return;
		main?.focus({ preventScroll: true });
	});
</script>

<a
	href="#main"
	class="sr-only z-[70] rounded bg-accent px-3 py-2 text-accent-fg focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
	>Zum Inhalt springen</a
>

<div class="flex min-h-dvh">
	<!-- desktop sidebar -->
	<aside class="sticky top-0 hidden h-dvh w-60 shrink-0 border-r border-border bg-surface lg:block">
		<Sidebar />
	</aside>

	<!-- mobile sidebar -->
	{#if mobileOpen}
		<div class="fixed inset-0 z-40 lg:hidden">
			<button
				type="button"
				class="absolute inset-0 bg-overlay"
				aria-label="Navigation schließen"
				onclick={() => (mobileOpen = false)}
			></button>
			<aside class="relative h-full w-64 max-w-[85vw] border-r border-border bg-surface shadow-lg">
				<Sidebar onnavigate={() => (mobileOpen = false)} />
			</aside>
		</div>
	{/if}

	<div class="flex min-w-0 flex-1 flex-col">
		<Topbar onmenu={() => (mobileOpen = true)} />
		<main
			id="main"
			bind:this={main}
			tabindex="-1"
			class="min-w-0 flex-1 px-4 py-5 focus:outline-none sm:px-6 lg:px-8"
		>
			{@render children()}
		</main>
	</div>
</div>
