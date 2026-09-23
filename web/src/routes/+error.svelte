<script lang="ts">
	import { page } from '$app/state';
	import Button from '$lib/components/ui/Button.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
</script>

<svelte:head>
	<title>{page.status === 404 ? 'Nicht gefunden' : 'Fehler'} · NetScope</title>
</svelte:head>

<EmptyState
	icon={page.status === 404 ? 'search' : 'alert'}
	title={page.status === 404 ? 'Seite nicht gefunden' : `Fehler ${page.status}`}
	description={page.status === 404
		? `Unter ${page.url.pathname} gibt es nichts.`
		: (page.error?.message ?? 'Unbekannter Fehler')}
>
	{#snippet actions()}
		<Button href="/" variant="primary" icon="dashboard">Zum Dashboard</Button>
	{/snippet}
</EmptyState>
