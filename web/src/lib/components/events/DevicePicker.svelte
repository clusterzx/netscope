<!--
  Compact device filter: shows the chosen device (or "Alle Geräte") and opens a searchable
  popover (GET /api/v1/devices?q=…). Used by the events and diff pages.

  <DevicePicker value={deviceId} name={knownName} onchange={(id) => …} />
-->
<script lang="ts">
	import { api, errorMessage } from '$lib/api';
	import type { DeviceRow } from '$lib/api';
	import { Button, Icon, Popover, Spinner } from '$lib/components/ui';
	import { debounce } from '$lib/utils/url';

	interface Props {
		value: number | null;
		/** name of the device if already known (avoids a lookup) */
		name?: string | null;
		size?: 'sm' | 'md';
		class?: string;
		onchange: (id: number | null, name: string | null) => void;
	}

	let { value, name = null, size = 'md', class: klass = '', onchange }: Props = $props();

	const uid = $props.id();
	let open = $state(false);
	let anchor: HTMLDivElement | null = $state(null);
	let input: HTMLInputElement | null = $state(null);
	let text = $state('');
	let results = $state<DeviceRow[]>([]);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let active = $state(0);
	let resolved = $state<{ id: number; name: string } | null>(null);

	// resolve the name of a device given only by id (e.g. /events?device=12)
	$effect(() => {
		const id = value;
		if (!id || name || resolved?.id === id) return;
		const ctrl = new AbortController();
		api
			.get('/api/v1/devices/{id}', { path: { id }, signal: ctrl.signal })
			.then((d) => (resolved = { id, name: d.name }))
			.catch(() => (resolved = { id, name: `Gerät #${id}` }));
		return () => ctrl.abort();
	});

	const display = $derived(
		value ? (name ?? (resolved?.id === value ? resolved.name : `Gerät #${value}`)) : ''
	);

	let ctrl: AbortController | null = null;
	async function search(q: string) {
		ctrl?.abort();
		const c = new AbortController();
		ctrl = c;
		loading = true;
		error = null;
		try {
			const res = await api.get('/api/v1/devices', {
				query: { q: q.trim() || null, limit: 20, sort: 'name' },
				signal: c.signal
			});
			results = res.items ?? [];
			active = 0;
		} catch (e) {
			if (e instanceof DOMException && e.name === 'AbortError') return;
			results = [];
			error = errorMessage(e);
		} finally {
			if (ctrl === c) loading = false;
		}
	}
	const searchLater = debounce(search, 250);
	$effect(() => () => {
		searchLater.cancel();
		ctrl?.abort();
	});

	function toggle() {
		open = !open;
		if (open) {
			text = '';
			search('');
			requestAnimationFrame(() => input?.focus());
		}
	}

	function choose(d: DeviceRow | null) {
		open = false;
		onchange(d ? d.id : null, d ? d.name : null);
		anchor?.querySelector('button')?.focus();
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			active = Math.min(results.length - 1, active + 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			active = Math.max(0, active - 1);
		} else if (e.key === 'Enter') {
			e.preventDefault();
			if (results[active]) choose(results[active]);
		}
	}
</script>

<div bind:this={anchor} class="inline-flex max-w-full items-center {klass}">
	<button
		type="button"
		class="inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-md border bg-surface text-left shadow-sm transition-colors hover:border-border-strong focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus
			{value ? 'border-accent/50 text-fg' : 'border-border text-fg-muted'}
			{size === 'sm' ? 'h-7 px-2 text-[0.8125rem]' : 'h-8.5 px-2.5 text-sm'} {value ? 'rounded-r-none' : ''}"
		aria-haspopup="dialog"
		aria-expanded={open}
		aria-label={value ? `Gerät: ${display} – ändern` : 'Gerät wählen'}
		onclick={toggle}
	>
		<Icon name="devices" size={14} class="text-fg-subtle" />
		<span class="truncate">{value ? display : 'Alle Geräte'}</span>
		<Icon name="chevron-down" size={14} class="text-fg-subtle" />
	</button>
	{#if value}
		<Button
			size={size === 'sm' ? 'sm' : 'md'}
			icon="x"
			label="Gerätefilter entfernen"
			class="-ml-px rounded-l-none"
			onclick={() => choose(null)}
		/>
	{/if}
</div>

<Popover
	bind:open
	{anchor}
	placement="bottom-start"
	label="Gerät wählen"
	class="w-80 max-w-[calc(100vw-1rem)]"
>
	<div class="border-b border-border p-2">
		<label for="{uid}-q" class="sr-only">Gerät suchen</label>
		<div class="relative flex items-center">
			<span class="pointer-events-none absolute left-2 text-fg-subtle"><Icon name="search" size={14} /></span>
			<input
				bind:this={input}
				id="{uid}-q"
				type="search"
				autocomplete="off"
				placeholder="Name, IP, MAC …"
				bind:value={text}
				oninput={() => searchLater(text)}
				onkeydown={onKey}
				role="combobox"
				aria-expanded="true"
				aria-controls="{uid}-list"
				aria-activedescendant={results[active] ? `${uid}-o${results[active].id}` : undefined}
				class="h-7 w-full rounded-md border border-border bg-surface pr-2 pl-7 text-[0.8125rem] text-fg placeholder:text-fg-subtle focus:border-accent focus:ring-2 focus:ring-focus focus:outline-none"
			/>
		</div>
	</div>
	<ul id="{uid}-list" role="listbox" aria-label="Geräte" class="max-h-72 overflow-y-auto p-1">
		{#if loading && !results.length}
			<li class="flex items-center gap-2 px-2 py-3 text-sm text-fg-muted"><Spinner size={14} /> Suche …</li>
		{:else if error}
			<li class="px-2 py-3 text-sm text-danger">{error}</li>
		{:else}
			{#each results as d, i (d.id)}
				<li
					id="{uid}-o{d.id}"
					role="option"
					aria-selected={i === active}
					class="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm {i === active
						? 'bg-accent-soft text-fg'
						: 'text-fg-muted hover:bg-surface-2'}"
					onpointerenter={() => (active = i)}
					onclick={() => choose(d)}
					onkeydown={() => {}}
				>
					<span class="inline-block size-2 shrink-0 rounded-full {d.online ? 'bg-online' : 'bg-offline'}"
					></span>
					<span class="min-w-0 flex-1 truncate text-fg">{d.name}</span>
					{#if d.ip && d.ip !== d.name}<span class="mono shrink-0 text-xs text-fg-subtle">{d.ip}</span>{/if}
				</li>
			{:else}
				<li class="px-2 py-3 text-sm text-fg-muted">Keine Geräte gefunden</li>
			{/each}
		{/if}
	</ul>
</Popover>
