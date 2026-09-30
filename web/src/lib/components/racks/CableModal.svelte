<!--
	Draws a patch cable from a port to a port chosen from a list – also in another rack:
	POST /api/v1/rack-cables {a, b, color, label}. Also completes a cable drawn by clicking
	(target preset), to pick colour and label.
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { RackSummary, RackView } from '$lib/api';
	import { Alert, Button, Input, Modal, Select } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { toast } from '$lib/stores/toast.svelte';
	import { CABLE_COLORS, cableCss, itemName, portDevice, unitsText } from './rack';

	interface Props {
		open?: boolean;
		/** the rack of the start port */
		rack: RackView;
		from: { itemId: number; port: string } | null;
		/** preset target (drawn by clicking) */
		to?: { itemId: number; port: string } | null;
		racks?: RackSummary[];
		onsaved?: () => void;
	}

	let { open = $bindable(false), rack, from, to = null, racks = [], onsaved }: Props = $props();

	let rackId = $state('');
	let itemId = $state('');
	let port = $state('');
	let color = $state('');
	let text = $state('');
	let other = $state<RackView | null>(null);
	let errors = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);
	let busy = $state(false);

	let wasOpen = false;
	$effect(() => {
		if (open && !wasOpen) {
			rackId = String(rack.id);
			itemId = to ? String(to.itemId) : '';
			port = to?.port ?? '';
			text = '';
			errors = {};
			formError = null;
		}
		wasOpen = open;
	});

	// the rack to pick the target from
	$effect(() => {
		const id = Number(rackId);
		if (!open || !id || id === rack.id) {
			other = null;
			return;
		}
		const ctrl = new AbortController();
		api
			.get('/api/v1/racks/{id}', { path: { id }, signal: ctrl.signal })
			.then((v) => (other = v))
			.catch(() => {});
		return () => ctrl.abort();
	});
	const target = $derived(Number(rackId) === rack.id ? rack : other);

	const fromItem = $derived(rack.items.find((i) => i.id === from?.itemId));
	const itemOptions = $derived(
		(target?.items ?? [])
			.filter((i) => i.ports.length)
			.map((i) => ({
				value: String(i.id),
				label: `${itemName(i)} (${t('HE {units}', { units: unitsText(target!, i) })})`
			}))
	);
	const portOptions = $derived.by(() => {
		const it = target?.items.find((i) => String(i.id) === itemId);
		if (!it) return [];
		const limit = it.kind === 'device' ? 1 : 2;
		return it.ports
			.filter((p) => !(it.id === from?.itemId && p.name === from?.port))
			.map((p) => {
				const full = p.cables.length >= limit || (it.kind === 'device' && !!p.device);
				const d = portDevice(p);
				return {
					value: p.name,
					label: `${p.name}${p.label ? ` – ${p.label}` : ''}${d ? ` (${d.name})` : ''}${full ? ` · ${t('belegt')}` : ''}`,
					disabled: full
				};
			});
	});
	$effect(() => {
		if (itemId && !itemOptions.some((o) => o.value === itemId)) itemId = '';
	});

	async function save() {
		errors = {};
		formError = null;
		if (!from) return;
		if (!itemId) errors.b = t('Element wählen');
		else if (!port) errors.b = t('Port wählen');
		if (Object.keys(errors).length) return;
		busy = true;
		try {
			await api.post('/api/v1/rack-cables', {
				body: { a: from, b: { itemId: Number(itemId), port }, color, label: text.trim() }
			});
			toast.success(t('Kabel eingetragen'));
			open = false;
			onsaved?.();
		} catch (e) {
			errors = fieldErrors(e);
			if (!Object.keys(errors).length) formError = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

<Modal
	bind:open
	title={t('Patchkabel eintragen')}
	description={fromItem && from
		? t('Von {name} Port {port}', { name: itemName(fromItem), port: from.port })
		: undefined}
	as="form"
	onsubmit={save}
	{busy}
>
	<div class="flex flex-col gap-4">
		{#if formError}<Alert tone="danger">{formError}</Alert>{/if}
		{#if errors.a}<Alert tone="danger">{errors.a}</Alert>{/if}
		{#if racks.length > 1}
			<Select
				label={t('Rack')}
				bind:value={rackId}
				options={racks.map((r) => ({ value: String(r.id), label: r.name }))}
			/>
		{/if}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<Select
				label={t('Element')}
				bind:value={itemId}
				options={itemOptions}
				placeholder={t('Element wählen …')}
				error={!port ? errors.b : undefined}
			/>
			<Select
				label={t('Port')}
				bind:value={port}
				options={portOptions}
				placeholder={t('Port wählen …')}
				disabled={!itemId}
				error={port ? errors.b : undefined}
			/>
		</div>
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<div>
				<Select
					label={t('Farbe')}
					bind:value={color}
					options={CABLE_COLORS.map((c) => ({ value: c.value, label: c.label() }))}
					error={errors.color}
				/>
				<span
					class="mt-1.5 block h-1.5 w-full rounded-full"
					style="background:{cableCss(color)}"
					aria-hidden="true"
				></span>
			</div>
			<Input
				label={t('Beschriftung')}
				bind:value={text}
				maxlength={100}
				placeholder={t('optional, z. B. Kabelnummer')}
			/>
		</div>
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>{t('Abbrechen')}</Button>
		<Button type="submit" variant="primary" icon="link" loading={busy}>{t('Kabel eintragen')}</Button>
	{/snippet}
</Modal>
