<!--
	Coloured status dot with optional label.
	<StatusDot status="online" />  <StatusDot status="offline" label="Offline" />
	status: online | offline | up | down | degraded | unknown | running | ok | warn | danger | idle
-->
<script lang="ts">
	interface Props {
		status: string;
		label?: string;
		pulse?: boolean;
		size?: number;
		class?: string;
	}

	let { status, label, pulse, size = 8, class: klass = '' }: Props = $props();

	const colors: Record<string, string> = {
		online: 'bg-online',
		up: 'bg-online',
		ok: 'bg-ok',
		success: 'bg-ok',
		offline: 'bg-offline',
		idle: 'bg-offline',
		down: 'bg-danger',
		danger: 'bg-danger',
		failed: 'bg-danger',
		degraded: 'bg-warn',
		warn: 'bg-warn',
		unknown: 'bg-unknown',
		running: 'bg-live',
		queued: 'bg-accent'
	};
	const defaultLabels: Record<string, string> = {
		online: 'Online',
		offline: 'Offline',
		up: 'Up',
		down: 'Down',
		degraded: 'Beeinträchtigt',
		unknown: 'Unbekannt',
		running: 'Läuft'
	};
	const color = $derived(colors[status] ?? 'bg-fg-subtle');
	const doPulse = $derived(pulse ?? (status === 'running' || status === 'down'));
</script>

<span class="inline-flex items-center gap-1.5 {klass}">
	<span class="relative inline-flex shrink-0" style="width:{size}px;height:{size}px">
		{#if doPulse}
			<span class="absolute inset-0 animate-ping rounded-full opacity-60 {color}"></span>
		{/if}
		<span class="relative inline-flex h-full w-full rounded-full {color}"></span>
	</span>
	{#if label !== undefined}
		<span class="text-sm">{label}</span>
	{:else}
		<span class="sr-only">{defaultLabels[status] ?? status}</span>
	{/if}
</span>
