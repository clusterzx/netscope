<!-- Steps of a sign-in configuration test, each with its outcome. -->
<script lang="ts">
	import type { AuthTestStep } from '$lib/api/generated';
	import { Icon } from '$lib/components/ui';

	let { steps }: { steps: AuthTestStep[] } = $props();
</script>

<ol class="flex flex-col gap-1.5">
	{#each steps as s, i (i)}
		<li class="flex items-start gap-2 text-sm">
			<Icon
				name={s.ok ? 'check-circle' : 'x-circle'}
				size={16}
				class="mt-0.5 shrink-0 {s.ok ? 'text-ok' : 'text-danger'}"
			/>
			<span class="min-w-0">
				<span class={s.ok ? 'text-fg' : 'font-medium text-danger'}>{s.name}</span>
				{#if s.error}<span class="block text-xs break-words text-fg-muted">{s.error}</span>{/if}
			</span>
		</li>
	{/each}
</ol>
