<!--
	Editor for a plugin scope (which subnets/devices a plugin works on) or a credential scope
	(where the credential applies; mode="credential").
	<ScopeEditor bind:value={scope} targets={p.info.targets} errors={genericErrors} idPrefix="pl-arpscan" />
	Error keys: scope, scope.subnets, scope.groups, scope.tags, scope.devices, scope.query.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import type { PluginScope } from '$lib/api';
	import QueryInput from '$lib/components/QueryInput.svelte';
	import { Alert, MultiSelect, TagInput } from '$lib/components/ui';
	import { groups, subnets, tags } from '$lib/stores/catalog.svelte';
	import DevicePicker from './DevicePicker.svelte';

	interface Props {
		value: PluginScope;
		targets?: string;
		/** texts for a plugin scope (default) or a credential scope */
		mode?: 'plugin' | 'credential';
		errors?: Record<string, string>;
		disabled?: boolean;
		idPrefix?: string;
	}

	let {
		value = $bindable(),
		targets = 'devices',
		mode = 'plugin',
		errors = {},
		disabled = false,
		idPrefix = 'scope'
	}: Props = $props();

	const cred = $derived(mode === 'credential');

	onMount(() => {
		subnets.load().catch(() => {});
		groups.load().catch(() => {});
		tags.load().catch(() => {});
	});

	const subnetOptions = $derived.by(() => {
		const list = (subnets.value ?? []).map((s) => ({
			value: s.cidr,
			label: s.cidr,
			description: [s.name, s.vlan ? `VLAN ${s.vlan}` : '', s.enabled ? '' : 'inaktiv']
				.filter(Boolean)
				.join(' · ')
		}));
		// keep stored CIDRs that are no longer configured visible
		for (const c of value.subnets ?? [])
			if (!list.some((o) => o.value === c))
				list.push({ value: c, label: c, description: 'nicht mehr konfiguriert' });
		return list;
	});

	const groupOptions = $derived(
		(groups.value ?? []).map((g) => ({
			value: String(g.id),
			label: g.name,
			description: `${g.kind === 'query' ? 'regelbasiert' : 'manuell'} · ${g.memberCount} Geräte`
		}))
	);

	const tagSuggestions = $derived((tags.value ?? []).map((t) => t.tag));

	let groupIds = $state<string[]>([]);
	$effect(() => {
		groupIds = (value.groups ?? []).map(String);
	});

	const restricted = $derived(
		(value.groups?.length ?? 0) > 0 ||
			(value.tags?.length ?? 0) > 0 ||
			(value.devices?.length ?? 0) > 0 ||
			!!value.query
	);

	function setMode(all: boolean) {
		value.allSubnets = all;
	}

	const err = (k: string) => errors[`scope.${k}`];
</script>

<div class="flex flex-col gap-5">
	{#if errors.scope}<Alert tone="danger">{errors.scope}</Alert>{/if}

	<fieldset class="flex flex-col gap-2" {disabled}>
		<legend class="mb-1 text-[0.8125rem] font-medium text-fg">{cred ? 'Netzbereich' : 'Subnetze'}</legend>
		<label class="flex cursor-pointer items-start gap-2 text-sm">
			<input
				type="radio"
				name="{idPrefix}-mode"
				class="mt-0.5 accent-(--accent)"
				checked={value.allSubnets}
				onchange={() => setMode(true)}
			/>
			<span>
				{#if cred}
					{restricted ? 'Keine Einschränkung nach Subnetz' : 'Überall'}
					<span class="block text-xs text-fg-subtle">
						{restricted
							? 'Gilt für die Geräte der Auswahl unten, egal in welchem Netz.'
							: 'Gilt für jedes Ziel, auch außerhalb der erfassten Subnetze. Spezifischere Zugangsdaten werden vorher probiert.'}
					</span>
				{:else}
					Alle aktiven Subnetze
					<span class="block text-xs text-fg-subtle">Neue Subnetze werden automatisch mit abgedeckt.</span>
				{/if}
			</span>
		</label>
		<label class="flex cursor-pointer items-start gap-2 text-sm">
			<input
				type="radio"
				name="{idPrefix}-mode"
				class="mt-0.5 accent-(--accent)"
				checked={!value.allSubnets}
				onchange={() => setMode(false)}
			/>
			<span>
				{cred ? 'Nur in ausgewählten Subnetzen' : 'Nur ausgewählte Subnetze'}
				<span class="block text-xs text-fg-subtle">
					{#if cred}
						{restricted
							? 'Nur Geräte der Auswahl unten, die in diesen Subnetzen liegen.'
							: 'Für alle Ziele mit einer Adresse in diesen Subnetzen. Ohne Auswahl: überall.'}
					{:else}
						{restricted
							? 'Leer lassen, um nur über die Geräteauswahl unten zu arbeiten.'
							: 'Ohne Auswahl und ohne Geräteauswahl gelten alle aktiven Subnetze.'}
					{/if}
				</span>
			</span>
		</label>
		{#if !value.allSubnets}
			<MultiSelect
				id="{idPrefix}-subnets"
				label="Ausgewählte Subnetze"
				options={subnetOptions}
				bind:value={value.subnets}
				placeholder="Subnetze wählen …"
				error={err('subnets')}
				class="mt-1 sm:ml-6"
				{disabled}
			/>
		{:else if err('subnets')}
			<p class="text-xs text-danger">{err('subnets')}</p>
		{/if}
	</fieldset>

	<fieldset class="flex flex-col gap-3" {disabled}>
		<legend class="text-[0.8125rem] font-medium text-fg"
			>{cred ? 'Nur für bestimmte Geräte' : 'Geräte einschränken'}
			<span class="font-normal text-fg-subtle">(optional)</span></legend
		>
		<p class="-mt-1 text-xs text-fg-subtle">
			{#if cred}
				Ist hier etwas gesetzt, gilt das Credential nur für diese Geräte – und wird dort vor allgemeineren
				Zugangsdaten probiert (einzeln zugewiesene Geräte zuerst). Gruppen, Tags und einzelne Geräte werden
				zusammengefasst; ein Filter schränkt diese Menge weiter ein (allein verwendet: alle passenden Geräte).
			{:else}
				{targets === 'subnets'
					? 'Ist hier etwas gesetzt, scannt das Plugin nur noch die IP-Adressen dieser Geräte statt ganzer Subnetze.'
					: 'Ist hier etwas gesetzt, arbeitet das Plugin nur auf diesen Geräten.'}
				Gruppen, Tags und einzelne Geräte werden zusammengefasst; ein Filter schränkt diese Menge weiter ein (allein
				verwendet: alle passenden Geräte). Ignorierte Geräte werden nur bei expliziter Auswahl berücksichtigt.
			{/if}
		</p>
		<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
			<MultiSelect
				id="{idPrefix}-groups"
				label="Gruppen"
				options={groupOptions}
				bind:value={groupIds}
				onchange={(v) => (value.groups = v.map(Number))}
				placeholder={groupOptions.length ? 'Gruppen wählen …' : 'Keine Gruppen angelegt'}
				error={err('groups')}
				{disabled}
			/>
			<TagInput
				id="{idPrefix}-tags"
				label="Tags"
				bind:value={value.tags}
				suggestions={tagSuggestions}
				normalize={(s) => s.trim().toLowerCase()}
				placeholder="Tag hinzufügen …"
				error={err('tags')}
				{disabled}
			/>
		</div>
		<DevicePicker
			id="{idPrefix}-devices"
			label="Einzelne Geräte"
			bind:value={value.devices}
			error={err('devices')}
			{disabled}
		/>
		<QueryInput
			id="{idPrefix}-query"
			label="Filter (Abfragesprache)"
			showLabel
			bind:value={value.query}
			error={err('query')}
			placeholder="z. B. os:linux tag:server"
		/>
	</fieldset>
</div>
