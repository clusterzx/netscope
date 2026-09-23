<!--
	WireGuard tunnel of a subnet: pick a stored tunnel configuration or paste/upload a new
	one. Shows what NetScope understood (POST /tunnels/inspect – public data and hints only)
	and offers a connection test (POST /tunnels/test, handshake with the server).
	<TunnelSection bind:mode bind:credentialId bind:name bind:config cidr={form.cidr} tunnels={wgCreds} status={editing?.tunnel} {errors} />
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { Credential, TunnelStatus, TunnelSummary, TunnelTestResult } from '$lib/api';
	import { Alert, Button, CopyButton, Input, RelativeTime, Select, Textarea } from '$lib/components/ui';
	import { formatBytes } from '$lib/utils/format';
	import TunnelState from './TunnelState.svelte';

	interface Props {
		mode: 'existing' | 'new';
		credentialId: number | null;
		name: string;
		config: string;
		/** subnet routed through the tunnel (for the hints) */
		cidr: string;
		/** stored WireGuard credentials */
		tunnels: Credential[];
		/** current state when editing a subnet that already uses the tunnel */
		status?: TunnelStatus | null;
		errors?: Record<string, string>;
	}

	let {
		mode = $bindable(),
		credentialId = $bindable(),
		name = $bindable(),
		config = $bindable(),
		cidr,
		tunnels,
		status = null,
		errors = {}
	}: Props = $props();

	let summary = $state<TunnelSummary | null>(null);
	let inspectError = $state('');
	let testing = $state(false);
	let result = $state<TunnelTestResult | null>(null);

	const input = $derived(
		mode === 'existing' ? (credentialId ? { credentialId } : null) : config.trim() ? { config } : null
	);
	const liveStatus = $derived(
		mode === 'existing' && status && status.credentialId === credentialId ? status : null
	);

	// inspect the configuration while typing (debounced)
	let seq = 0;
	$effect(() => {
		const body = input;
		const subnets = cidr.trim() ? [cidr.trim()] : [];
		result = null;
		if (!body) {
			summary = null;
			inspectError = '';
			return;
		}
		const my = ++seq;
		const timer = setTimeout(async () => {
			try {
				const s = await api.post('/api/v1/tunnels/inspect', { body: { ...body, subnets } });
				if (my === seq) {
					summary = s;
					inspectError = '';
				}
			} catch (e) {
				if (my === seq) {
					const f = fieldErrors(e);
					summary = null;
					inspectError = f.config ?? f.credentialId ?? errorMessage(e);
				}
			}
		}, 400);
		return () => clearTimeout(timer);
	});

	async function test() {
		if (!input) return;
		testing = true;
		result = null;
		try {
			result = await api.post('/api/v1/tunnels/test', { body: input });
		} catch (e) {
			result = { ok: false, message: errorMessage(e) };
		} finally {
			testing = false;
		}
	}

	let fileInput = $state<HTMLInputElement>();
	let fileError = $state('');
	async function loadFile(e: Event) {
		const el = e.currentTarget as HTMLInputElement;
		const f = el.files?.[0];
		el.value = '';
		fileError = '';
		if (!f) return;
		if (f.size > 64 * 1024) {
			fileError = 'Datei zu groß – eine WireGuard-Konfiguration hat nur wenige Zeilen.';
			return;
		}
		config = (await f.text()).trim() + '\n';
		if (!name.trim()) name = f.name.replace(/\.conf$/i, '');
	}

	const tunnelOptions = $derived(tunnels.map((t) => ({ value: String(t.id), label: t.name })));
</script>

<div class="flex flex-col gap-4 rounded-lg border border-border bg-surface-2 p-4">
	{#if tunnels.length}
		<div class="flex flex-wrap gap-x-5 gap-y-2 text-sm" role="radiogroup" aria-label="Tunnel-Konfiguration">
			<label class="flex cursor-pointer items-center gap-2">
				<input type="radio" value="existing" bind:group={mode} class="accent-(--accent)" />
				Vorhandenen Tunnel verwenden
			</label>
			<label class="flex cursor-pointer items-center gap-2">
				<input type="radio" value="new" bind:group={mode} class="accent-(--accent)" />
				Neue Konfiguration
			</label>
		</div>
	{/if}

	{#if mode === 'existing'}
		<Select
			label="Tunnel"
			required
			value={credentialId ? String(credentialId) : ''}
			options={tunnelOptions}
			placeholder="Tunnel wählen …"
			onchange={(e) => (credentialId = Number((e.currentTarget as HTMLSelectElement).value) || null)}
			error={errors.tunnelCredentialId ?? (inspectError || null)}
			hint="Mehrere Subnetze können denselben Tunnel nutzen, z. B. das LAN und das IPMI-Netz eines Rechenzentrums."
		/>
	{:else}
		<Input
			label="Name des Tunnels"
			bind:value={name}
			required
			maxlength={200}
			placeholder="z. B. Rechenzentrum"
			error={errors.name}
		/>
		<div class="flex flex-col items-start gap-1">
			<Textarea
				label="WireGuard-Konfiguration"
				bind:value={config}
				mono
				rows={9}
				required
				spellcheck={false}
				autocomplete="off"
				class="w-full"
				placeholder={'[Interface]\nPrivateKey = …\nAddress = 10.10.10.3/32\n\n[Peer]\nPublicKey = …\nEndpoint = vpn.example.org:51820\nAllowedIPs = 192.168.1.0/24'}
				hint="Inhalt der .conf-Datei, z. B. aus dem Peer-Generator der OPNsense. Einen eigenen Zugang nur für NetScope anlegen – die Konfiguration eines anderen Geräts nicht wiederverwenden. Der private Schlüssel wird verschlüsselt gespeichert."
				error={errors.config ?? (inspectError || null)}
			/>
			<Button size="xs" variant="ghost" icon="upload" onclick={() => fileInput?.click()}
				>Aus Datei laden</Button
			>
			<input
				bind:this={fileInput}
				type="file"
				accept=".conf,.txt,text/plain"
				class="hidden"
				onchange={loadFile}
				tabindex="-1"
			/>
			{#if fileError}<span class="text-xs text-danger">{fileError}</span>{/if}
		</div>
	{/if}

	{#if summary}
		<dl class="grid grid-cols-1 gap-x-6 gap-y-2.5 text-sm sm:grid-cols-2">
			<div class="min-w-0">
				<dt class="text-xs text-fg-subtle">Gegenstelle</dt>
				<dd class="mono break-all">{summary.endpoint}</dd>
			</div>
			<div class="min-w-0">
				<dt class="text-xs text-fg-subtle">Tunnel-Adresse von NetScope</dt>
				<dd class="mono break-all">{summary.addresses.join(', ')}</dd>
			</div>
			<div class="min-w-0 sm:col-span-2">
				<dt class="text-xs text-fg-subtle">
					Öffentlicher Schlüssel von NetScope (muss auf dem Server als Peer eingetragen sein)
				</dt>
				<dd class="flex items-center gap-1">
					<span class="mono min-w-0 break-all">{summary.publicKey}</span>
					<CopyButton text={summary.publicKey} size="xs" label="Schlüssel kopieren" />
				</dd>
			</div>
			<div class="min-w-0 sm:col-span-2">
				<dt class="text-xs text-fg-subtle">Durch den Tunnel geleitet</dt>
				<dd>
					<span class="mono">{cidr.trim() || '—'}</span>
					<span class="text-xs text-fg-subtle">
						– nur die Subnetze, die diesen Tunnel nutzen; der übrige Verkehr des Servers bleibt unverändert</span
					>
				</dd>
			</div>
		</dl>
		{#if summary.warnings.length}
			<Alert tone="warn" title="Hinweise zur Konfiguration">
				<ul class="list-disc space-y-0.5 pl-4">
					{#each summary.warnings as w (w)}<li>{w}</li>{/each}
				</ul>
			</Alert>
		{/if}
	{/if}

	<div class="flex flex-col gap-2">
		<div>
			<Button size="sm" icon="zap" onclick={test} loading={testing} disabled={!summary || testing}
				>Verbindung testen</Button
			>
		</div>
		{#if result}
			<Alert tone={result.ok ? 'ok' : 'danger'} title={result.ok ? 'Verbindung klappt' : 'Keine Verbindung'}>
				{result.message}
			</Alert>
		{/if}
	</div>

	{#if liveStatus}
		<div class="flex flex-col gap-2 border-t border-border pt-3 text-sm">
			<div class="flex flex-wrap items-center gap-2">
				<TunnelState status={liveStatus} />
				<span class="mono text-xs text-fg-subtle">{liveStatus.interface}</span>
			</div>
			{#if liveStatus.error}<p class="text-xs text-danger">{liveStatus.error}</p>{/if}
			<p class="text-xs text-fg-muted">
				Letzter Handshake:
				{#if liveStatus.lastHandshake}<RelativeTime value={liveStatus.lastHandshake} />{:else}noch keiner{/if}
				· empfangen {formatBytes(liveStatus.rxBytes)} · gesendet {formatBytes(liveStatus.txBytes)}
			</p>
		</div>
	{/if}
</div>
