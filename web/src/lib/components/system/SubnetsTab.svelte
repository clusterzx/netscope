<!--
	Subnets: CRUD /api/v1/subnets. A subnet is reached directly (attached, ARP), through a
	router (layer 3 only) or through NetScope's own WireGuard tunnel (credential of type
	wireguard; a new configuration is stored as credential before the subnet is saved).
-->
<script lang="ts">
	import { api, fieldErrors } from '$lib/api';
	import type { Credential, Subnet, SubnetAccess, SubnetInput, TunnelOverview } from '$lib/api';
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
	import { auth } from '$lib/stores/auth.svelte';
	import { credentials as credentialCatalog, subnets as subnetCatalog } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatNumber } from '$lib/utils/format';
	import { apiErrors, cidrError, ipInCidr, isIP } from './system';
	import TunnelSection from './TunnelSection.svelte';
	import TunnelState from './TunnelState.svelte';

	const canManage = $derived(auth.can('network.manage'));

	const list = new AsyncData<Subnet[]>();
	$effect(() => {
		list.run(async (signal) => (await api.get('/api/v1/subnets', { signal })) ?? []);
	});

	// tunnel availability and live state changes
	let tunnelInfo = $state<TunnelOverview | null>(null);
	async function loadTunnels() {
		try {
			tunnelInfo = await api.get('/api/v1/tunnels');
		} catch {
			tunnelInfo = null;
		}
	}
	$effect(() => {
		loadTunnels();
		const off = live.on('system', (m) => {
			if (m.type === 'tunnel') list.reload();
		});
		// handshake times change without state changes
		const timer = setInterval(() => {
			if (list.data?.some((s) => s.access === 'wireguard')) list.reload();
		}, 30_000);
		return () => {
			off();
			clearInterval(timer);
		};
	});
	const wgCreds = $derived(
		((credentialCatalog.value ?? []) as Credential[]).filter((c) => c.type === 'wireguard')
	);

	const accessOptions: { value: SubnetAccess; label: string; description: string }[] = [
		{
			value: 'direct',
			label: 'Direkt angeschlossen',
			description: 'NetScope hängt selbst in diesem Netz – ARP-Scans und MAC-Adressen.'
		},
		{
			value: 'routed',
			label: 'Über einen Router',
			description:
				'Erreichbar über ein Gateway, z. B. ein anderes VLAN. Kein ARP; Geräte werden über die IP erkannt.'
		},
		{
			value: 'wireguard',
			label: 'Über WireGuard-Tunnel',
			description: 'NetScope baut selbst einen Tunnel in ein entferntes Netz auf, z. B. ins Rechenzentrum.'
		}
	];

	interface Form {
		cidr: string;
		name: string;
		access: SubnetAccess;
		interface: string;
		vlan: number | null;
		gateway: string;
		enabled: boolean;
		notes: string;
		tunnelMode: 'existing' | 'new';
		tunnelCredentialId: number | null;
		tunnelName: string;
		tunnelConfig: string;
	}
	const empty = (): Form => ({
		cidr: '',
		name: '',
		access: 'direct',
		interface: '',
		vlan: null,
		gateway: '',
		enabled: true,
		notes: '',
		tunnelMode: 'new',
		tunnelCredentialId: null,
		tunnelName: '',
		tunnelConfig: ''
	});

	let open = $state(false);
	let editing = $state<Subnet | null>(null);
	let form = $state<Form>(empty());
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);

	function openForm(s: Subnet | null) {
		editing = s;
		if (auth.can('credentials.view')) credentialCatalog.load().catch(() => {});
		form = s
			? {
					...empty(),
					cidr: s.cidr,
					name: s.name,
					access: (s.access || 'direct') as SubnetAccess,
					interface: s.interface,
					vlan: s.vlan ?? null,
					gateway: s.gateway,
					enabled: s.enabled,
					notes: s.notes,
					tunnelMode: s.tunnelCredentialId ? 'existing' : 'new',
					tunnelCredentialId: s.tunnelCredentialId ?? null
				}
			: empty();
		if ((!s && wgCreds.length) || !auth.can('credentials.manage')) form.tunnelMode = 'existing';
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
		if (form.access === 'direct') {
			if (form.vlan !== null && (!Number.isInteger(form.vlan) || form.vlan < 1 || form.vlan > 4094))
				e.vlan = '1–4094';
			if (form.interface && !/^[A-Za-z0-9_.@:-]{1,32}$/.test(form.interface))
				e.interface = 'Buchstaben, Ziffern, _ . @ : - (max. 32)';
		}
		if (form.access === 'wireguard') {
			if (form.tunnelMode === 'existing' && !form.tunnelCredentialId) e.tunnelCredentialId = 'Tunnel wählen';
			if (form.tunnelMode === 'new') {
				if (!form.tunnelName.trim()) e.name = 'Pflichtfeld';
				if (!form.tunnelConfig.trim()) e.config = 'Konfiguration einfügen oder aus Datei laden';
			}
		}
		return e;
	}

	/** Stores a new tunnel configuration and switches the form to it. */
	async function createTunnel(): Promise<boolean> {
		try {
			const c = await api.post('/api/v1/credentials', {
				body: {
					name: form.tunnelName.trim(),
					type: 'wireguard',
					description: `Tunnel für ${form.cidr.trim()}`,
					values: { config: form.tunnelConfig }
				}
			});
			form.tunnelCredentialId = c.id;
			form.tunnelMode = 'existing';
			form.tunnelConfig = '';
			credentialCatalog.refresh().catch(() => {});
			return true;
		} catch (e) {
			const f = fieldErrors(e);
			errors = { ...(f.config ? { config: f.config } : {}), ...(f.name ? { name: f.name } : {}) };
			if (!Object.keys(errors).length) ({ general } = apiErrors(e, []));
			return false;
		}
	}

	async function save() {
		general = null;
		errors = validate();
		if (Object.keys(errors).length) return;
		saving = true;
		try {
			if (form.access === 'wireguard' && form.tunnelMode === 'new' && !(await createTunnel())) return;
			const body: SubnetInput = {
				cidr: form.cidr.trim(),
				name: form.name.trim(),
				access: form.access,
				interface: form.access === 'direct' ? form.interface.trim() : '',
				vlan: form.access === 'direct' ? (form.vlan ?? undefined) : undefined,
				gateway: form.gateway.trim(),
				enabled: form.enabled,
				notes: form.notes,
				tunnelCredentialId: form.access === 'wireguard' ? (form.tunnelCredentialId ?? undefined) : undefined
			} as SubnetInput;
			const saved = editing
				? await api.put('/api/v1/subnets/{id}', { path: { id: editing.id }, body })
				: await api.post('/api/v1/subnets', { body });
			toast.success(editing ? `Subnetz ${saved.cidr} gespeichert` : `Subnetz ${saved.cidr} angelegt`);
			open = false;
			list.reload();
			subnetCatalog.refresh().catch(() => {});
			if (form.access === 'wireguard') setTimeout(() => list.reload(), 3000);
		} catch (e) {
			({ errors, general } = apiErrors(e, [
				'cidr',
				'gateway',
				'vlan',
				'interface',
				'access',
				'tunnelCredentialId'
			]));
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
				(s.deviceCount > 0
					? `${formatNumber(s.deviceCount)} Geräte sind diesem Subnetz zugeordnet. Die Geräte bleiben erhalten, werden aber nicht mehr per Subnetz gescannt und zugeordnet.`
					: 'Das Subnetz wird nicht mehr gescannt.') +
				(s.access === 'wireguard'
					? ' Nutzt kein anderes Subnetz den Tunnel, wird er abgebaut; die Konfiguration bleibt unter Credentials gespeichert.'
					: ''),
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

	const tunnelName = (id: number | undefined) => wgCreds.find((c) => c.id === id)?.name;

	const columns: Column<Subnet>[] = [
		{ key: 'cidr', label: 'Subnetz' },
		{ key: 'access', label: 'Erreichbarkeit', hideBelow: 'md' },
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
		{#if canManage}
			<Button size="sm" variant="primary" icon="plus" onclick={() => openForm(null)}>Subnetz anlegen</Button>
		{/if}
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
				{:else if col.key === 'access'}
					{#if s.access === 'wireguard'}
						<div class="flex flex-col items-start gap-0.5">
							{#if s.enabled}<TunnelState status={s.tunnel} />{:else}<Badge>Tunnel</Badge>{/if}
							<span class="text-xs text-fg-subtle"
								>{s.tunnel?.name ?? tunnelName(s.tunnelCredentialId) ?? 'WireGuard'}{#if s.tunnel?.endpoint}
									· <span class="mono">{s.tunnel.endpoint}</span>{/if}</span
							>
							{#if s.tunnel?.error}<span class="max-w-xs text-xs text-danger">{s.tunnel.error}</span>{/if}
						</div>
					{:else if s.access === 'routed'}
						<Badge tone="info">Über Router</Badge>
					{:else}
						<span class="mono">{s.interface || 'direkt'}</span>
						{#if s.vlan}<Badge class="ml-1">VLAN {s.vlan}</Badge>{/if}
					{/if}
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
						disabled={!canManage}
						onchange={(v) => toggleEnabled(s, v)}
					/>
				{:else if col.key === 'actions'}
					<Menu
						label="Aktionen für {s.cidr}"
						items={canManage
							? [
									{ label: 'Bearbeiten', icon: 'edit', onclick: () => openForm(s) },
									{
										label: 'Geräte anzeigen',
										icon: 'devices',
										href: `/devices?q=${encodeURIComponent('subnet:' + s.cidr)}`
									},
									{ separator: true },
									{ label: 'Löschen', icon: 'trash', danger: true, onclick: () => remove(s) }
								]
							: [
									{
										label: 'Geräte anzeigen',
										icon: 'devices',
										href: `/devices?q=${encodeURIComponent('subnet:' + s.cidr)}`
									}
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
		{#if !canManage}
			<p class="border-t border-border px-4 py-2 text-xs text-fg-subtle">
				Nur lesen – dafür fehlt die Berechtigung „Subnetze und Tunnel verwalten“.
			</p>
		{/if}
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
		</div>

		<fieldset class="flex flex-col gap-2">
			<legend class="mb-1 text-[0.8125rem] font-medium text-fg">Erreichbarkeit</legend>
			<div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
				{#each accessOptions as o (o.value)}
					{@const blocked = o.value === 'wireguard' && tunnelInfo !== null && !tunnelInfo.available}
					<label
						class="flex items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors
							{blocked ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'}
							{form.access === o.value
							? 'border-accent bg-accent-soft'
							: 'border-border hover:border-border-strong hover:bg-surface-2'}"
						title={blocked ? tunnelInfo?.reason : undefined}
					>
						<input
							type="radio"
							name="subnet-access"
							value={o.value}
							bind:group={form.access}
							disabled={blocked && form.access !== 'wireguard'}
							class="mt-1 accent-(--accent)"
						/>
						<span class="min-w-0">
							<span class="block text-sm font-medium">{o.label}</span>
							<span class="block text-xs text-fg-muted">{o.description}</span>
						</span>
					</label>
				{/each}
			</div>
			{#if errors.access}<p class="text-xs text-danger">{errors.access}</p>{/if}
			{#if tunnelInfo && !tunnelInfo.available}
				<p class="text-xs text-fg-subtle">
					WireGuard-Tunnel sind auf diesem Host nicht verfügbar: {tunnelInfo.reason}
				</p>
			{/if}
		</fieldset>

		{#if form.access === 'wireguard'}
			<TunnelSection
				bind:mode={form.tunnelMode}
				bind:credentialId={form.tunnelCredentialId}
				bind:name={form.tunnelName}
				bind:config={form.tunnelConfig}
				cidr={form.cidr}
				tunnels={wgCreds}
				status={editing?.tunnel}
				{errors}
			/>
		{/if}

		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			{#if form.access === 'direct'}
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
			{/if}
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
