<!-- Subnets: CRUD /api/v1/subnets. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { Subnet } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		Card,
		EmptyState,
		ErrorState,
		Input,
		Menu,
		Modal,
		Table,
		Textarea,
		Toggle
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { subnets as subnetCatalog } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatNumber } from '$lib/utils/format';
	import { apiErrors, cidrError, ipInCidr, isIP } from './system';

	const list = new AsyncData<Subnet[]>();
	$effect(() => {
		list.run(async (signal) => (await api.get('/api/v1/subnets', { signal })) ?? []);
	});

	interface Form {
		cidr: string;
		name: string;
		interface: string;
		vlan: number | null;
		gateway: string;
		enabled: boolean;
		notes: string;
	}
	const empty = (): Form => ({
		cidr: '',
		name: '',
		interface: '',
		vlan: null,
		gateway: '',
		enabled: true,
		notes: ''
	});

	let open = $state(false);
	let editing = $state<Subnet | null>(null);
	let form = $state<Form>(empty());
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);

	function openForm(s: Subnet | null) {
		editing = s;
		form = s
			? {
					cidr: s.cidr,
					name: s.name,
					interface: s.interface,
					vlan: s.vlan ?? null,
					gateway: s.gateway,
					enabled: s.enabled,
					notes: s.notes
				}
			: empty();
		errors = {};
		general = null;
		open = true;
	}

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		const c = cidrError(form.cidr);
		if (c) e.cidr = c;
		const gw = form.gateway.trim();
		if (gw && !isIP(gw)) e.gateway = 'Ungültige IP-Adresse';
		else if (gw && !c && !ipInCidr(gw, form.cidr.trim())) e.gateway = 'Gateway liegt nicht im Subnetz';
		if (form.vlan !== null && (!Number.isInteger(form.vlan) || form.vlan < 1 || form.vlan > 4094))
			e.vlan = '1–4094';
		if (form.interface && !/^[A-Za-z0-9_.@:-]{1,32}$/.test(form.interface))
			e.interface = 'Buchstaben, Ziffern, _ . @ : - (max. 32)';
		return e;
	}

	async function save() {
		general = null;
		errors = validate();
		if (Object.keys(errors).length) return;
		saving = true;
		const body = {
			cidr: form.cidr.trim(),
			name: form.name.trim(),
			interface: form.interface.trim(),
			vlan: form.vlan ?? undefined,
			gateway: form.gateway.trim(),
			enabled: form.enabled,
			notes: form.notes
		} as Subnet;
		try {
			const saved = editing
				? await api.put('/api/v1/subnets/{id}', { path: { id: editing.id }, body })
				: await api.post('/api/v1/subnets', { body });
			toast.success(editing ? `Subnetz ${saved.cidr} gespeichert` : `Subnetz ${saved.cidr} angelegt`);
			open = false;
			list.reload();
			subnetCatalog.refresh().catch(() => {});
		} catch (e) {
			({ errors, general } = apiErrors(e, ['cidr', 'gateway', 'vlan', 'interface']));
		} finally {
			saving = false;
		}
	}

	async function toggleEnabled(s: Subnet, v: boolean) {
		try {
			await api.put('/api/v1/subnets/{id}', { path: { id: s.id }, body: { ...s, enabled: v } });
			toast.success(`${s.cidr} ${v ? 'aktiviert' : 'deaktiviert'}`);
			subnetCatalog.refresh().catch(() => {});
		} catch (e) {
			toast.error(e);
		} finally {
			list.reload();
		}
	}

	async function remove(s: Subnet) {
		const ok = await confirm({
			title: `Subnetz ${s.cidr} löschen?`,
			message:
				s.deviceCount > 0
					? `${formatNumber(s.deviceCount)} Geräte sind diesem Subnetz zugeordnet. Die Geräte bleiben erhalten, werden aber nicht mehr per Subnetz gescannt und zugeordnet.`
					: 'Das Subnetz wird nicht mehr gescannt.',
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/subnets/{id}', { path: { id: s.id } });
			toast.success(`Subnetz ${s.cidr} gelöscht`);
			list.reload();
			subnetCatalog.refresh().catch(() => {});
		} catch (e) {
			toast.error(e);
		}
	}

	const columns: Column<Subnet>[] = [
		{ key: 'cidr', label: 'Subnetz' },
		{ key: 'interface', label: 'Interface / VLAN', hideBelow: 'md' },
		{ key: 'gateway', label: 'Gateway', hideBelow: 'sm' },
		{ key: 'devices', label: 'Geräte', align: 'right', width: '6rem' },
		{ key: 'enabled', label: 'Aktiv', width: '5rem' },
		{ key: 'actions', label: '', align: 'right', width: '3rem' }
	];
</script>

<Card
	title="Subnetze"
	description="Aktive Subnetze werden von den Scannern abgedeckt; Adressen werden ihnen automatisch zugeordnet"
	icon="network"
	padding="none"
>
	{#snippet actions()}
		<Button size="sm" variant="primary" icon="plus" onclick={() => openForm(null)}>Subnetz anlegen</Button>
	{/snippet}
	{#if list.error && !list.data}
		<ErrorState error={list.error} onretry={() => list.reload()} />
	{:else}
		<Table
			{columns}
			rows={list.data ?? []}
			key={(s) => s.id}
			loading={list.loading && !list.data}
			class="rounded-none border-0"
			caption="Subnetze"
			rowClass={(s) => (s.enabled ? '' : 'opacity-70')}
		>
			{#snippet cell(s, col)}
				{#if col.key === 'cidr'}
					<span class="mono font-medium">{s.cidr}</span>
					{#if s.name}<span class="block text-xs text-fg-muted">{s.name}</span>{/if}
					{#if s.notes}<span class="hidden max-w-sm truncate text-xs text-fg-subtle md:block" title={s.notes}
							>{s.notes}</span
						>{/if}
				{:else if col.key === 'interface'}
					<span class="mono">{s.interface || '–'}</span>
					{#if s.vlan}<Badge class="ml-1">VLAN {s.vlan}</Badge>{/if}
				{:else if col.key === 'gateway'}
					<span class="mono text-fg-muted">{s.gateway || '–'}</span>
				{:else if col.key === 'devices'}
					<a href="/devices?q={encodeURIComponent('subnet:' + s.cidr)}" class="link tabular"
						>{formatNumber(s.deviceCount)}</a
					>
				{:else if col.key === 'enabled'}
					<Toggle
						size="sm"
						checked={s.enabled}
						label="{s.cidr} aktiv"
						hideLabel
						onchange={(v) => toggleEnabled(s, v)}
					/>
				{:else if col.key === 'actions'}
					<Menu
						label="Aktionen für {s.cidr}"
						items={[
							{ label: 'Bearbeiten', icon: 'edit', onclick: () => openForm(s) },
							{
								label: 'Geräte anzeigen',
								icon: 'devices',
								href: `/devices?q=${encodeURIComponent('subnet:' + s.cidr)}`
							},
							{ separator: true },
							{ label: 'Löschen', icon: 'trash', danger: true, onclick: () => remove(s) }
						]}
					/>
				{/if}
			{/snippet}
			{#snippet empty()}
				<EmptyState
					compact
					icon="network"
					title="Keine Subnetze"
					description="Ohne Subnetz scannen die Netzwerk-Scanner nichts – zuerst das lokale Netz anlegen, z. B. 192.168.1.0/24."
				/>
			{/snippet}
		</Table>
	{/if}
</Card>

<Modal
	bind:open
	title={editing ? `Subnetz ${editing.cidr} bearbeiten` : 'Subnetz anlegen'}
	size="lg"
	as="form"
	onsubmit={save}
	busy={saving}
>
	<div class="flex flex-col gap-4">
		{#if general}<Alert tone="danger" title="Speichern fehlgeschlagen">{general}</Alert>{/if}
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			<Input
				label="CIDR"
				bind:value={form.cidr}
				mono
				required
				placeholder="192.168.1.0/24"
				hint="IPv4 maximal /16"
				error={errors.cidr}
				oninput={() => delete errors.cidr}
			/>
			<Input label="Name" bind:value={form.name} placeholder="z. B. Heimnetz" maxlength={100} />
			<Input
				label="Interface"
				bind:value={form.interface}
				mono
				placeholder="eth0"
				hint="Für ARP-Scans (leer = automatisch)"
				error={errors.interface}
				oninput={() => delete errors.interface}
			/>
			<Input
				label="VLAN"
				type="number"
				min={1}
				max={4094}
				bind:value={form.vlan}
				placeholder="optional"
				error={errors.vlan}
				oninput={() => delete errors.vlan}
			/>
			<Input
				label="Gateway"
				bind:value={form.gateway}
				mono
				placeholder="192.168.1.1"
				hint="Für die L3-Topologie"
				error={errors.gateway}
				oninput={() => delete errors.gateway}
			/>
			<div class="flex items-end pb-1.5">
				<Toggle bind:checked={form.enabled} label="Aktiv" description="Von Scannern abgedeckt" />
			</div>
		</div>
		<Textarea label="Notizen" bind:value={form.notes} rows={2} />
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={saving}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="save" loading={saving}
			>{editing ? 'Speichern' : 'Anlegen'}</Button
		>
	{/snippet}
</Modal>
