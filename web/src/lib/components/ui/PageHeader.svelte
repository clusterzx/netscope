<!--
	Page title row. Also sets the document title ("<title> · NetScope").
	<PageHeader title="Geräte" description="…">
		{#snippet actions()}<Button …/>{/snippet}
	</PageHeader>
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		description?: string;
		/** document title if different from `title` */
		docTitle?: string;
		class?: string;
		meta?: Snippet;
		actions?: Snippet;
		breadcrumb?: Snippet;
	}

	let { title, description, docTitle, class: klass = '', meta, actions, breadcrumb }: Props = $props();
</script>

<svelte:head>
	<title>{docTitle ?? title} · NetScope</title>
</svelte:head>

<div class="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between {klass}">
	<div class="min-w-0">
		{#if breadcrumb}<div class="mb-1 text-xs text-fg-subtle">{@render breadcrumb()}</div>{/if}
		<h1 class="truncate text-xl font-semibold tracking-tight text-fg">{title}</h1>
		{#if description}<p class="mt-0.5 text-sm text-fg-muted">{description}</p>{/if}
		{#if meta}<div class="mt-1.5 flex flex-wrap items-center gap-2 text-sm text-fg-muted">
				{@render meta()}
			</div>{/if}
	</div>
	{#if actions}
		<div class="flex flex-wrap items-center gap-2">{@render actions()}</div>
	{/if}
</div>
