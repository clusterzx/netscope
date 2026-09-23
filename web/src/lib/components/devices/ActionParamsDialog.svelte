<!-- Parameters of a plugin device action (rendered from its schema fields). -->
<script lang="ts">
	import { errorMessage, fieldErrors } from '$lib/api';
	import type { DeviceAction } from '$lib/api';
	import { SchemaForm, schemaInitial, schemaPayload, validateSchema } from '$lib/components/schema';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';

	interface Props {
		open: boolean;
		action: DeviceAction | null;
		/** e.g. "3 Geräte" */
		target: string;
		onrun: (params: Record<string, unknown>) => Promise<void>;
	}

	let { open = $bindable(false), action, target, onrun }: Props = $props();

	let values = $state<Record<string, unknown>>({});
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);
	let error = $state('');

	$effect(() => {
		if (open && action) {
			values = schemaInitial(action.params ?? [], {});
			errors = {};
			error = '';
		}
	});

	async function submit() {
		if (!action) return;
		const fields = action.params ?? [];
		errors = validateSchema(fields, values);
		if (Object.keys(errors).length) return;
		busy = true;
		error = '';
		try {
			await onrun(schemaPayload(fields, values));
			open = false;
		} catch (e) {
			errors = fieldErrors(e);
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

<Modal
	bind:open
	title={action?.label ?? 'Aktion'}
	description="{action?.pluginName} · {target}"
	as="form"
	onsubmit={submit}
	{busy}
>
	<div class="flex flex-col gap-4">
		{#if action?.description}<p class="text-sm text-fg-muted">{action.description}</p>{/if}
		{#if action?.confirm}<Alert tone="warn">{action.confirm}</Alert>{/if}
		{#if error}<Alert tone="danger">{error}</Alert>{/if}
		{#if action?.params?.length}
			<SchemaForm
				fields={action.params}
				bind:values
				{errors}
				idPrefix="act-{action.plugin}-{action.name}"
				compact
			/>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="play" loading={busy}>Ausführen</Button>
	{/snippet}
</Modal>
