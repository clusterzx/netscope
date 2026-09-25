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
					a.otherName.localeCompare(b.otherName, 'de')
			)
	);

	async function remove(r: Row) {
		const ok = await confirm({
			title: 'Verbindung löschen?',
			message: `${r.parentName} → ${r.childName} (${relationKindLabel[r.kind] ?? r.kind}) wird entfernt.${
				r.source === 'manual'
					? ''
					: ' Automatisch erkannte Verbindungen kommen beim nächsten Topologie-Lauf wieder.'
			}`,
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/topology/edges/{id}', { path: { id: r.id } });
			toast.success('Verbindung gelöscht');
			data.invalidate();
			onchanged();
		} catch (e) {
			toast.error(e);
		}
	}

	const columns: Column<Row>[] = [
		{ key: 'dir', label: 'Richtung', cell: dirCell },
		{ key: 'other', label: 'Gerät', cell: otherCell },
		{ key: 'kind', label: 'Art', value: (r) => relationKindLabel[r.kind] ?? r.kind },
		{ key: 'ports', label: 'Ports', hideBelow: 'md', cell: portsCell },
		{ key: 'source', label: 'Quelle', hideBelow: 'lg', cell: sourceCell },
		{ key: 'lastSeen', label: 'Zuletzt bestätigt', hideBelow: 'lg', cell: seenCell },
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
			errors = { other: 'Gerät auswählen' };
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
			toast.success('Verbindung angelegt');
			addOpen = false;
			data.invalidate();
			onchanged();
		} catch (e) {
			const msg = errorMessage(e);
			if (/Gerät|selbst/.test(msg)) errors = { other: msg };
			else error = msg;
		} finally {
			busy = false;
		}
	}
</script>

{#snippet dirCell(r: Row)}
	<span class="inline-flex items-center gap-1.5 whitespace-nowrap text-fg-muted">
		<Icon name={r.dir === 'parent' ? 'arrow-up' : 'arrow-down'} size={13} />
		{r.dir === 'parent' ? 'Eltern' : 'Kind'}
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
		{#if r.protected}<Badge tone="accent" title="Manuell angelegt, wird von Scans nicht verändert"
				>geschützt</Badge
			>{/if}
	</span>
{/snippet}
{#snippet seenCell(r: Row)}<RelativeTime value={r.lastSeen} class="text-fg-muted" />{/snippet}
{#snippet actionCell(r: Row)}
	{#if canEdit && (r.source === 'manual' || r.protected)}
		<Button size="xs" variant="ghost" icon="trash" label="Verbindung löschen" onclick={() => remove(r)} />
	{/if}
{/snippet}

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-center gap-2">
		<h2 class="flex-1 text-sm font-semibold">Beziehungen</h2>
		<Button size="sm" variant="ghost" href="/topology" iconRight="arrow-right">Topologie</Button>
		{#if canEdit}
			<Button size="sm" variant="primary" icon="plus" onclick={openAdd}>Verbindung hinzufügen</Button>
		{/if}
	</div>
	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton rows={4} />
	{:else}
		<Table {columns} {rows} key={(r) => r.id} dense caption="Beziehungen des Geräts" loading={data.loading}>
			{#snippet empty()}
				<EmptyState
					compact
					icon="topology"
					title="Keine Beziehungen"
					description="Beziehungen entstehen aus SNMP/LLDP, Proxmox, Docker und dem Routing – oder manuell."
				/>
			{/snippet}
		</Table>
		<p class="text-xs text-fg-subtle">
			Eltern = übergeordnetes Gerät (z. B. Switch, Hypervisor, Router); Kind = untergeordnet. Manuelle
			Verbindungen sind geschützt und werden von Scans nicht verändert.
		</p>
	{/if}
</div>

<Modal
	bind:open={addOpen}
	title="Verbindung hinzufügen"
	description="Manuelle, geschützte Kante für „{device.name || device.ip}“."
	as="form"
	onsubmit={submitAdd}
	{busy}
>
	<div class="flex flex-col gap-3">
		{#if error}<Alert tone="danger">{error}</Alert>{/if}
		<Select
			label="Dieses Gerät ist"
			bind:value={dir}
			options={[
				{ value: 'parent', label: 'Kind des anderen Geräts (hängt an / läuft auf)' },
				{ value: 'child', label: 'Eltern des anderen Geräts (übergeordnet)' }
			]}
		/>
		<DevicePicker
			label={dir === 'parent' ? 'Übergeordnetes Gerät' : 'Untergeordnetes Gerät'}
			bind:value={other}
			exclude={[id]}
			error={errors.other}
			required
		/>
		<Select
			label="Art"
			bind:value={kind}
			options={KINDS.map((k) => ({ value: k, label: relationKindLabel[k] ?? k }))}
		/>
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<Input
				label="Port am Eltern-Gerät (optional)"
				bind:value={parentPort}
				mono
				placeholder="z. B. Port 7"
			/>
			<Input label="Port am Kind-Gerät (optional)" bind:value={childPort} mono placeholder="z. B. eth0" />
		</div>
		<Input label="Beschriftung (optional)" bind:value={label} maxlength={120} />
	</div>
	{#snippet footer()}
		<Button onclick={() => (addOpen = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="plus" loading={busy}>Anlegen</Button>
	{/snippet}
</Modal>
