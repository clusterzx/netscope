<!-- Renders the toast stack (mounted once in the root layout). -->
<script lang="ts">
	import { toast } from '$lib/stores/toast.svelte';
	import { t } from '$lib/i18n';
	import Icon from './Icon.svelte';

	const icon = { success: 'check-circle', error: 'x-circle', info: 'info', warning: 'alert' } as const;
	const color = {
		success: 'text-ok',
		error: 'text-danger',
		info: 'text-accent',
		warning: 'text-warn'
	} as const;
</script>

<div
	class="pointer-events-none fixed right-3 bottom-3 z-[60] flex w-[min(24rem,calc(100vw-1.5rem))] flex-col gap-2"
	aria-live="polite"
	aria-relevant="additions"
>
	{#each toast.items as item (item.id)}
		<div
			class="pointer-events-auto flex items-start gap-2.5 rounded-lg border border-border bg-surface px-3.5 py-3 text-sm shadow-lg"
			role={item.kind === 'error' ? 'alert' : 'status'}
		>
			<Icon name={icon[item.kind]} size={18} class="mt-px {color[item.kind]}" />
			<div class="min-w-0 flex-1">
				{#if item.title}<p class="font-medium text-fg">{item.title}</p>{/if}
				<p class="break-words {item.title ? 'text-fg-muted' : 'text-fg'}">{item.message}</p>
				{#if item.action}
					<button
						type="button"
						class="mt-1 text-sm font-medium text-accent hover:underline"
						onclick={() => {
							item.action?.onClick();
							toast.dismiss(item.id);
						}}>{item.action.label}</button
					>
				{/if}
			</div>
			<button
				type="button"
				class="-mt-0.5 -mr-1 rounded p-1 text-fg-subtle hover:bg-surface-3 hover:text-fg"
				aria-label={t('Meldung schließen')}
				onclick={() => toast.dismiss(item.id)}
			>
				<Icon name="x" size={14} />
			</button>
		</div>
	{/each}
</div>
