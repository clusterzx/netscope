<!-- System settings (GET/PUT /api/v1/system/settings) and the runtime log level. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { SystemSettings } from '$lib/api';
	import {
		Alert,
		Button,
		Card,
		ErrorState,
		Input,
		Select,
		Skeleton,
		TagInput,
		Toggle
	} from '$lib/components/ui';
	import { LOCALES, t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { meta } from '$lib/stores/catalog.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { deviceTypeName } from '$lib/utils/labels';
	import OrderedList from './OrderedList.svelte';
	import { apiErrors, LOG_LEVELS, logLevelLabel } from './system';

	const KNOWN_SOURCES = [
		'manual',
		'dhcp',
		'openwrt',
		'opnsense',
		'pfsense',
		'unifi',
		'mikrotik',
		'fortigate',
		'sophos',
		'meraki',
		'fritzbox',
		'pihole',
		'windows_dhcp',
		'ssh',
		'snmp',
		'proxmox',
		'dns',
		'mdns',
		'netbios',
		'upnp',
		'docker',
		'nmap',
		'netalertx',
		'csv',
		'http'
	];
	const sourceLabel: Record<string, string> = {
		manual: t('Manuell'),
		dhcp: 'DHCP',
		openwrt: 'OpenWrt',
		opnsense: 'OPNsense',
		pfsense: 'pfSense',
		unifi: 'UniFi',
		mikrotik: 'MikroTik',
		fortigate: 'FortiGate',
		sophos: 'Sophos',
		meraki: 'Meraki',
		fritzbox: 'FRITZ!Box',
		pihole: 'Pi-hole',
		windows_dhcp: t('Windows-DHCP'),
		ssh: 'SSH',
		snmp: 'SNMP',
		proxmox: 'Proxmox',
		dns: t('DNS (Reverse-Lookup)'),
		mdns: 'mDNS / Bonjour',
		netbios: 'NetBIOS',
		upnp: 'UPnP',
		docker: 'Docker',
		nmap: 'Nmap',
		netalertx: 'NetAlertX',
		csv: t('CSV-Import'),
		http: t('HTTP-Erkennung')
	};

	const canManage = $derived(auth.can('system.manage'));

	const data = new AsyncData<SystemSettings>();
	$effect(() => {
		data.run((signal) => api.get('/api/v1/system/settings', { signal }));
	});

	let form = $state<SystemSettings | null>(null);
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);

	$effect(() => {
		const d = data.data;
		if (d) form = structuredClone($state.snapshot(d)) as SystemSettings;
	});

	const dirty = $derived(!!form && !!data.data && JSON.stringify(form) !== JSON.stringify(data.data));

	function validate(f: SystemSettings): Record<string, string> {
		const e: Record<string, string> = {};
		if (f.publicUrl.trim()) {
			try {
				const u = new URL(f.publicUrl.trim());
				if ((u.protocol !== 'http:' && u.protocol !== 'https:') || !u.host) throw new Error();
			} catch {
				e.publicUrl = t('Gültige http(s)-URL erwartet, z. B. https://netscope.lan');
			}
		}
		const int = (v: unknown, lo: number, hi: number) =>
			typeof v === 'number' && Number.isInteger(v) && v >= lo && v <= hi;
		if (!int(f.offlineAfterMissed, 1, 100)) e.offlineAfterMissed = '1–100';
		if (!int(f.maxParallelRuns, 1, 64)) e.maxParallelRuns = '1–64';
		if (!int(f.observationRawMaxKb, 0, 16384)) e.observationRawMaxKb = '0–16384 KB';
		if (!f.deviceTypes?.length) e.deviceTypes = t('Mindestens ein Gerätetyp');
		if (!LOCALES.some((l) => l.value === f.language)) e.language = t('Sprache wählen');
		return e;
	}

	async function save() {
		if (!form) return;
		general = null;
		errors = validate(form);
		if (Object.keys(errors).length) return;
		saving = true;
		try {
			const body = { ...form, publicUrl: form.publicUrl.trim() };
			const saved = await api.put('/api/v1/system/settings', { body });
			data.set(saved);
			meta.refresh().catch(() => {});
			toast.success(t('Einstellungen gespeichert'));
		} catch (e) {
			({ errors, general } = apiErrors(e, [
				'publicUrl',
				'language',
				'offlineAfterMissed',
				'maxParallelRuns',
				'observationRawMaxKb',
				'hostnamePriority',
				'deviceTypes'
			]));
		} finally {
			saving = false;
		}
	}

	function reset() {
		if (data.data) form = structuredClone($state.snapshot(data.data)) as SystemSettings;
		errors = {};
		general = null;
	}

	// ---------------------------------------------------------------- log level
	let level = $state('');
	let levelLoaded = $state('');
	let levelBusy = $state(false);
	$effect(() => {
		api
			.get('/api/v1/system/info')
			.then((i) => {
				levelLoaded = (i.logLevel ?? '').toLowerCase();
				level = levelLoaded;
			})
			.catch(() => {});
	});

	async function saveLevel() {
		levelBusy = true;
		try {
			const res = await api.put('/api/v1/system/loglevel', { body: { level } });
			levelLoaded = res.level.toLowerCase();
			level = levelLoaded;
			toast.success(t('Log-Level ist jetzt „{level}“', { level: logLevelLabel[levelLoaded] ?? res.level }));
		} catch (e) {
			toast.error(e);
		} finally {
			levelBusy = false;
		}
	}
</script>

<div class="flex flex-col gap-4">
	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !form}
		<Card><Skeleton lines={8} /></Card>
	{:else}
		<form
			novalidate
			onsubmit={(e) => {
				e.preventDefault();
				if (canManage) save();
			}}
			class="flex flex-col gap-4"
		>
			{#if !canManage}
				<p class="text-xs text-fg-subtle">
					{t('Nur lesen – dafür fehlt die Berechtigung „Systemeinstellungen ändern“.')}
				</p>
			{/if}
			{#if general}<Alert tone="danger" title={t('Speichern fehlgeschlagen')}>{general}</Alert>{/if}

			<fieldset disabled={!canManage} class="contents">
				<Card title={t('Allgemein')} icon="globe">
					<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
						<Input
							label={t('Öffentliche URL')}
							type="url"
							bind:value={form.publicUrl}
							placeholder="https://netscope.example.lan"
							hint={t('Basis für Deep-Links in Benachrichtigungen und Berichten')}
							error={errors.publicUrl}
							class="md:col-span-2"
						/>
						<!-- the language names stay in their own language, so everyone finds theirs -->
						<Select
							label={t('Sprache für Benachrichtigungen und Berichte')}
							bind:value={form.language}
							options={LOCALES}
							hint={t(
								'Benachrichtigungen (E-Mail, Telegram, ntfy, Webhook …) und geplante Berichte werden in dieser Sprache verschickt. Die Oberfläche richtet sich nach der Einstellung jedes Benutzers.'
							)}
							error={errors.language}
							required
						/>
						<Input
							label={t('Offline nach verpassten Läufen')}
							type="number"
							min={1}
							max={100}
							bind:value={form.offlineAfterMissed}
							hint={t('Aufeinanderfolgende Präsenz-Läufe ohne Antwort, bis ein Gerät als offline gilt')}
							error={errors.offlineAfterMissed}
							required
						/>
						<Input
							label={t('Parallele Plugin-Läufe')}
							type="number"
							min={1}
							max={64}
							bind:value={form.maxParallelRuns}
							hint={t('Obergrenze gleichzeitig laufender Scans, Importe und Prozessoren')}
							error={errors.maxParallelRuns}
							required
						/>
						<Input
							label={t('Rohdaten je Beobachtung (KB)')}
							type="number"
							min={0}
							max={16384}
							bind:value={form.observationRawMaxKb}
							hint={t('Gespeicherte Rohausgabe pro Beobachtung (0 = keine Rohdaten)')}
							error={errors.observationRawMaxKb}
							required
						/>
						<div class="flex flex-col gap-1.5">
							<Toggle
								bind:checked={form.metricsPublic}
								label={t('Metriken öffentlich')}
								description={t('/metrics ohne Anmeldung ausliefern (für Prometheus ohne Token)')}
							/>
							{#if form.metricsPublic}
								<p class="text-xs text-warn">
									{t('Jeder im Netz kann dann Kennzahlen (Gerätezahlen, Plugin-Status) abrufen.')}
								</p>
							{/if}
						</div>
					</div>
				</Card>

				<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
					<Card title={t('Hostname-Priorität')} icon="sort">
						<OrderedList
							bind:value={form.hostnamePriority}
							label={t('Quellen (höchste zuerst)')}
							hint={t(
								'Der Hostname eines Geräts kommt aus der ersten Quelle der Liste, die einen liefert. Nicht gelistete Quellen folgen danach. Änderungen berechnen alle Gerätenamen neu.'
							)}
							suggestions={KNOWN_SOURCES}
							itemLabel={(v) => sourceLabel[v] ?? v}
							error={errors.hostnamePriority}
						/>
					</Card>
					<Card title={t('Gerätetypen')} icon="devices">
						<TagInput
							label={t('Auswählbare Typen')}
							bind:value={form.deviceTypes}
							suggestions={[
								'router',
								'switch',
								'access-point',
								'firewall',
								'server',
								'hypervisor',
								'vm',
								'container',
								'nas',
								'desktop',
								'laptop',
								'phone',
								'tablet',
								'tv',
								'media-player',
								'speaker',
								'printer',
								'camera',
								'smart-home',
								'iot',
								'game-console',
								'ups',
								'other'
							]}
							normalize={(s) => s.trim().toLowerCase().replace(/\s+/g, '-')}
							hint={t(
								'Kleinbuchstaben mit Bindestrich, z. B. access-point. Bekannte Typen haben deutsche Anzeigenamen.'
							)}
							error={errors.deviceTypes}
						/>
						<p class="mt-2 flex flex-wrap gap-1 text-xs text-fg-subtle">
							{#each form.deviceTypes as dt (dt)}<span class="rounded bg-surface-2 px-1.5 py-0.5"
									>{deviceTypeName(dt)}</span
								>{/each}
						</p>
					</Card>
				</div>
			</fieldset>

			{#if canManage}
				<div
					class="z-10 flex flex-wrap items-center justify-end gap-2 rounded-lg border border-border bg-surface/95 px-3 py-2 backdrop-blur
					{dirty ? 'sticky bottom-2 shadow-md' : ''}"
				>
					<span class="mr-auto text-sm {dirty ? 'text-warn' : 'text-fg-subtle'}">
						{dirty ? t('Ungespeicherte Änderungen') : t('Alle Änderungen gespeichert')}
					</span>
					<Button onclick={reset} disabled={!dirty || saving}>{t('Verwerfen')}</Button>
					<Button type="submit" variant="primary" icon="save" loading={saving} disabled={!dirty}
						>{t('Speichern')}</Button
					>
				</div>
			{/if}
		</form>
	{/if}

	<Card
		title={t('Log-Level')}
		description={t('Wirkt sofort, bis zum nächsten Neustart (Standard aus der Konfigurationsdatei)')}
		icon="terminal"
	>
		<div class="flex flex-wrap items-end gap-2">
			<Select
				label={t('Mindest-Level')}
				bind:value={level}
				options={LOG_LEVELS.map((l) => ({ value: l, label: logLevelLabel[l] }))}
				class="w-48"
				disabled={!canManage}
			/>
			{#if canManage}
				<Button onclick={saveLevel} loading={levelBusy} disabled={!level || level === levelLoaded}
					>{t('Übernehmen')}</Button
				>
			{/if}
			{#if auth.can('audit.view')}
				<Button variant="ghost" href="?tab=logs" iconRight="arrow-right">{t('Log-Viewer')}</Button>
			{/if}
		</div>
		{#if level === 'debug' && levelLoaded !== 'debug'}
			<p class="mt-2 text-xs text-fg-subtle">
				{t('Debug erzeugt deutlich mehr Protokollzeilen, auch im Log-Viewer.')}
			</p>
		{/if}
	</Card>
</div>
