<!--
	Edit the manual data of a device (PATCH /api/v1/devices/{id}): display name, manual
	overrides for hostname/vendor/model/type/OS (empty = automatic), location, owner,
	criticality, state, tags and custom fields. Only changed fields are sent.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { CustomField, DeviceDetail, DeviceUpdate } from '$lib/api/types';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import TagInput from '$lib/components/ui/TagInput.svelte';
	import { customFields, meta, tags } from '$lib/stores/catalog.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { CRITICALITIES, criticalityLabel, deviceTypeName, stateLabel } from '$lib/utils/labels';

	interface Props {
		open: boolean;
		device: DeviceDetail;
		onsaved: (next: DeviceDetail) => void;
	}

	let { open = $bindable(false), device, onsaved }: Props = $props();

	const OVERRIDES = ['hostname', 'vendor', 'model', 'type', 'os'] as const;
	type Override = (typeof OVERRIDES)[number];

	let displayName = $state('');
	let manual = $state<Record<Override, string>>({ hostname: '', vendor: '', model: '', type: '', os: '' });
	let location = $state('');
	let owner = $state('');
	let criticality = $state('normal');
	let devState = $state('unknown');
	let tagList = $state<string[]>([]);
	let custom = $state<Record<string, string | number | null>>({});

	let errors = $state<Record<string, string>>({});
	let error = $state('');
	let busy = $state(false);

	const defs = $derived(customFields.value ?? []);

	function customInitial(def: CustomField, v: unknown): string | number | null {
		if (v === undefined || v === null) return def.type === 'number' ? null : '';
		if (def.type === 'bool') return v === true || v === 'true' ? 'true' : 'false';
		if (def.type === 'number') return typeof v === 'number' ? v : Number(v);
		if (def.type === 'date' && typeof v === 'string') return v.slice(0, 10);
		return String(v);
	}

	function reset() {
		const d = device;
		displayName = d.displayName ?? '';
		manual = {
			hostname: d.manual?.hostname ?? '',
			vendor: d.manual?.vendor ?? '',
			model: d.manual?.model ?? '',
			type: d.manual?.type ?? '',
			os: d.manual?.os ?? ''
		};
		location = d.location ?? '';
		owner = d.owner ?? '';
		criticality = d.criticality || 'normal';
		devState = d.state || 'unknown';
		tagList = [...(d.tags ?? [])];
		const c: Record<string, string | number | null> = {};
		for (const def of defs) c[def.key] = customInitial(def, d.custom?.[def.key]);
		custom = c;
		errors = {};
		error = '';
	}

	$effect(() => {
		if (!open) return;
		customFields.load().catch(() => {});
		meta.load().catch(() => {});
		tags.load().catch(() => {});
		untrack(reset);
	});
	// custom field definitions may arrive after opening
	$effect(() => {
		const list = defs;
		untrack(() => {
			for (const def of list)
				if (!(def.key in custom)) custom[def.key] = customInitial(def, device.custom?.[def.key]);
		});
	});

	/** value the automatic sources provide (shown as placeholder) */
	function autoValue(kind: Override): string {
		if (!device.manual?.[kind]) {
			const v = (device as unknown as Record<string, string>)[kind] ?? '';
			return kind === 'type' ? (v ? deviceTypeName(v) : '') : v;
		}
		const f = (device.facts ?? [])
			.filter((x) => x.kind === kind && x.source !== 'manual')
			.sort((a, b) => new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime())[0];
		return f ? (kind === 'type' ? deviceTypeName(f.value) : f.value) : '';
	}

	const typeOptions = $derived([
		{ value: '', label: `Automatisch${autoValue('type') ? ` (${autoValue('type')})` : ''}` },
		...(meta.value?.deviceTypes ?? []).map((t) => ({ value: t, label: deviceTypeName(t) })),
		...(manual.type && !(meta.value?.deviceTypes ?? []).includes(manual.type)
			? [{ value: manual.type, label: manual.type }]
			: [])
	]);

	function validate(): boolean {
		const e: Record<string, string> = {};
		for (const def of defs) {
			const v = custom[def.key];
			if (v === null || v === '') continue;
			if (def.type === 'number' && (typeof v !== 'number' || !isFinite(v)))
				e['cf.' + def.key] = 'Zahl erwartet';
			if (def.type === 'url' && typeof v === 'string' && !/^https?:\/\/\S+$/i.test(v.trim()))
				e['cf.' + def.key] = 'URL mit http:// oder https://';
			if (def.type === 'date' && typeof v === 'string' && !/^\d{4}-\d{2}-\d{2}$/.test(v))
				e['cf.' + def.key] = 'Datum erwartet';
		}
		for (const t of tagList) if (t.length > 64) e.tags = 'Tags höchstens 64 Zeichen';
		errors = e;
		return !Object.keys(e).length;
	}

	function customValue(def: CustomField, v: string | number | null): unknown {
		if (v === null || v === '') return null;
		if (def.type === 'bool') return v === 'true';
		if (def.type === 'number') return typeof v === 'number' ? v : Number(v);
		return typeof v === 'string' ? v.trim() : v;
	}

	function buildPatch(): DeviceUpdate {
		const d = device;
		const p: DeviceUpdate = {};
		if (displayName.trim() !== (d.displayName ?? '')) p.displayName = displayName.trim();
		for (const k of OVERRIDES) if (manual[k].trim() !== (d.manual?.[k] ?? '')) p[k] = manual[k].trim();
		if (location.trim() !== (d.location ?? '')) p.location = location.trim();
		if (owner.trim() !== (d.owner ?? '')) p.owner = owner.trim();
		if (criticality !== d.criticality) p.criticality = criticality;
		if (devState !== d.state) p.state = devState;
		const before = [...(d.tags ?? [])].sort().join('\n');
		if ([...tagList].sort().join('\n') !== before) p.tags = tagList;
		const c: Record<string, unknown> = {};
		for (const def of defs) {
			const next = customValue(def, custom[def.key] ?? null);
			const prev = d.custom?.[def.key] ?? null;
			if (JSON.stringify(next) !== JSON.stringify(prev)) c[def.key] = next;
		}
		if (Object.keys(c).length) p.custom = c;
		return p;
	}

	function mapServerError(msg: string): Record<string, string> {
		if (/^Kritikalität/.test(msg)) return { criticality: msg };
		if (/^Zustand/.test(msg)) return { state: msg };
		for (const def of defs) if (msg.startsWith(def.label + ':')) return { ['cf.' + def.key]: msg };
		return {};
	}

	async function submit() {
		if (!validate()) return;
		const body = buildPatch();
		if (!Object.keys(body).length) {
			open = false;
			return;
		}
		busy = true;
		error = '';
		try {
			const next = await api.patch('/api/v1/devices/{id}', { path: { id: device.id }, body });
			toast.success('Gerät gespeichert');
			if (body.tags) tags.refresh().catch(() => {});
			open = false;
			onsaved(next);
		} catch (e) {
			const msg = errorMessage(e);
			const fe = { ...mapServerError(msg), ...fieldErrors(e) };
			errors = fe;
			if (!Object.keys(fe).length) error = msg;
		} finally {
			busy = false;
		}
	}

	const labels: Record<Override, string> = {
		hostname: 'Hostname',
		vendor: 'Hersteller',
		model: 'Modell',
		type: 'Typ',
		os: 'Betriebssystem'
	};
</script>

<Modal
	bind:open
	title="Gerät bearbeiten"
	description="Manuelle Angaben haben Vorrang vor allen Scannern und überleben jeden Scan und Import."
	size="lg"
	as="form"
	onsubmit={submit}
	{busy}
>
	<div class="flex flex-col gap-5">
		{#if error}<Alert tone="danger">{error}</Alert>{/if}

		<fieldset class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<legend class="mb-2 text-xs font-semibold tracking-wider text-fg-subtle uppercase">Identität</legend>
			<Input
				label="Anzeigename"
				bind:value={displayName}
				maxlength={120}
				placeholder={device.hostname || device.ip || ''}
				hint="Leer = Hostname bzw. IP"
				class="sm:col-span-2"
			/>
			{#each OVERRIDES as k (k)}
				{#if k === 'type'}
					<Select
						label="{labels[k]} (manuell)"
						bind:value={manual.type}
						options={typeOptions}
						error={errors.type}
					/>
				{:else}
					<Input
						label="{labels[k]} (manuell)"
						bind:value={manual[k]}
						error={errors[k]}
						maxlength={200}
						mono={k === 'hostname'}
						placeholder={autoValue(k) ? `automatisch: ${autoValue(k)}` : 'automatisch'}
					/>
				{/if}
			{/each}
		</fieldset>

		<fieldset class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<legend class="mb-2 text-xs font-semibold tracking-wider text-fg-subtle uppercase">Einordnung</legend>
			<Input label="Standort" bind:value={location} maxlength={200} placeholder="z. B. Keller, Rack 1" />
			<Input label="Besitzer" bind:value={owner} maxlength={200} />
			<Select
				label="Kritikalität"
				bind:value={criticality}
				error={errors.criticality}
				options={CRITICALITIES.map((c) => ({ value: c, label: criticalityLabel[c] }))}
			/>
			<Select
				label="Zustand"
				bind:value={devState}
				error={errors.state}
				options={['known', 'unknown', 'ignored'].map((s) => ({ value: s, label: stateLabel[s] }))}
				hint="„Ignoriert“ blendet das Gerät in Regeln und Übersichten aus"
			/>
			<TagInput
				label="Tags"
				bind:value={tagList}
				suggestions={(tags.value ?? []).map((t) => t.tag)}
				normalize={(s) => s.trim().toLowerCase()}
				error={errors.tags}
				hint="Enter oder Komma trennt Tags"
				class="sm:col-span-2"
			/>
		</fieldset>

		{#if defs.length}
			<fieldset class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<legend class="mb-2 text-xs font-semibold tracking-wider text-fg-subtle uppercase"
					>Custom Fields</legend
				>
				{#each defs as def (def.id)}
					{#if def.type === 'bool'}
						<Select
							label={def.label}
							hint={def.description || undefined}
							value={String(custom[def.key] ?? '')}
							onchange={(e) => (custom[def.key] = (e.currentTarget as HTMLSelectElement).value)}
							error={errors['cf.' + def.key]}
							options={[
								{ value: '', label: '–' },
								{ value: 'true', label: 'Ja' },
								{ value: 'false', label: 'Nein' }
							]}
						/>
					{:else}
						<Input
							label={def.label}
							hint={def.description || undefined}
							type={def.type === 'number'
								? 'number'
								: def.type === 'date'
									? 'date'
									: def.type === 'url'
										? 'url'
										: 'text'}
							step={def.type === 'number' ? 'any' : undefined}
							bind:value={custom[def.key]}
							error={errors['cf.' + def.key]}
							placeholder={def.type === 'url' ? 'https://…' : undefined}
						/>
					{/if}
				{/each}
			</fieldset>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="save" loading={busy}>Speichern</Button>
	{/snippet}
</Modal>
