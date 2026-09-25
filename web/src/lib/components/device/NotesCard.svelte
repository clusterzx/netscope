<!-- Device notes: rendered Markdown, editor with preview (PATCH /devices/{id} {notes}). -->
<script lang="ts">
	import { tick } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import type { DeviceDetail } from '$lib/api/types';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import MarkdownView from '$lib/components/ui/MarkdownView.svelte';
	import Tabs from '$lib/components/ui/Tabs.svelte';
	import Textarea from '$lib/components/ui/Textarea.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toast.svelte';

	interface Props {
		device: DeviceDetail;
		editing?: boolean;
		onsaved: (next: DeviceDetail) => void;
	}

	let { device, editing = $bindable(false), onsaved }: Props = $props();

	let text = $state('');
	let mode = $state('write');
	let busy = $state(false);
	let error = $state('');
	let area: HTMLTextAreaElement | null = $state(null);
	let card: HTMLDivElement | null = $state(null);
	let wasEditing = false;

	// entering edit mode (also from the header menu): copy the notes, focus the editor
	$effect(() => {
		if (editing && !wasEditing) {
			text = device.notes ?? '';
			mode = 'write';
			error = '';
			tick().then(() => {
				card?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
				area?.focus();
			});
		}
		wasEditing = editing;
	});

	const dirty = $derived(editing && text !== (device.notes ?? ''));

	async function save() {
		busy = true;
		error = '';
		try {
			const next = await api.patch('/api/v1/devices/{id}', {
				path: { id: device.id },
				body: { notes: text }
			});
			toast.success('Notiz gespeichert');
			editing = false;
			onsaved(next);
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
			e.preventDefault();
			save();
		} else if (e.key === 'Escape' && !dirty) {
			e.preventDefault();
			editing = false;
		}
	}
</script>

<div bind:this={card}>
	<Card title="Notizen" icon="note" padding="md">
		{#snippet actions()}
			{#if !editing && auth.can('devices.edit')}
				<Button size="xs" variant="ghost" icon="edit" onclick={() => (editing = true)}>
					{device.notes ? 'Bearbeiten' : 'Hinzufügen'}
				</Button>
			{/if}
		{/snippet}
		{#if editing}
			<div class="flex flex-col gap-2">
				<Tabs
					items={[
						{ id: 'write', label: 'Schreiben' },
						{ id: 'preview', label: 'Vorschau' }
					]}
					bind:active={mode}
					idPrefix="notes-"
					label="Notiz-Editor"
				/>
				{#if mode === 'write'}
					<div role="tabpanel" id="notes-panel-write" aria-labelledby="notes-tab-write">
						<Textarea
							label="Notiz (Markdown)"
							bind:value={text}
							bind:ref={area}
							rows={10}
							mono
							onkeydown={onKey}
							hint="Markdown: **fett**, _kursiv_, `Code`, - Listen, [Link](https://…). Strg+Enter speichert."
						/>
					</div>
				{:else}
					<div
						role="tabpanel"
						id="notes-panel-preview"
						aria-labelledby="notes-tab-preview"
						class="min-h-24 rounded-md border border-border bg-surface-2 px-3 py-2"
					>
						{#if text.trim()}
							<MarkdownView source={text} class="[&>:first-child]:mt-0" />
						{:else}
							<p class="text-sm text-fg-subtle">Keine Notiz.</p>
						{/if}
					</div>
				{/if}
				{#if error}<Alert tone="danger">{error}</Alert>{/if}
				<div class="flex justify-end gap-2">
					<Button size="sm" onclick={() => (editing = false)} disabled={busy}>Abbrechen</Button>
					<Button size="sm" variant="primary" icon="save" loading={busy} onclick={save} disabled={!dirty}
						>Speichern</Button
					>
				</div>
			</div>
		{:else if device.notes?.trim()}
			<MarkdownView source={device.notes} class="text-sm [&>:first-child]:mt-0" />
		{:else}
			<p class="text-sm text-fg-subtle">
				Noch keine Notiz – z. B. Zweck, Zugangsdaten-Ort, Garantie oder Besonderheiten.
			</p>
		{/if}
	</Card>
</div>
