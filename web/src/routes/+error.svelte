<script lang="ts">
	import { page } from '$app/state';
	import Button from '$lib/components/ui/Button.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import { t } from '$lib/i18n';
</script>

<svelte:head>
	<title>{page.status === 404 ? t('Nicht gefunden') : t('Fehler')} · NetScope</title>
</svelte:head>

<EmptyState
	icon={page.status === 404 ? 'search' : 'alert'}
	title={page.status === 404 ? t('Seite nicht gefunden') : t('Fehler {status}', { status: page.status })}
	description={page.status === 404
		? t('Unter {path} gibt es nichts.', { path: page.url.pathname })
		: (page.error?.message ?? t('Unbekannter Fehler'))}
>
	{#snippet actions()}
		<Button href="/" variant="primary" icon="dashboard">{t('Zum Dashboard')}</Button>
	{/snippet}
</EmptyState>
