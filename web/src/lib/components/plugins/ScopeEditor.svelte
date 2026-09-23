<!--
	Editor for a plugin scope (which subnets/devices a plugin works on).
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
		targets: string;
		errors?: Record<string, string>;
		disabled?: boolean;
		idPrefix?: string;
	}

	let { value = $bindable(), targets, errors = {}, disabled = false, idPrefix = 'scope' }: Props = $props();

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
		<legend class="mb-1 text-[0.8125rem] font-medium text-fg">Subnetze</legend>
		<label class="flex cursor-pointer items-start gap-2 text-sm">
			<input
				type="radio"
				name="{idPrefix}-mode"
				class="mt-0.5 accent-(--accent)"
				checked={value.allSubnets}
				onchange={() => setMode(true)}
			/>
			<span>
				Alle aktiven Subnetze
				<span class="block text-xs text-fg-subtle">Neue Subnetze werden automatisch mit abgedeckt.</span>
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
				Nur ausgewählte Subnetze
				<span class="block text-xs text-fg-subtle">
					{restricted
						? 'Leer lassen, um nur über die Geräteauswahl unten zu arbeiten.'
						: 'Ohne Auswahl und ohne Geräteauswahl gelten alle aktiven Subnetze.'}
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
			>Geräte einschränken <span class="font-normal text-fg-subtle">(optional)</span></legend
		>
		<p class="-mt-1 text-xs text-fg-subtle">
			{targets === 'subnets'
				? 'Ist hier etwas gesetzt, scannt das Plugin nur noch die IP-Adressen dieser Geräte statt ganzer Subnetze.'
				: 'Ist hier etwas gesetzt, arbeitet das Plugin nur auf diesen Geräten.'}
			Gruppen, Tags und einzelne Geräte werden zusammengefasst; ein Filter schränkt diese Menge weiter ein (allein
			verwendet: alle passenden Geräte). Ignorierte Geräte werden nur bei expliziter Auswahl berücksichtigt.
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
