<!--
	Agents: systems with the NetScope agent (status, version, last inventory), the install
	command for Linux and Windows and the installation tokens.
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { Agent, AgentEnrollment, AgentsResponse, EnrollmentCreated } from '$lib/api';
	import { t, tn } from '$lib/i18n';
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
	import { formatBytes, formatDate, formatDateTime, formatNumber } from '$lib/utils/format';

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
		const timer = setInterval(() => data.reload(), 60_000);
		const off = live.on('agent', () => {
			data.reload();
			if (canManage) tokens.reload();
		});
		return () => {
			clearInterval(timer);
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
		{ value: '1', label: tn(1, '{n} Tag', '{n} Tage') },
		{ value: '7', label: tn(7, '{n} Tag', '{n} Tage') },
		{ value: '30', label: tn(30, '{n} Tag', '{n} Tage') },
		{ value: '365', label: t('1 Jahr') },
		{ value: 'never', label: t('Läuft nie ab') }
	];
	const USES = [
		{ value: 'unlimited', label: t('Beliebig viele Systeme') },
		{ value: '1', label: t('Ein System') },
		{ value: 'custom', label: t('Anzahl festlegen …') }
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
			errors = { name: t('Name erforderlich') };
			return;
		}
		let maxUses: number | undefined;
		if (uses === '1') maxUses = 1;
		if (uses === 'custom') {
			maxUses = Number(customUses);
			if (!Number.isInteger(maxUses) || maxUses < 1) {
				errors = { maxUses: t('Ganze Zahl ab 1') };
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
			title: t('Installations-Token „{name}“ widerrufen?', { name: e.name }),
			message: t(
				'Mit dem Befehl lassen sich keine weiteren Agents mehr installieren. Bereits installierte Agents laufen weiter.'
			),
			confirmLabel: t('Widerrufen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/agent-enrollments/{id}', { path: { id: e.id } });
			toast.success(t('Installations-Token widerrufen'));
			tokens.reload();
		} catch (err) {
			toast.error(err);
		}
	}

	// ---------------------------------------------------------------- agents
	async function refresh(a: Agent) {
		try {
			await api.post('/api/v1/agents/{id}/refresh', { path: { id: a.id } });
			toast.success(t('Inventar bei {host} angefordert', { host: a.hostname }));
		} catch (e) {
			toast.error(e);
		}
	}

	async function remove(a: Agent) {
		const ok = await confirm({
			title: t('Agent auf „{host}“ entfernen?', { host: a.hostname }),
			message: t(
				'Der Agent wird abgemeldet und beendet sich beim nächsten Kontakt. Das Gerät und seine Daten bleiben. Zum vollständigen Entfernen auf dem System: {how}',
				{
					how:
						a.platform === 'windows'
							? t('den Installationsbefehl mit -Uninstall statt -Token ausführen')
							: '… install.sh | sudo sh -s -- --uninstall'
				}
			),
			confirmLabel: t('Entfernen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/agents/{id}', { path: { id: a.id } });
			toast.success(t('Agent auf {host} entfernt', { host: a.hostname }));
			data.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	function tokenState(e: AgentEnrollment): { tone: 'ok' | 'neutral' | 'warn'; label: string } {
		if (e.revokedAt) return { tone: 'neutral', label: t('widerrufen') };
		if (e.expiresAt && new Date(e.expiresAt).getTime() < Date.now())
			return { tone: 'neutral', label: t('abgelaufen') };
		if (e.maxUses && e.uses >= e.maxUses) return { tone: 'neutral', label: t('aufgebraucht') };
		return { tone: 'ok', label: t('gültig') };
	}

	// sentences with code or bold text inside: split at the placeholders
	const linuxText = t(
		'Auf dem Linux-System als root bzw. mit sudo ausführen. Der Befehl lädt den Agent von dieser Instanz, prüft die Prüfsumme, legt den Benutzer {user} an und startet den Dienst. Das System erscheint danach hier und in der Geräteliste.'
	).split('{user}');
	const windowsText = t(
		'In einer PowerShell {admin} ausführen (Windows 10/11, Windows Server 2016 und neuer). Der Befehl lädt den Agent von dieser Instanz, prüft die Prüfsumme und richtet den Dienst {service} unter einem eigenen virtuellen Dienstkonto ohne Administratorrechte ein. Das System erscheint danach hier und in der Geräteliste.'
	).split(/(\{admin\}|\{service\})/);
	const dhcpText = t(
		'Auf einem DHCP-Server liest der Agent auch die Leases (als Mitglied der Gruppe „DHCP Users“). Auf einem Domänencontroller gibt es diese lokale Gruppe nicht – dort {flag} anhängen.'
	).split('{flag}');
	const availableText = $derived(
		t('Verfügbar für {platforms}. Entfernen auf dem System: derselbe Befehl mit {flag} statt des Tokens.', {
			platforms: binaries.map((b) => `${b.platform} (${formatBytes(b.size)})`).join(', ')
		}).split('{flag}')
	);

	/** "3× nutzbar, bis 27.10.2026" for the token warning of a created install command */
	function tokenDetails(c: EnrollmentCreated): string {
		const e = c.enrollment;
		return [
			e?.maxUses ? t('{n}× nutzbar', { n: e.maxUses }) : t('beliebig oft nutzbar'),
			e?.expiresAt ? t('bis {date}', { date: formatDate(e.expiresAt) }) : ''
		]
			.filter(Boolean)
			.join(', ');
	}

	const columns: Column<Agent>[] = [
		{ key: 'host', label: 'System' },
		{ key: 'device', label: t('Gerät'), hideBelow: 'md' },
		{ key: 'status', label: 'Status' },
		{ key: 'version', label: 'Version', hideBelow: 'lg' },
		{ key: 'inventory', label: t('Letztes Inventar'), hideBelow: 'md' },
		{ key: 'actions', label: '', align: 'right', width: '5rem' }
	];
</script>

<PageHeader
	title="Agents"
	description={t(
		'Systeme mit installiertem NetScope-Agent liefern Inventar und Auslastung von sich aus – ohne SSH-Zugang, auch hinter NAT oder Firewalls.'
	)}
>
	{#snippet actions()}
		{#if canManage}
			<Button variant="primary" icon="plus" onclick={openCreate} disabled={!data.data || !binaries.length}
				>{t('Agent installieren')}</Button
			>
		{/if}
	{/snippet}
</PageHeader>

<div class="flex flex-col gap-4">
	{#if data.data && !binaries.length}
		<Alert tone="warn" title={t('Keine Agent-Builds in dieser NetScope-Version')}>
			{t(
				'Die Agent-Programme werden mit dem Container-Image gebaut (/usr/share/netscope/agent). In einer Entwicklungsversion ohne Image lassen sich keine Agents installieren.'
			)}
		</Alert>
	{/if}

	<Card
		title={t('Systeme mit Agent')}
		description={agents.length
			? t('{online} von {total} melden sich', {
					online: formatNumber(online),
					total: formatNumber(agents.length)
				})
			: undefined}
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
						<span class="font-medium">{a.hostname || t('unbekannt')}</span>
						<span class="block text-xs text-fg-subtle">{[a.os, a.arch].filter(Boolean).join(' · ')}</span>
						{#if a.lastError}<span class="block text-xs text-warn">{a.lastError}</span>{/if}
					{:else if col.key === 'device'}
						{#if a.deviceId}
							<a class="link" href="/devices/{a.deviceId}">{a.deviceName || `#${a.deviceId}`}</a>
						{:else}
							<span class="text-fg-subtle">{t('wartet auf Inventar')}</span>
						{/if}
					{:else if col.key === 'status'}
						<span class="inline-flex items-center gap-1.5">
							<StatusDot status={a.online ? 'online' : 'offline'} pulse={false} />
							{a.online ? t('meldet sich') : t('keine Meldung')}
						</span>
						{#if a.lastSeenAt}<span class="block text-xs text-fg-subtle"
								><RelativeTime value={a.lastSeenAt} /> · {a.ip}</span
							>{/if}
						{#if a.fullDisks.length}<Badge tone="danger" title={t('Über der Schwelle')}>
								{t('voll: {list}', { list: a.fullDisks.join(', ') })}</Badge
							>{/if}
					{:else if col.key === 'version'}
						<span class="mono text-xs">{a.version || '–'}</span>
						{#if a.outdated}<Badge tone="info" title={t('Der Agent aktualisiert sich beim nächsten Kontakt')}
								>{t('Update folgt')}</Badge
							>{/if}
						{#if a.docker}<span class="block text-xs text-fg-subtle">{t('mit Docker')}</span>{/if}
					{:else if col.key === 'inventory'}
						{#if a.lastInventoryAt}<RelativeTime
								value={a.lastInventoryAt}
								class="text-fg-muted"
							/>{:else}<span class="text-fg-subtle">{t('noch keins')}</span>{/if}
					{:else if col.key === 'actions'}
						<span class="inline-flex gap-1">
							{#if auth.can('devices.scan')}
								<Button
									size="sm"
									variant="ghost"
									icon="refresh"
									label={t('Inventar von {host} jetzt anfordern', { host: a.hostname })}
									onclick={() => refresh(a)}
								/>
							{/if}
							{#if canManage}
								<Button
									size="sm"
									variant="ghost"
									icon="trash"
									label={t('Agent auf {host} entfernen', { host: a.hostname })}
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
						title={t('Noch kein Agent installiert')}
						description={canManage
							? t(
									'„Agent installieren“ erzeugt einen Befehl, der auf dem System ausgeführt den Agent einrichtet.'
								)
							: t('Agents richtet ein Benutzer mit dem Recht „Agents verwalten“ ein.')}
					/>
				{/snippet}
			</Table>
		{/if}
	</Card>

	{#if canManage}
		<Card
			title={t('Installations-Tokens')}
			description={t(
				'Jeder Installationsbefehl enthält ein Token; bereits installierte Agents laufen unabhängig davon weiter'
			)}
			icon="key"
			padding="none"
		>
			{#if tokens.error && !tokens.data}
				<ErrorState error={tokens.error} onretry={() => tokens.reload()} />
			{:else if (tokens.data ?? []).length === 0}
				<p class="px-4 py-3 text-sm text-fg-subtle">{t('Noch keine Installationsbefehle erzeugt.')}</p>
			{:else}
				<ul class="divide-y divide-border">
					{#each tokens.data ?? [] as e (e.id)}
						{@const st = tokenState(e)}
						<li class="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2.5 text-sm">
							<div class="min-w-0 flex-1">
								<span class="font-medium">{e.name}</span>
								<span class="mono ml-1 text-xs text-fg-subtle">{e.prefix}…</span>
								<span class="block text-xs text-fg-subtle">
									{e.maxUses
										? t('{uses} von {max} Installationen', {
												uses: formatNumber(e.uses),
												max: formatNumber(e.maxUses)
											})
										: tn(e.uses, '{n} Installation', '{n} Installationen')} ·
									{tn(e.agents, '{n} Agent', '{n} Agents')} ·
									{e.expiresAt
										? t('gültig bis {date}', { date: formatDate(e.expiresAt) })
										: t('läuft nie ab')}
									{#if e.tags.length}· Tags: {e.tags.join(', ')}{/if}
									· {t('von {user} am {date}', {
										user: e.createdBy || '–',
										date: formatDateTime(e.createdAt)
									})}
								</span>
							</div>
							<Badge tone={st.tone}>{st.label}</Badge>
							{#if e.usable}
								<Button size="sm" variant="ghost" onclick={() => revoke(e)}>{t('Widerrufen')}</Button>
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
	title={created ? t('Installationsbefehl') : t('Agent installieren')}
	size="lg"
	as="form"
	onsubmit={() => (created ? (open = false) : create())}
	busy={saving}
>
	{#if created}
		{@const cmd = installCommand(created.token)}
		<div class="flex flex-col gap-3 text-sm">
			<div class="flex w-fit rounded-md border border-border p-0.5" role="group" aria-label={t('Zielsystem')}>
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
					{linuxText[0]}<code class="mono">netscope-agent</code>{linuxText[1]}
				</p>
			{:else}
				<p class="text-fg-muted">
					{#each windowsText as part, i (i)}{#if part === '{admin}'}<strong>{t('als Administrator')}</strong
							>{:else if part === '{service}'}<code class="mono">NetScopeAgent</code>{:else}{part}{/if}{/each}
				</p>
			{/if}
			<Input
				label={t('Adresse von NetScope aus Sicht der Systeme')}
				bind:value={base}
				mono
				hint={t(
					'Vorbelegt mit der Adresse, über die du gerade zugreifst – anpassen, wenn die Systeme NetScope anders erreichen (Proxy, VPN, anderes Netz).'
				)}
			/>
			<div class="flex items-start gap-2 rounded-md border border-border bg-surface-2 py-1.5 pr-1.5 pl-3">
				<code
					class="mono min-w-0 flex-1 py-1 text-[0.8rem] break-all select-all"
					aria-label={t('Installationsbefehl')}>{cmd}</code
				>
				<CopyButton text={cmd} label={t('Befehl kopieren')} size="sm" />
			</div>
			{#if platform === 'linux'}
				<Toggle
					bind:checked={docker}
					label={t('Docker-Container erfassen (--docker)')}
					description={t(
						'Nimmt den Agent in die Gruppe docker auf – das entspricht root-Rechten auf dem System.'
					)}
				/>
			{:else}
				<p class="text-xs text-fg-subtle">
					{dhcpText[0]}<code class="mono">-RunAsSystem</code>{dhcpText[1]}
				</p>
			{/if}
			<Alert tone="warn" title={t('Token nur jetzt sichtbar')}>
				{t(
					'Der Befehl enthält das Installations-Token „{name}“ ({details}). Wer ihn hat, kann Systeme anmelden.',
					{
						name: created.enrollment?.name ?? '',
						details: tokenDetails(created)
					}
				)}
			</Alert>
			{#if insecure}
				<p class="text-xs text-fg-subtle">
					{t(
						'Die Instanz ist per http erreichbar: Befehl und Agent laufen unverschlüsselt durchs Netz. Hinter einem Reverse-Proxy mit HTTPS (öffentliche URL in den Systemeinstellungen) wird der Befehl mit https erzeugt.'
					)}
				</p>
			{/if}
			{#if binaries.length}
				<p class="text-xs text-fg-subtle">
					{availableText[0]}<code class="mono">{platform === 'linux' ? '--uninstall' : '-Uninstall'}</code
					>{availableText[1]}
				</p>
			{/if}
		</div>
	{:else}
		<div class="flex flex-col gap-4">
			{#if general}<Alert tone="danger">{general}</Alert>{/if}
			<Input
				label={t('Bezeichnung')}
				bind:value={name}
				required
				maxlength={100}
				error={errors.name}
				hint={t('z. B. Colo-VMs')}
			/>
			<TagInput
				label={t('Tags für die Geräte')}
				bind:value={tags}
				hint={t('Werden beim ersten Inventar auf das Gerät gesetzt')}
				error={errors.tags}
			/>
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<Select label={t('Gültig')} bind:value={validity} options={VALIDITY} error={errors.expiresAt} />
				<Select label={t('Nutzbar für')} bind:value={uses} options={USES} />
			</div>
			{#if uses === 'custom'}
				<Input
					label={t('Anzahl Systeme')}
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
			<Button type="submit" variant="primary">{t('Fertig')}</Button>
		{:else}
			<Button onclick={() => (open = false)} disabled={saving}>{t('Abbrechen')}</Button>
			<Button type="submit" variant="primary" icon="key" loading={saving}>{t('Befehl erzeugen')}</Button>
		{/if}
	{/snippet}
</Modal>
