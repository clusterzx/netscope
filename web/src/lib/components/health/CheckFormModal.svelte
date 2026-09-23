<!--
	Create/edit dialog for a health check.
	<CheckFormModal bind:open check={editing} preset={{ deviceId: 12 }} onsaved={(c) => …} />
	check = null creates a new check; preset pre-fills fields of a new check.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { HealthCheck } from '$lib/api';
	import { Alert, Button, Checkbox, Icon, Input, Modal, Select, Toggle } from '$lib/components/ui';
	import { toast } from '$lib/stores/toast.svelte';
	import DevicePicker from './DevicePicker.svelte';
	import {
		CHECK_TYPES,
		checkTypeDescription,
		checkTypeLabel,
		defaultPort,
		ADVANCED_FIELDS,
		emptyForm,
		formErrors,
		formFromCheck,
		payloadFromForm,
		validateForm,
		type CheckForm,
		type CheckType
	} from './health';

	interface Props {
		open?: boolean;
		check?: HealthCheck | null;
		preset?: Partial<CheckForm>;
		onsaved?: (c: HealthCheck) => void;
		onclose?: () => void;
	}

	let { open = $bindable(false), check = null, preset, onsaved, onclose }: Props = $props();

	const uid = $props.id();
	let form = $state<CheckForm>(emptyForm());
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	let advanced = $state(false);
	let nameTouched = $state(false);

	// reset the form whenever the dialog opens
	$effect(() => {
		if (!open) return;
		untrack(() => {
			form = check ? formFromCheck(check) : { ...emptyForm(), ...(preset ?? {}) };
			if (!check && form.port === null) form.port = defaultPort(form.type);
			errors = {};
			general = null;
			nameTouched = !!check || !!preset?.name;
			advanced = false;
		});
	});

	const usesPort = $derived(
		form.type === 'tcp' || form.type === 'tls' || (form.type === 'http' && !form.url.trim())
	);

	function suggestName(): string {
		const who = form.deviceName || form.target.trim() || (form.type === 'http' ? form.url.trim() : '');
		if (!who) return '';
		const what =
			form.type === 'icmp'
				? 'Ping'
				: form.type === 'http'
					? form.url.trim()
						? 'HTTP'
						: `HTTP ${form.port ?? ''}`.trim()
					: `${checkTypeLabel[form.type]} ${form.port ?? ''}`.trim();
		return form.type === 'http' && form.url.trim() && !form.deviceName ? `HTTP ${who}` : `${who} ${what}`;
	}

	// keep a generated name in sync until the user edits it
	$effect(() => {
		const s = suggestName();
		untrack(() => {
			if (!nameTouched && open) form.name = s;
		});
	});

	function setType(t: CheckType) {
		const prevDefault = defaultPort(form.type);
		form.type = t;
		if (form.port === null || form.port === prevDefault) form.port = defaultPort(t);
		if (t === 'icmp' && !form.count) form.count = 3;
		delete errors.type;
		delete errors.port;
	}

	/** Form fields rendered for the current type (for error display). */
	function visibleFields(): string[] {
		const common = ['name', 'type', 'target', 'deviceId', 'degradedMs', ...ADVANCED_FIELDS];
		if (form.type === 'http') return [...common, 'port', 'url', 'method', 'expectStatus', 'bodyMatch'];
		if (form.type === 'tls') return [...common, 'port', 'minDays'];
		if (form.type === 'icmp') return [...common, 'count'];
		return [...common, 'port'];
	}

	function onDevice() {
		// without an explicit target the check follows the device's primary IP
		delete errors.deviceId;
		delete errors.target;
	}

	async function save() {
		general = null;
		errors = validateForm(form);
		if (Object.keys(errors).length) {
			if (ADVANCED_FIELDS.some((k) => errors[k])) advanced = true;
			return;
		}
		saving = true;
		try {
			const body = payloadFromForm(form) as HealthCheck;
			const saved = check
				? await api.put('/api/v1/health-checks/{id}', { path: { id: check.id }, body })
				: await api.post('/api/v1/health-checks', { body });
			toast.success(check ? 'Health-Check gespeichert' : 'Health-Check angelegt');
			open = false;
			onsaved?.(saved);
		} catch (e) {
			errors = formErrors(fieldErrors(e));
			if (ADVANCED_FIELDS.some((k) => errors[k])) advanced = true;
			// errors without a visible field (or without field list) go into the alert
			const hidden = Object.entries(errors).filter(([k]) => !visibleFields().includes(k));
			if (!Object.keys(errors).length) general = errorMessage(e);
			else if (hidden.length) general = hidden.map(([, m]) => m).join(' · ');
		} finally {
			saving = false;
		}
	}
</script>

<Modal
	bind:open
	title={check ? 'Health-Check bearbeiten' : 'Health-Check anlegen'}
	description={check ? check.name : 'Prüft einen Dienst regelmäßig und meldet Zustandswechsel als Event.'}
	size="lg"
	as="form"
	onsubmit={save}
	busy={saving}
	{onclose}
>
	<div class="flex flex-col gap-4">
		{#if general}
			<Alert tone="danger" title="Speichern fehlgeschlagen">{general}</Alert>
		{/if}

		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			<DevicePicker
				bind:value={form.deviceId}
				bind:name={form.deviceName}
				error={errors.deviceId}
				hint="Optional – ohne Ziel wird die primäre IP des Geräts geprüft."
				onselect={onDevice}
			/>
			<Input
				label="Name"
				bind:value={form.name}
				required
				maxlength={120}
				error={errors.name}
				oninput={() => {
					nameTouched = true;
					delete errors.name;
				}}
			/>
		</div>

		<fieldset>
			<legend class="mb-1.5 text-[0.8125rem] font-medium text-fg">Typ</legend>
			<div class="grid grid-cols-2 gap-2 sm:grid-cols-4" role="radiogroup" aria-label="Typ">
				{#each CHECK_TYPES as t (t)}
					<label
						class="flex cursor-pointer flex-col gap-0.5 rounded-md border px-3 py-2 text-sm transition-colors has-focus-visible:ring-2 has-focus-visible:ring-focus
							{form.type === t
							? 'border-accent bg-accent-soft'
							: 'border-border hover:border-border-strong hover:bg-surface-2'}"
					>
						<input
							type="radio"
							name="{uid}-type"
							value={t}
							checked={form.type === t}
							onchange={() => setType(t)}
							class="sr-only"
						/>
						<span class="font-medium {form.type === t ? 'text-accent' : 'text-fg'}">{checkTypeLabel[t]}</span>
						<span class="text-xs leading-snug text-fg-subtle">{checkTypeDescription[t]}</span>
					</label>
				{/each}
			</div>
			{#if errors.type}<p class="mt-1 text-xs text-danger" role="alert">{errors.type}</p>{/if}
		</fieldset>

		{#if form.type === 'http'}
			<Input
				label="URL"
				type="url"
				bind:value={form.url}
				mono
				placeholder="https://nas.lan:5001/"
				hint="Optional – ersetzt Ziel und Port (Schema, Host, Port und Pfad)."
				error={errors.url}
				oninput={() => delete errors.url}
			/>
		{/if}

		<div class="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_9rem]">
			<Input
				label="Ziel (Host/IP)"
				bind:value={form.target}
				mono
				placeholder={form.deviceId ? 'primäre IP des Geräts' : '192.168.1.10 oder nas.lan'}
				disabled={form.type === 'http' && !!form.url.trim()}
				error={errors.target}
				oninput={() => delete errors.target}
			/>
			{#if form.type !== 'icmp'}
				<Input
					label="Port"
					type="number"
					bind:value={form.port}
					min={1}
					max={65535}
					required={usesPort}
					disabled={!usesPort}
					error={errors.port}
					oninput={() => delete errors.port}
				/>
			{/if}
		</div>

		{#if form.type === 'http'}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
				<Select label="Methode" bind:value={form.method} options={['GET', 'HEAD']} error={errors.method} />
				<Input
					label="Erwarteter Status"
					bind:value={form.expectStatus}
					mono
					placeholder="200-399"
					hint="Bereiche/Liste, z. B. 200-299,301"
					error={errors.expectStatus}
					oninput={() => delete errors.expectStatus}
				/>
				<Input
					label="Body-Regex"
					bind:value={form.bodyMatch}
					mono
					placeholder="optional"
					error={errors.bodyMatch}
					oninput={() => delete errors.bodyMatch}
				/>
			</div>
			<div class="flex flex-wrap gap-x-6 gap-y-2">
				<Checkbox
					bind:checked={form.verifyTls}
					label="Zertifikat prüfen"
					description="Kette und Hostname bei https:// validieren"
				/>
				<Checkbox bind:checked={form.followRedirects} label="Weiterleitungen folgen" />
			</div>
		{:else if form.type === 'tls'}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
				<Input
					label="Servername (SNI)"
					bind:value={form.serverName}
					mono
					placeholder="optional, z. B. nas.example.org"
				/>
				<Input
					label="Mindestlaufzeit Zertifikat (Tage)"
					type="number"
					bind:value={form.minDays}
					min={0}
					max={3650}
					hint="Beeinträchtigt, wenn das Zertifikat früher abläuft (0 = aus)"
					error={errors.minDays}
				/>
			</div>
			<Checkbox
				bind:checked={form.verifyTls}
				label="Zertifikatskette prüfen"
				description="Selbstsignierte Zertifikate gelten dann als Fehler"
			/>
		{:else if form.type === 'icmp'}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
				<Input
					label="Anzahl Pings"
					type="number"
					bind:value={form.count}
					min={1}
					max={20}
					hint="1–20 Pakete je Prüfung"
					error={errors.count}
				/>
			</div>
		{/if}

		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			<Input
				label="Beeinträchtigt ab (ms)"
				type="number"
				bind:value={form.degradedMs}
				min={0}
				placeholder="aus"
				hint="Latenz, ab der der Check als beeinträchtigt gilt"
				error={errors.degradedMs}
			/>
			<div class="flex items-end pb-1.5">
				<Toggle bind:checked={form.enabled} label="Aktiv" description="Check wird im Intervall ausgeführt" />
			</div>
		</div>

		<div class="rounded-md border border-border">
			<button
				type="button"
				class="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-sm font-medium text-fg hover:bg-surface-2"
				aria-expanded={advanced}
				aria-controls="{uid}-adv"
				onclick={() => (advanced = !advanced)}
			>
				<span>
					Intervall & Schwellwerte
					<span class="ml-1 font-normal text-fg-subtle">
						{form.intervalSeconds ?? '–'} s · Timeout {form.timeoutSeconds ?? '–'} s · {form.failThreshold ??
							'–'}× Fehler / {form.recoverThreshold ?? '–'}× OK
					</span>
				</span>
				<Icon
					name="chevron-down"
					size={15}
					class="text-fg-subtle transition-transform {advanced ? 'rotate-180' : ''}"
				/>
			</button>
			<div
				id="{uid}-adv"
				hidden={!advanced}
				class="grid grid-cols-1 gap-4 border-t border-border p-3 sm:grid-cols-2"
			>
				<Input
					label="Intervall (Sekunden)"
					type="number"
					bind:value={form.intervalSeconds}
					min={30}
					max={86400}
					required
					hint="30 s bis 24 h"
					error={errors.intervalSeconds}
				/>
				<Input
					label="Timeout (Sekunden)"
					type="number"
					bind:value={form.timeoutSeconds}
					min={1}
					max={120}
					required
					hint="1–120 s"
					error={errors.timeoutSeconds}
				/>
				<Input
					label="Down nach Fehlschlägen"
					type="number"
					bind:value={form.failThreshold}
					min={1}
					max={20}
					required
					hint="Aufeinanderfolgende Fehler bis „Down“ (Flap-Dämpfung)"
					error={errors.failThreshold}
				/>
				<Input
					label="Up nach Erfolgen"
					type="number"
					bind:value={form.recoverThreshold}
					min={1}
					max={20}
					required
					hint="Aufeinanderfolgende Erfolge bis „Up“"
					error={errors.recoverThreshold}
				/>
			</div>
		</div>
	</div>

	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={saving}>Abbrechen</Button>
		<Button type="submit" variant="primary" loading={saving} icon="save">
			{check ? 'Speichern' : 'Anlegen'}
		</Button>
	{/snippet}
</Modal>
