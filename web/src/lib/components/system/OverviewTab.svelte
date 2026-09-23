<!-- System overview: version, runtime, database, environment, client as seen by the server. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { SystemInfo } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		Card,
		CopyButton,
		DescItem,
		DescList,
		ErrorState,
		Icon,
		Skeleton
	} from '$lib/components/ui';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { formatBytes, formatDateTime, formatNumber, formatSeconds } from '$lib/utils/format';

	const info = new AsyncData<SystemInfo>();
	$effect(() => {
		info.run((signal) => api.get('/api/v1/system/info', { signal }));
		const t = setInterval(() => info.reload(), 30_000);
		return () => clearInterval(t);
	});

	const i = $derived(info.data);
	const missing = $derived(Object.entries(i?.missingBinaries ?? {}));
	const browserScheme = typeof window !== 'undefined' ? window.location.protocol.replace(':', '') : '';
	const browserHost = typeof window !== 'undefined' ? window.location.host : '';
	const schemeMismatch = $derived(!!i && !!i.client?.scheme && i.client.scheme !== browserScheme);
</script>

{#if info.error && !i}
	<ErrorState error={info.error} onretry={() => info.reload()} />
{:else if !i}
	<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
		{#each Array(4) as _, k (k)}<Card><Skeleton lines={5} /></Card>{/each}
	</div>
{:else}
	<div class="flex flex-col gap-4">
		<section aria-label="Kennzahlen" class="grid grid-cols-2 gap-3 md:grid-cols-4">
			{#each [['Version', i.version, i.goVersion], ['Laufzeit', formatSeconds(i.uptimeSeconds), `seit ${formatDateTime(i.startedAt)}`], ['Datenbank', formatBytes(i.dbSizeBytes), `Schema-Version ${i.schemaVersion}`], ['Speicher', formatBytes(i.memoryBytes), `${formatNumber(i.goroutines)} Goroutinen · ${i.plugins} Plugins`]] as [label, value, detail] (label)}
				<div class="rounded-lg border border-border bg-surface p-3.5 shadow-sm">
					<div class="text-[0.8125rem] font-medium text-fg-muted">{label}</div>
					<div class="mt-0.5 truncate text-xl font-semibold tracking-tight tabular" title={value}>
						{value}
					</div>
					<div class="truncate text-xs text-fg-subtle" title={detail}>{detail}</div>
				</div>
			{/each}
		</section>

		{#if missing.length}
			<Alert tone="warn" title="Fehlende Programme">
				Einige Plugins können nicht vollständig arbeiten, weil Programme fehlen:
				<ul class="mt-1 list-disc pl-5">
					{#each missing as [plugin, bins] (plugin)}
						<li>
							<a href="/plugins/{plugin}" class="link">{plugin}</a>:
							{#each bins as b, k (b)}<code class="mono">{b}</code>{k < bins.length - 1 ? ', ' : ''}{/each}
						</li>
					{/each}
				</ul>
			</Alert>
		{/if}

		<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
			<Card title="Umgebung" icon="server">
				<DescList cols={2}>
					<DescItem label="Datenverzeichnis" mono value={i.dataDir} />
					<DescItem label="Konfigurationsdatei" mono value={i.configPath} />
					<DescItem label="Datenbank" mono value={i.dbPath} />
					<DescItem label="Listen" mono value={i.listen} />
					<DescItem label="Zeitzone" value={i.timeZone} />
					<DescItem label="Log-Level / Format" value="{i.logLevel} / {i.logFormat}" />
					<DescItem label="Trusted Proxies" class="sm:col-span-2">
						{#if i.trustedProxies?.length}
							<span class="flex flex-wrap gap-1">
								{#each i.trustedProxies as p (p)}<Badge variant="outline"><span class="mono">{p}</span></Badge
									>{/each}
							</span>
						{:else}
							<span class="text-fg-subtle">keine – X-Forwarded-* wird ignoriert</span>
						{/if}
					</DescItem>
				</DescList>
			</Card>

			<Card title="Client (wie vom Server gesehen)" icon="globe">
				<DescList cols={2}>
					<DescItem label="IP-Adresse" mono value={i.client?.ip} />
					<DescItem label="Schema" value={i.client?.scheme} />
					<DescItem label="Host" mono value={i.client?.host} class="sm:col-span-2" />
				</DescList>
				{#if schemeMismatch}
					<Alert tone="warn" title="Schema weicht ab" class="mt-3">
						Der Browser nutzt <strong>{browserScheme}</strong>, der Server sieht
						<strong>{i.client.scheme}</strong>. Hinter einem Reverse-Proxy (z. B. Traefik) muss dessen Adresse
						unter <code class="mono">trusted_proxies</code> stehen, damit
						<code class="mono">X-Forwarded-Proto</code> übernommen wird – sonst stimmen Deep-Links und Cookie-Flags
						nicht.
					</Alert>
				{:else}
					<p class="mt-3 text-xs text-fg-subtle">
						Browser: <span class="mono">{browserScheme}://{browserHost}</span>. Weichen IP oder Schema hinter
						einem Reverse-Proxy ab, die Trusted Proxies prüfen.
					</p>
				{/if}
			</Card>

			<Card title="Vault" icon="lock">
				{#snippet actions()}
					<Button size="xs" variant="ghost" href="?tab=vault" iconRight="arrow-right">Verwalten</Button>
				{/snippet}
				<DescList cols={2}>
					<DescItem label="Schlüssel-ID">
						<span class="mono">{i.vaultKeyId}</span>
						<CopyButton text={i.vaultKeyId} label="Schlüssel-ID kopieren" />
					</DescItem>
					<DescItem label="Quelle" value={i.vaultKeySource} />
				</DescList>
			</Card>

			<Card title="Schnittstellen" icon="link">
				<ul class="flex flex-col gap-2 text-sm">
					<li class="flex items-center gap-2">
						<Icon name="file" size={15} class="text-fg-subtle" />
						<a href="/api/docs" class="link" target="_blank" rel="noopener">API-Dokumentation</a>
						<span class="text-xs text-fg-subtle"
							>OpenAPI unter <code class="mono">/api/openapi.json</code></span
						>
					</li>
					<li class="flex items-center gap-2">
						<Icon name="activity" size={15} class="text-fg-subtle" />
						<a href="/metrics" class="link" target="_blank" rel="noopener">Prometheus-Metriken</a>
						<span class="text-xs text-fg-subtle"
							>/metrics – Zugriff lt. Einstellung „Metriken öffentlich“</span
						>
					</li>
					<li class="flex items-center gap-2">
						<Icon name="health" size={15} class="text-fg-subtle" />
						<a href="/api/v1/health" class="link" target="_blank" rel="noopener">Health-Endpoint</a>
						<span class="text-xs text-fg-subtle">ohne Anmeldung, für Monitoring</span>
					</li>
				</ul>
			</Card>
		</div>
		<p class="text-xs text-fg-subtle">Aktualisiert sich alle 30 Sekunden.</p>
	</div>
{/if}
