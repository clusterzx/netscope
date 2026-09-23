<!--
	Error display for failed loads, with retry.
	<ErrorState error={data.error} onretry={() => data.reload()} />
	`compact` renders an inline Alert instead of a centered block.
-->
<script lang="ts">
	import { errorMessage, ApiError } from '$lib/api/client';
	import Alert from './Alert.svelte';
	import Button from './Button.svelte';
	import Icon from './Icon.svelte';

	interface Props {
		error: unknown;
		title?: string;
		compact?: boolean;
		onretry?: () => void;
		class?: string;
	}

	let {
		error,
		title = 'Laden fehlgeschlagen',
		compact = false,
		onretry,
		class: klass = ''
	}: Props = $props();

	const msg = $derived(errorMessage(error));
	const status = $derived(error instanceof ApiError && error.status ? `HTTP ${error.status}` : '');
</script>

{#if compact}
	<Alert tone="danger" {title} class={klass}>
		{msg}
		{#snippet actions()}
			{#if onretry}<Button size="xs" icon="refresh" onclick={onretry}>Erneut</Button>{/if}
		{/snippet}
	</Alert>
{:else}
	<div class="flex flex-col items-center justify-center gap-2 py-10 text-center {klass}" role="alert">
		<div class="flex h-12 w-12 items-center justify-center rounded-full bg-danger-soft text-danger">
			<Icon name="alert" size={22} />
		</div>
		<p class="font-medium text-fg">{title}</p>
		<p class="max-w-md text-sm text-fg-muted">
			{msg}{#if status}<span class="text-fg-subtle"> ({status})</span>{/if}
		</p>
		{#if onretry}
			<Button size="sm" icon="refresh" onclick={onretry} class="mt-1">Erneut versuchen</Button>
		{/if}
	</div>
{/if}
