<!--
	One system NetScope reads with credentials (sources step): switched on, the card shows the
	settings form of the plugin – credentials are created right at their field ("Neu anlegen",
	on the first start there are none) – and tests the connection. "Übernehmen" stores and
	enables it; switching off disables it again.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import type { PluginView } from '$lib/api';
	import CredentialCreator from '$lib/components/credentials/CredentialCreator.svelte';
	import ConnectionTest from '$lib/components/plugins/ConnectionTest.svelte';
	import { splitConfigErrors } from '$lib/components/plugins/plugin';
	import { SchemaForm, schemaInitial, schemaPayload, validateSchema } from '$lib/components/schema';
	import { Badge, Button, Toggle } from '$lib/components/ui';
	import { t } from '$lib/i18n';

	interface Props {
		plugin: PluginView;
		onsaved: (p: PluginView) => void;
	}

	let { plugin, onsaved }: Props = $props();

	const fields = $derived(plugin.schema?.fields ?? []);
	const enabled = $derived(!!plugin.config?.enabled);
	const idp = $derived(`src-${plugin.info.id}`);
	let open = $state(untrack(() => !!plugin.config?.enabled));
	let values = $state<Record<string, unknown>>(
		untrack(() => schemaInitial(plugin.schema?.fields ?? [], plugin.config?.settings))
	);
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	// failure of switching off (the panel is closed then, so it is shown in the header)
	let toggleError = $state('');
	let saving = $state(false);

	/** Stores the configuration; false when it failed (the message is in formError). */
	async function save(on: boolean): Promise<boolean> {
		formError = '';
		if (on) {
			const e = validateSchema(fields, values);
			errors = Object.fromEntries(Object.entries(e).map(([k, v]) => ['settings.' + k, v]));
			if (Object.keys(e).length) return false;
		}
		saving = true;
		try {
			const next = await api.put('/api/v1/plugins/{id}/config', {
				path: { id: plugin.info.id },
				body: on ? { enabled: true, settings: schemaPayload(fields, values) } : { enabled: false }
			});
			errors = {};
			onsaved(next);
			return true;
		} catch (e) {
			const split = splitConfigErrors(e);
			errors = split.settings;
			formError = Object.keys(split.settings).length
				? t('Bitte die markierten Felder korrigieren.')
				: errorMessage(e);
			return false;
		} finally {
			saving = false;
		}
	}

	async function toggle(on: boolean) {
		toggleError = '';
		open = on;
		if (on || !enabled) return;
		// switching off an enabled source: only closed once the server agreed
		if (!(await save(false))) {
			toggleError = formError;
			formError = '';
			open = true;
		}
	}
</script>

<div class="rounded-lg border {open ? 'border-accent' : 'border-border'} bg-surface">
	<div class="flex items-start gap-3 px-4 py-3">
		<Toggle
			checked={open}
			onchange={toggle}
			label={plugin.info.name}
			hideLabel
			class="mt-0.5"
			disabled={saving}
		/>
		<div class="min-w-0 flex-1">
			<div class="flex flex-wrap items-center gap-2">
				<span class="font-medium text-fg">{plugin.info.name}</span>
				{#if enabled}<Badge tone="ok" dot>{t('eingerichtet')}</Badge>{/if}
			</div>
			<p class="mt-0.5 text-sm text-fg-muted">{plugin.info.description}</p>
			{#if toggleError}
				<p class="mt-1 text-sm text-danger" role="alert">
					{t('Ausschalten fehlgeschlagen: {error}', { error: toggleError })}
				</p>
			{/if}
		</div>
	</div>
	{#if open}
		<div class="flex flex-col gap-4 border-t border-border px-4 py-4">
			<CredentialCreator>
				<SchemaForm {fields} bind:values {errors} errorPrefix="settings." idPrefix={idp} />
			</CredentialCreator>
			<ConnectionTest
				{plugin}
				settings={() => schemaPayload(fields, values)}
				onfielderrors={(e) =>
					(errors = Object.fromEntries(Object.entries(e).map(([k, v]) => ['settings.' + k, v])))}
			/>
			<div class="flex flex-wrap items-center justify-end gap-2 border-t border-border pt-3">
				{#if formError}<span class="mr-auto text-sm text-danger">{formError}</span>{/if}
				<Button variant="primary" icon="save" loading={saving} onclick={() => save(true)}>
					{enabled ? t('Änderungen übernehmen') : t('Übernehmen und aktivieren')}
				</Button>
			</div>
		</div>
	{/if}
</div>
