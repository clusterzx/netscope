<!--
	Renders a form from a plugin settings schema (plugin.Schema fields). No per-plugin UI.

	<script>
		let values = $state(schemaInitial(plugin.schema.fields, plugin.config.settings));
		let errors = $state<Record<string, string>>({});
		async function save() {
			errors = validateSchema(fields, values);
			if (Object.keys(errors).length) return;
			try { await api.put(…, { body: { settings: schemaPayload(fields, values) } }); }
			catch (e) { errors = fieldErrors(e); toast.error(e); }
		}
	</script>
	<SchemaForm fields={plugin.schema.fields} bind:values {errors} idPrefix="pl-{plugin.info.id}" />

	Props
	- fields: SchemaField[]            the schema
	- values: Record<string, unknown>  bindable; must be a $state object (canonical types, see schema.ts)
	- errors: Record<string,string>    field key → message (server field errors via fieldErrors(e); keys
	                                   may carry `errorPrefix`, e.g. "settings.")
	- errorPrefix, disabled, idPrefix (unique per page), compact (tighter spacing)
	Field types: string, secret, int, bool, cron, enum (single/multi), string-list, subnet-list,
	credential-ref (single/multi, filtered by credentialTypes), duration; widget "file" uploads.
	Honours label/description/placeholder/required/default/group/advanced/visibleIf/validation.
	Layout: ungrouped fields first, then one section per group; advanced fields of a group are
	collapsed ("Erweiterte Einstellungen"), ungrouped advanced fields come last.
-->
<script lang="ts">
	import type { SchemaField as Field } from '$lib/api/types';
	import Icon from '$lib/components/ui/Icon.svelte';
	import SchemaField from './SchemaField.svelte';
	import { groupFields, isVisible } from './schema';

	interface Props {
		fields: Field[];
		values: Record<string, unknown>;
		errors?: Record<string, string>;
		errorPrefix?: string;
		disabled?: boolean;
		idPrefix?: string;
		compact?: boolean;
		class?: string;
	}

	let {
		fields,
		values = $bindable(),
		errors = {},
		errorPrefix = '',
		disabled = false,
		idPrefix = 'sf',
		compact = false,
		class: klass = ''
	}: Props = $props();

	type Section = { name: string; basic: Field[]; adv: Field[] };

	const sections = $derived.by((): Section[] => {
		const out: Section[] = [];
		let tail: Field[] = [];
		for (const g of groupFields(fields ?? [])) {
			const visible = g.fields.filter((f) => isVisible(f, values, fields));
			const basic = visible.filter((f) => !f.advanced);
			const adv = visible.filter((f) => f.advanced);
			if (g.name === '') {
				if (basic.length) out.push({ name: '', basic, adv: [] });
				tail = adv;
			} else if (basic.length || adv.length) out.push({ name: g.name, basic, adv });
		}
		if (tail.length) out.push({ name: '', basic: [], adv: tail });
		return out;
	});

	function errorOf(key: string): string | undefined {
		return errors?.[key] ?? errors?.[errorPrefix + key];
	}

	/** errors for fields not in the schema (e.g. "unbekanntes Feld") */
	const stray = $derived(
		Object.entries(errors ?? {}).filter(
			([k]) =>
				!(fields ?? []).some((f) => k === f.key || k === errorPrefix + f.key) &&
				(!errorPrefix || k.startsWith(errorPrefix))
		)
	);
	const uid = $props.id();
</script>

<div class="flex flex-col {compact ? 'gap-5' : 'gap-7'} {klass}">
	{#if (fields ?? []).length === 0}
		<p class="text-sm text-fg-subtle">Keine Einstellungen vorhanden.</p>
	{/if}
	{#each sections as s, si (si)}
		<div
			class="flex min-w-0 flex-col {compact ? 'gap-3' : 'gap-4'}"
			role={s.name ? 'group' : undefined}
			aria-labelledby={s.name ? `${uid}-g${si}` : undefined}
		>
			{#if s.name}
				<h3 id="{uid}-g{si}" class="border-b border-border pb-1.5 text-[0.8125rem] font-semibold text-fg">
					{s.name}
				</h3>
			{/if}
			{#each s.basic as f (f.key)}
				<SchemaField field={f} bind:values error={errorOf(f.key)} {disabled} {idPrefix} />
			{/each}
			{#if s.adv.length}
				<details
					class="group rounded-md border border-border"
					open={s.adv.some((f) => errorOf(f.key)) || undefined}
				>
					<summary
						class="flex cursor-pointer list-none items-center gap-1.5 px-3 py-2 text-[0.8125rem] font-medium text-fg-muted select-none hover:text-fg [&::-webkit-details-marker]:hidden"
					>
						<Icon name="chevron-right" size={14} class="transition-transform group-open:rotate-90" />
						Erweiterte Einstellungen
						<span class="text-xs font-normal text-fg-subtle">({s.adv.length})</span>
					</summary>
					<div class="flex flex-col gap-4 border-t border-border px-3 py-3">
						{#each s.adv as f (f.key)}
							<SchemaField field={f} bind:values error={errorOf(f.key)} {disabled} {idPrefix} />
						{/each}
					</div>
				</details>
			{/if}
		</div>
	{/each}
	{#if stray.length}
		<ul class="rounded-md border border-danger/35 bg-danger-soft px-3 py-2 text-sm text-danger" role="alert">
			{#each stray as [k, msg] (k)}
				<li><span class="mono">{k}</span>: {msg}</li>
			{/each}
		</ul>
	{/if}
</div>
