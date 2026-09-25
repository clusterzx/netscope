<script lang="ts">
	import '../app.css';
	import { page } from '$app/state';
	import AppShell from '$lib/components/layout/AppShell.svelte';
	import ConfirmHost from '$lib/components/ui/ConfirmHost.svelte';
	import Toaster from '$lib/components/ui/Toaster.svelte';
	import { theme } from '$lib/stores/theme.svelte';

	let { children } = $props();

	theme.init();

	// login and the first-login setup render without the shell
	const bare = $derived(page.url.pathname === '/login' || page.url.pathname === '/setup');
</script>

{#if bare}
	{@render children()}
{:else}
	<AppShell>
		{@render children()}
	</AppShell>
{/if}

<ConfirmHost />
<Toaster />
