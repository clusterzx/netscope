<!--
	Create a health check for a device (POST /api/v1/health-checks with deviceId).
	Types tcp / http / tls / icmp with their specific options (see healthcheck/store.go).
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { DeviceDetail, HealthCheck, HealthCheckConfig } from '$lib/api/types';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Checkbox from '$lib/components/ui/Checkbox.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import Toggle from '$lib/components/ui/Toggle.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { t } from '$lib/i18n';

	interface Props {
		open: boolean;
		device: DeviceDetail;
		oncreated: (c: HealthCheck) => void;
	}

	let { open = $bindable(false), device, oncreated }: Props = $props();

	type CheckType = 'tcp' | 'http' | 'tls' | 'icmp';
	const TYPES: { value: CheckType; label: string }[] = [
		{ value: 'tcp', label: t('TCP-Verbindung') },
		{ value: 'http', label: t('HTTP(S)-Abruf') },
		{ value: 'tls', label: t('TLS-Handshake / Zertifikat') },
		{ value: 'icmp', label: 'Ping (ICMP)' }
	];
	const typeShort: Record<CheckType, string> = { tcp: 'TCP', http: 'HTTP', tls: 'TLS', icmp: 'Ping' };

	let type = $state<CheckType>('tcp');
	let name = $state('');
	let nameTouched = $state(false);
	let target = $state('');
	let port = $state<number | null>(null);
	let url = $state('');
	let method = $state('GET');
	let expectStatus = $state('');
	let bodyMatch = $state('');
	let verifyTls = $state(false);
	let followRedirects = $state(true);
	let serverName = $state('');
	let minDays = $state<number | null>(14);
	let count = $state<number | null>(3);
	let degradedMs = $state<number | null>(null);
	let interval = $state<number | null>(60);
	let timeout = $state<number | null>(10);
	let failThreshold = $state<number | null>(3);
	let recoverThreshold = $state<number | null>(2);
	let enabled = $state(true);
	let advanced = $state(false);

	let errors = $state<Record<string, string>>({});
	let error = $state('');
	let busy = $state(false);

	const devName = $derived(device.name || device.ip || t('Gerät {id}', { id: device.id }));
	const openPorts = $derived([
		...new Set((device.ports ?? []).filter((p) => p.endsWith('/tcp')).map((p) => Number(p.split('/')[0])))
	]);

	function defaultPort(ct: CheckType): number | null {
		const has = (p: number) => openPorts.includes(p);
		if (ct === 'http') return has(80) ? 80 : has(8080) ? 8080 : has(443) ? 443 : 80;
		if (ct === 'tls') return has(443) ? 443 : has(8443) ? 8443 : 443;
		if (ct === 'tcp') return openPorts[0] ?? 22;
		return null;
	}

	function reset() {
		type = openPorts.length ? 'tcp' : 'icmp';
		port = defaultPort(type);
		nameTouched = false;
		target = '';
		url = '';
		method = 'GET';
		expectStatus = '';
		bodyMatch = '';
		verifyTls = false;
		followRedirects = true;
		serverName = '';
		minDays = 14;
		count = 3;
		degradedMs = null;
		interval = 60;
		timeout = 10;
		failThreshold = 3;
		recoverThreshold = 2;
		enabled = true;
		advanced = false;
		errors = {};
		error = '';
	}

	// reset on open only (not when the device refreshes while the dialog is open)
	$effect(() => {
		if (open) untrack(reset);
	});

	// name follows type/port until the user edits it
	$effect(() => {
		const auto = `${typeShort[type]}${type !== 'icmp' && port && !(type === 'http' && url) ? ` ${port}` : ''} · ${devName}`;
		if (!nameTouched) name = auto;
	});

	function changeType(v: string) {
		type = v as CheckType;
		port = defaultPort(type);
		errors = {};
	}

	const intIn = (v: number | null, lo: number, hi: number) =>
		v !== null && Number.isInteger(v) && v >= lo && v <= hi;

	function statusValid(s: string): boolean {
		if (!s.trim()) return true;
		return s.split(',').every((part) => {
			const m = /^\s*(\d{3})\s*(?:-\s*(\d{3})\s*)?$/.exec(part);
			if (!m) return false;
			const a = Number(m[1]);
			const b = m[2] ? Number(m[2]) : a;
			return a >= 100 && b <= 599 && a <= b;
		});
	}

	function validate(): boolean {
		const e: Record<string, string> = {};
		if (!name.trim()) e.name = t('Name erforderlich');
		if (target.trim() && /[\s/]/.test(target.trim())) e.target = t('Hostname oder IP ohne Leerzeichen/Pfad');
		if (type === 'tcp' || type === 'tls') {
			if (!intIn(port, 1, 65535)) e.port = 'Port 1–65535';
		}
		if (type === 'http') {
			if (url.trim()) {
				try {
					const u = new URL(url.trim());
					if (u.protocol !== 'http:' && u.protocol !== 'https:')
						e['config.url'] = t('URL muss mit http:// oder https:// beginnen');
				} catch {
					e['config.url'] = t('Ungültige URL (http:// oder https://)');
				}
			} else if (!intIn(port, 1, 65535)) e.port = t('Port 1–65535 oder URL angeben');
			if (!statusValid(expectStatus)) e['config.expectStatus'] = t('z. B. 200-399 oder 200,204');
			if (bodyMatch) {
				try {
					new RegExp(bodyMatch);
				} catch {
					e['config.bodyMatch'] = t('Ungültiger regulärer Ausdruck');
				}
			}
		}
		if (type === 'tls' && minDays !== null && !intIn(minDays, 0, 3650))
			e['config.minDays'] = t('0–3650 Tage');
		if (type === 'icmp' && !intIn(count, 1, 20)) e['config.count'] = '1–20 Pings';
		if (degradedMs !== null && !intIn(degradedMs, 0, 600000)) e['config.degradedMs'] = t('Millisekunden ≥ 0');
		if (!intIn(interval, 30, 86400)) e.intervalSeconds = t('30 Sekunden bis 24 Stunden');
		if (!intIn(timeout, 1, 120)) e.timeoutSeconds = t('1–120 Sekunden');
		if (!intIn(failThreshold, 1, 20)) e.failThreshold = '1–20';
		if (!intIn(recoverThreshold, 1, 20)) e.recoverThreshold = '1–20';
		errors = e;
		if (ADVANCED.some((k) => e[k])) advanced = true;
		return Object.keys(e).length === 0;
	}

	const ADVANCED = ['intervalSeconds', 'timeoutSeconds', 'failThreshold', 'recoverThreshold'];

	/** field keys (server names) that have an input in the current form state */
	function visibleFields(): Set<string> {
		const keys = ['name', 'type', 'config.degradedMs', ...ADVANCED];
		if (type !== 'http' || !url.trim()) keys.push('target');
		if (type !== 'icmp' && (type !== 'http' || !url.trim())) keys.push('port');
		if (type === 'http') keys.push('config.url', 'config.method', 'config.expectStatus', 'config.bodyMatch');
		if (type === 'tls') keys.push('config.minDays');
		if (type === 'icmp') keys.push('config.count');
		return new Set(keys);
	}

	async function submit() {
		if (!validate()) return;
		const cfg: HealthCheckConfig = {};
		if (degradedMs) cfg.degradedMs = degradedMs;
		if (type === 'http') {
			if (url.trim()) cfg.url = url.trim();
			cfg.method = method;
			if (expectStatus.trim()) cfg.expectStatus = expectStatus.trim();
			if (bodyMatch) cfg.bodyMatch = bodyMatch;
			cfg.verifyTls = verifyTls;
			cfg.followRedirects = followRedirects;
		} else if (type === 'tls') {
			if (serverName.trim()) cfg.serverName = serverName.trim();
			if (minDays) cfg.minDays = minDays;
			cfg.verifyTls = verifyTls;
		} else if (type === 'icmp') {
			cfg.count = count ?? 3;
		}
		// id, state and timestamps are set by the server
		const body = {
			deviceId: device.id,
			name: name.trim(),
			type,
			target: target.trim(),
			port: type === 'icmp' ? 0 : (port ?? 0),
			config: cfg,
			intervalSeconds: interval ?? 60,
			timeoutSeconds: timeout ?? 10,
			failThreshold: failThreshold ?? 3,
			recoverThreshold: recoverThreshold ?? 2,
			enabled
		} as HealthCheck;
		busy = true;
		error = '';
		try {
			const created = await api.post('/api/v1/health-checks', { body });
			toast.success(t('Health-Check „{name}“ angelegt', { name: created.name }));
			open = false;
			oncreated(created);
		} catch (e) {
			// validation errors (400, code "validation") arrive per field in error.fields
			const fe = fieldErrors(e);
			errors = fe;
			if (ADVANCED.some((k) => fe[k])) advanced = true;
			const shown = visibleFields();
			const other = Object.entries(fe).filter(([k]) => !shown.has(k));
			if (!Object.keys(fe).length) error = errorMessage(e);
			else if (other.length) error = other.map(([, m]) => m).join(' · ');
		} finally {
			busy = false;
		}
	}
</script>

<Modal
	bind:open
	title={t('Health-Check anlegen')}
	description={t('Für „{name}“ – Zustand Up/Down/Beeinträchtigt mit Verfügbarkeit und Ausfallhistorie.', {
		name: devName
	})}
	size="md"
	as="form"
	onsubmit={submit}
	{busy}
>
	<div class="flex flex-col gap-3.5">
		{#if error}<Alert tone="danger">{error}</Alert>{/if}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<Select
				label={t('Art')}
				value={type}
				options={TYPES}
				error={errors.type}
				onchange={(e) => changeType((e.currentTarget as HTMLSelectElement).value)}
			/>
			<Input
				label="Name"
				bind:value={name}
				error={errors.name}
				required
				maxlength={120}
				oninput={() => (nameTouched = true)}
			/>
		</div>

		{#if type !== 'http' || !url.trim()}
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_8rem]">
				<Input
					label={t('Ziel (Host/IP)')}
					bind:value={target}
					error={errors.target}
					mono
					placeholder={device.ip || t('Hostname oder IP')}
					hint={t('Leer = immer die aktuelle IP des Geräts')}
				/>
				{#if type !== 'icmp'}
					<Input
						label="Port"
						type="number"
						bind:value={port}
						error={errors.port}
						min={1}
						max={65535}
						required={type !== 'http'}
						list="hc-ports"
						mono
					/>
					<datalist id="hc-ports">
						{#each openPorts as p (p)}<option value={p}></option>{/each}
					</datalist>
				{/if}
			</div>
		{/if}

		{#if type === 'http'}
			<Input
				label="URL (optional)"
				type="url"
				bind:value={url}
				error={errors['config.url']}
				mono
				placeholder="https://{device.ip || 'host'}/health"
				hint={t('Ersetzt Ziel und Port, z. B. für einen bestimmten Pfad')}
			/>
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<Select
					label={t('Methode')}
					bind:value={method}
					options={['GET', 'HEAD']}
					error={errors['config.method']}
				/>
				<Input
					label={t('Erwarteter Status')}
					bind:value={expectStatus}
					error={errors['config.expectStatus']}
					placeholder="200-399"
					mono
					hint={t('Bereiche oder Liste, z. B. 200,204')}
				/>
			</div>
			<Input
				label={t('Body muss enthalten (Regex, optional)')}
				bind:value={bodyMatch}
				error={errors['config.bodyMatch']}
				mono
				placeholder={t('z. B. {example}', { example: '\\bOK\\b' })}
			/>
			<div class="flex flex-col gap-2 sm:flex-row sm:gap-6">
				<Checkbox bind:checked={followRedirects} label={t('Weiterleitungen folgen')} />
				<Checkbox bind:checked={verifyTls} label={t('TLS-Zertifikat prüfen')} />
			</div>
		{:else if type === 'tls'}
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<Input
					label={t('Servername (SNI, optional)')}
					bind:value={serverName}
					mono
					placeholder={device.hostname || 'example.lan'}
				/>
				<Input
					label={t('Beeinträchtigt, wenn Ablauf in weniger als')}
					type="number"
					bind:value={minDays}
					error={errors['config.minDays']}
					min={0}
					hint={t('Tagen (0 = aus)')}
				/>
			</div>
			<Checkbox bind:checked={verifyTls} label={t('Zertifikatskette prüfen (Down bei ungültiger Kette)')} />
		{:else if type === 'icmp'}
			<Input
				label={t('Anzahl Pings pro Prüfung')}
				type="number"
				bind:value={count}
				error={errors['config.count']}
				min={1}
				max={20}
				class="sm:w-60"
			/>
		{/if}

		<Input
			label={t('Beeinträchtigt ab Latenz (ms, optional)')}
			type="number"
			bind:value={degradedMs}
			error={errors['config.degradedMs']}
			min={0}
			placeholder={t('aus')}
			class="sm:w-60"
		/>

		<div class="rounded-md border border-border">
			<button
				type="button"
				class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm font-medium hover:bg-surface-2"
				aria-expanded={advanced}
				onclick={() => (advanced = !advanced)}
			>
				<Icon name={advanced ? 'chevron-down' : 'chevron-right'} size={14} class="text-fg-subtle" />
				{t('Intervall & Schwellwerte')}
				<span class="ml-auto text-xs font-normal text-fg-subtle">
					{t('alle {interval} s · Timeout {timeout} s', {
						interval: interval ?? '–',
						timeout: timeout ?? '–'
					})}
				</span>
			</button>
			{#if advanced}
				<div class="grid grid-cols-2 gap-3 border-t border-border p-3">
					<Input
						label={t('Intervall (s)')}
						type="number"
						bind:value={interval}
						error={errors.intervalSeconds}
						min={30}
						max={86400}
					/>
					<Input
						label="Timeout (s)"
						type="number"
						bind:value={timeout}
						error={errors.timeoutSeconds}
						min={1}
						max={120}
					/>
					<Input
						label={t('Down nach Fehlern')}
						type="number"
						bind:value={failThreshold}
						error={errors.failThreshold}
						min={1}
						max={20}
						hint={t('aufeinanderfolgend')}
					/>
					<Input
						label={t('Up nach Erfolgen')}
						type="number"
						bind:value={recoverThreshold}
						error={errors.recoverThreshold}
						min={1}
						max={20}
						hint={t('Flap-Dämpfung')}
					/>
				</div>
			{/if}
		</div>
		<Toggle
			bind:checked={enabled}
			label={t('Aktiv')}
			description={t('Deaktivierte Checks werden nicht ausgeführt.')}
		/>
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>{t('Abbrechen')}</Button>
		<Button type="submit" variant="primary" icon="plus" loading={busy}>{t('Anlegen')}</Button>
	{/snippet}
</Modal>
