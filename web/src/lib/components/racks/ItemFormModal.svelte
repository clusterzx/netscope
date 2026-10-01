<!--
	Mounts an element in a rack or changes it: POST /api/v1/racks/{id}/items,
	PUT /api/v1/rack-items/{id} (also moves it into another rack).
	The position is entered as labelled in the rack ("from U"), whatever the numbering.
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { RackItem, RackSummary, RackView } from '$lib/api';
	import DevicePicker from '$lib/components/device/DevicePicker.svelte';
	import { Alert, Button, Checkbox, Input, Modal, Select } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { toast } from '$lib/stores/toast.svelte';
	import { KINDS, WIDTHS, hasPorts, itemName, kindLabel, type Slot } from './rack';

	interface Props {
		open?: boolean;
		rack: RackView;
		/** the item to change (null = mount a new one) */
		item?: RackItem | null;
		/** preset place for a new item */
		slot?: Slot | null;
		/** all racks (to move an item) */
		racks?: RackSummary[];
		onsaved?: () => void;
	}

	let { open = $bindable(false), rack, item = null, slot = null, racks = [], onsaved }: Props = $props();

	let kind = $state<string>('device');
	let deviceId = $state<number | null>(null);
	let label = $state('');
	let rackId = $state('');
	let from = $state<number | string>(1);
	let height = $state<number | string>(1);
	let face = $state<string>('front');
	let fullDepth = $state(false);
	let cols = $state('6');
	let col = $state('0');
	let portCount = $state<number | string>('');
	let portPrefix = $state('');
	let errors = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);
	let busy = $state(false);

	const target = $derived(racks.find((r) => String(r.id) === rackId) ?? rack);

	/** unit label of the first unit (in label order) of a place */
	function fromUnit(r: { height: number; numbering: string }, position: number, h: number): number {
		return r.numbering === 'top' ? r.height - (position + h - 1) + 1 : position;
	}
	function toPosition(r: { height: number; numbering: string }, u: number, h: number): number {
		return r.numbering === 'top' ? r.height - u - h + 2 : u;
	}

	let wasOpen = false;
	$effect(() => {
		if (open && !wasOpen) {
			const src = item ?? {
				kind: 'device',
				label: '',
				position: slot?.position ?? 1,
				height: 1,
				face: slot?.face ?? 'front',
				fullDepth: false,
				col: slot?.col ?? 0,
				cols: slot?.cols ?? 6,
				portCount: 0,
				portPrefix: ''
			};
			kind = src.kind;
			deviceId = item?.deviceId ?? null;
			label = src.label;
			rackId = String(rack.id);
			height = src.height;
			from = fromUnit(rack, src.position, src.height);
			face = src.face;
			fullDepth = src.fullDepth;
			cols = String(src.cols);
			col = String(src.col);
			portCount = src.portCount || '';
			portPrefix = src.portPrefix;
			errors = {};
			formError = null;
		}
		wasOpen = open;
	});

	const colOptions = $derived.by(() => {
		const n = Number(cols);
		if (n === 3)
			return [
				{ value: '0', label: t('Links') },
				{ value: '3', label: t('Rechts') }
			];
		if (n === 2)
			return [
				{ value: '0', label: t('Links') },
				{ value: '2', label: t('Mitte') },
				{ value: '4', label: t('Rechts') }
			];
		return [];
	});
	$effect(() => {
		if (!colOptions.length) col = '0';
		else if (!colOptions.some((o) => o.value === col)) col = colOptions[0].value;
	});

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		const h = Number(height);
		const u = Number(from);
		const pc = portCount === '' ? 0 : Number(portCount);
		if (!item && kind === 'device' && !deviceId) e.deviceId = t('Gerät wählen');
		if (!Number.isInteger(h) || h < 1 || h > target.height)
			e.height = t('1 bis {max} Höheneinheiten', { max: target.height });
		if (!Number.isInteger(u) || u < 1 || u + h - 1 > target.height)
			e.position = t('HE 1 bis {max}', { max: Math.max(1, target.height - h + 1) });
		if (!Number.isInteger(pc) || pc < 0 || pc > 128) e.portCount = t('0 bis {max} Ports', { max: 128 });
		return e;
	}

	async function save() {
		errors = validate();
		formError = null;
		if (Object.keys(errors).length) return;
		busy = true;
		const h = Number(height);
		const body = {
			kind,
			deviceId: kind === 'device' ? (deviceId ?? undefined) : undefined,
			label: label.trim(),
			position: toPosition(target, Number(from), h),
			height: h,
			face,
			fullDepth,
			col: Number(col),
			cols: Number(cols),
			portCount: portCount === '' ? 0 : Number(portCount),
			portPrefix: portPrefix.trim(),
			rackId: item && target.id !== rack.id ? target.id : undefined
		};
		try {
			if (item) await api.put('/api/v1/rack-items/{id}', { path: { id: item.id }, body });
			else await api.post('/api/v1/racks/{id}/items', { path: { id: rack.id }, body });
			toast.success(item ? t('Gespeichert') : t('Eingebaut'));
			open = false;
			onsaved?.();
		} catch (e) {
			errors = fieldErrors(e);
			if (!Object.keys(errors).length) formError = errorMessage(e);
		} finally {
			busy = false;
		}
	}

	const kindOptions = KINDS.map((k) => ({ value: k, label: kindLabel(k) }));
</script>

<Modal
	bind:open
	title={item ? t('{name} bearbeiten', { name: itemName(item) }) : t('Einbauen')}
	description={item
		? undefined
		: t('Ein Gerät aus dem Inventar oder ein passives Element (Patchfeld, Fachboden …).')}
	as="form"
	onsubmit={save}
	{busy}
	size="lg"
>
	<div class="flex flex-col gap-4">
		{#if formError}<Alert tone="danger">{formError}</Alert>{/if}
		{#if !item}
			<Select label={t('Art')} bind:value={kind} options={kindOptions} error={errors.kind} />
		{/if}
		{#if kind === 'device' && !item}
			<DevicePicker label={t('Gerät')} bind:value={deviceId} required error={errors.deviceId} />
		{/if}
		<Input
			label={t('Beschriftung')}
			bind:value={label}
			maxlength={100}
			error={errors.label}
			placeholder={kind === 'device' ? t('leer: Name des Geräts') : t('z. B. Patchfeld Büro')}
		/>
		{#if item && racks.length > 1}
			<Select
				label={t('Rack')}
				bind:value={rackId}
				options={racks.map((r) => ({ value: String(r.id), label: r.name }))}
				hint={t('Ein anderes Rack verschiebt das Element dorthin.')}
			/>
		{/if}
		<div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
			<Input
				label={t('Ab HE')}
				type="number"
				min={1}
				max={target.height}
				bind:value={from}
				error={errors.position}
			/>
			<Input
				label={t('Höhe (HE)')}
				type="number"
				min={1}
				max={target.height}
				bind:value={height}
				error={errors.height}
			/>
			<Select
				label={t('Seite')}
				bind:value={face}
				options={[
					{ value: 'front', label: t('Vorderseite') },
					{ value: 'rear', label: t('Rückseite') }
				]}
				error={errors.face}
			/>
			<div class="flex items-end pb-1.5">
				<Checkbox bind:checked={fullDepth} label={t('Volle Tiefe')} />
			</div>
		</div>
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<Select
				label={t('Breite')}
				bind:value={cols}
				options={WIDTHS.map((w) => ({ value: String(w.cols), label: w.label() }))}
				error={errors.cols}
				hint={t('Halbe und drittel Breite für Mini-PCs, Raspberry Pis …')}
			/>
			{#if colOptions.length}
				<Select label={t('Platz')} bind:value={col} options={colOptions} error={errors.col} />
			{/if}
		</div>
		{#if hasPorts(kind) || Number(portCount) > 0}
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<Input
					label={t('Anzahl Ports')}
					type="number"
					min={0}
					max={128}
					bind:value={portCount}
					error={errors.portCount}
					hint={kind === 'device'
						? t('Leer lassen, um die Ports aus SNMP und erkannten Verbindungen zu nehmen.')
						: undefined}
				/>
				<Input
					label={t('Präfix der Portnamen')}
					bind:value={portPrefix}
					maxlength={20}
					mono
					error={errors.portPrefix}
					placeholder={t('z. B. ether')}
					hint={t('Ports heißen dann {name}', {
						name: `${portPrefix.trim() || ''}1, ${portPrefix.trim() || ''}2 …`
					})}
				/>
			</div>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>{t('Abbrechen')}</Button>
		<Button type="submit" variant="primary" icon={item ? 'save' : 'plus'} loading={busy}>
			{item ? t('Speichern') : t('Einbauen')}
		</Button>
	{/snippet}
</Modal>
