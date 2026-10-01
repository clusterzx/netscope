<!--
	Creates or changes a rack: POST /api/v1/racks, PUT /api/v1/racks/{id}.
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { Rack } from '$lib/api';
	import { Alert, Button, Input, Modal, Select, Textarea } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { toast } from '$lib/stores/toast.svelte';

	interface Props {
		open?: boolean;
		/** the rack to change (null = new rack) */
		rack?: Rack | null;
		onsaved?: (r: Rack) => void;
	}

	let { open = $bindable(false), rack = null, onsaved }: Props = $props();

	let name = $state('');
	let location = $state('');
	let width = $state('19');
	let height = $state<number | string>(42);
	let numbering = $state('bottom');
	let notes = $state('');
	let errors = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);
	let busy = $state(false);

	let wasOpen = false;
	$effect(() => {
		if (open && !wasOpen) {
			name = rack?.name ?? '';
			location = rack?.location ?? '';
			width = rack?.width ?? '19';
			height = rack?.height ?? 42;
			numbering = rack?.numbering ?? 'bottom';
			notes = rack?.notes ?? '';
			errors = {};
			formError = null;
		}
		wasOpen = open;
	});

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		const h = Number(height);
		if (!name.trim()) e.name = t('Name fehlt');
		if (!Number.isInteger(h) || h < 1 || h > 60) e.height = t('1 bis {max} Höheneinheiten', { max: 60 });
		return e;
	}

	async function save() {
		errors = validate();
		formError = null;
		if (Object.keys(errors).length) return;
		busy = true;
		const body = {
			name: name.trim(),
			location: location.trim(),
			width,
			height: Number(height),
			numbering,
			notes
		};
		try {
			const r = rack
				? await api.put('/api/v1/racks/{id}', { path: { id: rack.id }, body })
				: await api.post('/api/v1/racks', { body });
			toast.success(rack ? t('Rack gespeichert') : t('Rack angelegt'));
			open = false;
			onsaved?.(r);
		} catch (e) {
			errors = fieldErrors(e);
			if (!Object.keys(errors).length) formError = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

<Modal bind:open title={rack ? t('Rack bearbeiten') : t('Rack anlegen')} as="form" onsubmit={save} {busy}>
	<div class="flex flex-col gap-4">
		{#if formError}<Alert tone="danger">{formError}</Alert>{/if}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<Input
				label={t('Name')}
				bind:value={name}
				required
				maxlength={100}
				error={errors.name}
				placeholder={t('z. B. Keller')}
			/>
			<Input
				label={t('Standort')}
				bind:value={location}
				maxlength={200}
				error={errors.location}
				placeholder={t('z. B. Hauswirtschaftsraum')}
			/>
		</div>
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
			<Select
				label={t('Breite')}
				bind:value={width}
				options={[
					{ value: '19', label: '19"' },
					{ value: '10', label: '10"' }
				]}
				error={errors.width}
			/>
			<Input
				label={t('Höheneinheiten')}
				type="number"
				min={1}
				max={60}
				bind:value={height}
				error={errors.height}
				hint={t('z. B. 42, 24 oder 12')}
			/>
			<Select
				label={t('Zählung')}
				bind:value={numbering}
				options={[
					{ value: 'bottom', label: t('HE 1 unten') },
					{ value: 'top', label: t('HE 1 oben') }
				]}
				error={errors.numbering}
			/>
		</div>
		<Textarea label={t('Notizen')} bind:value={notes} rows={3} error={errors.notes} />
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>{t('Abbrechen')}</Button>
		<Button type="submit" variant="primary" icon="save" loading={busy}>
			{rack ? t('Speichern') : t('Rack anlegen')}
		</Button>
	{/snippet}
</Modal>
