<!--
	Progress bar; without total it is indeterminate.
	<ProgressBar done={3} total={10} label="Fortschritt" />
-->
<script lang="ts">
	interface Props {
		done?: number;
		total?: number;
		label?: string;
		tone?: 'accent' | 'live' | 'ok' | 'warn' | 'danger';
		class?: string;
	}

	let { done = 0, total = 0, label = 'Fortschritt', tone = 'accent', class: klass = '' }: Props = $props();
	const pct = $derived(total > 0 ? Math.min(100, Math.round((done / total) * 100)) : null);
	const fill = { accent: 'bg-accent', live: 'bg-live', ok: 'bg-ok', warn: 'bg-warn', danger: 'bg-danger' };
</script>

<div
	class="relative h-1.5 w-full overflow-hidden rounded-full bg-surface-3 {klass}"
	role="progressbar"
	aria-label={label}
	aria-valuemin={0}
	aria-valuemax={100}
	aria-valuenow={pct ?? undefined}
>
	{#if pct !== null}
		<div class="h-full rounded-full transition-[width] duration-300 {fill[tone]}" style="width:{pct}%"></div>
	{:else}
		<div
			class="absolute inset-y-0 w-1/3 animate-[ns-indeterminate_1.2s_ease-in-out_infinite] rounded-full {fill[
				tone
			]}"
		></div>
	{/if}
</div>
