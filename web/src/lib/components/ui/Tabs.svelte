<!--
	Tab bar (WAI-ARIA tabs, ←/→ keys). Render the panel yourself based on `active`.
	<Tabs items={[{ id: 'overview', label: 'Überblick' }, { id: 'ports', label: 'Ports', count: 12 }]} bind:active />
	{#if active === 'overview'}<div role="tabpanel" id="panel-overview" aria-labelledby="tab-overview">…</div>{/if}
	Tab ids are `tab-<id>`, panel ids should be `panel-<id>` (set `idPrefix` if a page has several tab bars).
-->
<script lang="ts" module>
	import type { IconName } from './icons';
	export interface TabItem {
		id: string;
		label: string;
		count?: number | null;
		icon?: IconName;
		disabled?: boolean;
	}
</script>

<script lang="ts">
	import Icon from './Icon.svelte';
	import { formatNumber } from '$lib/utils/format';

	interface Props {
		items: TabItem[];
		active?: string;
		idPrefix?: string;
		label?: string;
		class?: string;
		onchange?: (id: string) => void;
	}

	let {
		items,
		active = $bindable(),
		idPrefix = '',
		label = 'Bereiche',
		class: klass = '',
		onchange
	}: Props = $props();

	let list: HTMLDivElement | null = $state(null);

	function select(id: string) {
		active = id;
		onchange?.(id);
	}

	function onKey(e: KeyboardEvent) {
		const enabled = items.filter((t) => !t.disabled);
		const i = enabled.findIndex((t) => t.id === active);
		let next = -1;
		if (e.key === 'ArrowRight') next = (i + 1) % enabled.length;
		else if (e.key === 'ArrowLeft') next = (i - 1 + enabled.length) % enabled.length;
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = enabled.length - 1;
		if (next < 0) return;
		e.preventDefault();
		select(enabled[next].id);
		list?.querySelector<HTMLButtonElement>(`#${idPrefix}tab-${CSS.escape(enabled[next].id)}`)?.focus();
	}
</script>

<div
	bind:this={list}
	role="tablist"
	aria-label={label}
	tabindex="-1"
	onkeydown={onKey}
	class="-mb-px flex gap-0.5 overflow-x-auto border-b border-border {klass}"
>
	{#each items as t (t.id)}
		<button
			type="button"
			role="tab"
			id="{idPrefix}tab-{t.id}"
			aria-selected={active === t.id}
			aria-controls="{idPrefix}panel-{t.id}"
			tabindex={active === t.id ? 0 : -1}
			disabled={t.disabled}
			onclick={() => select(t.id)}
			class="inline-flex shrink-0 items-center gap-1.5 border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors
				disabled:cursor-not-allowed disabled:opacity-40
				{active === t.id
				? 'border-accent font-medium text-fg'
				: 'border-transparent text-fg-muted hover:border-border-strong hover:text-fg'}"
		>
			{#if t.icon}<Icon name={t.icon} size={15} />{/if}
			{t.label}
			{#if t.count !== undefined && t.count !== null}
				<span
					class="rounded px-1.5 text-[0.7rem] tabular {active === t.id
						? 'bg-accent-soft text-accent'
						: 'bg-surface-3 text-fg-subtle'}">{formatNumber(t.count)}</span
				>
			{/if}
		</button>
	{/each}
</div>
