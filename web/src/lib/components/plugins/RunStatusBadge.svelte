<!--
	Status badge of a plugin run (Wartend / Läuft / Erfolgreich / …).
	<RunStatusBadge status={run.status} />
-->
<script lang="ts">
	import Badge from '$lib/components/ui/Badge.svelte';
	import { runStatusLabel, runStatusTone } from '$lib/utils/labels';

	interface Props {
		status: string | undefined;
		title?: string;
		class?: string;
	}

	let { status, title, class: klass = '' }: Props = $props();
</script>

<Badge tone={runStatusTone(status)} dot={status === 'running' || status === 'queued'} {title} class={klass}>
	{runStatusLabel[status ?? ''] ?? status ?? '–'}
</Badge>
