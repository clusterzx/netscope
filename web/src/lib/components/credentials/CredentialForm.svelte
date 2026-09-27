<!--
	Create/edit dialog for a vault credential. Create: choose the type first, then the
	type's schema form. Edit: type is fixed; stored secrets show "gesetzt" and are kept
	(SECRET_MASK) unless replaced or removed. "Gilt für" edits the credential scope that
	plugins use to pick credentials per target.
	<CredentialForm bind:open credential={c | null} types={types} onsaved={(c) => …} />
-->
<script lang="ts">
	import { api, ApiError, errorMessage, fieldErrors } from '$lib/api';
	import type { Credential, CredentialType, PluginScope } from '$lib/api';
	import ScopeEditor from '$lib/components/plugins/ScopeEditor.svelte';
	import {
		credentialScopeLevel,
		credentialScopeSummary,
		emptyScope,
		normScope
	} from '$lib/components/plugins/plugin';
	import { groups } from '$lib/stores/catalog.svelte';
	import { SchemaForm, schemaInitial, schemaPayload, validateSchema } from '$lib/components/schema';
	import { Alert, Button, Icon, Input, Modal, Textarea } from '$lib/components/ui';
	import { t } from '$lib/i18n';

	interface Props {
		open: boolean;
		/** credential to edit, null = create */
		credential: Credential | null;
		types: CredentialType[];
		onsaved?: (c: Credential) => void;
	}

	let { open = $bindable(false), credential, types, onsaved }: Props = $props();

	let typeId = $state('');
	let step = $state<'type' | 'form'>('type');
	let name = $state('');
	let description = $state('');
	let values = $state<Record<string, unknown>>({});
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	let busy = $state(false);
	let scope = $state<PluginScope>(emptyScope());
	let showScope = $state(false);

	const groupName = (id: number) => groups.value?.find((g) => g.id === id)?.name ?? `#${id}`;
	const scopeText = $derived(credentialScopeSummary(scope, groupName));
	const scopeErrors = $derived(
		Object.fromEntries(Object.entries(errors).filter(([k]) => k.startsWith('scope')))
	);

	const type = $derived(types.find((x) => x.type === typeId));
	const fields = $derived(type?.schema?.fields ?? []);
	const schemaErrors = $derived(
		Object.fromEntries(Object.entries(errors).filter(([k]) => k !== 'name' && !k.startsWith('scope')))
	);

	// (re)initialise whenever the dialog opens
	let wasOpen = false;
	$effect(() => {
		const o = open;
		if (o && !wasOpen) init();
		wasOpen = o;
	});

	function init() {
		errors = {};
		formError = '';
		scope = normScope(credential?.scope ?? emptyScope());
		if (!scope.allSubnets && !scope.subnets?.length && credentialScopeLevel(scope) === 'everywhere')
			scope.allSubnets = true;
		showScope = credentialScopeLevel(scope) !== 'everywhere';
		groups.load().catch(() => {});
		if (credential) {
			typeId = credential.type;
			step = 'form';
			name = credential.name;
			description = credential.description ?? '';
			const ct = types.find((x) => x.type === credential.type);
			values = schemaInitial(ct?.schema?.fields ?? [], credential.public ?? {}, credential.secretsSet ?? []);
		} else {
			typeId = '';
			step = 'type';
			name = '';
			description = '';
			values = {};
		}
	}

	function chooseType(ct: CredentialType) {
		typeId = ct.type;
		values = schemaInitial(ct.schema?.fields ?? [], {});
		errors = {};
		step = 'form';
		requestAnimationFrame(() => document.getElementById('cred-name')?.focus());
	}

	async function submit() {
		if (step === 'type') {
			if (type) chooseType(type);
			return;
		}
		formError = '';
		const e = validateSchema(fields, values);
		if (!name.trim()) e.name = t('Pflichtfeld');
		errors = e;
		if (Object.keys(e).length) return;
		busy = true;
		try {
			const body = {
				name: name.trim(),
				type: typeId,
				description: description.trim(),
				values: schemaPayload(fields, values),
				scope: {
					...scope,
					subnets: scope.allSubnets ? [] : scope.subnets,
					query: scope.query?.trim() ?? ''
				}
			};
			const saved = credential
				? await api.put('/api/v1/credentials/{id}', { path: { id: credential.id }, body })
				: await api.post('/api/v1/credentials', { body });
			open = false;
			onsaved?.(saved);
		} catch (err) {
			errors = fieldErrors(err);
			if (Object.keys(errors).some((k) => k.startsWith('scope'))) showScope = true;
			if (err instanceof ApiError && /name/i.test(err.message) && !err.fields.length)
				errors = { name: err.message };
			// messages that belong to a field are shown there only
			formError = Object.keys(errors).length ? '' : errorMessage(err);
		} finally {
			busy = false;
		}
	}
</script>

<Modal
	bind:open
	title={credential
		? t('Credential „{name}“ bearbeiten', { name: credential.name })
		: t('Credential anlegen')}
	description={type ? `${type.label} – ${type.description}` : t('Art der Zugangsdaten wählen')}
	size="lg"
	as="form"
	onsubmit={submit}
	{busy}
>
	{#if step === 'type'}
		<fieldset>
			<legend class="sr-only">{t('Typ')}</legend>
			<div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
				{#each types as ct (ct.type)}
					<label
						class="flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2.5 transition-colors
							{typeId === ct.type
							? 'border-accent bg-accent-soft'
							: 'border-border hover:border-border-strong hover:bg-surface-2'}"
					>
						<input
							type="radio"
							name="cred-type"
							value={ct.type}
							bind:group={typeId}
							class="mt-1 accent-(--accent)"
							ondblclick={() => chooseType(ct)}
						/>
						<span class="min-w-0">
							<span class="block text-sm font-medium">{ct.label}</span>
							<span class="block text-xs text-fg-muted">{ct.description}</span>
						</span>
					</label>
				{/each}
			</div>
		</fieldset>
	{:else}
		<div class="flex flex-col gap-4">
			{#if formError}<Alert tone="danger"><span class="break-words">{formError}</span></Alert>{/if}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
				<Input id="cred-name" label="Name" required bind:value={name} error={errors.name} maxlength={200} />
				<div class="flex flex-col gap-1">
					<span class="text-[0.8125rem] font-medium text-fg">{t('Typ')}</span>
					<span class="flex h-8.5 items-center gap-2 text-sm">
						<Icon name="key" size={14} class="text-fg-subtle" />
						{type?.label ?? typeId}
						{#if !credential}
							<button type="button" class="link text-xs" onclick={() => (step = 'type')}>{t('ändern')}</button
							>
						{/if}
					</span>
				</div>
			</div>
			<Textarea id="cred-desc" label={t('Beschreibung')} bind:value={description} rows={2} />
			<div class="border-t border-border pt-4">
				<SchemaForm {fields} bind:values errors={schemaErrors} idPrefix="cred-{typeId}" compact />
			</div>
			{#if typeId !== 'wireguard'}
				<section class="border-t border-border pt-4" aria-labelledby="cred-scope-title">
					<div class="flex flex-wrap items-start justify-between gap-2">
						<div class="min-w-0">
							<h3 id="cred-scope-title" class="text-[0.8125rem] font-medium text-fg">{t('Gilt für')}</h3>
							<p class="text-sm break-words text-fg-muted">{scopeText}</p>
						</div>
						<Button
							size="sm"
							icon={showScope ? 'chevron-up' : 'edit'}
							aria-expanded={showScope}
							aria-controls="cred-scope"
							onclick={() => (showScope = !showScope)}>{showScope ? t('Einklappen') : t('Anpassen')}</Button
						>
					</div>
					<p class="mt-1 text-xs text-fg-subtle">
						{t(
							'Plugins ohne eigene Auswahl nehmen pro Gerät automatisch die passenden Zugangsdaten – zuerst die dem Gerät zugewiesenen, dann Gruppe, Tag oder Filter, dann Subnetz, zuletzt „überall“.'
						)}
					</p>
					{#if showScope}
						<div id="cred-scope" class="mt-4">
							<ScopeEditor bind:value={scope} mode="credential" errors={scopeErrors} idPrefix="cred-scope" />
						</div>
					{/if}
				</section>
			{/if}
			<p class="flex items-start gap-1.5 text-xs text-fg-subtle">
				<Icon name="lock" size={13} class="mt-px shrink-0" />
				{t('Secrets werden verschlüsselt gespeichert und danach nie wieder angezeigt.')}
				{credential ? t('Leer lassen bzw. „gesetzt“ belassen, um den gespeicherten Wert zu behalten.') : ''}
			</p>
		</div>
	{/if}
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>{t('Abbrechen')}</Button>
		{#if step === 'type'}
			<Button type="submit" variant="primary" iconRight="arrow-right" disabled={!typeId}>{t('Weiter')}</Button
			>
		{:else}
			<Button type="submit" variant="primary" icon="save" loading={busy}
				>{credential ? t('Speichern') : t('Anlegen')}</Button
			>
		{/if}
	{/snippet}
</Modal>
