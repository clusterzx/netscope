<!--
	Agents: systems with the NetScope agent (status, version, last inventory), the install
	command for Linux and Windows and the installation tokens.
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { Agent, AgentEnrollment, AgentsResponse, EnrollmentCreated } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		Card,
		CopyButton,
		EmptyState,
		ErrorState,
		Input,
		Modal,
		PageHeader,
		RelativeTime,
		Select,
		StatusDot,
		Table,
		TagInput,
		Toggle
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { apiErrors } from '$lib/components/system/system';
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatBytes, formatDate, formatDateTime } from '$lib/utils/format';

	const canManage = $derived(auth.can('agents.manage'));
	const data = new AsyncData<AgentsResponse>();
	const tokens = new AsyncData<AgentEnrollment[]>();
	$effect(() => {
		data.run((signal) => api.get('/api/v1/agents', { signal }));
	});
	$effect(() => {
		if (canManage)
			tokens.run(async (signal) => (await api.get('/api/v1/agent-enrollments', { signal })) ?? []);
	});
	// status depends on the time of the last contact: refresh now and then
	$effect(() => {
		const t = setInterval(() => data.reload(), 60_000);
		const off = live.on('agent', () => {
			data.reload();
			if (canManage) tokens.reload();
		});
		return () => {
			clearInterval(t);
			off();
		};
	});

	const agents = $derived(data.data?.agents ?? []);
	const binaries = $derived(data.data?.binaries ?? []);
	const online = $derived(agents.filter((a) => a.online).length);
	/** address the systems reach this instance at (editable in the install dialog) */
	let base = $state('');
	const insecure = $derived(base.startsWith('http://'));

	// ---------------------------------------------------------------- install command
	let open = $state(false);
	let name = $state('Agents');
	let tags = $state<string[]>([]);
	let validity = $state('30');
	let uses = $state('unlimited');
	let customUses = $state('10');
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	let created = $state<EnrollmentCreated | null>(null);
	let docker = $state(false);
	/** target system of the install command */
	let platform = $state<'linux' | 'windows'>('linux');

	const PLATFORMS = [
		{ id: 'linux', label: 'Linux' },
		{ id: 'windows', label: 'Windows' }
	] as const;

	/** install command for the edited address (the same one-liners the API returns) */
	function installCommand(token: string): string {
		const b = base.replace(/\/+$/, '');
		if (platform === 'linux')
			return `curl -fsSL ${b}/agent/install.sh | sudo sh -s -- --token ${token}${docker ? ' --docker' : ''}`;
		const tls = b.startsWith('https:') ? "[Net.ServicePointManager]::SecurityProtocol = 'Tls12'; " : '';
		return `${tls}& ([scriptblock]::Create((irm '${b.replace(/'/g, "''")}/agent/install.ps1'))) -Token ${token}`;
	}

	const VALIDITY = [
		{ value: '1', label: '1 Tag' },
		{ value: '7', label: '7 Tage' },
		{ value: '30', label: '30 Tage' },
		{ value: '365', label: '1 Jahr' },
		{ value: 'never', label: 'Läuft nie ab' }
	];
	const USES = [
		{ value: 'unlimited', label: 'Beliebig viele Systeme' },
		{ value: '1', label: 'Ein System' },
		{ value: 'custom', label: 'Anzahl festlegen …' }
	];

	function openCreate() {
		name = 'Agents';
		tags = [];
		validity = '30';
		uses = 'unlimited';
		customUses = '10';
		errors = {};
		general = null;
		created = null;
		docker = false;
		platform = 'linux';
		base = data.data?.baseUrl ?? '';
		open = true;
	}

	async function create() {
		errors = {};
		general = null;
		if (!name.trim()) {
			errors = { name: 'Name erforderlich' };
			return;
		}
		let maxUses: number | undefined;
		if (uses === '1') maxUses = 1;
		if (uses === 'custom') {
			maxUses = Number(customUses);
			if (!Number.isInteger(maxUses) || maxUses < 1) {
				errors = { maxUses: 'Ganze Zahl ab 1' };
				return;
			}
		}
		const expiresAt =
			validity === 'never' ? undefined : new Date(Date.now() + Number(validity) * 86400_000).toISOString();
		saving = true;
		try {
			created = await api.post('/api/v1/agent-enrollments', {
				body: { name: name.trim(), tags, maxUses, expiresAt }
			});
			tokens.reload();
		} catch (e) {
			({ errors, general } = apiErrors(e, ['name', 'maxUses', 'expiresAt', 'tags']));
		} finally {
			saving = false;
		}
	}

	async function revoke(e: AgentEnrollment) {
		const ok = await confirm({
			title: `Installations-Token „${e.name}“ widerrufen?`,
			message:
				'Mit dem Befehl lassen sich keine weiteren Agents mehr installieren. Bereits installierte Agents laufen weiter.',
			confirmLabel: 'Widerrufen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/agent-enrollments/{id}', { path: { id: e.id } });
			toast.success('Installations-Token widerrufen');
			tokens.reload();
		} catch (err) {
			toast.error(err);
		}
	}

	// ---------------------------------------------------------------- agents
	async function refresh(a: Agent) {
		try {
			await api.post('/api/v1/agents/{id}/refresh', { path: { id: a.id } });
			toast.success(`Inventar bei ${a.hostname} angefordert`);
		} catch (e) {
			toast.error(e);
		}
	}

	async function remove(a: Agent) {
		const ok = await confirm({
			title: `Agent auf „${a.hostname}“ entfernen?`,
			message: `Der Agent wird abgemeldet und beendet sich beim nächsten Kontakt. Das Gerät und seine Daten bleiben. Zum vollständigen Entfernen auf dem System: ${
				a.platform === 'windows'
					? 'den Installationsbefehl mit -Uninstall statt -Token ausführen'
					: '… install.sh | sudo sh -s -- --uninstall'
			}`,
			confirmLabel: 'Entfernen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/agents/{id}', { path: { id: a.id } });
			toast.success(`Agent auf ${a.hostname} entfernt`);
			data.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	function tokenState(e: AgentEnrollment): { tone: 'ok' | 'neutral' | 'warn'; label: string } {
		if (e.revokedAt) return { tone: 'neutral', label: 'widerrufen' };
		if (e.expiresAt && new Date(e.expiresAt).getTime() < Date.now())
			return { tone: 'neutral', label: 'abgelaufen' };
		if (e.maxUses && e.uses >= e.maxUses) return { tone: 'neutral', label: 'aufgebraucht' };
		return { tone: 'ok', label: 'gültig' };
	}

	const columns: Column<Agent>[] = [
		{ key: 'host', label: 'System' },
		{ key: 'device', label: 'Gerät', hideBelow: 'md' },
		{ key: 'status', label: 'Status' },
		{ key: 'version', label: 'Version', hideBelow: 'lg' },
		{ key: 'inventory', label: 'Letztes Inventar', hideBelow: 'md' },
		{ key: 'actions', label: '', align: 'right', width: '5rem' }
	];
</script>

<PageHeader
	title="Agents"
	description="Systeme mit installiertem NetScope-Agent liefern Inventar und Auslastung von sich aus – ohne SSH-Zugang, auch hinter NAT oder Firewalls."
>
	{#snippet actions()}
		{#if canManage}
			<Button variant="primary" icon="plus" onclick={openCreate} disabled={!data.data || !binaries.length}
				>Agent installieren</Button
			>
		{/if}
	{/snippet}
</PageHeader>

<div class="flex flex-col gap-4">
	{#if data.data && !binaries.length}
		<Alert tone="warn" title="Keine Agent-Builds in dieser NetScope-Version">
			Die Agent-Programme werden mit dem Container-Image gebaut (/usr/share/netscope/agent). In einer
			Entwicklungsversion ohne Image lassen sich keine Agents installieren.
		</Alert>
	{/if}

	<Card
		title="Systeme mit Agent"
		description={agents.length ? `${online} von ${agents.length} melden sich` : undefined}
		icon="cpu"
		padding="none"
	>
		{#if data.error && !data.data}
			<ErrorState error={data.error} onretry={() => data.reload()} />
		{:else}
			<Table
				{columns}
				rows={agents}
				key={(a) => a.id}
				loading={data.loading && !data.data}
				class="rounded-none border-0"
				caption="Agents"
			>
				{#snippet cell(a, col)}
					{#if col.key === 'host'}
						<span class="font-medium">{a.hostname || 'unbekannt'}</span>
						<span class="block text-xs text-fg-subtle">{[a.os, a.arch].filter(Boolean).join(' · ')}</span>
						{#if a.lastError}<span class="block text-xs text-warn">{a.lastError}</span>{/if}
					{:else if col.key === 'device'}
						{#if a.deviceId}
							<a class="link" href="/devices/{a.deviceId}">{a.deviceName || `#${a.deviceId}`}</a>
						{:else}
							<span class="text-fg-subtle">wartet auf Inventar</span>
						{/if}
					{:else if col.key === 'status'}
						<span class="inline-flex items-center gap-1.5">
							<StatusDot status={a.online ? 'online' : 'offline'} pulse={false} />
							{a.online ? 'meldet sich' : 'keine Meldung'}
						</span>
						{#if a.lastSeenAt}<span class="block text-xs text-fg-subtle"
								><RelativeTime value={a.lastSeenAt} /> · {a.ip}</span
							>{/if}
						{#if a.fullDisks.length}<Badge tone="danger" title="Über der Schwelle">
								voll: {a.fullDisks.join(', ')}</Badge
							>{/if}
					{:else if col.key === 'version'}
						<span class="mono text-xs">{a.version || '–'}</span>
						{#if a.outdated}<Badge tone="info" title="Der Agent aktualisiert sich beim nächsten Kontakt"
								>Update folgt</Badge
							>{/if}
						{#if a.docker}<span class="block text-xs text-fg-subtle">mit Docker</span>{/if}
					{:else if col.key === 'inventory'}
						{#if a.lastInventoryAt}<RelativeTime
								value={a.lastInventoryAt}
								class="text-fg-muted"
							/>{:else}<span class="text-fg-subtle">noch keins</span>{/if}
					{:else if col.key === 'actions'}
						<span class="inline-flex gap-1">
							{#if auth.can('devices.scan')}
								<Button
									size="sm"
									variant="ghost"
									icon="refresh"
									label="Inventar von {a.hostname} jetzt anfordern"
									onclick={() => refresh(a)}
								/>
							{/if}
							{#if canManage}
								<Button
									size="sm"
									variant="ghost"
									icon="trash"
									label="Agent auf {a.hostname} entfernen"
									onclick={() => remove(a)}
								/>
							{/if}
						</span>
					{/if}
				{/snippet}
				{#snippet empty()}
					<EmptyState
						compact
						icon="cpu"
						title="Noch kein Agent installiert"
						description={canManage
							? '„Agent installieren“ erzeugt einen Befehl, der auf dem System ausgeführt den Agent einrichtet.'
							: 'Agents richtet ein Benutzer mit dem Recht „Agents verwalten“ ein.'}
					/>
				{/snippet}
			</Table>
		{/if}
	</Card>

	{#if canManage}
		<Card
			title="Installations-Tokens"
			description="Jeder Installationsbefehl enthält ein Token; bereits installierte Agents laufen unabhängig davon weiter"
			icon="key"
			padding="none"
		>
			{#if tokens.error && !tokens.data}
				<ErrorState error={tokens.error} onretry={() => tokens.reload()} />
			{:else if (tokens.data ?? []).length === 0}
				<p class="px-4 py-3 text-sm text-fg-subtle">Noch keine Installationsbefehle erzeugt.</p>
			{:else}
				<ul class="divide-y divide-border">
					{#each tokens.data ?? [] as e (e.id)}
						{@const st = tokenState(e)}
						<li class="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2.5 text-sm">
							<div class="min-w-0 flex-1">
								<span class="font-medium">{e.name}</span>
								<span class="mono ml-1 text-xs text-fg-subtle">{e.prefix}…</span>
								<span class="block text-xs text-fg-subtle">
									{e.uses}{e.maxUses ? ` von ${e.maxUses}` : ''} Installationen · {e.agents} Agents ·
									{e.expiresAt ? `gültig bis ${formatDate(e.expiresAt)}` : 'läuft nie ab'}
									{#if e.tags.length}· Tags: {e.tags.join(', ')}{/if}
									· von {e.createdBy || '–'} am {formatDateTime(e.createdAt)}
								</span>
							</div>
							<Badge tone={st.tone}>{st.label}</Badge>
							{#if e.usable}
								<Button size="sm" variant="ghost" onclick={() => revoke(e)}>Widerrufen</Button>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</Card>
	{/if}
</div>

<Modal
	bind:open
	title={created ? 'Installationsbefehl' : 'Agent installieren'}
	size="lg"
	as="form"
	onsubmit={() => (created ? (open = false) : create())}
	busy={saving}
>
	{#if created}
		{@const cmd = installCommand(created.token)}
		<div class="flex flex-col gap-3 text-sm">
			<div class="flex w-fit rounded-md border border-border p-0.5" role="group" aria-label="Zielsystem">
				{#each PLATFORMS as p (p.id)}
					<button
						type="button"
						class="rounded px-3 py-1 text-xs {platform === p.id
							? 'bg-accent-soft font-medium text-accent'
							: 'text-fg-muted hover:text-fg'}"
						aria-pressed={platform === p.id}
						onclick={() => (platform = p.id)}>{p.label}</button
					>
				{/each}
			</div>
			{#if platform === 'linux'}
				<p class="text-fg-muted">
					Auf dem Linux-System als root bzw. mit sudo ausführen. Der Befehl lädt den Agent von dieser Instanz,
					prüft die Prüfsumme, legt den Benutzer <code class="mono">netscope-agent</code> an und startet den Dienst.
					Das System erscheint danach hier und in der Geräteliste.
				</p>
			{:else}
				<p class="text-fg-muted">
					In einer PowerShell <strong>als Administrator</strong> ausführen (Windows 10/11, Windows Server 2016
					und neuer). Der Befehl lädt den Agent von dieser Instanz, prüft die Prüfsumme und richtet den Dienst
					<code class="mono">NetScopeAgent</code> unter einem eigenen virtuellen Dienstkonto ohne Administratorrechte
					ein. Das System erscheint danach hier und in der Geräteliste.
				</p>
			{/if}
			<Input
				label="Adresse von NetScope aus Sicht der Systeme"
				bind:value={base}
				mono
				hint="Vorbelegt mit der Adresse, über die du gerade zugreifst – anpassen, wenn die Systeme NetScope anders erreichen (Proxy, VPN, anderes Netz)."
			/>
			<div class="flex items-start gap-2 rounded-md border border-border bg-surface-2 py-1.5 pr-1.5 pl-3">
				<code
					class="mono min-w-0 flex-1 py-1 text-[0.8rem] break-all select-all"
					aria-label="Installationsbefehl">{cmd}</code
				>
				<CopyButton text={cmd} label="Befehl kopieren" size="sm" />
			</div>
			{#if platform === 'linux'}
				<Toggle
					bind:checked={docker}
					label="Docker-Container erfassen (--docker)"
					description="Nimmt den Agent in die Gruppe docker auf – das entspricht root-Rechten auf dem System."
				/>
			{:else}
				<p class="text-xs text-fg-subtle">
					Auf einem DHCP-Server liest der Agent auch die Leases (als Mitglied der Gruppe „DHCP Users“). Auf
					einem Domänencontroller gibt es diese lokale Gruppe nicht – dort <code class="mono"
						>-RunAsSystem</code
					> anhängen.
				</p>
			{/if}
			<Alert tone="warn" title="Token nur jetzt sichtbar">
				Der Befehl enthält das Installations-Token „{created.enrollment?.name}“ ({created.enrollment?.maxUses
					? `${created.enrollment.maxUses}× nutzbar`
					: 'beliebig oft nutzbar'}{created.enrollment?.expiresAt
					? `, bis ${formatDate(created.enrollment.expiresAt)}`
					: ''}). Wer ihn hat, kann Systeme anmelden.
			</Alert>
			{#if insecure}
				<p class="text-xs text-fg-subtle">
					Die Instanz ist per http erreichbar: Befehl und Agent laufen unverschlüsselt durchs Netz. Hinter
					einem Reverse-Proxy mit HTTPS (öffentliche URL in den Systemeinstellungen) wird der Befehl mit https
					erzeugt.
				</p>
			{/if}
			{#if binaries.length}
				<p class="text-xs text-fg-subtle">
					Verfügbar für {binaries.map((b) => `${b.platform} (${formatBytes(b.size)})`).join(', ')}. Entfernen
					auf dem System: derselbe Befehl mit
					<code class="mono">{platform === 'linux' ? '--uninstall' : '-Uninstall'}</code> statt des Tokens.
				</p>
			{/if}
		</div>
	{:else}
		<div class="flex flex-col gap-4">
			{#if general}<Alert tone="danger">{general}</Alert>{/if}
			<Input
				label="Bezeichnung"
				bind:value={name}
				required
				maxlength={100}
				error={errors.name}
				hint="z. B. Colo-VMs"
			/>
			<TagInput
				label="Tags für die Geräte"
				bind:value={tags}
				hint="Werden beim ersten Inventar auf das Gerät gesetzt"
				error={errors.tags}
			/>
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<Select label="Gültig" bind:value={validity} options={VALIDITY} error={errors.expiresAt} />
				<Select label="Nutzbar für" bind:value={uses} options={USES} />
			</div>
			{#if uses === 'custom'}
				<Input
					label="Anzahl Systeme"
					type="number"
					min="1"
					bind:value={customUses}
					error={errors.maxUses}
					class="w-40"
				/>
			{/if}
		</div>
	{/if}
	{#snippet footer()}
		{#if created}
			<Button type="submit" variant="primary">Fertig</Button>
		{:else}
			<Button onclick={() => (open = false)} disabled={saving}>Abbrechen</Button>
			<Button type="submit" variant="primary" icon="key" loading={saving}>Befehl erzeugen</Button>
		{/if}
	{/snippet}
</Modal>
