<!--
	credential-ref: select vault entries (filtered by allowed credential types).
	Single → number (0 = none), multi → number[].
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import MultiSelect from '$lib/components/ui/MultiSelect.svelte';
	import { t } from '$lib/i18n';
	import Select from '$lib/components/ui/Select.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { credentials } from '$lib/stores/catalog.svelte';
	import { credentialCreator } from '$lib/components/credentials/creator';

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
	// inside a <CredentialCreator>: create credentials right here
	const create = credentialCreator();
	const canCreate = $derived(!!create && auth.can('credentials.manage') && !disabled);

	function createNew() {
		create?.(types, (c) => {
			value = multi ? [...((value as number[]) ?? []), c.id] : c.id;
		});
	}
	onMount(() => {
		if (canView) credentials.load().catch(() => {});
	});

	const options = $derived(
		(credentials.value ?? [])
			.filter((c) => !types.length || types.includes(c.type))
			.map((c) => ({ value: String(c.id), label: `${c.name} (${c.type})`, description: c.description }))
	);
	const typeHint = $derived(types.length ? t('Typ: {types}', { types: types.join(', ') }) : '');
	const fullHint = $derived(
		[
			hint,
			!canView
				? t('Auswahl nicht einsehbar – dafür fehlt die Berechtigung „Credentials einsehen“.')
				: credentials.value && options.length === 0
					? canCreate
						? t('Noch kein passendes Credential – mit „Neu anlegen“ gleich hier anlegen.')
						: t('Noch kein passendes Credential – unter „Credentials“ anlegen.')
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
		placeholder={credentials.loading ? t('Lädt …') : t('Credentials wählen …')}
		labelExtra={canCreate ? newButton : undefined}
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
		placeholder={credentials.loading ? t('Lädt …') : t('— kein Credential —')}
		value={single}
		onchange={(e) => (value = Number((e.currentTarget as HTMLSelectElement).value) || 0)}
		labelExtra={canCreate ? newButton : undefined}
	/>
{/if}

{#snippet newButton()}
	<button type="button" class="link inline-flex items-center gap-1 text-xs" onclick={createNew}>
		<Icon name="plus" size={12} />{t('Neu anlegen')}
	</button>
{/snippet}
