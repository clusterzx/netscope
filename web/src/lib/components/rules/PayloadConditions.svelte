<!--
	Payload condition rows (field, operator, value). Field suggestions come from the payload
	fields of the selected event types. Error keys: conditions.payload.<i>.field / .op.
-->
<script lang="ts">
	import type { EventSpec } from '$lib/api';
	import { Button, Input, Select } from '$lib/components/ui';
	import { OPS, fieldMessage, opLabel } from './rule';

	type Cond = { field: string; op: string; value: string };

	interface Props {
		value?: Cond[];
		/** event types whose payload fields are suggested */
		types: EventSpec[];
		errors?: Record<string, string>;
	}

	let { value = $bindable([]), types, errors = {} }: Props = $props();

	const uid = $props.id();

	/** unique payload fields of the selected types (key → type + description) */
	const fields = $derived.by(() => {
		const map = new Map<string, { type: string; description: string }>();
		for (const t of types)
			for (const f of t.payload ?? [])
				if (!map.has(f.key)) map.set(f.key, { type: f.type, description: f.description });
		return [...map.entries()].sort(([a], [b]) => a.localeCompare(b));
	});

	const opOptions = OPS.map((o) => ({
		value: o,
		label: o === 'contains' ? 'enthält' : `${o}  (${opLabel[o]})`
	}));

	function add() {
		value = [...(value ?? []), { field: '', op: '=', value: '' }];
	}

	function remove(i: number) {
		value = value.filter((_, j) => j !== i);
	}

	function typeOf(field: string): string {
		return fields.find(([k]) => k === field)?.[1].type ?? '';
	}
</script>

<div class="flex flex-col gap-2">
	<datalist id="{uid}-fields">
		{#each fields as [k, f] (k)}
			<option value={k}>{f.description || f.type}</option>
		{/each}
	</datalist>
	{#if !value?.length}
		<p class="text-sm text-fg-subtle">Keine Payload-Bedingungen – alle Events der gewählten Typen passen.</p>
	{/if}
	{#each value as c, i (i)}
		<div
			class="flex flex-wrap items-start gap-2 rounded-md border border-border p-2 sm:flex-nowrap sm:border-0 sm:p-0"
		>
			<Input
				id="{uid}-f{i}"
				label="Feld"
				bind:value={c.field}
				list="{uid}-fields"
				placeholder="z. B. cvss"
				mono
				size="sm"
				error={fieldMessage(errors[`conditions.payload.${i}.field`])}
				hint={typeOf(c.field) ? `Typ: ${typeOf(c.field)}` : undefined}
				class="w-full sm:w-auto sm:flex-[1.2]"
			/>
			<Select
				id="{uid}-o{i}"
				label="Operator"
				size="sm"
				options={opOptions}
				bind:value={c.op}
				error={fieldMessage(errors[`conditions.payload.${i}.op`])}
				class="w-40 shrink-0"
			/>
			<Input
				id="{uid}-v{i}"
				label="Wert"
				bind:value={c.value}
				size="sm"
				placeholder={typeOf(c.field) === 'number' ? 'Zahl' : 'Text'}
				class="min-w-24 flex-1"
			/>
			<Button
				size="sm"
				variant="ghost"
				icon="trash"
				label="Bedingung {i + 1} entfernen"
				onclick={() => remove(i)}
				class="mt-5.5 shrink-0"
			/>
		</div>
	{/each}
	<div>
		<Button size="sm" icon="plus" onclick={add}>Payload-Bedingung</Button>
	</div>
	{#if fields.length}
		<p class="text-xs text-fg-subtle">
			Verfügbare Felder: {fields.map(([k]) => k).join(', ')}. Zahlen werden numerisch verglichen, Text ohne
			Groß-/Kleinschreibung.
		</p>
	{/if}
</div>
