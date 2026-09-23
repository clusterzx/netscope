<!--
  Creates a manual (protected) topology edge: POST /api/v1/topology/edges
  {parentId, childId, kind, parentPort, childPort, label} → 201 {id}.
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { GraphNode } from '$lib/api';
	import { Alert, Button, Input, Modal, Select } from '$lib/components/ui';
	import { toast } from '$lib/stores/toast.svelte';
	import { label, relationKindLabel } from '$lib/utils/labels';

	interface Props {
		open?: boolean;
		/** device nodes that can be connected */
		devices: GraphNode[];
		/** preselected node ids (d<id>) */
		parent?: string;
		child?: string;
		onsaved?: (id: number) => void;
	}

	let { open = $bindable(false), devices, parent = '', child = '', onsaved }: Props = $props();

	const KINDS = ['manual', 'switch_port', 'lldp', 'wireless', 'l3', 'runs_on'];
	const KIND_HINT: Record<string, string> = {
		manual: 'Allgemeine Verbindung ohne besondere Bedeutung.',
		switch_port: 'Das untergeordnete Gerät hängt an einem Port des Switches.',
		lldp: 'Direkte Nachbarn laut LLDP/CDP.',
		wireless: 'Das untergeordnete Gerät ist mit dem Access Point verbunden.',
		l3: 'Das untergeordnete Gerät wird über das übergeordnete Gerät (Gateway) geroutet.',
		runs_on: 'VM oder Container läuft auf dem übergeordneten Host.'
	};

	let parentId = $state('');
	let childId = $state('');
	let kind = $state('manual');
	let parentPort = $state('');
	let childPort = $state('');
	let text = $state('');
	let errors = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);
	let busy = $state(false);

	// reset the form whenever the dialog opens
	let wasOpen = false;
	$effect(() => {
		if (open && !wasOpen) {
			parentId = parent;
			childId = child;
			kind = 'manual';
			parentPort = childPort = text = '';
			errors = {};
			formError = null;
		}
		wasOpen = open;
	});

	const options = $derived(
		[...devices]
			.sort((a, b) => a.label.localeCompare(b.label, 'de', { numeric: true }))
			.map((d) => ({ value: d.id, label: d.ip && d.ip !== d.label ? `${d.label} (${d.ip})` : d.label }))
	);
	const kindOptions = KINDS.map((k) => ({ value: k, label: label(relationKindLabel, k) }));

	function devId(nodeId: string): number {
		return devices.find((d) => d.id === nodeId)?.deviceId ?? 0;
	}

	function swap() {
		[parentId, childId] = [childId, parentId];
		[parentPort, childPort] = [childPort, parentPort];
	}

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		if (!parentId) e.parentId = 'Gerät wählen';
		if (!childId) e.childId = 'Gerät wählen';
		if (parentId && childId && parentId === childId)
			e.childId = 'Ein Gerät kann nicht mit sich selbst verbunden werden';
		if (parentPort.length > 100) e.parentPort = 'Höchstens 100 Zeichen';
		if (childPort.length > 100) e.childPort = 'Höchstens 100 Zeichen';
		if (text.length > 200) e.label = 'Höchstens 200 Zeichen';
		return e;
	}

	async function save() {
		errors = validate();
		formError = null;
		if (Object.keys(errors).length) return;
		busy = true;
		try {
			const res = await api.post('/api/v1/topology/edges', {
				body: {
					parentId: devId(parentId),
					childId: devId(childId),
					kind,
					parentPort: parentPort.trim(),
					childPort: childPort.trim(),
					label: text.trim()
				}
			});
			toast.success('Verbindung angelegt');
			open = false;
			onsaved?.(res.id);
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
	title="Verbindung anlegen"
	description="Manuelle Verbindungen sind geschützt und werden von Scans nicht verändert."
	as="form"
	onsubmit={save}
	{busy}
>
	<div class="flex flex-col gap-4">
		{#if formError}<Alert tone="danger">{formError}</Alert>{/if}
		<div class="grid grid-cols-1 items-end gap-3 sm:grid-cols-[1fr_auto_1fr]">
			<Select
				label="Übergeordnetes Gerät"
				bind:value={parentId}
				{options}
				placeholder="Gerät wählen …"
				required
				error={errors.parentId}
			/>
			<Button
				icon="swap"
				label="Richtung tauschen"
				class={errors.parentId || errors.childId ? 'sm:mb-6' : ''}
				onclick={swap}
			/>
			<Select
				label="Untergeordnetes Gerät"
				bind:value={childId}
				{options}
				placeholder="Gerät wählen …"
				required
				error={errors.childId}
			/>
		</div>
		<Select label="Art" bind:value={kind} options={kindOptions} hint={KIND_HINT[kind]} error={errors.kind} />
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<Input
				label="Port (übergeordnet)"
				bind:value={parentPort}
				placeholder="z. B. Gi1/0/12"
				mono
				maxlength={100}
				error={errors.parentPort}
			/>
			<Input
				label="Port (untergeordnet)"
				bind:value={childPort}
				placeholder="z. B. eth0"
				mono
				maxlength={100}
				error={errors.childPort}
			/>
		</div>
		<Input
			label="Beschriftung"
			bind:value={text}
			placeholder="optional, z. B. Glasfaser Keller"
			maxlength={200}
			error={errors.label}
		/>
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="link" loading={busy}>Verbindung anlegen</Button>
	{/snippet}
</Modal>
