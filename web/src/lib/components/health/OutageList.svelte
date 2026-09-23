<!--
	List of outages (down/degraded periods).
	<OutageList outages={list} showCheck onopen={(checkId) => …} />
-->
<script lang="ts">
	import type { Outage } from '$lib/api';
	import { Badge, RelativeTime } from '$lib/components/ui';
	import { formatDateTime, formatSeconds } from '$lib/utils/format';
	import { healthStateLabel, healthTone } from '$lib/utils/labels';

	interface Props {
		outages: Outage[];
		/** show the check name (board-wide list) */
		showCheck?: boolean;
		onopen?: (checkId: number) => void;
		class?: string;
	}

	let { outages, showCheck = false, onopen, class: klass = '' }: Props = $props();
</script>

<ul class="flex flex-col divide-y divide-border {klass}">
	{#each outages as o (o.id)}
		<li class="flex flex-col gap-1 py-2.5 sm:flex-row sm:items-center sm:gap-3">
			<div class="flex min-w-0 flex-1 items-start gap-2.5">
				<Badge tone={healthTone(o.state)} class="mt-0.5 w-24 justify-center"
					>{healthStateLabel[o.state] ?? o.state}</Badge
				>
				<div class="min-w-0 flex-1">
					{#if showCheck}
						<p class="truncate text-sm">
							{#if onopen}
								<button
									type="button"
									class="font-medium text-fg hover:text-accent hover:underline"
									onclick={() => onopen?.(o.checkId)}>{o.checkName || `Check #${o.checkId}`}</button
								>
							{:else}
								<span class="font-medium">{o.checkName || `Check #${o.checkId}`}</span>
							{/if}
							{#if o.deviceId}
								<span class="text-fg-subtle">{' · '}</span><a
									href="/devices/{o.deviceId}"
									class="link text-xs">zum Gerät</a
								>
							{/if}
						</p>
					{/if}
					{#if o.reason}
						<p class="line-clamp-2 text-xs break-words text-fg-muted" title={o.reason}>{o.reason}</p>
					{/if}
				</div>
			</div>
			<div
				class="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-0.5 pl-26.5 text-xs text-fg-subtle sm:pl-0 sm:text-right"
			>
				<span title={formatDateTime(o.startedAt, true)}>
					<RelativeTime value={o.startedAt} absolute />
				</span>
				<span class="font-medium tabular sm:w-40 sm:text-right {o.endedAt ? 'text-fg-muted' : ''}">
					{#if o.endedAt}
						{formatSeconds(o.seconds)}
					{:else}
						<Badge tone={o.state === 'down' ? 'danger' : 'warn'} dot
							>andauernd · {formatSeconds(o.seconds)}</Badge
						>
					{/if}
				</span>
			</div>
		</li>
	{/each}
</ul>
