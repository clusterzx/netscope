<!--
  Searchable node list next to the graph – makes the topology usable with the keyboard:
  type to filter (matches are highlighted in the graph), ↓ jumps into the list, ↑/↓/Home/End
  move between entries, Enter selects and centers the node.
-->
<script lang="ts">
	import type { GraphNode } from '$lib/api';
	import { Icon, Input } from '$lib/components/ui';
	import { formatNumber } from '$lib/utils/format';
	import { typeIcon } from './graph';

	interface Props {
		/** nodes to list (already filtered by the search) */
		nodes: GraphNode[];
		total: number;
		search?: string;
		selected: string | null;
		connectMode?: boolean;
		class?: string;
		onpick: (node: GraphNode) => void;
	}

	let {
		nodes,
		total,
		search = $bindable(''),
		selected,
		connectMode = false,
		class: klass = '',
		onpick
	}: Props = $props();

	let list: HTMLUListElement | null = $state(null);

	function items(): HTMLButtonElement[] {
		return Array.from(list?.querySelectorAll<HTMLButtonElement>('button[data-node]') ?? []);
	}

	function onSearchKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			items()[0]?.focus();
		} else if (e.key === 'Enter') {
			e.preventDefault();
			if (nodes[0]) onpick(nodes[0]);
		}
	}

	function onItemKey(e: KeyboardEvent) {
		const all = items();
		const i = all.indexOf(e.currentTarget as HTMLButtonElement);
		let next = -1;
		if (e.key === 'ArrowDown') next = Math.min(all.length - 1, i + 1);
		else if (e.key === 'ArrowUp') {
			if (i === 0) {
				e.preventDefault();
				list?.parentElement?.querySelector<HTMLInputElement>('input')?.focus();
				return;
			}
			next = i - 1;
		} else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = all.length - 1;
		if (next < 0) return;
		e.preventDefault();
		all[next]?.focus();
	}

	// keep the selected entry visible when it was selected in the graph
	$effect(() => {
		if (!selected || !list) return;
		const el = list.querySelector<HTMLElement>(`[data-node="${CSS.escape(selected)}"]`);
		if (el && document.activeElement !== el) el.scrollIntoView({ block: 'nearest' });
	});
</script>

<div class="flex min-h-0 flex-col {klass}">
	<div class="border-b border-border p-2">
		<Input
			type="search"
			size="sm"
			icon="search"
			bind:value={search}
			placeholder="Name, IP, Hersteller, Tag …"
			aria-label="Geräte im Graph suchen"
			autocomplete="off"
			onkeydown={onSearchKey}
		/>
		<p class="mt-1.5 px-0.5 text-xs text-fg-subtle" aria-live="polite">
			{#if search.trim()}
				{formatNumber(nodes.length)} von {formatNumber(total)} Treffern
			{:else}
				{formatNumber(total)} Knoten{#if connectMode}&nbsp;· Endpunkt wählen{/if}
			{/if}
		</p>
	</div>
	<ul bind:this={list} class="min-h-0 flex-1 overflow-y-auto p-1" aria-label="Knoten im Graph">
		{#each nodes as n (n.id)}
			{@const icon = typeIcon(n.type)}
			{@const sel = n.id === selected}
			<li>
				<button
					type="button"
					data-node={n.id}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-focus
						{sel ? 'bg-accent-soft text-fg' : 'text-fg-muted hover:bg-surface-2 hover:text-fg'}"
					aria-current={sel ? 'true' : undefined}
					onclick={() => onpick(n)}
					onkeydown={onItemKey}
				>
					<span
						class="inline-block size-2 shrink-0 {n.kind === 'container'
							? 'rounded-[2px]'
							: 'rounded-full'} {n.online ? 'bg-online' : 'bg-offline'} {n.state === 'unknown'
							? 'ring-2 ring-unknown/70'
							: ''}"
						aria-hidden="true"
					></span>
					<span class="sr-only">{n.online ? 'online' : 'offline'},</span>
					{#if icon}
						<Icon name={icon} size={14} class="text-fg-subtle" />
					{:else}
						<span class="inline-block w-3.5 shrink-0"></span>
					{/if}
					<span class="min-w-0 flex-1 truncate {sel ? 'font-medium' : ''}">{n.label}</span>
					{#if n.ip && n.ip !== n.label}
						<span class="mono hidden shrink-0 text-xs text-fg-subtle xl:inline">{n.ip}</span>
					{/if}
				</button>
			</li>
		{:else}
			<li class="px-2 py-6 text-center text-sm text-fg-muted">Keine Treffer</li>
		{/each}
	</ul>
</div>
