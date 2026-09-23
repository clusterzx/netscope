<!-- Column selection for the device list (grouped checkboxes, catalogue order). -->
<script lang="ts">
	import Button from '$lib/components/ui/Button.svelte';
	import Popover from '$lib/components/ui/Popover.svelte';
	import { DEFAULT_COLUMNS, type DeviceColumnDef } from './columns';

	interface Props {
		catalogue: DeviceColumnDef[];
		value: string[];
		onchange: (keys: string[]) => void;
	}

	let { catalogue, value, onchange }: Props = $props();
	let open = $state(false);
	let anchor: HTMLSpanElement | null = $state(null);

	const groups = $derived.by(() => {
		const out: { name: string; cols: DeviceColumnDef[] }[] = [];
		for (const c of catalogue) {
			let g = out.find((x) => x.name === c.group);
			if (!g) out.push((g = { name: c.group, cols: [] }));
			g.cols.push(c);
		}
		return out;
	});

	function toggle(key: string, on: boolean) {
		const set = new Set(value);
		if (on) set.add(key);
		else set.delete(key);
		if (!set.has('name')) set.add('name');
		onchange(catalogue.map((c) => c.key).filter((k) => set.has(k)));
	}
</script>

<span bind:this={anchor} class="inline-flex">
	<Button
		icon="columns"
		label="Spalten wählen"
		aria-haspopup="dialog"
		aria-expanded={open}
		onclick={() => (open = !open)}
	>
		<span class="hidden sm:inline">Spalten</span>
	</Button>
</span>

<Popover
	bind:open
	{anchor}
	placement="bottom-end"
	label="Spalten wählen"
	class="w-[min(34rem,calc(100vw-1rem))]"
>
	<div class="flex items-center justify-between border-b border-border px-4 py-2.5">
		<h2 class="text-sm font-semibold">Spalten</h2>
		<button
			type="button"
			class="text-xs text-accent hover:underline"
			onclick={() => onchange([...DEFAULT_COLUMNS])}
		>
			Standard wiederherstellen
		</button>
	</div>
	<div class="grid grid-cols-1 gap-x-6 gap-y-4 px-4 py-3 sm:grid-cols-2">
		{#each groups as g (g.name)}
			<fieldset>
				<legend class="mb-1.5 text-[0.7rem] font-semibold tracking-wider text-fg-subtle uppercase"
					>{g.name}</legend
				>
				<div class="flex flex-col gap-1">
					{#each g.cols as c (c.key)}
						<label class="flex cursor-pointer items-center gap-2 text-sm">
							<input
								type="checkbox"
								class="h-4 w-4 accent-(--accent)"
								checked={value.includes(c.key)}
								disabled={c.key === 'name'}
								onchange={(e) => toggle(c.key, (e.currentTarget as HTMLInputElement).checked)}
							/>
							{c.label}
						</label>
					{/each}
				</div>
			</fieldset>
		{/each}
	</div>
</Popover>
