<!--
	Device search (GET /api/v1/devices?q=) with keyboard navigation.
	Multi: <DevicePicker label="Geräte" bind:value={ids} />            value: number[]
	Single: <DevicePicker label="Gerät" single bind:value={ids} />      value: [] or [id]
	Names of preselected ids are resolved via GET /api/v1/devices/{id}.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { DeviceRow } from '$lib/api';
	import FormField from '$lib/components/ui/FormField.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Spinner from '$lib/components/ui/Spinner.svelte';
	import { debounce } from '$lib/utils/url';

	interface Props {
		value?: number[];
		label: string;
		hint?: string;
		error?: string | null;
		single?: boolean;
		placeholder?: string;
		disabled?: boolean;
		id?: string;
		class?: string;
		onchange?: (ids: number[]) => void;
	}

	let {
		value = $bindable([]),
		label,
		hint,
		error,
		single = false,
		placeholder = 'Gerät suchen (Name, IP, MAC …)',
		disabled = false,
		id,
		class: klass = '',
		onchange
	}: Props = $props();

	type Known = { id: number; name: string; ip: string; missing?: boolean };

	const uid = $props.id();
	let text = $state('');
	let focused = $state(false);
	let active = $state(-1);
	let loading = $state(false);
	let results = $state<DeviceRow[]>([]);
	let searchError = $state('');
	let known = $state<Record<number, Known>>({});
	let input: HTMLInputElement | null = $state(null);
	let ctrl: AbortController | null = null;

	function rowName(d: DeviceRow): string {
		return d.name || d.displayName || d.hostname || d.ip || d.mac || `#${d.id}`;
	}

	// resolve names of ids we have not seen in a search result
	$effect(() => {
		const ids = value ?? [];
		untrack(() => resolve(ids));
	});

	function resolve(ids: number[]) {
		for (const did of ids) {
			if (known[did]) continue;
			known[did] = { id: did, name: `#${did}`, ip: '' };
			api
				.get('/api/v1/devices/{id}', { path: { id: did } })
				.then((d) => {
					known[did] = { id: did, name: d.name || d.hostname || d.ip || `#${did}`, ip: d.ip ?? '' };
				})
				.catch(() => {
					known[did] = { id: did, name: `#${did}`, ip: '', missing: true };
				});
		}
	}

	const search = debounce(async (q: string) => {
		ctrl?.abort();
		const c = new AbortController();
		ctrl = c;
		loading = true;
		searchError = '';
		try {
			const res = await api.get('/api/v1/devices', {
				query: { q, limit: 8, sort: 'name' },
				signal: c.signal
			});
			if (ctrl !== c) return;
			results = res.items ?? [];
			active = results.length ? 0 : -1;
		} catch (e) {
			if (e instanceof DOMException && e.name === 'AbortError') return;
			results = [];
			searchError = 'Suche fehlgeschlagen';
		} finally {
			if (ctrl === c) loading = false;
		}
	}, 250);

	$effect(() => () => {
		search.cancel();
		ctrl?.abort();
	});

	const matches = $derived(results.filter((d) => !(value ?? []).includes(d.id)));
	const showList = $derived(focused && text.trim().length > 0);

	function onInput() {
		const q = text.trim();
		if (!q) {
			results = [];
			return;
		}
		search(q);
	}

	function pick(d: DeviceRow) {
		known[d.id] = { id: d.id, name: rowName(d), ip: d.ip };
		const next = single ? [d.id] : [...(value ?? []), d.id];
		value = next;
		onchange?.(next);
		text = '';
		results = [];
		active = -1;
		if (single) input?.blur();
	}

	function remove(did: number) {
		const next = (value ?? []).filter((x) => x !== did);
		value = next;
		onchange?.(next);
		input?.focus();
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' && matches.length) {
			e.preventDefault();
			active = (active + 1) % matches.length;
		} else if (e.key === 'ArrowUp' && matches.length) {
			e.preventDefault();
			active = (active - 1 + matches.length) % matches.length;
		} else if (e.key === 'Enter') {
			if (showList && active >= 0 && matches[active]) {
				e.preventDefault();
				pick(matches[active]);
			} else if (text.trim()) e.preventDefault();
		} else if (e.key === 'Backspace' && !text && (value?.length ?? 0) > 0) {
			remove(value[value.length - 1]);
		} else if (e.key === 'Escape' && text) {
			e.stopPropagation();
			text = '';
			results = [];
		}
	}
</script>

<FormField {label} {hint} {error} {id} class={klass}>
	{#snippet children(fid, describedby)}
		<div class="relative">
			<div
				class="flex min-h-8.5 w-full flex-wrap items-center gap-1 rounded-md border bg-surface px-1.5 py-1 shadow-sm transition-colors
					focus-within:border-accent focus-within:ring-2 focus-within:ring-focus
					{error ? 'border-danger' : 'border-border hover:border-border-strong'} {disabled ? 'opacity-60' : ''}"
			>
				{#each value ?? [] as did (did)}
					{@const k = known[did]}
					<span
						class="inline-flex max-w-full items-center gap-1 rounded py-0.5 pr-0.5 pl-1.5 text-[0.8125rem]
							{k?.missing ? 'bg-danger-soft text-danger' : 'bg-surface-3 text-fg'}"
						title={k?.missing ? 'Gerät existiert nicht mehr' : k?.ip}
					>
						<Icon name="devices" size={12} class="text-fg-subtle" />
						<span class="truncate">{k?.name ?? `#${did}`}</span>
						{#if k?.ip}<span class="mono text-xs text-fg-subtle">{k.ip}</span>{/if}
						{#if !disabled}
							<button
								type="button"
								class="rounded p-0.5 text-fg-subtle hover:bg-border hover:text-fg"
								aria-label="{k?.name ?? did} entfernen"
								onclick={() => remove(did)}
							>
								<Icon name="x" size={12} />
							</button>
						{/if}
					</span>
				{/each}
				<input
					bind:this={input}
					bind:value={text}
					id={fid}
					{disabled}
					type="search"
					autocomplete="off"
					role="combobox"
					aria-expanded={showList}
					aria-controls="{uid}-list"
					aria-autocomplete="list"
					aria-describedby={describedby}
					aria-activedescendant={showList && active >= 0 ? `${uid}-opt-${active}` : undefined}
					placeholder={(value?.length ?? 0) ? (single ? 'Anderes Gerät …' : 'Weiteres Gerät …') : placeholder}
					oninput={onInput}
					onkeydown={onKey}
					onfocus={() => (focused = true)}
					onblur={() => (focused = false)}
					class="h-6 min-w-32 flex-1 bg-transparent px-1 text-sm text-fg placeholder:text-fg-subtle focus:outline-none [&::-webkit-search-cancel-button]:hidden"
				/>
				{#if loading}<Spinner size={14} class="mr-1 text-fg-subtle" />{/if}
			</div>
			{#if showList}
				<ul
					id="{uid}-list"
					role="listbox"
					aria-label="Gefundene Geräte"
					class="absolute z-40 mt-1 max-h-64 w-full overflow-auto rounded-md border border-border bg-surface py-1 shadow-lg"
				>
					{#if searchError}
						<li class="px-3 py-1.5 text-sm text-danger" role="presentation">{searchError}</li>
					{:else if !loading && matches.length === 0}
						<li class="px-3 py-1.5 text-sm text-fg-subtle" role="presentation">Kein passendes Gerät</li>
					{/if}
					{#each matches as d, i (d.id)}
						<li
							id="{uid}-opt-{i}"
							role="option"
							aria-selected={i === active}
							class="flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm {i === active
								? 'bg-accent-soft text-accent'
								: 'hover:bg-surface-2'}"
							onmousedown={(e) => {
								e.preventDefault();
								pick(d);
							}}
						>
							<span class="min-w-0 flex-1 truncate">{rowName(d)}</span>
							<span class="mono shrink-0 text-xs text-fg-subtle">{d.ip}</span>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/snippet}
</FormField>
