<!-- Site selector of a central instance: restricts devices, topology, events, vulnerabilities,
     the dashboard and exports to one NetScope site (hidden without sites). -->
<script lang="ts">
	import Icon from '$lib/components/ui/Icon.svelte';
	import Popover from '$lib/components/ui/Popover.svelte';
	import StatusDot from '$lib/components/ui/StatusDot.svelte';
	import { federation, siteFilter } from '$lib/stores/federation.svelte';

	type Option = { value: string; label: string; status?: string; hint?: string };

	const disconnected = $derived(federation.sites.filter((s) => !s.connected && s.contacted).length);
	const options = $derived<Option[]>([
		{ value: '', label: 'Alle Standorte' },
		{ value: 'local', label: federation.localName, hint: 'diese Instanz' },
		...federation.sites.map((s) => ({
			value: s.slug,
			label: s.name,
			status: s.connected ? 'online' : s.contacted ? 'warn' : 'idle',
			hint: s.connected ? '' : s.contacted ? 'getrennt' : 'noch keine Meldung'
		}))
	]);
	const current = $derived(options.find((o) => o.value === siteFilter.value) ?? options[0]);

	let open = $state(false);
	let button: HTMLButtonElement | null = $state(null);
	let panel: HTMLDivElement | null = $state(null);

	function items() {
		return Array.from(panel?.querySelectorAll<HTMLElement>('[role="option"]') ?? []);
	}

	$effect(() => {
		if (open && panel)
			requestAnimationFrame(() => {
				const list = items();
				(list.find((el) => el.getAttribute('aria-selected') === 'true') ?? list[0])?.focus();
			});
	});

	function onKey(e: KeyboardEvent) {
		const list = items();
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

	function choose(value: string) {
		siteFilter.value = value;
		open = false;
		button?.focus();
	}
</script>

{#if federation.isCentral}
	<button
		bind:this={button}
		type="button"
		aria-haspopup="listbox"
		aria-expanded={open}
		aria-label="Standort: {current.label}"
		title={disconnected
			? `${disconnected} Standort(e) melden sich zurzeit nicht`
			: 'Ansicht auf einen Standort einschränken'}
		onclick={() => (open = !open)}
		class="inline-flex h-8 max-w-[10rem] items-center gap-1.5 rounded-md border border-border bg-surface-2 pr-1.5 pl-2 text-xs font-medium
			text-fg hover:border-border-strong focus-visible:ring-2 focus-visible:ring-accent/40 focus-visible:outline-none sm:max-w-[14rem]
			{open ? 'border-border-strong' : ''}"
	>
		<Icon name="globe" size={15} class="shrink-0 {disconnected ? 'text-warn' : 'text-fg-subtle'}" />
		<span class="truncate">{current.label}</span>
		<Icon name="chevron-down" size={13} class="shrink-0 text-fg-subtle" />
	</button>
	<Popover
		bind:open
		bind:panel
		anchor={button}
		placement="bottom-end"
		role="listbox"
		label="Standort"
		class="min-w-56 py-1"
	>
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div onkeydown={onKey}>
			{#each options as o, i (o.value)}
				{#if i === 2}<div class="my-1 border-t border-border" aria-hidden="true"></div>{/if}
				<button
					type="button"
					role="option"
					aria-selected={o.value === siteFilter.value}
					tabindex="-1"
					onclick={() => choose(o.value)}
					class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-fg hover:bg-surface-2 focus:bg-surface-2 focus:outline-none"
				>
					<span class="inline-flex w-4 shrink-0 justify-center">
						{#if o.status}
							<StatusDot status={o.status} pulse={false} />
						{:else}
							<Icon name={o.value ? 'home' : 'globe'} size={15} class="text-fg-subtle" />
						{/if}
					</span>
					<span class="min-w-0 flex-1 truncate">{o.label}</span>
					{#if o.hint}<span class="text-xs whitespace-nowrap text-fg-subtle">{o.hint}</span>{/if}
					<span class="inline-flex w-3.5 shrink-0">
						{#if o.value === siteFilter.value}<Icon name="check" size={14} class="text-accent" />{/if}
					</span>
				</button>
			{/each}
		</div>
	</Popover>
{/if}
