<!--
	Dropdown menu with keyboard navigation (↑/↓/Home/End, Enter, Escape).
	<Menu label="Aktionen" icon="more" items={[
		{ label: 'Bearbeiten', icon: 'edit', onclick: edit },
		{ label: 'Details', href: '/x' },
		{ separator: true },
		{ label: 'Löschen', icon: 'trash', danger: true, onclick: del }
	]} />
	Pass `text` to show a text button instead of an icon-only button.
-->
<script lang="ts" module>
	import type { IconName } from './icons';
	export type MenuItem =
		| {
				label: string;
				icon?: IconName;
				onclick?: () => void;
				href?: string;
				danger?: boolean;
				disabled?: boolean;
				hint?: string;
				separator?: false;
		  }
		| { separator: true; label?: string };
</script>

<script lang="ts">
	import Button from './Button.svelte';
	import Icon from './Icon.svelte';
	import Popover from './Popover.svelte';

	interface Props {
		items: MenuItem[];
		label: string;
		text?: string;
		icon?: IconName;
		variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'subtle';
		size?: 'xs' | 'sm' | 'md';
		placement?: 'bottom-start' | 'bottom-end';
		disabled?: boolean;
	}

	let {
		items,
		label,
		text,
		icon = 'more',
		variant = 'ghost',
		size = 'sm',
		placement = 'bottom-end',
		disabled = false
	}: Props = $props();

	let open = $state(false);
	let wrap: HTMLSpanElement | null = $state(null);
	let panel: HTMLDivElement | null = $state(null);

	function focusables() {
		return Array.from(
			panel?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([aria-disabled="true"])') ?? []
		);
	}

	$effect(() => {
		if (open && panel) requestAnimationFrame(() => focusables()[0]?.focus());
	});

	function onKey(e: KeyboardEvent) {
		const list = focusables();
		const i = list.indexOf(document.activeElement as HTMLElement);
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			list[(i + 1) % list.length]?.focus();
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			list[(i - 1 + list.length) % list.length]?.focus();
		} else if (e.key === 'Home') {
			e.preventDefault();
			list[0]?.focus();
		} else if (e.key === 'End') {
			e.preventDefault();
			list[list.length - 1]?.focus();
		} else if (e.key === 'Tab') {
			open = false;
		}
	}

	function run(item: MenuItem) {
		if (item.separator || item.disabled) return;
		open = false;
		item.onclick?.();
	}
</script>

<span bind:this={wrap} class="inline-flex">
	<Button
		{variant}
		{size}
		icon={text ? undefined : icon}
		iconRight={text ? 'chevron-down' : undefined}
		label={text ? undefined : label}
		{disabled}
		aria-haspopup="menu"
		aria-expanded={open}
		onclick={() => (open = !open)}
	>
		{#if text}{text}{/if}
	</Button>
</span>

<Popover bind:open bind:panel anchor={wrap} {placement} role="menu" {label} class="min-w-44 py-1">
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div onkeydown={onKey}>
		{#each items as item, i (i)}
			{#if item.separator}
				<div class="my-1 border-t border-border" role="separator">
					{#if item.label}<div
							class="px-3 pt-1.5 text-[0.7rem] font-semibold tracking-wide text-fg-subtle uppercase"
						>
							{item.label}
						</div>{/if}
				</div>
			{:else if item.href && !item.disabled}
				<a
					href={item.href}
					role="menuitem"
					tabindex="-1"
					onclick={() => (open = false)}
					class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2 focus:bg-surface-2 focus:outline-none
						{item.danger ? 'text-danger' : 'text-fg'}"
				>
					{#if item.icon}<Icon name={item.icon} size={15} class="text-fg-subtle" />{/if}
					<span class="flex-1">{item.label}</span>
					{#if item.hint}<span class="text-xs text-fg-subtle">{item.hint}</span>{/if}
				</a>
			{:else}
				<button
					type="button"
					role="menuitem"
					tabindex="-1"
					aria-disabled={item.disabled ? 'true' : undefined}
					onclick={() => run(item)}
					class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2 focus:bg-surface-2 focus:outline-none
						{item.disabled ? 'cursor-not-allowed opacity-50' : ''} {item.danger ? 'text-danger' : 'text-fg'}"
				>
					{#if item.icon}<Icon name={item.icon} size={15} class={item.danger ? '' : 'text-fg-subtle'} />{/if}
					<span class="flex-1">{item.label}</span>
					{#if item.hint}<span class="text-xs text-fg-subtle">{item.hint}</span>{/if}
				</button>
			{/if}
		{/each}
	</div>
</Popover>
