<!--
	"Beziehungen" tab: parent/child relations of the device (GET /devices/{id}/relations),
	manual edges can be added (POST /api/v1/topology/edges) and deleted.
-->
<script lang="ts">
	import { api, errorMessage } from '$lib/api';
	import type { DeviceDetail, Relation } from '$lib/api/types';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import Table, { type Column } from '$lib/components/ui/Table.svelte';
	import { locale, t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { relationKindLabel } from '$lib/utils/labels';
	import DevicePicker from './DevicePicker.svelte';
	import { LazyData, sourceName } from './util';

	interface Props {
		device: DeviceDetail;
		version: number;
		active: boolean;
		onchanged: () => void;
	}

	let { device, version, active, onchanged }: Props = $props();
	const id = $derived(device.id);
	const canEdit = $derived(auth.can('devices.edit'));

	const data = new LazyData<Relation[]>();
	$effect(() => {
		if (!active) return;
		data.ensure(
			String(version),
			async (signal) =>
				((await api.get('/api/v1/devices/{id}/relations', { path: { id }, signal })) ?? []) as Relation[]
		);
	});
	$effect(() => () => data.abort());

	type Row = Relation & { dir: 'parent' | 'child'; otherId: number; otherName: string };
	const rows = $derived<Row[]>(
		(data.data ?? [])
			.map((r): Row => {
				const isParent = r.childId === id; // the other side is our parent
				return {
					...r,
					dir: isParent ? 'parent' : 'child',
					otherId: isParent ? r.parentId : r.childId,
					otherName: isParent ? r.parentName : r.childName
				};
			})
			.sort(
				(a, b) =>
					(a.dir === b.dir ? 0 : a.dir === 'parent' ? -1 : 1) ||
					a.kind.localeCompare(b.kind) ||
					a.otherName.localeCompare(b.otherName, locale)
			)
	);

	async function remove(r: Row) {
		const ok = await confirm({
			title: t('Verbindung löschen?'),
			message: [
				t('{edge} wird entfernt.', {
					edge: `${r.parentName} → ${r.childName} (${relationKindLabel[r.kind] ?? r.kind})`
				}),
				r.source === 'manual'
					? ''
					: t('Automatisch erkannte Verbindungen kommen beim nächsten Topologie-Lauf wieder.')
			]
				.filter(Boolean)
				.join(' '),
			confirmLabel: t('Löschen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/topology/edges/{id}', { path: { id: r.id } });
			toast.success(t('Verbindung gelöscht'));
			data.invalidate();
			onchanged();
		} catch (e) {
			toast.error(e);
		}
	}

	const columns: Column<Row>[] = [
		{ key: 'dir', label: t('Richtung'), cell: dirCell }, // i18n-ignore: 'dir' is the column id
		{ key: 'other', label: t('Gerät'), cell: otherCell },
		{ key: 'kind', label: t('Art'), value: (r) => relationKindLabel[r.kind] ?? r.kind },
		{ key: 'ports', label: 'Ports', hideBelow: 'md', cell: portsCell },
		{ key: 'source', label: t('Quelle'), hideBelow: 'lg', cell: sourceCell },
		{ key: 'lastSeen', label: t('Zuletzt bestätigt'), hideBelow: 'lg', cell: seenCell },
		{ key: 'actions', label: '', align: 'right', cell: actionCell }
	];

	// ---------------------------------------------------------------- add edge
	const KINDS = ['manual', 'runs_on', 'switch_port', 'lldp', 'l3', 'wireless'];
	let addOpen = $state(false);
	let dir = $state<'parent' | 'child'>('parent');
	let other = $state<number | null>(null);
	let kind = $state('manual');
	let parentPort = $state('');
	let childPort = $state('');
	let label = $state('');
	let errors = $state<Record<string, string>>({});
	let error = $state('');
	let busy = $state(false);

	function openAdd() {
		dir = 'parent';
		other = null;
		kind = 'manual';
		parentPort = childPort = label = '';
		errors = {};
		error = '';
		addOpen = true;
	}

	async function submitAdd() {
		errors = {};
		error = '';
		if (!other) {
			errors = { other: t('Gerät auswählen') };
			return;
		}
		const parentId = dir === 'parent' ? other : id;
		const childId = dir === 'parent' ? id : other;
		busy = true;
		try {
			await api.post('/api/v1/topology/edges', {
				body: {
					parentId,
					childId,
					kind,
					parentPort: parentPort.trim(),
					childPort: childPort.trim(),
					label: label.trim()
				}
			});
			toast.success(t('Verbindung angelegt'));
			addOpen = false;
			data.invalidate();
			onchanged();
		} catch (e) {
			const msg = errorMessage(e);
			// server errors about the chosen device (German or English, as the server answers)
			if (/Gerät|selbst|device|itself/i.test(msg)) errors = { other: msg };
			else error = msg;
		} finally {
			busy = false;
		}
	}
</script>

{#snippet dirCell(r: Row)}
	<span class="inline-flex items-center gap-1.5 whitespace-nowrap text-fg-muted">
		<Icon name={r.dir === 'parent' ? 'arrow-up' : 'arrow-down'} size={13} />
		{r.dir === 'parent' ? t('Eltern') : t('Kind')}
	</span>
{/snippet}
{#snippet otherCell(r: Row)}
	<a href="/devices/{r.otherId}" class="link">{r.otherName || `#${r.otherId}`}</a>
	{#if r.label}<span class="block text-xs text-fg-subtle">{r.label}</span>{/if}
{/snippet}
{#snippet portsCell(r: Row)}
	{#if r.parentPort || r.childPort}
		<span class="mono text-xs whitespace-nowrap">{r.parentPort || '–'} → {r.childPort || '–'}</span>
	{:else}<span class="text-fg-subtle">–</span>{/if}
{/snippet}
{#snippet sourceCell(r: Row)}
	<span class="inline-flex items-center gap-1.5">
		{sourceName(r.source)}
		{#if r.protected}<Badge tone="accent" title={t('Manuell angelegt, wird von Scans nicht verändert')}
				>{t('geschützt')}</Badge
			>{/if}
	</span>
{/snippet}
{#snippet seenCell(r: Row)}<RelativeTime value={r.lastSeen} class="text-fg-muted" />{/snippet}
{#snippet actionCell(r: Row)}
	{#if canEdit && (r.source === 'manual' || r.protected)}
		<Button
			size="xs"
			variant="ghost"
			icon="trash"
			label={t('Verbindung löschen')}
			onclick={() => remove(r)}
		/>
	{/if}
{/snippet}

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-center gap-2">
		<h2 class="flex-1 text-sm font-semibold">{t('Beziehungen')}</h2>
		<Button size="sm" variant="ghost" href="/topology" iconRight="arrow-right">{t('Topologie')}</Button>
		{#if canEdit}
			<Button size="sm" variant="primary" icon="plus" onclick={openAdd}>{t('Verbindung hinzufügen')}</Button>
		{/if}
	</div>
	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton rows={4} />
	{:else}
		<Table
			{columns}
			{rows}
			key={(r) => r.id}
			dense
			caption={t('Beziehungen des Geräts')}
			loading={data.loading}
		>
			{#snippet empty()}
				<EmptyState
					compact
					icon="topology"
					title={t('Keine Beziehungen')}
					description={t(
						'Beziehungen entstehen aus SNMP/LLDP, Proxmox, Docker und dem Routing – oder manuell.'
					)}
				/>
			{/snippet}
		</Table>
		<p class="text-xs text-fg-subtle">
			{t(
				'Eltern = übergeordnetes Gerät (z. B. Switch, Hypervisor, Router); Kind = untergeordnet. Manuelle Verbindungen sind geschützt und werden von Scans nicht verändert.'
			)}
		</p>
	{/if}
</div>

<Modal
	bind:open={addOpen}
	title={t('Verbindung hinzufügen')}
	description={t('Manuelle, geschützte Kante für „{name}“.', { name: device.name || device.ip })}
	as="form"
	onsubmit={submitAdd}
	{busy}
>
	<div class="flex flex-col gap-3">
		{#if error}<Alert tone="danger">{error}</Alert>{/if}
		<Select
			label={t('Dieses Gerät ist')}
			bind:value={dir}
			options={[
				{ value: 'parent', label: t('Kind des anderen Geräts (hängt an / läuft auf)') },
				{ value: 'child', label: t('Eltern des anderen Geräts (übergeordnet)') }
			]}
		/>
		<DevicePicker
			label={dir === 'parent' ? t('Übergeordnetes Gerät') : t('Untergeordnetes Gerät')}
			bind:value={other}
			exclude={[id]}
			error={errors.other}
			required
		/>
		<Select
			label={t('Art')}
			bind:value={kind}
			options={KINDS.map((k) => ({ value: k, label: relationKindLabel[k] ?? k }))}
		/>
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<Input
				label={t('Port am Eltern-Gerät (optional)')}
				bind:value={parentPort}
				mono
				placeholder={t('z. B. Port 7')}
			/>
			<Input
				label={t('Port am Kind-Gerät (optional)')}
				bind:value={childPort}
				mono
				placeholder={t('z. B. eth0')}
			/>
		</div>
		<Input label={t('Beschriftung (optional)')} bind:value={label} maxlength={120} />
	</div>
	{#snippet footer()}
		<Button onclick={() => (addOpen = false)} disabled={busy}>{t('Abbrechen')}</Button>
		<Button type="submit" variant="primary" icon="plus" loading={busy}>{t('Anlegen')}</Button>
	{/snippet}
</Modal>
