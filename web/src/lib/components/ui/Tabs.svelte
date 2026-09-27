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
	import { t } from '$lib/i18n';
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
		label = t('Bereiche'),
		class: klass = '',
		onchange
	}: Props = $props();

	let list: HTMLDivElement | null = $state(null);

	function select(id: string) {
		active = id;
		onchange?.(id);
	}

	function onKey(e: KeyboardEvent) {
		const enabled = items.filter((it) => !it.disabled);
		const i = enabled.findIndex((it) => it.id === active);
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
	{#each items as tab (tab.id)}
		<button
			type="button"
			role="tab"
			id="{idPrefix}tab-{tab.id}"
			aria-selected={active === tab.id}
			aria-controls="{idPrefix}panel-{tab.id}"
			tabindex={active === tab.id ? 0 : -1}
			disabled={tab.disabled}
			onclick={() => select(tab.id)}
			class="inline-flex shrink-0 items-center gap-1.5 border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors
				disabled:cursor-not-allowed disabled:opacity-40
				{active === tab.id
				? 'border-accent font-medium text-fg'
				: 'border-transparent text-fg-muted hover:border-border-strong hover:text-fg'}"
		>
			{#if tab.icon}<Icon name={tab.icon} size={15} />{/if}
			{tab.label}
			{#if tab.count !== undefined && tab.count !== null}
				<span
					class="rounded px-1.5 text-[0.7rem] tabular {active === tab.id
						? 'bg-accent-soft text-accent'
						: 'bg-surface-3 text-fg-subtle'}">{formatNumber(tab.count)}</span
				>
			{/if}
		</button>
	{/each}
</div>
