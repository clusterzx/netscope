<!--
	Free-form list of chips (tags) with suggestions. Enter, comma or Tab adds, Backspace on
	an empty input removes the last chip.
	<TagInput label="Tags" bind:value={tags} suggestions={allTags} placeholder="Tag hinzufügen" />
-->
<script lang="ts">
	import FormField from './FormField.svelte';
	import Icon from './Icon.svelte';

	interface Props {
		value?: string[];
		suggestions?: string[];
		label?: string;
		hint?: string;
		error?: string | null;
		placeholder?: string;
		required?: boolean;
		disabled?: boolean;
		/** normalise entries (default: trim) */
		normalize?: (s: string) => string;
		id?: string;
		class?: string;
		onchange?: (value: string[]) => void;
	}

	let {
		value = $bindable([]),
		suggestions = [],
		label,
		hint,
		error,
		placeholder = 'Hinzufügen …',
		required = false,
		disabled = false,
		normalize = (s: string) => s.trim(),
		id,
		class: klass = '',
		onchange
	}: Props = $props();

	let text = $state('');
	let focused = $state(false);
	let active = $state(-1);
	let input: HTMLInputElement | null = $state(null);
	const uid = $props.id();

	const matches = $derived.by(() => {
		const q = text.trim().toLowerCase();
		const have = new Set(value ?? []);
		return suggestions.filter((s) => !have.has(s) && (!q || s.toLowerCase().includes(q))).slice(0, 8);
	});
	const showList = $derived(focused && matches.length > 0 && (text.length > 0 || matches.length <= 8));

	function add(raw: string) {
		const parts = raw
			.split(',')
			.map((s) => normalize(s))
			.filter(Boolean);
		if (!parts.length) return;
		const next = [...(value ?? [])];
		for (const p of parts) if (!next.includes(p)) next.push(p);
		value = next;
		text = '';
		active = -1;
		onchange?.(next);
	}

	function remove(tag: string) {
		const next = (value ?? []).filter((t) => t !== tag);
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
		} else if (e.key === 'Enter' || e.key === ',' || (e.key === 'Tab' && text.trim())) {
			if (active >= 0 && showList) {
				e.preventDefault();
				add(matches[active]);
			} else if (text.trim()) {
				e.preventDefault();
				add(text);
			}
		} else if (e.key === 'Backspace' && !text && (value?.length ?? 0) > 0) {
			remove(value[value.length - 1]);
		} else if (e.key === 'Escape') {
			active = -1;
			focused = false;
		}
	}
</script>

<FormField {label} {hint} {error} {required} {id} class={klass}>
	{#snippet children(fid, describedby)}
		<div class="relative">
			<div
				class="flex min-h-8.5 w-full flex-wrap items-center gap-1 rounded-md border bg-surface px-1.5 py-1 shadow-sm transition-colors
					focus-within:border-accent focus-within:ring-2 focus-within:ring-focus
					{error ? 'border-danger' : 'border-border hover:border-border-strong'} {disabled ? 'opacity-60' : ''}"
			>
				{#each value ?? [] as tag (tag)}
					<span
						class="inline-flex items-center gap-0.5 rounded bg-surface-3 py-0.5 pr-0.5 pl-1.5 text-[0.8125rem] text-fg"
					>
						{tag}
						{#if !disabled}
							<button
								type="button"
								class="rounded p-0.5 text-fg-subtle hover:bg-border hover:text-fg"
								aria-label="{tag} entfernen"
								onclick={() => remove(tag)}
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
					role="combobox"
					aria-expanded={showList}
					aria-controls="{uid}-list"
					aria-autocomplete="list"
					aria-describedby={describedby}
					aria-activedescendant={active >= 0 ? `${uid}-opt-${active}` : undefined}
					placeholder={(value?.length ?? 0) ? '' : placeholder}
					onkeydown={onKey}
					onfocus={() => (focused = true)}
					onblur={() => {
						focused = false;
						if (text.trim()) add(text);
					}}
					class="h-6 min-w-24 flex-1 bg-transparent px-1 text-sm text-fg placeholder:text-fg-subtle focus:outline-none"
				/>
			</div>
			{#if showList}
				<ul
					id="{uid}-list"
					role="listbox"
					class="absolute z-40 mt-1 max-h-56 w-full overflow-auto rounded-md border border-border bg-surface py-1 shadow-lg"
				>
					{#each matches as m, i (m)}
						<li
							id="{uid}-opt-{i}"
							role="option"
							aria-selected={i === active}
							class="cursor-pointer px-3 py-1 text-sm {i === active
								? 'bg-accent-soft text-accent'
								: 'hover:bg-surface-2'}"
							onmousedown={(e) => {
								e.preventDefault();
								add(m);
							}}
						>
							{m}
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/snippet}
</FormField>
