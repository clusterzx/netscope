<!--
	Relative time ("vor 5 Minuten") that updates itself; absolute time in the tooltip.
	<RelativeTime value={dev.lastSeen} />   <RelativeTime value={run.startedAt} absolute />  (shows dd.MM.yyyy HH:mm)
-->
<script lang="ts" module>
	// one shared ticker for all instances
	let now = $state(Date.now());
	let users = 0;
	let timer: ReturnType<typeof setInterval> | null = null;
	function subscribe() {
		users++;
		if (!timer) timer = setInterval(() => (now = Date.now()), 20000);
		return () => {
			users--;
			if (users === 0 && timer) {
				clearInterval(timer);
				timer = null;
			}
		};
	}
</script>

<script lang="ts">
	import { formatDateTime, formatRelative, toDate } from '$lib/utils/format';

	interface Props {
		value: string | number | Date | null | undefined;
		/** show the absolute date and the relative time in the tooltip */
		absolute?: boolean;
		fallback?: string;
		class?: string;
	}

	let { value, absolute = false, fallback = '–', class: klass = '' }: Props = $props();

	$effect(() => subscribe());

	const d = $derived(toDate(value));
	// the shared ticker is up to 20 s old: take the real time whenever the value changes,
	// otherwise a just-received timestamp would look like it lies in the future
	const current = $derived.by(() => {
		void d;
		return Math.max(now, Date.now());
	});
</script>

{#if d}
	<time
		datetime={d.toISOString()}
		title={absolute ? formatRelative(d, current) : formatDateTime(d, true)}
		class="whitespace-nowrap {klass}">{absolute ? formatDateTime(d) : formatRelative(d, current)}</time
	>
{:else}
	<span class="text-fg-subtle {klass}">{fallback}</span>
{/if}
