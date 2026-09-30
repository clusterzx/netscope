<!--
	"Einstellungen" tab of a plugin: generic configuration (schedule, timeout, retries,
	concurrency, scope) plus the schema generated settings form. Saves everything with
	PUT /api/v1/plugins/{id}/config. `dirty` is bindable (navigation guard of the page).
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import type { PluginScope, PluginView } from '$lib/api';
	import { t } from '$lib/i18n';
	import {
		CronField,
		SchemaForm,
		schemaInitial,
		schemaPayload,
		validateSchema
	} from '$lib/components/schema'; // i18n-ignore (import path)
	import { Alert, Badge, Button, Card, Icon, Input, RelativeTime, Toggle } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatSeconds } from '$lib/utils/format';
	import CredentialCreator from '$lib/components/credentials/CredentialCreator.svelte';
	import ConnectionTest from './ConnectionTest.svelte';
	import ScopeEditor from './ScopeEditor.svelte';
	import { canRun, isPublisher, normScope, splitConfigErrors } from './plugin';

	interface Props {
		plugin: PluginView;
		/** true while the form differs from the stored configuration */
		dirty?: boolean;
		onsaved?: (p: PluginView) => void;
		/** immediate enable/disable (shared with the page header) */
		onenabled?: (enabled: boolean) => void;
		enabledBusy?: boolean;
	}

	let { plugin, dirty = $bindable(false), onsaved, onenabled, enabledBusy = false }: Props = $props();

	type Generic = {
		schedule: string;
		timeoutSeconds: number | null;
		retries: number | null;
		retryBackoffSeconds: number | null;
		concurrency: number | null;
		scope: PluginScope;
	};

	const fields = $derived(plugin.schema?.fields ?? []);
	const runner = $derived(canRun(plugin));
	const publisher = $derived(isPublisher(plugin));
	const hasScope = $derived(!!plugin.info.targets);
	const idp = $derived(`pl-${plugin.info.id}`);
	const canManage = $derived(auth.can('plugins.manage'));

	function genericFrom(p: PluginView): Generic {
		const c = p.config;
		return {
			schedule: c?.schedule ?? '',
			timeoutSeconds: c?.timeoutSeconds ?? null,
			retries: c?.retries ?? null,
			retryBackoffSeconds: c?.retryBackoffSeconds ?? null,
			concurrency: c?.concurrency ?? null,
			scope: normScope(c?.scope)
		};
	}

	let generic = $state<Generic>(untrack(() => genericFrom(plugin)));
	let values = $state<Record<string, unknown>>(
		untrack(() => schemaInitial(plugin.schema?.fields ?? [], plugin.config?.settings))
	);
	let baseline = $state(untrack(() => snapshot()));
	let genericErrors = $state<Record<string, string>>({});
	let settingsErrors = $state<Record<string, string>>({});
	let formError = $state('');
	let saving = $state(false);
	let changedMeanwhile = $state(false);

	function snapshot(): string {
		return JSON.stringify({ g: generic, v: values });
	}

	function reset(p: PluginView) {
		generic = genericFrom(p);
		values = schemaInitial(p.schema?.fields ?? [], p.config?.settings);
		baseline = snapshot();
		genericErrors = {};
		settingsErrors = {};
		formError = '';
		changedMeanwhile = false;
	}

	$effect(() => {
		dirty = snapshot() !== baseline;
	});

	/** what the form would look like for the stored configuration */
	function serverKey(p: PluginView): string {
		return JSON.stringify({
			g: genericFrom(p),
			v: schemaInitial(p.schema?.fields ?? [], p.config?.settings)
		});
	}

	// the configuration changed elsewhere (other tab, API): take it over unless edited here
	$effect(() => {
		const p = plugin;
		const key = serverKey(p);
		untrack(() => {
			if (key === baseline) return;
			if (!dirty) reset(p);
			else changedMeanwhile = true;
		});
	});

	function intError(v: number | null, min: number, max: number, what: string): string | null {
		if (v === null || v === undefined || (v as unknown) === '') return t('Pflichtfeld');
		if (!Number.isInteger(Number(v))) return t('Ganzzahl erwartet');
		if (v < min || v > max) return t('{what}: {min}–{max} erlaubt', { what, min, max });
		return null;
	}

	function validateGeneric(): Record<string, string> {
		const e: Record<string, string> = {};
		const to = intError(generic.timeoutSeconds, 10, 86400, t('Sekunden'));
		if (to) e.timeoutSeconds = to;
		if (!publisher) {
			const r = intError(generic.retries, 0, 10, t('Wiederholungen'));
			if (r) e.retries = r;
			const b = intError(generic.retryBackoffSeconds, 5, 86400, t('Sekunden'));
			if (b) e.retryBackoffSeconds = b;
			const c = intError(generic.concurrency, 1, 256, t('Parallelität'));
			if (c) e.concurrency = c;
		}
		return e;
	}

	async function save() {
		formError = '';
		genericErrors = validateGeneric();
		const se = validateSchema(fields, values);
		settingsErrors = Object.fromEntries(Object.entries(se).map(([k, v]) => ['settings.' + k, v]));
		if (Object.keys(genericErrors).length || Object.keys(se).length) {
			formError = t('Bitte die markierten Felder korrigieren.');
			return;
		}
		saving = true;
		try {
			const body: Record<string, unknown> = {
				timeoutSeconds: Number(generic.timeoutSeconds),
				settings: schemaPayload(fields, values)
			};
			if (runner) body.schedule = generic.schedule.trim();
			if (!publisher) {
				body.retries = Number(generic.retries);
				body.retryBackoffSeconds = Number(generic.retryBackoffSeconds);
				body.concurrency = Number(generic.concurrency);
			}
			if (hasScope) {
				const s = generic.scope;
				body.scope = {
					allSubnets: s.allSubnets,
					subnets: s.allSubnets ? [] : s.subnets,
					groups: s.groups,
					tags: s.tags,
					devices: s.devices,
					query: (s.query ?? '').trim()
				};
			}
			const next = await api.put('/api/v1/plugins/{id}/config', {
				path: { id: plugin.info.id },
				body
			});
			reset(next);
			toast.success(t('Einstellungen gespeichert – sie gelten ab sofort.'), { title: next.info.name });
			onsaved?.(next);
		} catch (e) {
			const split = splitConfigErrors(e);
			genericErrors = split.generic;
			settingsErrors = split.settings;
			formError =
				Object.keys(split.generic).length || Object.keys(split.settings).length
					? t('Bitte die markierten Felder korrigieren.')
					: errorMessage(e);
		} finally {
			saving = false;
		}
	}

	function discard() {
		reset(plugin);
	}
</script>

<!-- credential fields may create credentials right here (the dialog renders after the form) -->
<CredentialCreator>
	<form
		class="flex flex-col gap-4"
		novalidate
		onsubmit={(e) => {
			e.preventDefault();
			if (canManage) save();
		}}
	>
		{#if !canManage}
			<p class="text-xs text-fg-subtle">
				{t('Nur lesen – dafür fehlt die Berechtigung „Plugins konfigurieren“.')}
			</p>
		{/if}
		{#if changedMeanwhile}
			<Alert tone="warn" title={t('Konfiguration wurde zwischenzeitlich geändert')}>
				{t(
					'Die gespeicherte Konfiguration wurde an anderer Stelle geändert. Beim Speichern werden diese Änderungen überschrieben.'
				)}
				{#snippet actions()}
					<Button size="xs" onclick={discard}>{t('Neu laden')}</Button>
				{/snippet}
			</Alert>
		{/if}

		<fieldset disabled={!canManage} class="contents">
			<Card title={t('Allgemein')} icon="system">
				<div class="flex flex-col gap-5">
					<Toggle
						checked={plugin.config?.enabled ?? false}
						onchange={(v) => onenabled?.(v)}
						disabled={enabledBusy}
						label={t('Plugin aktiv')}
						description={runner
							? t(
									'Wirkt sofort. Inaktive Plugins laufen nicht nach Zeitplan; manuelle Läufe bleiben möglich.'
								)
							: publisher
								? t('Wirkt sofort. Nur aktive Publisher stellen Benachrichtigungen zu.')
								: t('Wirkt sofort.')}
					/>

					{#if runner}
						<CronField
							id="{idp}-schedule"
							label={t('Zeitplan')}
							bind:value={generic.schedule}
							error={genericErrors.schedule}
							hint={plugin.info.defaultSchedule
								? t('Standard: {schedule}. Leer = nur manuell.', { schedule: plugin.info.defaultSchedule })
								: t('Leer = nur manuell ausführen.')}
						/>
					{/if}

					<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 {publisher ? '' : 'xl:grid-cols-4'}">
						<Input
							id="{idp}-timeout"
							type="number"
							label={t('Timeout (Sekunden)')}
							min={10}
							max={86400}
							bind:value={generic.timeoutSeconds}
							error={genericErrors.timeoutSeconds}
							hint={generic.timeoutSeconds
								? `= ${formatSeconds(Number(generic.timeoutSeconds))}`
								: t('10 s bis 24 h')}
						/>
						{#if !publisher}
							<Input
								id="{idp}-retries"
								type="number"
								label={t('Wiederholungen')}
								min={0}
								max={10}
								bind:value={generic.retries}
								error={genericErrors.retries}
								hint={t('bei Fehlschlag, 0–10')}
							/>
							<Input
								id="{idp}-backoff"
								type="number"
								label={t('Wartezeit vor Wiederholung (s)')}
								min={5}
								max={86400}
								bind:value={generic.retryBackoffSeconds}
								error={genericErrors.retryBackoffSeconds}
								hint={generic.retryBackoffSeconds
									? t('= {duration}, verdoppelt sich je Versuch', {
											duration: formatSeconds(Number(generic.retryBackoffSeconds))
										})
									: t('verdoppelt sich je Versuch')}
							/>
							<Input
								id="{idp}-concurrency"
								type="number"
								label={t('Parallelität')}
								min={1}
								max={256}
								bind:value={generic.concurrency}
								error={genericErrors.concurrency}
								hint={t('gleichzeitige Ziele, 1–256')}
							/>
						{/if}
					</div>
					{#if publisher}
						<p class="text-xs text-fg-subtle">
							{t(
								'Fehlgeschlagene Zustellungen wiederholt der Regel-Dispatcher automatisch (bis zu 5 Versuche).'
							)}
						</p>
					{/if}
				</div>
			</Card>

			{#if hasScope}
				<Card
					title={t('Bereich')}
					icon="network"
					description={plugin.info.targets === 'subnets'
						? t('Welche Subnetze bzw. Geräte das Plugin abfragt.')
						: t('Auf welchen Geräten das Plugin arbeitet.')}
				>
					<ScopeEditor
						bind:value={generic.scope}
						targets={plugin.info.targets}
						errors={genericErrors}
						idPrefix="{idp}-scope"
					/>
				</Card>
			{/if}

			<Card title={t('Plugin-Einstellungen')} icon="edit">
				<SchemaForm
					{fields}
					bind:values
					errors={settingsErrors}
					errorPrefix="settings."
					idPrefix="{idp}-set"
				/>
				{#if plugin.capabilities?.includes('connectionTest')}
					<div class="mt-4 border-t border-border pt-4">
						<ConnectionTest
							{plugin}
							settings={() => schemaPayload(fields, values)}
							onfielderrors={(e) =>
								(settingsErrors = Object.fromEntries(
									Object.entries(e).map(([k, v]) => ['settings.' + k, v])
								))}
							disabled={!canManage}
						/>
						<p class="mt-2 text-xs text-fg-subtle">
							{t('Prüft Adresse und Anmeldung mit den Werten des Formulars, ohne sie zu speichern.')}
						</p>
					</div>
				{/if}
			</Card>
		</fieldset>

		<!-- sticky save bar -->
		{#if canManage}
			<div
				class="sticky bottom-0 z-10 -mx-1 flex flex-wrap items-center gap-2 rounded-lg border border-border bg-surface/95 px-3 py-2.5 shadow-md backdrop-blur"
			>
				<div class="flex min-w-0 flex-1 items-center gap-2 text-sm" aria-live="polite">
					{#if formError}
						<Icon name="x-circle" size={16} class="shrink-0 text-danger" />
						<span class="min-w-0 truncate text-danger" title={formError}>{formError}</span>
					{:else if dirty}
						<Badge tone="warn" dot>{t('Ungespeicherte Änderungen')}</Badge>
					{:else}
						<span class="text-fg-subtle">
							{t('Gespeichert')}
							{#if plugin.config?.updatedAt}<RelativeTime value={plugin.config.updatedAt} />{/if}
						</span>
					{/if}
				</div>
				<Button onclick={discard} disabled={!dirty || saving} icon="refresh">{t('Zurücksetzen')}</Button>
				<Button type="submit" variant="primary" icon="save" loading={saving} disabled={!dirty && !formError}>
					{t('Speichern')}
				</Button>
			</div>
		{/if}
	</form>
</CredentialCreator>
