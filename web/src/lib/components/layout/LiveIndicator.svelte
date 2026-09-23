<!-- SSE connection state ("Live" / "Verbinde …" / "Getrennt"). -->
<script lang="ts">
	import { live } from '$lib/stores/live.svelte';
	import { formatRelative } from '$lib/utils/format';

	const text = $derived(
		live.status === 'open'
			? 'Live'
			: live.status === 'reconnecting'
				? 'Getrennt'
				: live.status === 'connecting'
					? 'Verbinde'
					: 'Aus'
	);
	const title = $derived(
		live.status === 'open'
			? `Live-Updates aktiv${live.lastMessageAt ? ` – letzte Meldung ${formatRelative(live.lastMessageAt)}` : ''}`
			: live.status === 'reconnecting'
				? 'Live-Verbindung unterbrochen – verbinde neu …'
				: 'Live-Verbindung wird aufgebaut …'
	);
	const dot = $derived(
		live.status === 'open' ? 'bg-online' : live.status === 'reconnecting' ? 'bg-warn' : 'bg-offline'
	);
</script>

<span
	class="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-xs font-medium {live.status === 'open'
		? 'text-fg-muted'
		: 'text-warn'}"
	{title}
	role="status"
	aria-live="polite"
>
	<span class="relative flex h-2 w-2">
		{#if live.status === 'open'}<span class="absolute inset-0 animate-ping rounded-full bg-online opacity-50"
			></span>{/if}
		<span class="relative h-2 w-2 rounded-full {dot}"></span>
	</span>
	<span class="hidden sm:inline">{text}</span>
	<span class="sr-only sm:hidden">{title}</span>
</span>
