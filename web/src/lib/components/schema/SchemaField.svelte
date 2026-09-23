<!-- One field of a SchemaForm (dispatch by field type). -->
<script lang="ts">
	import type { SchemaField } from '$lib/api/types';
	import Checkbox from '$lib/components/ui/Checkbox.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import MultiSelect from '$lib/components/ui/MultiSelect.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import Textarea from '$lib/components/ui/Textarea.svelte';
	import Toggle from '$lib/components/ui/Toggle.svelte';
	import CredentialField from './CredentialField.svelte';
	import CronField from './CronField.svelte';
	import FileField from './FileField.svelte';
	import ListField from './ListField.svelte';
	import SecretField from './SecretField.svelte';
	import { isSecret } from './schema';

	interface Props {
		field: SchemaField;
		values: Record<string, unknown>;
		error?: string | null;
		disabled?: boolean;
		idPrefix: string;
	}

	let { field: f, values = $bindable(), error, disabled = false, idPrefix }: Props = $props();

	const id = $derived(`${idPrefix}-${f.key}`);
	const hint = $derived(f.description);
	const defaultHint = $derived.by(() => {
		if (f.default === undefined || f.default === null || f.default === '') return '';
		if (Array.isArray(f.default)) return f.default.length ? `Standard: ${f.default.join(', ')}` : '';
		if (typeof f.default === 'boolean') return '';
		return `Standard: ${f.default}`;
	});
	const fullHint = $derived(
		[hint, f.type === 'int' || f.type === 'duration' ? defaultHint : ''].filter(Boolean).join(' · ')
	);
	const inputType = $derived(
		f.validation?.format === 'url' ? 'url' : f.validation?.format === 'email' ? 'email' : 'text'
	);
	const options = $derived((f.options ?? []).map((o) => ({ value: o.value, label: o.label })));

	// typed accessors for the bound map
	function str(): string {
		const v = values[f.key];
		return typeof v === 'string' ? v : v === undefined || v === null ? '' : String(v);
	}
	function list(): string[] {
		const v = values[f.key];
		return Array.isArray(v) ? (v as string[]) : [];
	}
	function set(v: unknown) {
		values[f.key] = v;
	}
	function toggleMulti(v: string, on: boolean) {
		const cur = list();
		set(on ? [...cur, v] : cur.filter((x) => x !== v));
	}
</script>

{#if isSecret(f)}
	<SecretField
		{id}
		label={f.label}
		hint={fullHint}
		{error}
		required={f.required}
		multiline={f.multiline}
		placeholder={f.placeholder}
		{disabled}
		bind:value={() => str(), (v) => set(v)}
	/>
{:else if f.type === 'bool'}
	<div class="flex flex-col gap-1">
		<Toggle
			{id}
			label={f.label}
			description={f.description}
			{disabled}
			bind:checked={() => values[f.key] === true, (v) => set(v)}
		/>
		{#if error}<p class="text-xs text-danger" role="alert">{error}</p>{/if}
	</div>
{:else if f.type === 'int'}
	<Input
		{id}
		type="number"
		label={f.label}
		hint={fullHint}
		{error}
		required={f.required}
		placeholder={f.placeholder}
		min={f.validation?.min}
		max={f.validation?.max}
		step="1"
		{disabled}
		class="max-w-60"
		bind:value={
			() => (values[f.key] === null || values[f.key] === undefined ? '' : (values[f.key] as number)),
			(v) => set(v === '' || v === null || v === undefined ? null : Number(v))
		}
	/>
{:else if f.type === 'cron'}
	<CronField
		{id}
		label={f.label}
		hint={fullHint}
		{error}
		required={f.required}
		placeholder={f.placeholder}
		{disabled}
		bind:value={() => str(), (v) => set(v)}
	/>
{:else if f.type === 'enum' && f.multi}
	{#if options.length <= 8}
		<FormField label={f.label} hint={fullHint} {error} required={f.required} {id}>
			{#snippet children(fid, describedby)}
				<div
					id={fid}
					role="group"
					aria-describedby={describedby}
					class="flex flex-wrap gap-x-4 gap-y-1.5 pt-0.5"
				>
					{#each options as o (o.value)}
						<Checkbox
							label={o.label}
							{disabled}
							checked={list().includes(o.value)}
							onchange={(e) => toggleMulti(o.value, (e.currentTarget as HTMLInputElement).checked)}
						/>
					{/each}
				</div>
			{/snippet}
		</FormField>
	{:else}
		<MultiSelect
			{id}
			label={f.label}
			hint={fullHint}
			{error}
			required={f.required}
			{disabled}
			{options}
			value={list()}
			onchange={(v) => set(v)}
			placeholder={f.placeholder || 'Auswählen …'}
		/>
	{/if}
{:else if f.type === 'enum'}
	<Select
		{id}
		label={f.label}
		hint={fullHint}
		{error}
		required={f.required}
		{disabled}
		{options}
		placeholder={f.required && f.default ? undefined : '— keine Auswahl —'}
		class="max-w-md"
		bind:value={() => str(), (v) => set(v ?? '')}
	/>
{:else if f.type === 'string-list' || f.type === 'subnet-list'}
	<ListField
		{id}
		label={f.label}
		hint={[
			fullHint,
			f.type === 'subnet-list'
				? 'Ein Subnetz pro Zeile (CIDR, z. B. 192.168.8.0/24)'
				: 'Ein Eintrag pro Zeile'
		]
			.filter(Boolean)
			.join(' · ')}
		{error}
		required={f.required}
		placeholder={f.placeholder}
		mono={f.type === 'subnet-list'}
		{disabled}
		bind:value={() => list(), (v) => set(v)}
	/>
{:else if f.type === 'credential-ref'}
	<CredentialField
		{id}
		label={f.label}
		hint={fullHint}
		{error}
		required={f.required}
		multi={f.multi}
		types={f.credentialTypes}
		{disabled}
		bind:value={() => (values[f.key] as number | number[]) ?? (f.multi ? [] : 0), (v) => set(v)}
	/>
{:else if f.type === 'duration'}
	<Input
		{id}
		label={f.label}
		hint={fullHint || 'Dauer, z. B. 30s, 5m, 2h'}
		{error}
		required={f.required}
		placeholder={f.placeholder || 'z. B. 30s, 5m, 1h'}
		mono
		{disabled}
		class="max-w-60"
		spellcheck="false"
		bind:value={() => str(), (v) => set(v ?? '')}
	/>
{:else if f.widget === 'file'}
	<FileField
		{id}
		label={f.label}
		hint={fullHint}
		{error}
		required={f.required}
		{disabled}
		bind:value={() => str(), (v) => set(v)}
	/>
{:else if f.multiline}
	<Textarea
		{id}
		label={f.label}
		hint={fullHint}
		{error}
		required={f.required}
		placeholder={f.placeholder}
		{disabled}
		rows={4}
		bind:value={() => str(), (v) => set(v ?? '')}
	/>
{:else}
	<Input
		{id}
		type={inputType}
		label={f.label}
		hint={fullHint}
		{error}
		required={f.required}
		placeholder={f.placeholder}
		{disabled}
		maxlength={f.validation?.max}
		bind:value={() => str(), (v) => set(v === null || v === undefined ? '' : String(v))}
	/>
{/if}
