<!--
	One system NetScope reads with credentials (sources step): switched on, the card shows the
	settings form of the plugin, lets the user create credentials right here (on the first
	start there are none) and test the connection. "Übernehmen" stores and enables it;
	switching off disables it again.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import type { Credential, CredentialType, PluginView } from '$lib/api';
	import CredentialForm from '$lib/components/credentials/CredentialForm.svelte';
	import ConnectionTest from '$lib/components/plugins/ConnectionTest.svelte';
	import { splitConfigErrors } from '$lib/components/plugins/plugin';
	import { SchemaForm, schemaInitial, schemaPayload, validateSchema } from '$lib/components/schema';
	import { Badge, Button, Toggle } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { credentials } from '$lib/stores/catalog.svelte';

	interface Props {
		plugin: PluginView;
		credentialTypes: CredentialType[];
		onsaved: (p: PluginView) => void;
	}

	let { plugin, credentialTypes, onsaved }: Props = $props();

	const fields = $derived(plugin.schema?.fields ?? []);
	const enabled = $derived(!!plugin.config?.enabled);
	const idp = $derived(`src-${plugin.info.id}`);
	let open = $state(untrack(() => !!plugin.config?.enabled));
	let values = $state<Record<string, unknown>>(
		untrack(() => schemaInitial(plugin.schema?.fields ?? [], plugin.config?.settings))
	);
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	let saving = $state(false);
	let credOpen = $state(false);

	// credential types the plugin accepts (union of its credential-ref fields)
	const credFields = $derived(fields.filter((f) => f.type === 'credential-ref'));
	const allowedTypes = $derived(
		credentialTypes.filter((ct) =>
			credFields.some((f) => !f.credentialTypes?.length || f.credentialTypes.includes(ct.type))
		)
	);

	async function save(on: boolean) {
		formError = '';
		if (on) {
			const e = validateSchema(fields, values);
			errors = Object.fromEntries(Object.entries(e).map(([k, v]) => ['settings.' + k, v]));
			if (Object.keys(e).length) return;
		}
		saving = true;
		try {
			const next = await api.put('/api/v1/plugins/{id}/config', {
				path: { id: plugin.info.id },
				body: on ? { enabled: true, settings: schemaPayload(fields, values) } : { enabled: false }
			});
			errors = {};
			onsaved(next);
		} catch (e) {
			const split = splitConfigErrors(e);
			errors = split.settings;
			formError = Object.keys(split.settings).length
				? t('Bitte die markierten Felder korrigieren.')
				: errorMessage(e);
		} finally {
			saving = false;
		}
	}

	function toggle(on: boolean) {
		open = on;
		if (!on && enabled) save(false);
	}

	async function credentialCreated(c: Credential) {
		await credentials.refresh().catch(() => {});
		// select the new credential in the first field that accepts its type
		const f = credFields.find((x) => !x.credentialTypes?.length || x.credentialTypes.includes(c.type));
		if (!f) return;
		const cur = values[f.key];
		values[f.key] = f.multi ? [...(Array.isArray(cur) ? cur : []), c.id] : c.id;
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
		</div>
	</div>
	{#if open}
		<div class="flex flex-col gap-4 border-t border-border px-4 py-4">
			<SchemaForm {fields} bind:values {errors} errorPrefix="settings." idPrefix={idp} />
			{#if allowedTypes.length}
				<div class="flex flex-wrap items-center gap-2 text-sm text-fg-muted">
					<Button size="sm" icon="key" onclick={() => (credOpen = true)}>{t('Zugangsdaten anlegen')}</Button>
					<span>{t('Neue Zugangsdaten werden oben automatisch ausgewählt.')}</span>
				</div>
			{/if}
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

<CredentialForm bind:open={credOpen} credential={null} types={allowedTypes} onsaved={credentialCreated} />
