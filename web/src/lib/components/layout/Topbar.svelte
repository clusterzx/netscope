<script lang="ts">
	import Button from '$lib/components/ui/Button.svelte';
	import GlobalSearch from './GlobalSearch.svelte';
	import LiveIndicator from './LiveIndicator.svelte';
	import RunsIndicator from './RunsIndicator.svelte';
	import ThemeToggle from './ThemeToggle.svelte';
	import UserMenu from './UserMenu.svelte';

	let { onmenu }: { onmenu: () => void } = $props();
</script>

<header
	class="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b border-border bg-surface/85 px-3 backdrop-blur sm:px-5"
>
	<Button variant="ghost" icon="menu" label="Navigation öffnen" class="lg:hidden" onclick={onmenu} />
	<a href="/" class="flex items-center lg:hidden" aria-label="NetScope Startseite">
		<img src="/favicon.svg" alt="" width="24" height="24" class="rounded-md" />
	</a>
	<div class="min-w-0 flex-1">
		<GlobalSearch />
	</div>
	<div class="flex items-center gap-1">
		<RunsIndicator />
		<LiveIndicator />
		<ThemeToggle />
		<UserMenu />
	</div>
</header>
