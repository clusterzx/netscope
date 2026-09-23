<!-- SSH inventory of a Linux host (plugins/ssh Inventory JSON) as readable sections. -->
<script lang="ts">
	import Alert from '$lib/components/ui/Alert.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import { formatBytes, formatDateTime, formatNumber, formatSeconds } from '$lib/utils/format';

	interface BlockDevice {
		name: string;
		type: string;
		sizeBytes: number;
		model?: string;
		serial?: string;
		rotational?: boolean;
		transport?: string;
		fsType?: string;
		mountPoint?: string;
		children?: BlockDevice[];
	}
	interface SshData {
		user?: string;
		hostname?: string;
		osRelease?: Record<string, string>;
		kernel?: string;
		cpu?: {
			model?: string;
			vendor?: string;
			hardware?: string;
			sockets?: number;
			cores?: number;
			threads?: number;
			usable?: number;
		};
		memory?: { totalBytes: number; availableBytes: number; swapTotalBytes: number; swapFreeBytes: number };
		filesystems?: {
			device: string;
			type: string;
			mountPoint: string;
			sizeBytes: number;
			usedBytes: number;
			availBytes: number;
			usePercent: number;
		}[];
		disks?: BlockDevice[];
		interfaces?: {
			index: number;
			name: string;
			mac?: string;
			mtu?: number;
			state?: string;
			type?: string;
			master?: string;
			flags?: string[];
			addresses?: { address: string; prefixLen: number; family: string; scope?: string; dynamic?: boolean }[];
		}[];
		services?: { unit: string; description?: string }[];
		listening?: {
			proto: string;
			address: string;
			port: number;
			interface?: string;
			processes?: { name: string; pid?: number }[];
		}[];
		uptimeSeconds?: number;
		bootTime?: string;
		lastUpdate?: string;
		lastUpdateSource?: string;
		packageDbModified?: string;
		packageManager?: string;
		packageCount?: number;
		docker?: { containers: number; running: number; images: number };
		errors?: Record<string, string>;
		unavailable?: string[];
	}

	let { data: raw }: { data: unknown } = $props();
	const data = $derived((raw ?? {}) as SshData);

	const os = $derived(
		data.osRelease?.PRETTY_NAME ||
			[data.osRelease?.NAME, data.osRelease?.VERSION].filter(Boolean).join(' ') ||
			''
	);
	const memUsed = $derived(data.memory ? data.memory.totalBytes - data.memory.availableBytes : 0);
	const memPct = $derived(data.memory?.totalBytes ? Math.round((memUsed / data.memory.totalBytes) * 100) : 0);
	const updateSources: Record<string, string> = {
		'apt-history': 'APT-Historie',
		'dpkg-status': 'dpkg-Status',
		rpm: 'RPM',
		apk: 'APK'
	};

	let svcFilter = $state('');
	const services = $derived(
		(data.services ?? []).filter(
			(s) =>
				!svcFilter.trim() ||
				s.unit.toLowerCase().includes(svcFilter.toLowerCase()) ||
				(s.description ?? '').toLowerCase().includes(svcFilter.toLowerCase())
		)
	);
	const listening = $derived(
		[...(data.listening ?? [])].sort((a, b) => a.proto.localeCompare(b.proto) || a.port - b.port)
	);

	function flatDisks(list: BlockDevice[] | undefined, depth = 0): { d: BlockDevice; depth: number }[] {
		const out: { d: BlockDevice; depth: number }[] = [];
		for (const d of list ?? []) {
			out.push({ d, depth });
			out.push(...flatDisks(d.children, depth + 1));
		}
		return out;
	}
	const disks = $derived(flatDisks(data.disks));

	function barTone(p: number) {
		return p >= 90 ? 'bg-danger' : p >= 75 ? 'bg-warn' : 'bg-accent';
	}
	const th = 'border-b border-border px-2.5 py-1.5 font-semibold whitespace-nowrap';
	const td = 'border-b border-border px-2.5 py-1 align-top';
</script>

<div class="flex flex-col gap-5">
	<dl class="grid grid-cols-1 gap-x-6 gap-y-2.5 sm:grid-cols-2 xl:grid-cols-3">
		<div>
			<dt class="text-xs text-fg-subtle">Betriebssystem</dt>
			<dd class="text-sm">{os || '–'}</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">Kernel</dt>
			<dd class="mono text-sm break-all">{data.kernel || '–'}</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">Hostname</dt>
			<dd class="text-sm">{data.hostname || '–'}</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">Uptime</dt>
			<dd class="text-sm">
				{formatSeconds(data.uptimeSeconds)}
				{#if data.bootTime}<span class="text-xs text-fg-subtle">(Boot {formatDateTime(data.bootTime)})</span
					>{/if}
			</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">Letztes Update</dt>
			<dd class="text-sm">
				{formatDateTime(data.lastUpdate)}
				{#if data.lastUpdateSource}
					<span class="text-xs text-fg-subtle">
						({updateSources[data.lastUpdateSource] ?? data.lastUpdateSource})
					</span>
				{/if}
			</dd>
		</div>
		<div>
			<dt class="text-xs text-fg-subtle">Pakete</dt>
			<dd class="text-sm">
				{data.packageCount ? formatNumber(data.packageCount) : '–'}
				{#if data.packageManager}<span class="text-xs text-fg-subtle">({data.packageManager})</span>{/if}
				{#if data.packageDbModified}
					<span class="block text-xs text-fg-subtle"
						>Paketdatenbank geändert {formatDateTime(data.packageDbModified)}</span
					>
				{/if}
			</dd>
		</div>
		{#if data.cpu}
			<div class="sm:col-span-2">
				<dt class="text-xs text-fg-subtle">CPU</dt>
				<dd class="text-sm">
					{data.cpu.model || data.cpu.hardware || data.cpu.vendor || '–'}
					<span class="block text-xs text-fg-subtle">
						{[
							data.cpu.sockets ? `${data.cpu.sockets} Sockel` : '',
							data.cpu.cores ? `${data.cpu.cores} Kerne (Host)` : '',
							data.cpu.threads ? `${data.cpu.threads} Threads` : '',
							data.cpu.usable ? `${data.cpu.usable} nutzbar` : ''
						]
							.filter(Boolean)
							.join(' · ')}
					</span>
				</dd>
			</div>
		{/if}
		{#if data.memory}
			<div>
				<dt class="text-xs text-fg-subtle">Arbeitsspeicher</dt>
				<dd class="text-sm">
					{formatBytes(memUsed)} von {formatBytes(data.memory.totalBytes)} belegt
					<span
						class="mt-1 block h-1.5 overflow-hidden rounded-full bg-surface-3"
						role="meter"
						aria-label="RAM-Auslastung"
						aria-valuenow={memPct}
						aria-valuemin={0}
						aria-valuemax={100}
					>
						<span class="block h-full rounded-full {barTone(memPct)}" style="width:{memPct}%"></span>
					</span>
					{#if data.memory.swapTotalBytes}
						<span class="text-xs text-fg-subtle">
							Swap {formatBytes(data.memory.swapTotalBytes - data.memory.swapFreeBytes)} / {formatBytes(
								data.memory.swapTotalBytes
							)}
						</span>
					{/if}
				</dd>
			</div>
		{/if}
		{#if data.docker}
			<div>
				<dt class="text-xs text-fg-subtle">Docker</dt>
				<dd class="text-sm">
					{data.docker.running}/{data.docker.containers} Container laufen · {data.docker.images} Images
				</dd>
			</div>
		{/if}
		{#if data.user}
			<div>
				<dt class="text-xs text-fg-subtle">Angemeldet als</dt>
				<dd class="mono text-sm">{data.user}</dd>
			</div>
		{/if}
	</dl>

	{#if data.filesystems?.length}
		<section>
			<h4 class="mb-1.5 text-xs font-semibold text-fg-muted">Dateisysteme</h4>
			<div class="relative overflow-x-auto rounded-md border border-border">
				<table class="w-full min-w-[34rem] border-separate border-spacing-0 text-xs">
					<thead>
						<tr class="bg-surface-2 text-left text-fg-muted">
							<th scope="col" class={th}>Einhängepunkt</th>
							<th scope="col" class={th}>Gerät</th>
							<th scope="col" class={th}>Typ</th>
							<th scope="col" class="{th} text-right">Größe</th>
							<th scope="col" class="{th} text-right">Frei</th>
							<th scope="col" class="{th} w-40">Belegt</th>
						</tr>
					</thead>
					<tbody>
						{#each data.filesystems as fs (fs.mountPoint + fs.device)}
							<tr>
								<td class="{td} mono">{fs.mountPoint}</td>
								<td class="{td} mono break-all text-fg-muted">{fs.device}</td>
								<td class={td}>{fs.type}</td>
								<td class="{td} text-right tabular">{formatBytes(fs.sizeBytes)}</td>
								<td class="{td} text-right tabular">{formatBytes(fs.availBytes)}</td>
								<td class={td}>
									<span class="flex items-center gap-2">
										<span class="h-1.5 flex-1 overflow-hidden rounded-full bg-surface-3">
											<span
												class="block h-full rounded-full {barTone(fs.usePercent)}"
												style="width:{fs.usePercent}%"
											></span>
										</span>
										<span class="w-9 text-right tabular">{fs.usePercent} %</span>
									</span>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}

	{#if disks.length}
		<section>
			<h4 class="mb-1.5 text-xs font-semibold text-fg-muted">Datenträger</h4>
			<div class="relative overflow-x-auto rounded-md border border-border">
				<table class="w-full min-w-[34rem] border-separate border-spacing-0 text-xs">
					<thead>
						<tr class="bg-surface-2 text-left text-fg-muted">
							<th scope="col" class={th}>Name</th>
							<th scope="col" class={th}>Modell</th>
							<th scope="col" class={th}>Typ</th>
							<th scope="col" class="{th} text-right">Größe</th>
							<th scope="col" class={th}>Anschluss</th>
						</tr>
					</thead>
					<tbody>
						{#each disks as { d, depth }, i (d.name + i)}
							<tr class={depth ? 'text-fg-muted' : ''}>
								<td class="{td} mono whitespace-nowrap" style="padding-left:{0.625 + depth * 1.1}rem">
									{depth ? '└ ' : ''}{d.name}
								</td>
								<td class={td}>
									{d.model || ''}{#if d.serial}<span class="mono block text-fg-subtle">S/N {d.serial}</span
										>{/if}
								</td>
								<td class="{td} whitespace-nowrap">
									{d.type}{#if depth === 0 && d.type === 'disk' && d.rotational !== undefined}
										· {d.rotational ? 'HDD' : 'SSD'}{/if}
								</td>
								<td class="{td} text-right tabular whitespace-nowrap"
									>{d.sizeBytes ? formatBytes(d.sizeBytes) : '–'}</td
								>
								<td class={td}>{d.transport || ''}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}

	{#if data.interfaces?.length}
		<section>
			<h4 class="mb-1.5 text-xs font-semibold text-fg-muted">Netzwerk-Interfaces</h4>
			<div class="relative overflow-x-auto rounded-md border border-border">
				<table class="w-full min-w-[34rem] border-separate border-spacing-0 text-xs">
					<thead>
						<tr class="bg-surface-2 text-left text-fg-muted">
							<th scope="col" class={th}>Name</th>
							<th scope="col" class={th}>Status</th>
							<th scope="col" class={th}>MAC</th>
							<th scope="col" class={th}>Adressen</th>
							<th scope="col" class="{th} text-right">MTU</th>
						</tr>
					</thead>
					<tbody>
						{#each data.interfaces as it (it.index + it.name)}
							<tr>
								<td class="{td} mono whitespace-nowrap">
									{it.name}
									{#if it.master}<span class="block font-sans text-fg-subtle">an {it.master}</span>{/if}
								</td>
								<td class={td}>
									<Badge tone={it.state === 'UP' ? 'ok' : it.state === 'DOWN' ? 'neutral' : 'info'}>
										{it.state || '–'}
									</Badge>
								</td>
								<td class="{td} mono whitespace-nowrap text-fg-muted">{it.mac || '–'}</td>
								<td class={td}>
									{#each it.addresses ?? [] as a (a.address)}
										<span class="mono block whitespace-nowrap"
											>{a.address}/{a.prefixLen}{#if a.scope && a.scope !== 'global'}<span
													class="font-sans text-fg-subtle"
												>
													({a.scope})</span
												>{/if}{#if a.dynamic}<span class="font-sans text-fg-subtle"> DHCP</span>{/if}</span
										>
									{:else}<span class="text-fg-subtle">–</span>{/each}
								</td>
								<td class="{td} text-right tabular">{it.mtu ?? '–'}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}

	{#if listening.length}
		<section>
			<h4 class="mb-1.5 text-xs font-semibold text-fg-muted">Lauschende Sockets ({listening.length})</h4>
			<div class="relative max-h-80 overflow-auto rounded-md border border-border">
				<table class="w-full min-w-[34rem] border-separate border-spacing-0 text-xs">
					<thead class="sticky top-0">
						<tr class="bg-surface-2 text-left text-fg-muted">
							<th scope="col" class={th}>Protokoll</th>
							<th scope="col" class={th}>Adresse</th>
							<th scope="col" class={th}>Interface</th>
							<th scope="col" class={th}>Prozess</th>
						</tr>
					</thead>
					<tbody>
						{#each listening as s, i (i)}
							<tr>
								<td class="{td} uppercase">{s.proto}</td>
								<td class="{td} mono whitespace-nowrap"
									>{s.address.includes(':') && s.address !== '*' ? `[${s.address}]` : s.address}:{s.port}</td
								>
								<td class="{td} mono text-fg-muted">{s.interface || '–'}</td>
								<td class={td}>
									{(s.processes ?? []).map((p) => (p.pid ? `${p.name} (${p.pid})` : p.name)).join(', ') ||
										'–'}
								</td>
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
					Laufende Dienste ({data.services.length})
				</h4>
				<Input
					size="sm"
					icon="search"
					bind:value={svcFilter}
					placeholder="Dienste filtern"
					aria-label="Dienste filtern"
					class="w-full sm:w-56"
				/>
			</div>
			<ul
				class="relative max-h-72 divide-y divide-border overflow-auto rounded-md border border-border text-xs"
			>
				{#each services as s (s.unit)}
					<li class="flex flex-wrap gap-x-3 px-2.5 py-1">
						<span class="mono">{s.unit}</span>
						{#if s.description}<span class="text-fg-muted">{s.description}</span>{/if}
					</li>
				{:else}
					<li class="px-2.5 py-2 text-fg-subtle">Kein Dienst passt zum Filter.</li>
				{/each}
			</ul>
		</section>
	{/if}

	{#if data.errors && Object.keys(data.errors).length}
		<Alert tone="warn" title="Teilweise nicht erfasst">
			<ul class="text-xs">
				{#each Object.entries(data.errors) as [k, v] (k)}
					<li><span class="mono">{k}</span>: {v}</li>
				{/each}
			</ul>
		</Alert>
	{/if}
	{#if data.unavailable?.length}
		<p class="text-xs text-fg-subtle">
			Auf diesem System nicht vorhanden: <span class="mono">{data.unavailable.join(', ')}</span>
		</p>
	{/if}
</div>
