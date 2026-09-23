<!--
	Global device search in the top bar: quick results while typing, Enter opens
	/devices?q=…, "/" or Ctrl+K focuses the field.
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';
	import type { DeviceRow } from '$lib/api/types';
	import Icon from '$lib/components/ui/Icon.svelte';
	import StatusDot from '$lib/components/ui/StatusDot.svelte';

	let q = $state('');
	let results = $state<DeviceRow[]>([]);
	let total = $state(0);
	let open = $state(false);
	let active = $state(-1);
	let input: HTMLInputElement | null = $state(null);
	let timer: ReturnType<typeof setTimeout> | null = null;
	let ctrl: AbortController | null = null;
	const uid = $props.id();

	function search(text: string) {
		if (timer) clearTimeout(timer);
		ctrl?.abort();
		if (!text.trim()) {
			results = [];
			total = 0;
			return;
		}
		timer = setTimeout(async () => {
			ctrl = new AbortController();
			try {
				const res = await api.get('/api/v1/devices', {
					query: { q: text, limit: 6, sort: 'name' },
					signal: ctrl.signal
				});
				results = res.items ?? [];
				total = res.total;
				active = -1;
			} catch {
				// invalid query while typing – just show no quick results
				results = [];
				total = 0;
			}
		}, 220);
	}

	function submit() {
		const text = q.trim();
		open = false;
		if (active >= 0 && results[active]) {
			goto(`/devices/${results[active].id}`);
		} else {
			goto(text ? `/devices?q=${encodeURIComponent(text)}` : '/devices');
		}
		q = '';
		results = [];
		input?.blur();
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' && results.length) {
			e.preventDefault();
			open = true;
			active = (active + 1) % results.length;
		} else if (e.key === 'ArrowUp' && results.length) {
			e.preventDefault();
			active = (active - 1 + results.length) % results.length;
		} else if (e.key === 'Enter') {
			e.preventDefault();
			submit();
		} else if (e.key === 'Escape') {
			open = false;
			input?.blur();
		}
	}

	function onGlobalKey(e: KeyboardEvent) {
		const t = e.target as HTMLElement;
		const typing = t.closest('input, textarea, select, [contenteditable="true"]');
		if ((e.key === 'k' && (e.ctrlKey || e.metaKey)) || (e.key === '/' && !typing)) {
			e.preventDefault();
			input?.focus();
			input?.select();
		}
	}

	function label(d: DeviceRow) {
		return d.name || d.ip || d.mac || `#${d.id}`;
	}
</script>

<svelte:window onkeydown={onGlobalKey} />

<div class="relative w-full max-w-md">
	<form role="search" onsubmit={(e) => (e.preventDefault(), submit())}>
		<label for="{uid}-q" class="sr-only">Geräte suchen</label>
		<span class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-fg-subtle"
			><Icon name="search" size={15} /></span
		>
		<input
			bind:this={input}
			id="{uid}-q"
			type="search"
			bind:value={q}
			oninput={() => {
				open = true;
				search(q);
			}}
			onfocus={() => (open = true)}
			onblur={() => setTimeout(() => (open = false), 150)}
			onkeydown={onKey}
			role="combobox"
			aria-expanded={open && results.length > 0}
			aria-controls="{uid}-list"
			aria-autocomplete="list"
			aria-activedescendant={active >= 0 ? `${uid}-r${active}` : undefined}
			autocomplete="off"
			spellcheck="false"
			placeholder="Geräte suchen … (z. B. tag:iot port:22)"
			class="h-8.5 w-full rounded-md border border-border bg-surface-2 pr-12 pl-8 text-sm text-fg placeholder:text-fg-subtle
				focus:border-accent focus:bg-surface focus:ring-2 focus:ring-focus focus:outline-none"
		/>
		<kbd
			class="pointer-events-none absolute top-1/2 right-2 hidden -translate-y-1/2 rounded border border-border px-1.5 text-[0.68rem] text-fg-subtle sm:block"
			>Strg K</kbd
		>
	</form>
	{#if open && results.length > 0}
		<ul
			id="{uid}-list"
			role="listbox"
			class="absolute top-full right-0 left-0 z-50 mt-1 overflow-hidden rounded-lg border border-border bg-surface py-1 shadow-lg"
		>
			{#each results as d, i (d.id)}
				<li id="{uid}-r{i}" role="option" aria-selected={i === active}>
					<a
						href="/devices/{d.id}"
						tabindex="-1"
						onmousedown={(e) => e.preventDefault()}
						onclick={() => {
							open = false;
							q = '';
							results = [];
						}}
						class="flex items-center gap-2.5 px-3 py-1.5 text-sm {i === active
							? 'bg-accent-soft'
							: 'hover:bg-surface-2'}"
					>
						<StatusDot status={d.online ? 'online' : 'offline'} />
						<span class="min-w-0 flex-1 truncate text-fg">{label(d)}</span>
						<span class="mono text-xs text-fg-subtle">{d.ip}</span>
					</a>
				</li>
			{/each}
			{#if total > results.length}
				<li class="border-t border-border px-3 py-1.5 text-xs text-fg-subtle">
					Enter: alle {total} Treffer in der Geräteliste anzeigen
				</li>
			{/if}
		</ul>
	{/if}
</div>
