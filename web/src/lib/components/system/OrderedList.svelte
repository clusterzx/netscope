<!--
	Orderable list of strings (move up/down, remove, add with suggestions).
	<OrderedList bind:value={list} label="Hostname-Quellen" suggestions={['dns', 'mdns']} itemLabel={(v) => …} />
-->
<script lang="ts">
	import { Button, FormField } from '$lib/components/ui';

	interface Props {
		value: string[];
		label: string;
		hint?: string;
		error?: string | null;
		suggestions?: string[];
		itemLabel?: (v: string) => string;
		placeholder?: string;
		/** normalise new entries (default: trim + lowercase) */
		normalize?: (s: string) => string;
	}

	let {
		value = $bindable([]),
		label,
		hint,
		error,
		suggestions = [],
		itemLabel = (v: string) => v,
		placeholder = 'Quelle hinzufügen',
		normalize = (s: string) => s.trim().toLowerCase()
	}: Props = $props();

	const uid = $props.id();
	let text = $state('');
	let listEl: HTMLOListElement | null = $state(null);
	let announce = $state('');

	const available = $derived(suggestions.filter((s) => !value.includes(s)));

	function move(i: number, d: -1 | 1) {
		const j = i + d;
		if (j < 0 || j >= value.length) return;
		const next = [...value];
		[next[i], next[j]] = [next[j], next[i]];
		value = next;
		announce = `${itemLabel(next[j])} auf Position ${j + 1}`;
		// keep focus on the moved item's button
		requestAnimationFrame(() => {
			listEl
				?.querySelectorAll<HTMLButtonElement>(`[data-move="${d < 0 ? 'up' : 'down'}"]`)
				[j]?.focus({ preventScroll: false });
		});
	}

	function remove(i: number) {
		announce = `${itemLabel(value[i])} entfernt`;
		value = value.filter((_, k) => k !== i);
	}

	function add(raw: string) {
		const v = normalize(raw);
		if (!v || value.includes(v)) {
			text = '';
			return;
		}
		value = [...value, v];
		text = '';
		announce = `${itemLabel(v)} hinzugefügt`;
	}
</script>

<FormField {label} {hint} {error}>
	{#snippet children(fid, describedby)}
		<div class="flex flex-col gap-2">
			<ol
				bind:this={listEl}
				id={fid}
				aria-describedby={describedby}
				class="flex flex-col divide-y divide-border overflow-hidden rounded-md border border-border"
			>
				{#each value as v, i (v)}
					<li class="flex items-center gap-2 bg-surface px-2 py-1 text-sm">
						<span class="w-6 text-right text-xs text-fg-subtle tabular">{i + 1}.</span>
						<span class="min-w-0 flex-1 truncate">
							{itemLabel(v)}
							{#if itemLabel(v) !== v}<span class="mono text-xs text-fg-subtle">{v}</span>{/if}
						</span>
						<Button
							size="xs"
							variant="ghost"
							icon="arrow-up"
							label="{itemLabel(v)} nach oben"
							data-move="up"
							disabled={i === 0}
							onclick={() => move(i, -1)}
						/>
						<Button
							size="xs"
							variant="ghost"
							icon="arrow-down"
							label="{itemLabel(v)} nach unten"
							data-move="down"
							disabled={i === value.length - 1}
							onclick={() => move(i, 1)}
						/>
						<Button
							size="xs"
							variant="ghost"
							icon="x"
							label="{itemLabel(v)} entfernen"
							onclick={() => remove(i)}
						/>
					</li>
				{:else}
					<li class="px-3 py-2 text-sm text-fg-subtle">Keine Einträge</li>
				{/each}
			</ol>
			<div class="flex gap-2">
				<input
					type="text"
					bind:value={text}
					list="{uid}-sugg"
					aria-label="{label}: Eintrag hinzufügen"
					{placeholder}
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							e.preventDefault();
							add(text);
						}
					}}
					class="h-7 min-w-0 flex-1 rounded-md border border-border bg-surface px-2 text-[0.8125rem] text-fg shadow-sm placeholder:text-fg-subtle hover:border-border-strong focus:border-accent focus:ring-2 focus:ring-focus focus:outline-none"
				/>
				<datalist id="{uid}-sugg">
					{#each available as s (s)}<option value={s}>{itemLabel(s)}</option>{/each}
				</datalist>
				<Button size="sm" icon="plus" disabled={!text.trim()} onclick={() => add(text)}>Hinzufügen</Button>
			</div>
			<span class="sr-only" aria-live="polite">{announce}</span>
		</div>
	{/snippet}
</FormField>
