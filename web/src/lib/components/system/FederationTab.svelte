<!-- Federation: role of this instance (standalone, site, central) and the connection of a
     site to its central instance (GET/PUT /api/v1/federation). -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { FederationRole, FederationTestResult, FederationView } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		Card,
		DescItem,
		DescList,
		ErrorState,
		Input,
		RelativeTime,
		Skeleton
	} from '$lib/components/ui';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { federation } from '$lib/stores/federation.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDateTime } from '$lib/utils/format';
	import { apiErrors } from './system';

	const data = new AsyncData<FederationView>();
	$effect(() => {
		data.run((signal) => api.get('/api/v1/federation', { signal }));
	});
	// the delivery state changes in the background: refresh on live messages and every 30 s
	$effect(() =>
		live.on('system', (m) => {
			if (m.type === 'federation' || m.type === 'sites') data.reload();
		})
	);
	$effect(() => {
		const t = setInterval(() => data.reload(), 30_000);
		return () => clearInterval(t);
	});

	let role = $state<FederationRole>('standalone');
	let localName = $state('');
	let centralUrl = $state('');
	let token = $state('');
	let fingerprint = $state('');
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	let testing = $state(false);
	let test = $state<FederationTestResult | null>(null);
	let syncing = $state(false);
	let loadedFor = $state('');

	// fill the form once per loaded settings (not on every background refresh)
	$effect(() => {
		const d = data.data;
		if (!d) return;
		const key = JSON.stringify(d.settings);
		if (key === loadedFor) return;
		loadedFor = key;
		role = (d.settings.role as FederationRole) || 'standalone';
		localName = d.settings.localName ?? '';
		centralUrl = d.settings.centralUrl ?? '';
		fingerprint = d.settings.fingerprint ?? '';
		token = '';
	});

	const current = $derived(data.data?.settings);
	const managed = $derived(!!current?.managed);
	const dirty = $derived(
		!!current &&
			(role !== current.role ||
				localName !== (current.localName ?? '') ||
				centralUrl !== (current.centralUrl ?? '') ||
				fingerprint !== (current.fingerprint ?? '') ||
				token !== '')
	);
	const site = $derived(data.data?.site);

	const ROLES: { value: FederationRole; label: string; text: string }[] = [
		{ value: 'standalone', label: 'Eigenständig', text: 'Diese Instanz arbeitet für sich (Standard).' },
		{
			value: 'site',
			label: 'Standort',
			text: 'Scannt ihr eigenes Netz und liefert alles an eine Zentrale. Zugangsdaten bleiben hier.'
		},
		{
			value: 'central',
			label: 'Zentrale',
			text: 'Nimmt die Daten von Standorten an und zeigt alle Netze gemeinsam oder einzeln.'
		}
	];

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		if (role === 'site') {
			try {
				const u = new URL(centralUrl.trim());
				if ((u.protocol !== 'http:' && u.protocol !== 'https:') || !u.host) throw new Error();
			} catch {
				e.centralUrl = 'Adresse der Zentrale, z. B. https://netscope.example.org';
			}
			if (!token.trim() && !current?.hasToken) e.token = 'Token des Standorts aus der Zentrale einfügen';
			else if (token.trim() && !token.trim().startsWith('nss_'))
				e.token = 'Standort-Tokens beginnen mit nss_';
			const fp = fingerprint.trim().toLowerCase().replace(/[:\s]/g, '');
			if (fp && !/^[0-9a-f]{64}$/.test(fp)) e.fingerprint = 'SHA-256-Fingerprint: 64 Hex-Zeichen';
		}
		return e;
	}

	async function save() {
		general = null;
		test = null;
		errors = validate();
		if (Object.keys(errors).length) return;
		if (current?.role === 'central' && role !== 'central' && (data.data?.sites.length ?? 0) > 0) {
			const ok = await confirm({
				title: 'Rolle „Zentrale“ aufgeben?',
				message:
					'Die Standorte können dann nichts mehr einliefern. Ihre bisher gelieferten Geräte und Events bleiben erhalten.',
				confirmLabel: 'Rolle ändern',
				danger: true
			});
			if (!ok) return;
		}
		saving = true;
		try {
			const body = {
				role,
				localName: localName.trim(),
				centralUrl: role === 'site' ? centralUrl.trim() : '',
				fingerprint: role === 'site' ? fingerprint.trim() : '',
				...(token.trim() ? { token: token.trim() } : {})
			};
			const saved = await api.put('/api/v1/federation', { body });
			data.set(saved);
			federation.refresh().catch(() => {});
			toast.success('Verbund-Einstellungen gespeichert');
			if (saved.settings.role === 'site') runTest();
		} catch (e) {
			({ errors, general } = apiErrors(e, ['role', 'localName', 'centralUrl', 'token', 'fingerprint']));
		} finally {
			saving = false;
		}
	}

	async function runTest() {
		testing = true;
		test = null;
		try {
			test = await api.post('/api/v1/federation/test');
			data.reload();
		} catch (e) {
			toast.error(e);
		} finally {
			testing = false;
		}
	}

	async function resync() {
		const ok = await confirm({
			title: 'Vollständigen Abgleich anstoßen?',
			message:
				'Der Standort schickt den kompletten Bestand erneut an die Zentrale. Das ist nur nötig, wenn die Zentrale einen Stand verloren hat (z. B. nach einer Wiederherstellung dort).',
			confirmLabel: 'Abgleich starten'
		});
		if (!ok) return;
		syncing = true;
		try {
			await api.post('/api/v1/federation/resync');
			toast.success('Vollständiger Abgleich vorbereitet');
			data.reload();
		} catch (e) {
			toast.error(e);
		} finally {
			syncing = false;
		}
	}
</script>

<div class="flex flex-col gap-4">
	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Card><Skeleton lines={6} /></Card>
	{:else}
		<form
			novalidate
			onsubmit={(e) => {
				e.preventDefault();
				save();
			}}
			class="flex flex-col gap-4"
		>
			{#if general}<Alert tone="danger" title="Speichern fehlgeschlagen">{general}</Alert>{/if}
			{#if managed}
				<Alert tone="info" title="Über Umgebungsvariablen festgelegt">
					Die Anbindung an die Zentrale kommt aus NETSCOPE_CENTRAL_URL und NETSCOPE_CENTRAL_TOKEN und kann
					hier nicht geändert werden.
				</Alert>
			{/if}

			<Card title="Rolle dieser Instanz" icon="globe">
				<fieldset disabled={managed}>
					<legend class="sr-only">Rolle</legend>
					<div class="grid grid-cols-1 gap-2 md:grid-cols-3">
						{#each ROLES as r (r.value)}
							<label
								class="flex cursor-pointer items-start gap-2 rounded-md border px-3 py-2.5 text-sm has-focus-visible:ring-2 has-focus-visible:ring-focus
									{role === r.value ? 'border-accent bg-accent-soft' : 'border-border hover:bg-surface-2'}"
							>
								<input
									type="radio"
									name="fed-role"
									value={r.value}
									bind:group={role}
									class="mt-0.5 accent-(--accent)"
								/>
								<span>
									<span class="block font-medium">{r.label}</span>
									<span class="block text-xs text-fg-subtle">{r.text}</span>
								</span>
							</label>
						{/each}
					</div>
				</fieldset>
				{#if role === 'central'}
					<div class="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
						<Input
							label="Name dieser Instanz"
							bind:value={localName}
							maxlength={64}
							placeholder="Zentrale"
							hint="So heißt diese Instanz in der Standort-Auswahl (z. B. „Zuhause“)."
							error={errors.localName}
						/>
					</div>
					<p class="mt-3 text-sm text-fg-muted">
						Standorte werden unter <a href="/sites" class="text-accent hover:underline">Standorte</a> angelegt;
						dort gibt es das Token für die Anbindung.
					</p>
				{/if}
			</Card>

			{#if role === 'site'}
				<Card title="Zentrale" icon="send">
					<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
						<Input
							label="Adresse der Zentrale"
							type="url"
							bind:value={centralUrl}
							placeholder="https://netscope.example.org"
							hint="Der Standort baut die Verbindung auf; hier sind keine Freigaben nötig."
							error={errors.centralUrl}
							disabled={managed}
							required
							class="md:col-span-2"
						/>
						<Input
							label="Token des Standorts"
							type="password"
							autocomplete="off"
							bind:value={token}
							placeholder={current?.hasToken ? 'gespeichert – leer lassen, um es zu behalten' : 'nss_…'}
							hint="Wird in der Zentrale beim Anlegen des Standorts einmalig angezeigt. Es erlaubt nur das Einliefern."
							error={errors.token}
							disabled={managed}
							mono
						/>
						<Input
							label="Zertifikat-Fingerprint (optional)"
							bind:value={fingerprint}
							placeholder="SHA-256, z. B. 3a:9f:…"
							hint="Nur für selbst signierte Zertifikate: dann gilt genau dieses Zertifikat statt der üblichen Prüfung."
							error={errors.fingerprint}
							disabled={managed}
							mono
						/>
					</div>
				</Card>
			{/if}

			{#if !managed}
				<div
					class="z-10 flex flex-wrap items-center justify-end gap-2 rounded-lg border border-border bg-surface/95 px-3 py-2 backdrop-blur
						{dirty ? 'sticky bottom-2 shadow-md' : ''}"
				>
					<span class="mr-auto text-sm {dirty ? 'text-warn' : 'text-fg-subtle'}">
						{dirty ? 'Ungespeicherte Änderungen' : 'Alle Änderungen gespeichert'}
					</span>
					<Button type="submit" variant="primary" icon="save" loading={saving} disabled={!dirty}
						>Speichern</Button
					>
				</div>
			{/if}
		</form>

		{#if current?.role === 'site'}
			<Card title="Verbindung zur Zentrale" icon="activity">
				{#snippet actions()}
					<Button size="sm" icon="refresh" onclick={resync} loading={syncing}>Vollabgleich</Button>
					<Button size="sm" variant="primary" icon="zap" onclick={runTest} loading={testing}
						>Verbindung testen</Button
					>
				{/snippet}
				{#if test}
					<Alert
						tone={test.ok ? 'ok' : 'danger'}
						class="mb-4"
						title={test.ok ? 'Verbunden' : 'Keine Verbindung'}
					>
						{#if test.ok}
							Die Zentrale kennt diesen Standort als „{test.site}“ (Antwort in {test.durationMs} ms).
						{:else}
							{test.error}
						{/if}
					</Alert>
				{/if}
				{#if site}
					<DescList cols={3}>
						<DescItem label="Zustand">
							{#if site.connected}
								<Badge tone="ok" dot>verbunden</Badge>
							{:else if site.lastAttempt}
								<Badge tone="danger" dot>getrennt</Badge>
							{:else}
								<Badge tone="neutral">noch kein Kontakt</Badge>
							{/if}
							{#if site.syncing}<Badge tone="info" class="ml-1">Abgleich läuft</Badge>{/if}
						</DescItem>
						<DescItem label="Name in der Zentrale" value={site.siteName} />
						<DescItem label="Letzte Zustellung">
							{#if site.lastSuccess}<RelativeTime value={site.lastSuccess} />{:else}<span
									class="text-fg-subtle">–</span
								>{/if}
						</DescItem>
						<DescItem label="Im Puffer" hint={site.buffered ? 'Einträge warten auf Zustellung' : undefined}>
							<span class="tabular">{site.buffered.toLocaleString('de-DE')}</span>
						</DescItem>
						<DescItem label="Ältester Eintrag">
							{#if site.oldestItem}<RelativeTime value={site.oldestItem} />{:else}<span class="text-fg-subtle"
									>–</span
								>{/if}
						</DescItem>
						<DescItem label="Nächster Versuch">
							{#if site.nextAttempt}{formatDateTime(site.nextAttempt)}{:else}<span class="text-fg-subtle"
									>–</span
								>{/if}
						</DescItem>
					</DescList>
					{#if site.lastError}
						<Alert tone="warn" class="mt-4" title="Letzter Fehler">{site.lastError}</Alert>
					{/if}
					<p class="mt-4 text-xs text-fg-subtle">
						Ist die Zentrale nicht erreichbar, sammelt der Standort Beobachtungen und Events und liefert sie
						nach. Dieser Standort arbeitet unabhängig davon vollständig weiter.
					</p>
				{/if}
			</Card>
		{/if}
	{/if}
</div>
