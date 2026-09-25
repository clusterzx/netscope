<!-- widget "file": upload via POST /api/v1/uploads, the field holds the returned server path. -->
<script lang="ts">
	import { api, errorMessage } from '$lib/api/client';
	import Button from '$lib/components/ui/Button.svelte';
	import FormField from '$lib/components/ui/FormField.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { formatBytes } from '$lib/utils/format';

	interface Props {
		value: string;
		label: string;
		hint?: string;
		error?: string | null;
		required?: boolean;
		disabled?: boolean;
		id: string;
	}

	let { value = $bindable(''), label, hint, error, required = false, disabled = false, id }: Props = $props();

	let busy = $state(false);
	let uploadError = $state('');
	let info = $state('');
	let fileInput: HTMLInputElement | null = $state(null);

	async function onFile(e: Event) {
		const f = (e.currentTarget as HTMLInputElement).files?.[0];
		if (!f) return;
		busy = true;
		uploadError = '';
		try {
			const res = await api.upload(f);
			value = res.path;
			info = `${res.name} (${formatBytes(res.size)}) hochgeladen`;
		} catch (err) {
			uploadError = errorMessage(err);
		} finally {
			busy = false;
			if (fileInput) fileInput.value = '';
		}
	}
</script>

<FormField {label} hint={info || hint} error={error || uploadError} {required} {id}>
	{#snippet children(fid, describedby)}
		<div class="flex flex-wrap items-center gap-2">
			<input
				id={fid}
				bind:value
				{disabled}
				aria-describedby={describedby}
				placeholder="Pfad auf dem Server oder Datei hochladen"
				class="mono h-8.5 min-w-0 flex-1 rounded-md border bg-surface px-2.5 text-sm text-fg shadow-sm placeholder:text-fg-subtle focus:border-accent focus:ring-2 focus:ring-focus focus:outline-none
					{error || uploadError ? 'border-danger' : 'border-border'}"
			/>
			{#if auth.can('plugins.manage')}
				<input
					bind:this={fileInput}
					type="file"
					class="hidden"
					onchange={onFile}
					tabindex="-1"
					aria-hidden="true"
				/>
				<Button icon="upload" loading={busy} {disabled} onclick={() => fileInput?.click()}
					>Datei hochladen</Button
				>
			{/if}
		</div>
	{/snippet}
</FormField>
