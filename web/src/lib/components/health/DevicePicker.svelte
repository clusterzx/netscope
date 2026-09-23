<!--
	Device picker (combobox): searches devices via GET /api/v1/devices?q=… and selects one.
	<DevicePicker bind:value={deviceId} bind:name={deviceName} label="Gerät" />
	value 0 = no device.
-->
<script lang="ts">
	import { api, errorMessage } from '$lib/api';
	import type { DeviceRow } from '$lib/api';
	import { Button, FormField, Icon, Spinner, StatusDot } from '$lib/components/ui';
	import { debounce } from '$lib/utils/url';

	interface Props {
		value?: number;
		name?: string;
		label?: string;
		hint?: string;
		error?: string | null;
		id?: string;
		onselect?: (d: DeviceRow | null) => void;
	}

	let {
		value = $bindable(0),
		name = $bindable(''),
		label = 'Gerät',
		hint,
		error,
		id,
		onselect
	}: Props = $props();

	const uid = $props.id();
	let text = $state('');
	let open = $state(false);
	let active = $state(-1);
	let loading = $state(false);
	let results = $state<DeviceRow[]>([]);
	let searchError = $state<string | null>(null);
	let input: HTMLInputElement | null = $state(null);
	let ctrl: AbortController | null = null;

	const search = debounce(async (q: string) => {
		ctrl?.abort();
		const c = new AbortController();
		ctrl = c;
		loading = true;
		try {
			const res = await api.get('/api/v1/devices', {
				query: { q: q.trim() || null, limit: 8, sort: 'name' },
				signal: c.signal
			});
			if (ctrl !== c) return;
			results = res.items ?? [];
			searchError = null;
			active = results.length ? 0 : -1;
		} catch (e) {
			if (e instanceof DOMException && e.name === 'AbortError') return;
			results = [];
			searchError = errorMessage(e);
		} finally {
			if (ctrl === c) loading = false;
		}
	}, 250);

	$effect(() => () => {
		search.cancel();
		ctrl?.abort();
	});

	function onInput() {
		open = true;
		search(text);
	}

	// open on click / ArrowDown / typing – not on focus (dialogs autofocus the first field)
	function openList() {
		if (!open) {
			open = true;
			search(text);
		}
	}

	function pick(d: DeviceRow) {
		value = d.id;
		name = d.displayName || d.name || d.ip;
		text = '';
		open = false;
		results = [];
		onselect?.(d);
	}

	function clear() {
		value = 0;
		name = '';
		onselect?.(null);
		requestAnimationFrame(() => input?.focus());
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			if (!open) openList();
			else if (results.length) active = (active + 1) % results.length;
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			if (results.length) active = (active - 1 + results.length) % results.length;
		} else if (e.key === 'Enter') {
			if (open && active >= 0 && results[active]) {
				e.preventDefault();
				pick(results[active]);
			}
		} else if (e.key === 'Escape') {
			if (open) {
				e.preventDefault();
				e.stopPropagation();
				open = false;
			}
		}
	}
</script>

<FormField {label} {hint} {error} {id}>
	{#snippet children(fid, describedby)}
		{#if value}
			<div
				class="flex h-8.5 items-center gap-2 rounded-md border bg-surface-2 pr-1 pl-2.5 text-sm shadow-sm
					{error ? 'border-danger' : 'border-border'}"
			>
				<Icon name="devices" size={15} class="text-fg-subtle" />
				<a
					href="/devices/{value}"
					class="link min-w-0 flex-1 truncate"
					id={fid}
					aria-describedby={describedby}>{name || `Gerät #${value}`}</a
				>
				<Button variant="ghost" size="xs" icon="x" label="Gerät entfernen" onclick={clear} />
			</div>
		{:else}
			<div class="relative">
				<span class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-fg-subtle">
					{#if loading}<Spinner size={15} />{:else}<Icon name="search" size={15} />{/if}
				</span>
				<input
					bind:this={input}
					bind:value={text}
					id={fid}
					type="text"
					role="combobox"
					autocomplete="off"
					aria-expanded={open}
					aria-controls="{uid}-list"
					aria-autocomplete="list"
					aria-describedby={describedby}
					aria-invalid={error ? 'true' : undefined}
					aria-activedescendant={open && active >= 0 ? `${uid}-opt-${active}` : undefined}
					placeholder="Name, IP oder MAC suchen …"
					oninput={onInput}
					onclick={openList}
					onkeydown={onKey}
					onblur={() => setTimeout(() => (open = false), 150)}
					class="h-8.5 w-full min-w-0 rounded-md border bg-surface pr-2.5 pl-8 text-sm text-fg shadow-sm placeholder:text-fg-subtle
						focus:border-accent focus:ring-2 focus:ring-focus focus:outline-none
						{error ? 'border-danger' : 'border-border hover:border-border-strong'}"
				/>
				{#if open}
					<ul
						id="{uid}-list"
						role="listbox"
						aria-label="Gefundene Geräte"
						class="absolute z-40 mt-1 max-h-64 w-full overflow-auto rounded-md border border-border bg-surface py-1 shadow-lg"
					>
						{#if searchError}
							<li class="px-3 py-2 text-sm text-danger" role="presentation">{searchError}</li>
						{:else if results.length === 0}
							<li class="px-3 py-2 text-sm text-fg-subtle" role="presentation">
								{loading ? 'Suche …' : 'Keine Geräte gefunden'}
							</li>
						{/if}
						{#each results as d, i (d.id)}
							<li
								id="{uid}-opt-{i}"
								role="option"
								aria-selected={i === active}
								class="flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm {i === active
									? 'bg-accent-soft'
									: 'hover:bg-surface-2'}"
								onmousedown={(e) => {
									e.preventDefault();
									pick(d);
								}}
								onmouseenter={() => (active = i)}
							>
								<StatusDot status={d.online ? 'online' : 'offline'} />
								<span class="min-w-0 flex-1 truncate text-fg">{d.displayName || d.name || d.ip}</span>
								<span class="mono shrink-0 text-xs text-fg-subtle">{d.ip}</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		{/if}
	{/snippet}
</FormField>
