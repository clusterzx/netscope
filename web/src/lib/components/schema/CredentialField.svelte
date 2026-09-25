<!--
	credential-ref: select vault entries (filtered by allowed credential types).
	Single → number (0 = none), multi → number[].
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import MultiSelect from '$lib/components/ui/MultiSelect.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { credentials } from '$lib/stores/catalog.svelte';

	interface Props {
		value: number | number[];
		label: string;
		hint?: string;
		error?: string | null;
		required?: boolean;
		multi?: boolean;
		types?: string[];
		disabled?: boolean;
		id: string;
	}

	let {
		value = $bindable(),
		label,
		hint,
		error,
		required = false,
		multi = false,
		types = [],
		disabled = false,
		id
	}: Props = $props();

	const canView = $derived(auth.can('credentials.view'));
	onMount(() => {
		if (canView) credentials.load().catch(() => {});
	});

	const options = $derived(
		(credentials.value ?? [])
			.filter((c) => !types.length || types.includes(c.type))
			.map((c) => ({ value: String(c.id), label: `${c.name} (${c.type})`, description: c.description }))
	);
	const typeHint = $derived(types.length ? `Typ: ${types.join(', ')}` : '');
	const fullHint = $derived(
		[
			hint,
			!canView
				? 'Auswahl nicht einsehbar – dafür fehlt die Berechtigung „Credentials einsehen“.'
				: credentials.value && options.length === 0
					? 'Noch kein passendes Credential – unter „Credentials“ anlegen.'
					: typeHint
		]
			.filter(Boolean)
			.join(' · ')
	);

	let single = $state('');
	let many = $state<string[]>([]);
	$effect(() => {
		if (multi) many = ((value as number[]) ?? []).map(String);
		else single = value ? String(value) : '';
	});
</script>

{#if multi}
	<MultiSelect
		{label}
		hint={fullHint}
		{error}
		{required}
		{id}
		{disabled}
		{options}
		value={many}
		onchange={(v) => (value = v.map(Number))}
		placeholder={credentials.loading ? 'Lädt …' : 'Credentials wählen …'}
	/>
{:else}
	<Select
		{label}
		hint={fullHint}
		{error}
		{required}
		{id}
		{disabled}
		options={options.map((o) => ({ value: o.value, label: o.label }))}
		placeholder={credentials.loading ? 'Lädt …' : '— kein Credential —'}
		value={single}
		onchange={(e) => (value = Number((e.currentTarget as HTMLSelectElement).value) || 0)}
	/>
{/if}
