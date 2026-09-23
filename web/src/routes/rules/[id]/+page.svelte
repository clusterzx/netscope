<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import type { Rule } from '$lib/api';
	import { Button, Card, EmptyState, ErrorState, PageHeader, Skeleton } from '$lib/components/ui';
	import RuleEditor from '$lib/components/rules/RuleEditor.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';

	const id = $derived(Number(page.params.id));

	const data = new AsyncData<Rule>();
	$effect(() => {
		const rid = id;
		if (!Number.isInteger(rid) || rid <= 0) return;
		data.run((signal) => api.get('/api/v1/rules/{id}', { path: { id: rid }, signal }), true);
	});

	const invalidId = $derived(!Number.isInteger(id) || id <= 0);
	const notFound = $derived(invalidId || (data.error instanceof ApiError && data.error.isNotFound));
</script>

{#if notFound}
	<PageHeader title="Regel" />
	<EmptyState icon="rules" title="Regel nicht gefunden" description="Die Regel existiert nicht (mehr).">
		{#snippet actions()}
			<Button href="/rules">Zur Regelliste</Button>
		{/snippet}
	</EmptyState>
{:else if data.error && !data.data}
	<PageHeader title="Regel" />
	<ErrorState error={data.error} onretry={() => data.reload()} />
{:else if !data.data || data.data.id !== id}
	<PageHeader title="Regel" />
	<div class="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_27rem]">
		<Card><Skeleton lines={12} /></Card>
		<Card><Skeleton lines={8} /></Card>
	</div>
{:else}
	{#key data.data.id}
		<RuleEditor rule={data.data} />
	{/key}
{/if}
