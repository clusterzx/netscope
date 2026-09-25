<script lang="ts">
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import type { Rule } from '$lib/api';
	import { Button, Card, EmptyState, ErrorState, PageHeader, Skeleton } from '$lib/components/ui';
	import RuleEditor from '$lib/components/rules/RuleEditor.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';

	const canManage = $derived(auth.can('rules.manage'));

	/** ?from=<id> duplicates an existing rule */
	const from = $derived(Number(page.url.searchParams.get('from') ?? 0));

	// all rules: the new rule is appended to the evaluation order; the template for duplicates
	const data = new AsyncData<Rule[]>();
	$effect(() => {
		if (!canManage) return;
		data.run(async (signal) => (await api.get('/api/v1/rules', { signal })) ?? []);
	});

	const nextSortOrder = $derived(Math.max(0, ...(data.data ?? []).map((r) => r.sortOrder)) + 10);
	const template = $derived(from > 0 ? ((data.data ?? []).find((r) => r.id === from) ?? null) : null);
</script>

{#if !canManage}
	<PageHeader title="Neue Regel" />
	<EmptyState
		icon="lock"
		title="Keine Berechtigung"
		description="Zum Anlegen von Regeln fehlt die Berechtigung „Regeln verwalten“."
	>
		{#snippet actions()}
			<Button href="/rules">Zur Regelliste</Button>
		{/snippet}
	</EmptyState>
{:else if data.error && !data.data}
	<PageHeader title="Neue Regel" />
	<ErrorState error={data.error} onretry={() => data.reload()} />
{:else if !data.data}
	<PageHeader title="Neue Regel" />
	<div class="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_27rem]">
		<Card><Skeleton lines={12} /></Card>
		<Card><Skeleton lines={8} /></Card>
	</div>
{:else}
	{#key from}
		<RuleEditor rule={null} {template} {nextSortOrder} />
	{/key}
{/if}
