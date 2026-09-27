<!--
	Full page rule editor (conditions, actions) with the event simulation panel.
	Used by /rules/new (rule = null) and /rules/[id].
-->
<script lang="ts">
	import { beforeNavigate, goto } from '$app/navigation';
	import { onMount, untrack } from 'svelte';
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { Rule } from '$lib/api';
	import QueryInput from '$lib/components/QueryInput.svelte';
	import {
		Alert,
		Badge,
		Button,
		Card,
		Checkbox,
		Icon,
		Input,
		MultiSelect,
		PageHeader,
		RelativeTime,
		Select,
		TagInput,
		Textarea,
		Toggle
	} from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { groups, subnets, tags, eventTypes } from '$lib/stores/catalog.svelte';
	import { federation } from '$lib/stores/federation.svelte';
	import { t } from '$lib/i18n';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { SEVERITIES } from '$lib/api';
	import { severityLabel, stateLabel } from '$lib/utils/labels';
	import ActionEditor from './ActionEditor.svelte';
	import EventTypePicker from './EventTypePicker.svelte';
	import PayloadConditions from './PayloadConditions.svelte';
	import RuleSimulator from './RuleSimulator.svelte';
	import { loadPublishers, publishers } from './publishers.svelte';
	import {
		WEEKDAYS,
		emptyAction,
		emptyRule,
		fieldMessage,
		firstError,
		matchedTypes,
		normRule,
		rulePayload,
		validateRule
	} from './rule';

	interface Props {
		/** stored rule, or null for a new one */
		rule: Rule | null;
		/** template for a new rule (duplicate) */
		template?: Rule | null;
		/** sortOrder for a new rule */
		nextSortOrder?: number;
	}

	let { rule, template = null, nextSortOrder = 10 }: Props = $props();

	onMount(() => {
		loadPublishers();
		groups.load().catch(() => {});
		subnets.load().catch(() => {});
		tags.load().catch(() => {});
		eventTypes.load().catch(() => {});
	});

	const isNew = $derived(!rule);
	const canManage = $derived(auth.can('rules.manage'));
	const pubs = $derived(publishers.value ?? []);

	function initial(): Rule {
		if (stored) return normRule(stored);
		if (template) {
			const tpl = normRule(template);
			return {
				...tpl,
				id: 0,
				name: t('{name} (Kopie)', { name: tpl.name }),
				builtin: undefined,
				sortOrder: nextSortOrder
			};
		}
		const r = normRule(emptyRule());
		r.sortOrder = nextSortOrder;
		return r;
	}

	/** the rule as last loaded or saved (header, delete) */
	let stored = $state<Rule | null>(untrack(() => rule));
	let draft = $state<Rule>(untrack(() => initial()));
	let baseline = $state(untrack(() => JSON.stringify(rulePayload(draft))));
	let serverErrors = $state<Record<string, string>>({});
	let formError = $state('');
	let saving = $state(false);
	let deleting = $state(false);
	let showErrors = $state(false);

	// default publisher for a fresh rule: the first enabled one
	$effect(() => {
		const list = pubs;
		untrack(() => {
			if (!isNew || draft.actions.length !== 1 || draft.actions[0].publisher) return;
			const first = list.find((p) => p.enabled) ?? list[0];
			if (first) {
				draft.actions[0].publisher = first.id;
				baseline = JSON.stringify(rulePayload(draft));
			}
		});
	});

	const payload = $derived(rulePayload(draft));
	const dirty = $derived(JSON.stringify(payload) !== baseline);
	const clientErrors = $derived(validateRule(draft));
	// field errors (backend JSON paths): server errors of the last save plus – once the user
	// tried to save – the live client validation
	const errors = $derived(showErrors ? { ...serverErrors, ...clientErrors } : serverErrors);
	const FIX_FIELDS = t('Bitte die markierten Felder korrigieren.');
	$effect(() => {
		if (showErrors && !Object.keys(errors).length && untrack(() => formError) === FIX_FIELDS) formError = '';
	});

	const testRule = $derived({ ...payload, name: payload.name || t('Neue Regel') });
	const blocked = $derived.by(() => {
		const e = Object.entries(clientErrors).filter(([k]) => k !== 'name');
		if (!e.length) return '';
		const [k, msg] = e[0];
		const m = /^actions\.(\d+)\./.exec(k);
		return m ? t('Aktion {n}: {message}', { n: Number(m[1]) + 1, message: msg }) : msg;
	});

	const typesForPayload = $derived(matchedTypes(draft.conditions.eventTypes ?? [], eventTypes.value ?? []));

	// ---------------------------------------------------------------- condition helpers
	let siteIds = $state<string[]>(untrack(() => (draft.conditions.sites ?? []).map(String)));
	const siteOptions = $derived([
		{ value: '0', label: federation.localName, description: t('Events dieser Zentrale') },
		...federation.sites.map((s) => ({
			value: String(s.id),
			label: s.name,
			description: t('NetScope-Standort')
		}))
	]);
	let groupIds = $state<string[]>(untrack(() => (draft.conditions.groups ?? []).map(String)));
	const groupOptions = $derived(
		(groups.value ?? []).map((g) => ({
			value: String(g.id),
			label: g.name,
			description: g.kind === 'query' ? t('regelbasiert') : t('manuell')
		}))
	);
	const stateOptions = ['known', 'unknown', 'ignored'].map((s) => ({ value: s, label: stateLabel[s] }));
	const severityOptions = SEVERITIES.map((s) => ({
		value: s,
		label: t('ab {severity}', { severity: severityLabel[s] })
	}));

	function setTimeWindow(on: boolean) {
		draft.conditions.timeWindow = on ? { from: '08:00', to: '18:00', days: [1, 2, 3, 4, 5] } : undefined;
	}
	function toggleDay(d: number) {
		const tw = draft.conditions.timeWindow;
		if (!tw) return;
		const days = tw.days ?? [];
		tw.days = days.includes(d) ? days.filter((x) => x !== d) : [...days, d].sort();
	}

	function addAction() {
		const first = pubs.find((p) => p.enabled) ?? pubs[0];
		draft.actions.push(emptyAction(first?.id ?? ''));
	}
	function removeAction(i: number) {
		draft.actions.splice(i, 1);
	}
	function moveAction(i: number, dir: -1 | 1) {
		const j = i + dir;
		if (j < 0 || j >= draft.actions.length) return;
		const list = [...draft.actions];
		[list[i], list[j]] = [list[j], list[i]];
		draft.actions = list;
	}

	// ---------------------------------------------------------------- save / delete
	async function save() {
		formError = '';
		serverErrors = {};
		showErrors = true;
		if (Object.keys(clientErrors).length) {
			formError = FIX_FIELDS;
			return;
		}
		saving = true;
		try {
			const body = rulePayload(draft);
			const saved = rule
				? await api.put('/api/v1/rules/{id}', { path: { id: rule.id }, body })
				: await api.post('/api/v1/rules', { body });
			baseline = JSON.stringify(rulePayload(normRule(saved)));
			draft = normRule(saved);
			if (!isNew) stored = saved;
			showErrors = false;
			toast.success(isNew ? t('Regel angelegt') : t('Regel gespeichert'), { title: saved.name });
			if (isNew) {
				bypass = true;
				await goto(`/rules/${saved.id}`, { replaceState: true });
				bypass = false;
			}
		} catch (e) {
			// validation errors arrive as field errors with JSON paths (all at once)
			serverErrors = fieldErrors(e);
			formError = Object.keys(serverErrors).length ? FIX_FIELDS : errorMessage(e);
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!stored) return;
		const ok = await confirm({
			title: t('Regel „{name}“ löschen?', { name: stored.name }),
			message: stored.builtin
				? t('Das ist eine mitgelieferte Standardregel. Sie wird nicht automatisch wiederhergestellt.')
				: t('Bereits verschickte Benachrichtigungen bleiben im Verlauf erhalten.'),
			confirmLabel: t('Löschen'),
			danger: true
		});
		if (!ok) return;
		deleting = true;
		try {
			await api.delete('/api/v1/rules/{id}', { path: { id: stored.id } });
			toast.success(t('Regel „{name}“ gelöscht', { name: stored.name }));
			bypass = true;
			await goto('/rules');
		} catch (e) {
			toast.error(errorMessage(e), { title: t('Löschen fehlgeschlagen') });
		} finally {
			bypass = false;
			deleting = false;
		}
	}

	function discard() {
		draft = initial();
		groupIds = (draft.conditions.groups ?? []).map(String);
		siteIds = (draft.conditions.sites ?? []).map(String);
		baseline = JSON.stringify(rulePayload(draft));
		serverErrors = {};
		formError = '';
		showErrors = false;
	}

	// ---------------------------------------------------------------- unsaved changes guard
	let bypass = false;
	beforeNavigate((nav) => {
		if (!dirty || bypass) return;
		if (nav.to?.url.pathname === nav.from?.url.pathname) return;
		nav.cancel();
		if (nav.type === 'leave') return;
		const to = nav.to?.url;
		confirm({
			title: t('Ungespeicherte Änderungen verwerfen?'),
			message: t('Die Regel wurde geändert, aber noch nicht gespeichert.'),
			confirmLabel: t('Verwerfen'),
			cancelLabel: t('Weiter bearbeiten'),
			danger: true
		}).then((ok) => {
			if (!ok || !to) return;
			bypass = true;
			goto(to.pathname + to.search + to.hash).finally(() => (bypass = false));
		});
	});

	/** error for a JSON path (exact key or any nested key, e.g. conditions.subnets.2) */
	const err = (k: string) => firstError(errors, k);
</script>

<PageHeader
	title={isNew ? t('Neue Regel') : draft.name || stored?.name || t('Regel')}
	docTitle={isNew ? t('Neue Regel') : t('Regel: {name}', { name: stored?.name ?? '' })}
	description={isNew
		? t('Bedingungen festlegen und bestimmen, wer wie benachrichtigt wird.')
		: stored?.description || undefined}
>
	{#snippet breadcrumb()}
		<a href="/rules" class="hover:text-fg">{t('Regeln')}</a>
	{/snippet}
	{#snippet meta()}
		{#if stored}
			{#if stored.builtin}<Badge
					tone="accent"
					title={t('Mitgelieferte Standardregel ({id})', { id: stored.builtin })}>{t('Standardregel')}</Badge
				>{/if}
			{#if !stored.enabled}<Badge>{t('inaktiv')}</Badge>{/if}
			<span class="text-xs">{t('geändert')} <RelativeTime value={stored.updatedAt} /></span>
		{/if}
	{/snippet}
	{#snippet actions()}
		{#if stored}
			<Button variant="ghost" icon="bell" href="/rules?tab=notifications&rule={stored.id}"
				>{t('Benachrichtigungen')}</Button
			>
			{#if canManage}
				<Button icon="copy" href="/rules/new?from={stored.id}">{t('Duplizieren')}</Button>
				<Button variant="danger" icon="trash" loading={deleting} onclick={remove}>{t('Löschen')}</Button>
			{/if}
		{/if}
	{/snippet}
</PageHeader>

<div class="grid grid-cols-1 items-start gap-4 {canManage ? 'xl:grid-cols-[minmax(0,1fr)_27rem]' : ''}">
	<form
		class="flex min-w-0 flex-col gap-4"
		novalidate
		onsubmit={(e) => {
			e.preventDefault();
			if (canManage) save();
		}}
	>
		{#if !canManage}
			<p class="text-xs text-fg-subtle">
				{t('Nur lesen – dafür fehlt die Berechtigung „Regeln verwalten“.')}
			</p>
		{/if}
		<fieldset disabled={!canManage} class="contents">
			<Card title={t('Allgemein')} icon="rules">
				<div class="flex flex-col gap-4">
					<Input
						id="rule-name"
						label="Name"
						required
						bind:value={draft.name}
						error={err('name')}
						maxlength={200}
					/>
					<Textarea id="rule-desc" label={t('Beschreibung')} bind:value={draft.description} rows={2} />
					<div class="flex flex-col gap-3 sm:flex-row sm:gap-8">
						<Toggle
							bind:checked={draft.enabled}
							label={t('Aktiv')}
							description={t('Inaktive Regeln werden nicht ausgewertet.')}
						/>
						<Toggle
							bind:checked={draft.stop}
							label={t('Auswertung stoppen')}
							description={t('Greift diese Regel, werden nachfolgende Regeln nicht mehr ausgewertet.')}
						/>
					</div>
				</div>
			</Card>

			<Card
				title={t('Wenn …')}
				description={t('Alle gesetzten Bedingungen müssen erfüllt sein.')}
				icon="filter"
			>
				<div class="flex flex-col gap-6">
					<div class="grid grid-cols-1 gap-4 md:grid-cols-[minmax(0,1fr)_14rem]">
						<EventTypePicker
							id="rule-types"
							bind:value={draft.conditions.eventTypes}
							error={err('conditions.eventTypes')}
							hint={t('Leer = alle Event-Typen. Muster wie „port.*“ decken ganze Bereiche ab.')}
						/>
						<Select
							id="rule-sev"
							label={t('Mindest-Schweregrad')}
							options={severityOptions}
							placeholder={t('beliebig')}
							bind:value={draft.conditions.minSeverity}
							error={err('conditions.minSeverity')}
						/>
					</div>

					{#if federation.role === 'central' && (federation.sites.length || siteIds.length)}
						<MultiSelect
							id="rule-sites"
							label={t('Standorte (einer davon)')}
							options={siteOptions}
							bind:value={siteIds}
							onchange={(v) => (draft.conditions.sites = v.length ? v.map(Number) : undefined)}
							placeholder={t('alle Standorte')}
							hint={t('Events der Standorte kommen mit ihrem Gerät hierher; leer = Events von überall.')}
							error={err('conditions.sites')}
						/>
					{/if}

					<fieldset class="flex flex-col gap-3">
						<legend class="mb-1 text-sm font-semibold text-fg">{t('Gerät')}</legend>
						<p class="-mt-1 text-xs text-fg-subtle">
							{t('Gerätebedingungen gelten nur für Events mit Gerät; Events ohne Gerät passen dann nicht.')}
						</p>
						<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
							<TagInput
								id="rule-tags"
								label={t('Tags (eines davon)')}
								bind:value={draft.conditions.tags}
								suggestions={(tags.value ?? []).map((x) => x.tag)}
								normalize={(s) => s.trim().toLowerCase()}
								placeholder={t('Tag hinzufügen …')}
							/>
							<MultiSelect
								id="rule-groups"
								label={t('Gruppen (eine davon)')}
								options={groupOptions}
								bind:value={groupIds}
								onchange={(v) => (draft.conditions.groups = v.map(Number))}
								placeholder={groupOptions.length ? t('Gruppen wählen …') : t('Keine Gruppen angelegt')}
							/>
							<TagInput
								id="rule-subnets"
								label={t('Subnetze (eines davon)')}
								bind:value={draft.conditions.subnets}
								suggestions={(subnets.value ?? []).map((s) => s.cidr)}
								placeholder={t('CIDR, z. B. 192.168.1.0/24')}
								error={err('conditions.subnets')}
							/>
							<MultiSelect
								id="rule-states"
								label={t('Gerätezustand')}
								options={stateOptions}
								bind:value={draft.conditions.deviceStates}
								placeholder={t('beliebig')}
								hint={t('Ignorierte Geräte lösen nie Benachrichtigungen aus.')}
								error={err('conditions.deviceStates')}
							/>
						</div>
						<Checkbox
							id="rule-unknown"
							bind:checked={draft.conditions.onlyUnknown}
							label={t('Nur Geräte, die nicht als bekannt markiert sind')}
						/>
						<QueryInput
							id="rule-query"
							label={t('Geräte-Filter (Abfragesprache)')}
							showLabel
							bind:value={draft.conditions.deviceQuery}
							placeholder={t('z. B. tag:server crit>=high')}
						/>
					</fieldset>

					<fieldset class="flex flex-col gap-2">
						<legend class="mb-1 text-sm font-semibold text-fg">Payload</legend>
						<PayloadConditions bind:value={draft.conditions.payload} types={typesForPayload} {errors} />
					</fieldset>

					<fieldset class="flex flex-col gap-2">
						<legend class="mb-1 text-sm font-semibold text-fg">{t('Zeitfenster')}</legend>
						<Checkbox
							id="rule-tw"
							checked={!!draft.conditions.timeWindow}
							onchange={(e) => setTimeWindow((e.currentTarget as HTMLInputElement).checked)}
							label={t('Nur zu bestimmten Zeiten')}
							description={t('Außerhalb des Zeitfensters greift die Regel nicht (Zeitzone des Servers).')}
						/>
						{#if draft.conditions.timeWindow}
							{@const tw = draft.conditions.timeWindow}
							<div class="flex flex-col gap-3 sm:ml-6">
								<div role="group" aria-label={t('Wochentage')} class="flex flex-wrap gap-1">
									{#each WEEKDAYS as d (d.value)}
										{@const on = (tw.days ?? []).includes(d.value)}
										<button
											type="button"
											aria-pressed={on}
											title={d.long}
											onclick={() => toggleDay(d.value)}
											class="h-8 w-10 rounded-md border text-sm font-medium transition-colors
											{on
												? 'border-accent/50 bg-accent-soft text-accent'
												: 'border-border text-fg-muted hover:border-border-strong hover:text-fg'}"
										>
											{d.short}
										</button>
									{/each}
								</div>
								{#if err('conditions.timeWindow.days')}
									<p class="text-xs text-danger">{err('conditions.timeWindow.days')}</p>
								{/if}
								<p class="text-xs text-fg-subtle">
									{#if !(tw.days ?? []).length}{t('Kein Tag gewählt = jeden Tag.')}{/if}
									{t('Liegt „Bis“ vor „Von“, reicht das Fenster über Mitternacht.')}
								</p>
								<div class="grid grid-cols-2 gap-3 sm:w-80">
									<Input
										id="rule-tw-from"
										type="time"
										label={t('Von')}
										bind:value={tw.from}
										error={err('conditions.timeWindow.from')}
									/>
									<Input
										id="rule-tw-to"
										type="time"
										label={t('Bis')}
										bind:value={tw.to}
										error={err('conditions.timeWindow.to')}
									/>
								</div>
							</div>
						{/if}
					</fieldset>
				</div>
			</Card>

			<Card title={t('Dann …')} description={t('Aktionen werden der Reihe nach ausgeführt.')} icon="bell">
				<div class="flex flex-col gap-3">
					{#if errors.actions}<Alert tone="danger">{fieldMessage(errors.actions)}</Alert>{/if}
					{#if publishers.value && !pubs.some((p) => p.enabled)}
						<Alert tone="warn" title={t('Kein Publisher aktiv')}>
							{t(
								'Benachrichtigungen werden erst verschickt, wenn mindestens ein Publisher eingerichtet und aktiviert ist.'
							)}
							<a class="link" href="/plugins#kind-publisher">{t('Publisher einrichten')}</a>
						</Alert>
					{/if}
					{#each draft.actions as _, i (i)}
						<ActionEditor
							bind:action={draft.actions[i]}
							index={i}
							publishers={pubs}
							{errors}
							count={draft.actions.length}
							canRemove={draft.actions.length > 1}
							onremove={() => removeAction(i)}
							onmove={(dir) => moveAction(i, dir)}
						/>
					{/each}
					<div>
						<Button icon="plus" onclick={addAction}>{t('Aktion hinzufügen')}</Button>
					</div>
				</div>
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
						<Badge tone="warn" dot
							>{isNew ? t('Noch nicht gespeichert') : t('Ungespeicherte Änderungen')}</Badge
						>
					{:else}
						<span class="text-fg-subtle">{t('Gespeichert')}</span>
					{/if}
				</div>
				{#if !isNew}
					<Button onclick={discard} disabled={!dirty || saving} icon="refresh">{t('Zurücksetzen')}</Button>
				{:else}
					<Button href="/rules" variant="ghost">{t('Abbrechen')}</Button>
				{/if}
				<Button type="submit" variant="primary" icon="save" loading={saving} disabled={!isNew && !dirty}>
					{isNew ? t('Regel anlegen') : t('Speichern')}
				</Button>
			</div>
		{/if}
	</form>

	{#if canManage}
		<Card
			title={t('Event simulieren')}
			description={t('Prüft die Regel – auch ungespeichert – gegen ein Beispiel-Event.')}
			icon="play"
			class="xl:sticky xl:top-4"
		>
			<RuleSimulator rule={testRule} {blocked} />
		</Card>
	{/if}
</div>
