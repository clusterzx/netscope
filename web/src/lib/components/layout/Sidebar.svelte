<script lang="ts">
	import { page } from '$app/state';
	import Icon from '$lib/components/ui/Icon.svelte';
	import { nav, isActive } from '$lib/nav';
	import { eventCounts } from '$lib/stores/eventCounts.svelte';
	import { meta } from '$lib/stores/catalog.svelte';
	import { federation } from '$lib/stores/federation.svelte';
	import { auth } from '$lib/stores/auth.svelte';

	let { onnavigate }: { onnavigate?: () => void } = $props();
</script>

<div class="flex h-full flex-col">
	<a
		href="/"
		class="flex h-14 shrink-0 items-center gap-2.5 border-b border-border px-5"
		onclick={() => onnavigate?.()}
	>
		<img src="/favicon.svg" alt="" width="26" height="26" class="rounded-md" />
		<span class="text-[0.95rem] font-semibold tracking-tight text-fg">NetScope</span>
	</a>
	<nav class="flex-1 overflow-y-auto px-3 py-3" aria-label="Hauptnavigation">
		{#each nav as section (section.label)}
			<div class="mb-4">
				<p class="px-2.5 pb-1.5 text-[0.68rem] font-semibold tracking-wider text-fg-subtle uppercase">
					{section.label}
				</p>
				<ul class="flex flex-col gap-0.5">
					{#each section.items.filter((i) => (!i.central || federation.role === 'central') && (!i.perm || auth.can(i.perm))) as item (item.href)}
						{@const active = isActive(item.href, page.url.pathname)}
						<li>
							<a
								href={item.href}
								aria-current={active ? 'page' : undefined}
								onclick={() => onnavigate?.()}
								class="relative flex h-8.5 items-center gap-2.5 rounded-md px-2.5 text-sm transition-colors
									{active ? 'bg-accent-soft font-medium text-accent' : 'text-fg-muted hover:bg-surface-2 hover:text-fg'}"
							>
								{#if active}<span class="absolute top-1.5 bottom-1.5 -left-3 w-0.75 rounded-r bg-accent"
									></span>{/if}
								<Icon name={item.icon} size={17} />
								<span class="flex-1 truncate">{item.label}</span>
								{#if item.badge === 'events' && eventCounts.urgent > 0}
									<span
										class="min-w-5 rounded-full bg-sev-critical px-1.5 text-center text-[0.68rem] leading-5 font-semibold text-white tabular"
										title="{eventCounts.urgent} offene Events mit Schweregrad hoch oder kritisch"
										>{eventCounts.urgent > 99 ? '99+' : eventCounts.urgent}</span
									>
								{/if}
							</a>
						</li>
					{/each}
				</ul>
			</div>
		{/each}
	</nav>
	<div class="shrink-0 border-t border-border px-5 py-3 text-xs text-fg-subtle">
		<a href="/api/docs" target="_blank" rel="noopener" class="hover:text-fg">API-Dokumentation</a>
		{#if meta.value?.version}<span class="ml-1">· v{meta.value.version}</span>{/if}
	</div>
</div>
