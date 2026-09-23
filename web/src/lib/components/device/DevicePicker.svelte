<!--
	Device search combobox (GET /api/v1/devices?q=…): type to search, ↑/↓ + Enter to pick.
	<DevicePicker label="Gerät" bind:value={id} exclude={[self]} error={errors.device} />
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { DeviceRow } from '$lib/api/types';
	import FormField from '$lib/components/ui/FormField.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import StatusDot from '$lib/components/ui/StatusDot.svelte';
	import { debounce } from '$lib/utils/url';

	interface Props {
		value: number | null;
		label: string;
		exclude?: number[];
		error?: string | null;
		required?: boolean;
	}

	let { value = $bindable(null), label, exclude = [], error, required = false }: Props = $props();

	let text = $state('');
	let results = $state<DeviceRow[]>([]);
	let picked = $state<DeviceRow | null>(null);
	let openList = $state(false);
	let active = $state(-1);
	let loading = $state(false);
	let ctrl: AbortController | null = null;
	const uid = $props.id();

	const search = debounce(async (q: string) => {
		ctrl?.abort();
		const c = new AbortController();
		ctrl = c;
		loading = true;
		try {
			const res = await api.get('/api/v1/devices', { query: { q: q.trim(), limit: 12 }, signal: c.signal });
			results = (res.items ?? []).filter((d) => !exclude.includes(d.id));
			active = results.length ? 0 : -1;
		} catch {
			results = [];
		} finally {
			if (ctrl === c) loading = false;
		}
	}, 200);
	$effect(() => () => {
		search.cancel();
		ctrl?.abort();
	});

	function onInput() {
		picked = null;
		value = null;
		openList = true;
		search(text);
	}

	function pick(d: DeviceRow) {
		picked = d;
		value = d.id;
		text = d.name || d.ip || d.mac;
		openList = false;
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' && results.length) {
			e.preventDefault();
			openList = true;
			active = (active + 1) % results.length;
		} else if (e.key === 'ArrowUp' && results.length) {
			e.preventDefault();
			active = (active - 1 + results.length) % results.length;
		} else if (e.key === 'Enter' && openList && active >= 0 && results[active]) {
			e.preventDefault();
			pick(results[active]);
		} else if (e.key === 'Escape' && openList) {
			e.preventDefault();
			e.stopPropagation();
			openList = false;
		}
	}
</script>

<FormField
	{label}
	{error}
	{required}
	hint={picked ? `#${picked.id} · ${picked.ip || picked.mac}` : 'Name, IP oder MAC eingeben'}
>
	{#snippet children(id, describedby)}
		<div class="relative">
			<span class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-fg-subtle">
				<Icon name="search" size={14} />
			</span>
			<input
				{id}
				type="text"
				role="combobox"
				autocomplete="off"
				aria-expanded={openList && results.length > 0}
				aria-controls="{uid}-list"
				aria-activedescendant={openList && active >= 0 ? `${uid}-opt-${active}` : undefined}
				aria-describedby={describedby}
				aria-invalid={error ? 'true' : undefined}
				bind:value={text}
				oninput={onInput}
				onkeydown={onKey}
				onfocus={() => {
					if (!picked) {
						openList = true;
						search(text);
					}
				}}
				onblur={() => setTimeout(() => (openList = false), 150)}
				placeholder="Gerät suchen …"
				class="h-8.5 w-full rounded-md border bg-surface pr-8 pl-8 text-sm shadow-sm focus:border-accent focus:ring-2 focus:ring-focus focus:outline-none {error
					? 'border-danger'
					: 'border-border hover:border-border-strong'}"
			/>
			{#if picked}
				<span class="absolute top-1/2 right-2.5 -translate-y-1/2 text-ok"
					><Icon name="check" size={14} /></span
				>
			{/if}
			{#if openList && (results.length || loading)}
				<ul
					id="{uid}-list"
					role="listbox"
					aria-label="Gefundene Geräte"
					class="absolute z-20 mt-1 max-h-64 w-full overflow-auto rounded-md border border-border bg-surface py-1 shadow-md"
				>
					{#each results as d, i (d.id)}
						<li
							id="{uid}-opt-{i}"
							role="option"
							aria-selected={i === active}
							class="flex cursor-pointer items-center gap-2 px-2.5 py-1.5 text-sm {i === active
								? 'bg-surface-2'
								: ''}"
							onmousedown={(e) => {
								e.preventDefault();
								pick(d);
							}}
							onmouseenter={() => (active = i)}
						>
							<StatusDot status={d.online ? 'online' : 'offline'} pulse={false} />
							<span class="min-w-0 flex-1 truncate">{d.name || d.ip || d.mac}</span>
							<span class="mono text-xs text-fg-subtle">{d.ip}</span>
						</li>
					{:else}
						<li class="px-2.5 py-1.5 text-sm text-fg-subtle">Suche …</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/snippet}
</FormField>
