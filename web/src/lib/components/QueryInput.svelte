<!--
	Device filter query input (query language from GET /api/v1/meta → queryFields), e.g.
	`tag:iot port:22 os:linux cve>=7 seen<24h`. Autocompletes field names and known values
	(tags, groups, types, subnets, custom fields …), shows a help panel with clickable
	examples and the API validation error.

	<QueryInput bind:value={q} onsubmit={(q) => applyFilter(q)} error={queryError} />
	Props: value (bindable), onsubmit(q) on Enter / apply, error (message from the API, e.g.
	ApiError.message of a 400), placeholder, size, autofocus, id, label (sr-only by default).
	`live` (ms) additionally calls onsubmit debounced while typing.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Popover from '$lib/components/ui/Popover.svelte';
	import { customFields, groups, meta, subnets, tags } from '$lib/stores/catalog.svelte';
	import { deviceTypeLabel } from '$lib/utils/labels';

	interface Props {
		value?: string;
		onsubmit?: (q: string) => void;
		error?: string | null;
		placeholder?: string;
		label?: string;
		showLabel?: boolean;
		size?: 'sm' | 'md';
		live?: number;
		id?: string;
		class?: string;
	}

	let {
		value = $bindable(''),
		onsubmit,
		error,
		placeholder = 'Filter, z. B. tag:iot port:22 os:linux cve>=7 seen<24h',
		label = 'Geräte-Filter',
		showLabel = false,
		size = 'md',
		live = 0,
		id,
		class: klass = ''
	}: Props = $props();

	const uid = $props.id();
	const fid = $derived(id ?? `q-${uid}`);
	let input: HTMLInputElement | null = $state(null);
	let helpBtn: HTMLSpanElement | null = $state(null);
	let helpOpen = $state(false);
	let focused = $state(false);
	let caret = $state(0);
	let active = $state(-1);
	let dismissed = $state(false);
	let liveTimer: ReturnType<typeof setTimeout> | null = null;

	onMount(() => {
		meta.load().catch(() => {});
		tags.load().catch(() => {});
		groups.load().catch(() => {});
		subnets.load().catch(() => {});
		customFields.load().catch(() => {});
	});

	const fields = $derived.by(() => {
		const list = (meta.value?.queryFields ?? []).filter((f) => /^[a-z]+$/.test(f.field));
		const cf = (customFields.value ?? []).map((c) => ({
			field: `cf.${c.key}`,
			ops: ':',
			description: `Custom Field „${c.label}“`,
			example: `cf.${c.key}:`
		}));
		return [...list, ...cf];
	});

	const quote = (s: string) => (/[\s"]/.test(s) ? `"${s.replace(/"/g, '')}"` : s);

	function valuesFor(field: string): { value: string; hint?: string }[] {
		switch (field) {
			case 'tag':
				return (tags.value ?? []).map((t) => ({ value: t.tag, hint: `${t.count}` }));
			case 'group':
				return (groups.value ?? []).map((g) => ({
					value: quote(g.name),
					hint: g.kind === 'query' ? 'regelbasiert' : 'manuell'
				}));
			case 'type':
				return (meta.value?.deviceTypes ?? []).map((t) => ({ value: t, hint: deviceTypeLabel[t] }));
			case 'subnet':
				return (subnets.value ?? []).map((s) => ({ value: s.cidr, hint: s.name }));
			case 'state':
				return ['known', 'unknown', 'ignored'].map((v) => ({ value: v }));
			case 'is':
				return ['online', 'offline', 'new', 'known', 'unknown', 'ignored', 'randomized'].map((v) => ({
					value: v
				}));
			case 'online':
				return [{ value: 'yes' }, { value: 'no' }];
			case 'has':
				return [
					'notes',
					'cve',
					'cert',
					'health',
					'ports',
					'containers',
					'packages',
					'parent',
					'children',
					'tags',
					'http',
					'hostname'
				].map((v) => ({ value: v }));
			case 'crit':
				return ['low', 'normal', 'high', 'critical'].map((v) => ({ value: v }));
			case 'health':
				return ['up', 'down', 'degraded', 'unknown'].map((v) => ({ value: v }));
			case 'cert':
				return ['expired', 'selfsigned', 'weak'].map((v) => ({ value: v }));
			case 'source':
				return (meta.value?.scanners ?? []).map((s) => ({ value: s.id, hint: s.name }));
		}
		return [];
	}

	/** token under the caret: [start, end, text] */
	const token = $derived.by(() => {
		const text = value ?? '';
		let s = caret;
		while (s > 0 && !/\s/.test(text[s - 1])) s--;
		let e = caret;
		while (e < text.length && !/\s/.test(text[e])) e++;
		return { start: s, end: e, text: text.slice(s, caret) };
	});

	type Suggestion = { insert: string; label: string; hint?: string; final: boolean };
	const suggestions = $derived.by((): Suggestion[] => {
		const t = token.text;
		if (!t) return [];
		const neg = /^[-!]/.test(t) ? t[0] : '';
		const body = t.slice(neg.length);
		const m = /^([a-z][a-z0-9_.-]*)(>=|<=|!=|:|=|>|<)(.*)$/i.exec(body);
		if (m) {
			const [, field, op, partial] = m;
			const vals = valuesFor(field.toLowerCase());
			const p = partial.toLowerCase().replace(/^"/, '');
			return vals
				.filter((v) => v.value.toLowerCase().replace(/^"/, '').startsWith(p) && v.value !== partial)
				.slice(0, 10)
				.map((v) => ({
					insert: `${neg}${field}${op}${v.value} `,
					label: v.value,
					hint: v.hint,
					final: true
				}));
		}
		const lower = body.toLowerCase();
		if (!lower) return [];
		return fields
			.filter((f) => f.field.startsWith(lower) && f.field !== lower)
			.slice(0, 10)
			.map((f) => {
				const op = f.ops.trim().split(/\s+/)[0] || ':';
				return {
					insert: `${neg}${f.field}${op}`,
					label: `${f.field}${op}`,
					hint: f.description,
					final: false
				};
			});
	});
	const showSuggest = $derived(focused && !dismissed && suggestions.length > 0);

	function updateCaret() {
		caret = input?.selectionStart ?? (value ?? '').length;
	}

	function apply(s: Suggestion) {
		const text = value ?? '';
		const before = text.slice(0, token.start);
		const after = text.slice(token.end).replace(/^\s+/, '');
		value = before + s.insert + (s.final && after ? after : after ? ' ' + after : '');
		const pos = before.length + s.insert.length;
		active = -1;
		requestAnimationFrame(() => {
			input?.focus();
			input?.setSelectionRange(pos, pos);
			caret = pos;
		});
	}

	function submit() {
		dismissed = true;
		if (liveTimer) clearTimeout(liveTimer);
		onsubmit?.((value ?? '').trim());
	}

	function onKey(e: KeyboardEvent) {
		if (showSuggest && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
			e.preventDefault();
			const n = suggestions.length;
			active = e.key === 'ArrowDown' ? (active + 1) % n : (active - 1 + n) % n;
			return;
		}
		if (showSuggest && active >= 0 && (e.key === 'Enter' || e.key === 'Tab')) {
			e.preventDefault();
			apply(suggestions[active]);
			return;
		}
		if (showSuggest && e.key === 'Tab' && suggestions.length === 1) {
			e.preventDefault();
			apply(suggestions[0]);
			return;
		}
		if (e.key === 'Enter') {
			e.preventDefault();
			submit();
		} else if (e.key === 'Escape') {
			if (showSuggest) {
				e.preventDefault();
				dismissed = true;
			}
		}
	}

	function onInput() {
		dismissed = false;
		active = -1;
		updateCaret();
		if (live > 0) {
			if (liveTimer) clearTimeout(liveTimer);
			liveTimer = setTimeout(() => onsubmit?.((value ?? '').trim()), live);
		}
	}

	function clear() {
		value = '';
		submit();
		input?.focus();
	}

	function useExample(ex: string) {
		helpOpen = false;
		const text = (value ?? '').trim();
		value = text ? `${text} ${ex}` : ex;
		submit();
	}
</script>

<div class="relative min-w-0 {klass}">
	<label for={fid} class={showLabel ? 'mb-1 block text-[0.8125rem] font-medium text-fg' : 'sr-only'}
		>{label}</label
	>
	<div class="relative flex items-center">
		<span class="pointer-events-none absolute left-2.5 text-fg-subtle"
			><Icon name="filter" size={size === 'sm' ? 14 : 15} /></span
		>
		<input
			bind:this={input}
			id={fid}
			type="text"
			bind:value
			oninput={onInput}
			onkeydown={onKey}
			onkeyup={updateCaret}
			onclick={updateCaret}
			onfocus={() => {
				focused = true;
				updateCaret();
			}}
			onblur={() => setTimeout(() => (focused = false), 120)}
			role="combobox"
			aria-expanded={showSuggest}
			aria-controls="{uid}-sugg"
			aria-autocomplete="list"
			aria-activedescendant={showSuggest && active >= 0 ? `${uid}-s${active}` : undefined}
			aria-invalid={error ? 'true' : undefined}
			aria-describedby={error ? `${uid}-err` : undefined}
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			{placeholder}
			class="mono w-full min-w-0 rounded-md border bg-surface pr-16 pl-8 text-fg shadow-sm placeholder:font-sans placeholder:text-fg-subtle
				focus:border-accent focus:ring-2 focus:ring-focus focus:outline-none
				{error ? 'border-danger' : 'border-border hover:border-border-strong'}
				{size === 'sm' ? 'h-7 text-[0.8125rem]' : 'h-8.5 text-sm'}"
		/>
		<span class="absolute right-1 flex items-center gap-0.5" bind:this={helpBtn}>
			{#if value}
				<Button variant="ghost" size="xs" icon="x" label="Filter leeren" onclick={clear} />
			{/if}
			<Button
				variant="ghost"
				size="xs"
				icon="info"
				label="Hilfe zur Filtersprache"
				aria-expanded={helpOpen}
				onclick={() => (helpOpen = !helpOpen)}
			/>
		</span>
	</div>
	{#if showSuggest}
		<ul
			id="{uid}-sugg"
			role="listbox"
			aria-label="Vorschläge"
			class="absolute right-0 left-0 z-40 mt-1 max-h-72 overflow-auto rounded-md border border-border bg-surface py-1 shadow-lg"
		>
			{#each suggestions as s, i (s.insert)}
				<li
					id="{uid}-s{i}"
					role="option"
					aria-selected={i === active}
					class="flex cursor-pointer items-baseline gap-3 px-3 py-1 text-sm {i === active
						? 'bg-accent-soft'
						: 'hover:bg-surface-2'}"
					onmousedown={(e) => {
						e.preventDefault();
						apply(s);
					}}
				>
					<span class="mono shrink-0 {i === active ? 'text-accent' : 'text-fg'}">{s.label}</span>
					{#if s.hint}<span class="truncate text-xs text-fg-subtle">{s.hint}</span>{/if}
				</li>
			{/each}
			<li class="border-t border-border px-3 pt-1 text-[0.7rem] text-fg-subtle" role="presentation">
				↑↓ auswählen · Tab/Enter übernehmen · Esc schließen
			</li>
		</ul>
	{/if}
	{#if error}
		<p id="{uid}-err" class="mt-1 flex items-start gap-1 text-xs text-danger" role="alert">
			<Icon name="alert" size={13} class="mt-px" />{error}
		</p>
	{/if}
</div>

<Popover
	bind:open={helpOpen}
	anchor={helpBtn}
	placement="bottom-end"
	label="Filtersprache"
	class="w-[min(40rem,calc(100vw-1rem))]"
>
	<div class="border-b border-border px-4 py-3">
		<h2 class="text-sm font-semibold">Filtersprache</h2>
		<p class="mt-0.5 text-xs text-fg-muted">
			Begriffe werden mit UND verknüpft, <span class="mono">a|b</span> steht für ODER, ein vorangestelltes
			<span class="mono">-</span> verneint. Werte mit Leerzeichen in Anführungszeichen. Zeitangaben: m, h, d, w.
		</p>
	</div>
	<table class="w-full text-left text-xs">
		<thead class="sticky top-0 bg-surface-2 text-fg-muted">
			<tr>
				<th scope="col" class="px-4 py-1.5 font-medium">Feld</th>
				<th scope="col" class="px-2 py-1.5 font-medium">Beschreibung</th>
				<th scope="col" class="px-4 py-1.5 font-medium">Beispiel</th>
			</tr>
		</thead>
		<tbody>
			{#each meta.value?.queryFields ?? [] as f (f.field)}
				<tr class="border-t border-border align-top">
					<td class="px-4 py-1.5 whitespace-nowrap">
						<span class="mono text-fg">{f.field}</span>
						{#if f.ops}<span class="mono ml-1 text-fg-subtle">{f.ops}</span>{/if}
					</td>
					<td class="px-2 py-1.5 text-fg-muted">{f.description}</td>
					<td class="px-4 py-1.5">
						{#each f.example.split(' ') as ex (ex)}
							<button
								type="button"
								class="mono mr-1 mb-0.5 rounded bg-surface-3 px-1.5 py-0.5 text-fg hover:bg-accent-soft hover:text-accent"
								title="Zum Filter hinzufügen"
								onclick={() => useExample(ex)}>{ex}</button
							>
						{/each}
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</Popover>
