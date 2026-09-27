<!-- Inventory of a Windows host with the NetScope agent (internal/agent/wininv Inventory JSON). -->
<script lang="ts">
	import Alert from '$lib/components/ui/Alert.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import { t, tn } from '$lib/i18n';
	import {
		formatBps,
		formatBytes,
		formatDate,
		formatDateTime,
		formatSeconds,
		percentUnit
	} from '$lib/utils/format';

	interface WinData {
		hostname?: string;
		fqdn?: string;
		domain?: string;
		workgroup?: string;
		domainRole?: string;
		os?: {
			name: string;
			version?: string;
			displayVersion?: string;
			edition?: string;
			installationType?: string;
			architecture?: string;
			installDate?: string;
			timezone?: string;
		};
		hardware?: {
			manufacturer?: string;
			model?: string;
			family?: string;
			serial?: string;
			biosVendor?: string;
			biosVersion?: string;
			biosDate?: string;
			chassis?: string;
			virtual?: boolean;
		};
		cpu?: { name: string; cores: number; threads: number; mhz?: number }[];
		memoryBytes?: number;
		volumes?: { drive: string; label?: string; fs?: string; size: number; free: number }[];
		disks?: { model?: string; size?: number; interface?: string; media?: string; serial?: string }[];
		interfaces?: {
			index: number;
			name: string;
			description?: string;
			mac?: string;
			status?: string;
			speedBps?: number;
			virtual?: boolean;
			addresses?: {
				address: string;
				prefixLength: number;
				family: string;
				prefixOrigin?: string;
				suffixOrigin?: string;
			}[];
		}[];
		updates?: {
			rebootRequired: boolean;
			lastSearch?: string;
			lastInstall?: string;
			pending: {
				title: string;
				kb?: string[];
				severity?: string;
				categories?: string[];
				downloaded?: boolean;
			}[];
			pendingSecurity: number;
			hotfixes?: { id: string; description?: string; installedOn?: string }[];
			lastHotfix?: { id: string; installedOn?: string };
			known: boolean;
			/** Windows Update refused the list: the agent runs without administrator rights */
			pendingDenied?: boolean;
		};
		services?: { name: string; displayName?: string; state: string; startMode: string; account?: string }[];
		listening?: { proto: string; address: string; port: number; pid?: number; process?: string }[];
		firewall?: { profile: string; enabled: boolean }[];
		defender?: {
			antivirus: boolean;
			realtime: boolean;
			signatureUpdated?: string;
			signatureVersion?: string;
			productVersion?: string;
		};
		antivirus?: { name: string; enabled: boolean; current: boolean }[];
		dhcp?: {
			scopes: {
				id: string;
				mask: string;
				name?: string;
				state?: string;
				start?: string;
				end?: string;
				leases: number;
				reservations: number;
			}[];
			leases: number;
			reservations: number;
		};
		softwareCount?: number;
		uptimeSeconds?: number;
		bootTime?: string;
		errors?: Record<string, string>;
		unavailable?: string[];
	}

	let { data: raw }: { data: unknown } = $props();
	const data = $derived((raw ?? {}) as WinData);

	const osName = $derived([data.os?.name, data.os?.displayVersion].filter(Boolean).join(' ') || '–');
	const hardware = $derived(
		[data.hardware?.manufacturer, data.hardware?.family || data.hardware?.model].filter(Boolean).join(' ')
	);
	const chassisLabel: Record<string, string> = {
		desktop: 'Desktop',
		laptop: t('Notebook'),
		tablet: 'Tablet',
		server: 'Server'
	};
	const u = $derived(data.updates);
	const severityTone = (s?: string) =>
		s === 'Critical' ? 'danger' : s === 'Important' ? 'warn' : s ? 'info' : 'neutral';
	const severityLabel: Record<string, string> = {
		Critical: t('Kritisch'),
		Important: t('Wichtig'),
		Moderate: t('Mittel'),
		Low: t('Niedrig')
	};
	const adapterStatus: Record<string, string> = {
		Up: t('verbunden'),
		Disconnected: t('getrennt'),
		Disabled: t('deaktiviert'),
		'Not Present': t('nicht vorhanden')
	};
	const startModeLabel: Record<string, string> = {
		Auto: t('automatisch'),
		Manual: t('manuell'),
		Disabled: t('deaktiviert')
	};
	const linkLocal = (a: string) => a.startsWith('fe80:') || a.startsWith('169.254.');
	const firewallOff = $derived((data.firewall ?? []).filter((f) => !f.enabled).map((f) => f.profile));

	let svcFilter = $state('');
	let svcScope = $state<'running' | 'stopped-auto' | 'all'>('running');
	const SVC_SCOPES = [
		{ value: 'running', label: t('Laufende') },
		{ value: 'stopped-auto', label: t('Automatisch, aber gestoppt') },
		{ value: 'all', label: t('Alle') }
	];
	const services = $derived.by(() => {
		const q = svcFilter.trim().toLowerCase();
		return (data.services ?? [])
			.filter((s) =>
				svcScope === 'running'
					? s.state === 'Running'
					: svcScope === 'stopped-auto'
						? s.state !== 'Running' && s.startMode === 'Auto'
						: true
			)
			.filter(
				(s) => !q || s.name.toLowerCase().includes(q) || (s.displayName ?? '').toLowerCase().includes(q)
			)
			.sort((a, b) => (a.displayName || a.name).localeCompare(b.displayName || b.name));
	});
	const running = $derived((data.services ?? []).filter((s) => s.state === 'Running').length);
	const tcpListening = $derived((data.listening ?? []).filter((s) => s.proto === 'tcp'));
	let showUdp = $state(false);
	const listening = $derived(showUdp ? (data.listening ?? []) : tcpListening);
	let showHotfixes = $state(false);

	function barTone(p: number) {
		return p >= 90 ? 'bg-danger' : p >= 75 ? 'bg-warn' : 'bg-accent';
	}
	const pct = (used: number, size: number) => (size > 0 ? Math.round((used / size) * 100) : 0);
	const hostPort = (a: string, p: number) => (a.includes(':') ? `[${a}]:${p}` : `${a}:${p}`);
	const th = 'border-b border-border px-2.5 py-1.5 font-semibold whitespace-nowrap';
	const td = 'border-b border-border px-2.5 py-1 align-top';
</script>

<div class="flex flex-col gap-5">
	<dl class="grid grid-cols-1 gap-x-6 gap-y-2.5 sm:grid-cols-2 xl:grid-cols-3">
		<div>
			<dt class="text-xs text-fg-subtle">{t('Betriebssystem')}</dt>
			<dd class="text-sm">
				{osName}
				{#if data.os?.installationType && data.os.installationType !== 'Client'}
					<span class="text-xs text-fg-subtle">({data.os.installationType})</span>
				{/if}
			</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">Build</dt>
			<dd class="mono text-sm">
				{data.os?.version || '–'}
				{#if data.os?.architecture}<span class="font-sans text-xs text-fg-subtle">{data.os.architecture}</span
					>{/if}
			</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">{data.domain ? t('Domäne') : t('Arbeitsgruppe')}</dt>
			<dd class="text-sm">
				{data.domain || data.workgroup || '–'}
				{#if data.domainRole}<span class="text-xs text-fg-subtle">· {data.domainRole}</span>{/if}
			</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">Hostname</dt>
			<dd class="text-sm">
				{data.hostname || '–'}
				{#if data.fqdn}<span class="mono block text-xs text-fg-subtle">{data.fqdn}</span>{/if}
			</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">Uptime</dt>
			<dd class="text-sm">
				{formatSeconds(data.uptimeSeconds)}
				{#if data.bootTime}<span class="text-xs text-fg-subtle"
						>({t('Start {time}', { time: formatDateTime(data.bootTime) })})</span
					>{/if}
			</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">{t('Installiert')}</dt>
			<dd class="text-sm">
				{formatDate(data.os?.installDate)}
				{#if data.softwareCount}<span class="text-xs text-fg-subtle"
						>· {tn(data.softwareCount, '{n} Programm', '{n} Programme')}</span
					>{/if}
			</dd>
		</div>
		{#if hardware || data.hardware?.serial}
			<div>
				<dt class="text-xs text-fg-subtle">Hardware</dt>
				<dd class="text-sm">
					{hardware || '–'}
					{#if data.hardware?.virtual}<Badge tone="info">{t('virtuell')}</Badge
						>{:else if data.hardware?.chassis}<span class="text-xs text-fg-subtle"
							>({chassisLabel[data.hardware.chassis] ?? data.hardware.chassis})</span
						>{/if}
					<span class="block text-xs text-fg-subtle">
						{[
							data.hardware?.model && data.hardware.family ? data.hardware.model : '',
							data.hardware?.serial ? `S/N ${data.hardware.serial}` : ''
						]
							.filter(Boolean)
							.join(' · ')}
					</span>
				</dd>
			</div>
		{/if}
		{#if data.hardware?.biosVersion}
			<div>
				<dt class="text-xs text-fg-subtle">BIOS / UEFI</dt>
				<dd class="text-sm">
					{[data.hardware.biosVendor, data.hardware.biosVersion].filter(Boolean).join(' ')}
					{#if data.hardware.biosDate}<span class="text-xs text-fg-subtle"
							>({formatDate(data.hardware.biosDate)})</span
						>{/if}
				</dd>
			</div>
		{/if}
		{#each data.cpu ?? [] as c, i (i)}
			<div>
				<dt class="text-xs text-fg-subtle">CPU</dt>
				<dd class="text-sm">
					{c.name}
					<span class="block text-xs text-fg-subtle"
						>{t('{cores} Kerne · {threads} Threads', { cores: c.cores, threads: c.threads })}</span
					>
				</dd>
			</div>
		{/each}
		{#if data.memoryBytes}
			<div>
				<dt class="text-xs text-fg-subtle">{t('Arbeitsspeicher')}</dt>
				<dd class="text-sm">{formatBytes(data.memoryBytes)}</dd>
			</div>
		{/if}
		{#if data.os?.timezone}
			<div>
				<dt class="text-xs text-fg-subtle">{t('Zeitzone')}</dt>
				<dd class="text-sm">{data.os.timezone}</dd>
			</div>
		{/if}
	</dl>

	<section class="grid grid-cols-1 gap-3 lg:grid-cols-2">
		<div class="rounded-md border border-border p-3">
			<h4 class="mb-2 flex items-center gap-2 text-xs font-semibold text-fg-muted">
				Windows Update
				{#if u?.rebootRequired}<Badge tone="warn">{t('Neustart erforderlich')}</Badge>{/if}
			</h4>
			{#if u}
				<dl class="grid grid-cols-2 gap-2 text-sm">
					<div>
						<dt class="text-xs text-fg-subtle">{t('Ausstehend')}</dt>
						<dd>
							{#if u.known}
								{u.pending.length}
								{#if u.pendingSecurity}<span class="text-danger"
										>({t('{n} Sicherheit', { n: u.pendingSecurity })})</span
									>{/if}
							{:else if u.pendingDenied}<span class="text-fg-subtle"
									>{t('nur für Administratoren abrufbar')}</span
								>
								<span class="block text-xs text-fg-subtle"
									>{t(
										'Windows Update nennt ausstehende Updates nur Administratoren; der Agent läuft ohne Administratorrechte.'
									)}</span
								>
							{:else}<span class="text-fg-subtle">{t('nicht ermittelt')}</span>{/if}
						</dd>
					</div>
					<div>
						<dt class="text-xs text-fg-subtle">{t('Zuletzt installiert')}</dt>
						<dd>{formatDateTime(u.lastInstall)}</dd>
					</div>
					<div>
						<dt class="text-xs text-fg-subtle">{t('Zuletzt gesucht')}</dt>
						<dd>{formatDateTime(u.lastSearch)}</dd>
					</div>
					<div>
						<dt class="text-xs text-fg-subtle">{t('Letzter Hotfix')}</dt>
						<dd>
							{#if u.lastHotfix}<span class="mono">{u.lastHotfix.id}</span>
								<span class="text-xs text-fg-subtle">{formatDate(u.lastHotfix.installedOn)}</span
								>{:else}–{/if}
						</dd>
					</div>
				</dl>
				{#if u.pending.length}
					<ul class="mt-2 divide-y divide-border rounded-md border border-border text-xs">
						{#each u.pending as p, i (i)}
							<li class="flex items-start gap-2 px-2.5 py-1.5">
								<span class="flex-1">{p.title}</span>
								{#if p.severity}<Badge tone={severityTone(p.severity)}
										>{severityLabel[p.severity] ?? p.severity}</Badge
									>{/if}
							</li>
						{/each}
					</ul>
				{/if}
			{:else}
				<p class="text-sm text-fg-subtle">{t('Nicht erfasst.')}</p>
			{/if}
		</div>
		<div class="rounded-md border border-border p-3">
			<h4 class="mb-2 text-xs font-semibold text-fg-muted">{t('Schutz')}</h4>
			<dl class="grid grid-cols-2 gap-2 text-sm">
				<div>
					<dt class="text-xs text-fg-subtle">Firewall</dt>
					<dd>
						{#if !data.firewall?.length}–
						{:else if firewallOff.length}<span class="text-warn"
								>{t('aus: {profiles}', { profiles: firewallOff.join(', ') })}</span
							>
						{:else}<span class="text-ok">{t('alle Profile an')}</span>{/if}
					</dd>
				</div>
				<div>
					<dt class="text-xs text-fg-subtle">Microsoft Defender</dt>
					<dd>
						{#if !data.defender}–
						{:else if data.defender.antivirus && data.defender.realtime}<span class="text-ok"
								>{t('aktiv')}</span
							>
						{:else if data.defender.antivirus}<span class="text-warn">{t('Echtzeitschutz aus')}</span>
						{:else}<span class="text-fg-muted">{t('inaktiv')}</span>{/if}
						{#if data.defender?.signatureUpdated}
							<span class="block text-xs text-fg-subtle"
								>{t('Signaturen {time}', { time: formatDateTime(data.defender.signatureUpdated) })}</span
							>
						{/if}
					</dd>
				</div>
				{#if data.antivirus?.length}
					<div class="col-span-2">
						<dt class="text-xs text-fg-subtle">{t('Virenschutz (Sicherheitscenter)')}</dt>
						<dd class="flex flex-wrap gap-1.5">
							{#each data.antivirus as av (av.name)}
								<Badge tone={av.enabled ? (av.current ? 'ok' : 'warn') : 'neutral'}
									>{av.name}{av.enabled ? (av.current ? '' : ` · ${t('veraltet')}`) : ` · ${t('aus')}`}</Badge
								>
							{/each}
						</dd>
					</div>
				{/if}
			</dl>
		</div>
	</section>

	{#if data.volumes?.length}
		<section>
			<h4 class="mb-1.5 text-xs font-semibold text-fg-muted">{t('Laufwerke')}</h4>
			<div class="relative overflow-x-auto rounded-md border border-border">
				<table class="w-full min-w-[30rem] border-separate border-spacing-0 text-xs">
					<thead>
						<tr class="bg-surface-2 text-left text-fg-muted">
							<th scope="col" class={th}>{t('Laufwerk')}</th>
							<th scope="col" class={th}>{t('Dateisystem')}</th>
							<th scope="col" class="{th} text-right">{t('Größe')}</th>
							<th scope="col" class="{th} text-right">{t('Frei')}</th>
							<th scope="col" class="{th} w-40">{t('Belegt')}</th>
						</tr>
					</thead>
					<tbody>
						{#each data.volumes as v (v.drive)}
							{@const p = pct(v.size - v.free, v.size)}
							<tr>
								<td class="{td} mono">
									{v.drive}{#if v.label}<span class="font-sans text-fg-subtle"> {v.label}</span>{/if}
								</td>
								<td class={td}>{v.fs || '–'}</td>
								<td class="{td} text-right tabular">{formatBytes(v.size)}</td>
								<td class="{td} text-right tabular">{formatBytes(v.free)}</td>
								<td class={td}>
									<span class="flex items-center gap-2">
										<span class="h-1.5 flex-1 overflow-hidden rounded-full bg-surface-3">
											<span class="block h-full rounded-full {barTone(p)}" style="width:{p}%"></span>
										</span>
										<span class="w-9 text-right tabular">{percentUnit(p)}</span>
									</span>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			{#if data.disks?.length}
				<p class="mt-1.5 text-xs text-fg-subtle">
					{t('Datenträger: {disks}', {
						disks: data.disks
							.map((d) => [d.model, d.size ? formatBytes(d.size) : ''].filter(Boolean).join(' '))
							.join(' · ')
					})}
				</p>
			{/if}
		</section>
	{/if}

	{#if data.interfaces?.length}
		<section>
			<h4 class="mb-1.5 text-xs font-semibold text-fg-muted">{t('Netzwerkadapter')}</h4>
			<div class="relative overflow-x-auto rounded-md border border-border">
				<table class="w-full min-w-[36rem] border-separate border-spacing-0 text-xs">
					<thead>
						<tr class="bg-surface-2 text-left text-fg-muted">
							<th scope="col" class={th}>Name</th>
							<th scope="col" class={th}>Status</th>
							<th scope="col" class={th}>MAC</th>
							<th scope="col" class={th}>{t('Adressen')}</th>
							<th scope="col" class="{th} text-right">{t('Geschwindigkeit')}</th>
						</tr>
					</thead>
					<tbody>
						{#each data.interfaces as it (it.index)}
							<tr class={it.status === 'Up' ? '' : 'text-fg-muted'}>
								<td class={td}>
									{it.name}
									<span class="block text-fg-subtle"
										>{it.description}{it.virtual ? ` · ${t('virtuell')}` : ''}</span
									>
								</td>
								<td class={td}>
									<Badge tone={it.status === 'Up' ? 'ok' : 'neutral'}
										>{adapterStatus[it.status ?? ''] ?? it.status ?? '–'}</Badge
									>
								</td>
								<td class="{td} mono whitespace-nowrap">{it.mac || '–'}</td>
								<td class={td}>
									{#each it.addresses ?? [] as a (a.address)}
										<span class="mono block whitespace-nowrap"
											>{a.address}/{a.prefixLength}{#if a.prefixOrigin === 'Dhcp'}<span
													class="font-sans text-fg-subtle"
												>
													DHCP</span
												>{:else if a.suffixOrigin === 'Random'}<span class="font-sans text-fg-subtle">
													{t('temporär')}</span
												>{:else if linkLocal(a.address)}<span class="font-sans text-fg-subtle">
													{t('link-lokal')}</span
												>{/if}</span
										>
									{:else}<span class="text-fg-subtle">–</span>{/each}
								</td>
								<td class="{td} text-right tabular whitespace-nowrap"
									>{it.speedBps && it.status === 'Up' ? formatBps(it.speedBps) : '–'}</td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}

	{#if data.dhcp}
		<section>
			<h4 class="mb-1.5 text-xs font-semibold text-fg-muted">
				{t('DHCP-Server · {leases} Leases, {reservations} Reservierungen', {
					leases: data.dhcp.leases,
					reservations: data.dhcp.reservations
				})}
			</h4>
			<div class="relative overflow-x-auto rounded-md border border-border">
				<table class="w-full min-w-[30rem] border-separate border-spacing-0 text-xs">
					<thead>
						<tr class="bg-surface-2 text-left text-fg-muted">
							<th scope="col" class={th}>{t('Bereich')}</th>
							<th scope="col" class={th}>{t('Adressen')}</th>
							<th scope="col" class={th}>Status</th>
							<th scope="col" class="{th} text-right">Leases</th>
							<th scope="col" class="{th} text-right">{t('Reservierungen')}</th>
						</tr>
					</thead>
					<tbody>
						{#each data.dhcp.scopes as s (s.id)}
							<tr>
								<td class={td}>
									<span class="mono">{s.id}/{s.mask}</span>
									{#if s.name}<span class="block text-fg-subtle">{s.name}</span>{/if}
								</td>
								<td class="{td} mono whitespace-nowrap">{s.start} – {s.end}</td>
								<td class={td}>
									<Badge tone={s.state === 'Active' ? 'ok' : 'neutral'}
										>{s.state === 'Active' ? t('aktiv') : s.state || '–'}</Badge
									>
								</td>
								<td class="{td} text-right tabular">{s.leases}</td>
								<td class="{td} text-right tabular">{s.reservations}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="mt-1.5 text-xs text-fg-subtle">
				{t(
					'Die Leases ergänzen Namen und Adressen der Geräte im Netz (Quelle „Windows-DHCP“, Einstellungen im Plugin NetScope-Agent).'
				)}
			</p>
		</section>
	{/if}

	{#if data.listening?.length}
		<section>
			<div class="mb-1.5 flex flex-wrap items-center gap-2">
				<h4 class="flex-1 text-xs font-semibold text-fg-muted">
					{showUdp
						? t('Offene Ports ({tcp} TCP, {udp} UDP)', {
								tcp: tcpListening.length,
								udp: data.listening.length - tcpListening.length
							})
						: t('Offene Ports ({tcp} TCP)', { tcp: tcpListening.length })}
				</h4>
				<label class="flex items-center gap-1.5 text-xs text-fg-muted">
					<input type="checkbox" bind:checked={showUdp} />
					{t('UDP anzeigen')}
				</label>
			</div>
			<div class="relative max-h-80 overflow-auto rounded-md border border-border">
				<table class="w-full min-w-[26rem] border-separate border-spacing-0 text-xs">
					<thead class="sticky top-0">
						<tr class="bg-surface-2 text-left text-fg-muted">
							<th scope="col" class={th}>{t('Protokoll')}</th>
							<th scope="col" class={th}>{t('Adresse')}</th>
							<th scope="col" class={th}>{t('Prozess')}</th>
						</tr>
					</thead>
					<tbody>
						{#each listening as s, i (i)}
							<tr>
								<td class="{td} uppercase">{s.proto}</td>
								<td class="{td} mono whitespace-nowrap">{hostPort(s.address, s.port)}</td>
								<td class={td}>{s.process ? (s.pid ? `${s.process} (${s.pid})` : s.process) : '–'}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}

	{#if data.services?.length}
		<section>
			<div class="mb-1.5 flex flex-wrap items-center gap-2">
				<h4 class="flex-1 text-xs font-semibold text-fg-muted">
					{t('Dienste ({running} von {total} laufen)', { running, total: data.services.length })}
				</h4>
				<Select
					size="sm"
					bind:value={svcScope}
					options={SVC_SCOPES}
					aria-label={t('Dienste')}
					class="w-full sm:w-56"
				/>
				<Input
					size="sm"
					icon="search"
					bind:value={svcFilter}
					placeholder={t('Dienste filtern')}
					aria-label={t('Dienste filtern')}
					class="w-full sm:w-56"
				/>
			</div>
			<ul
				class="relative max-h-72 divide-y divide-border overflow-auto rounded-md border border-border text-xs"
			>
				{#each services as s (s.name)}
					<li class="flex flex-wrap items-baseline gap-x-3 px-2.5 py-1">
						<span>{s.displayName || s.name}</span>
						<span class="mono text-fg-subtle">{s.name}</span>
						<span class="ml-auto text-fg-subtle">
							{s.state === 'Running' ? t('läuft') : s.state === 'Stopped' ? t('gestoppt') : s.state} · {startModeLabel[
								s.startMode
							] ?? s.startMode}
						</span>
					</li>
				{:else}
					<li class="px-2.5 py-2 text-fg-subtle">{t('Kein Dienst passt zum Filter.')}</li>
				{/each}
			</ul>
		</section>
	{/if}

	{#if u?.hotfixes?.length}
		<section>
			<button
				type="button"
				class="text-xs font-semibold text-fg-muted hover:text-fg"
				aria-expanded={showHotfixes}
				onclick={() => (showHotfixes = !showHotfixes)}
				>{showHotfixes ? '▾' : '▸'} {t('Installierte Hotfixes ({n})', { n: u.hotfixes.length })}</button
			>
			{#if showHotfixes}
				<ul class="mt-1.5 grid grid-cols-1 gap-x-4 text-xs sm:grid-cols-2 lg:grid-cols-3">
					{#each u.hotfixes as h (h.id)}
						<li class="flex gap-2 py-0.5">
							<span class="mono">{h.id}</span>
							<span class="text-fg-subtle">{h.description}</span>
							<span class="ml-auto text-fg-subtle tabular">{formatDate(h.installedOn)}</span>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/if}

	{#if data.errors && Object.keys(data.errors).length}
		<Alert tone="warn" title={t('Teilweise nicht erfasst')}>
			<ul class="text-xs">
				{#each Object.entries(data.errors) as [k, v] (k)}
					<li><span class="mono">{k}</span>: {v}</li>
				{/each}
			</ul>
		</Alert>
	{/if}
	{#if data.unavailable?.length}
		<p class="text-xs text-fg-subtle">
			{t('Auf diesem System nicht vorhanden:')} <span class="mono">{data.unavailable.join(', ')}</span>
		</p>
	{/if}
</div>
