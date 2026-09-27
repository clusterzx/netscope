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
	import { t } from '$lib/i18n';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { formatBytes, formatDateTime, formatNumber, formatSeconds } from '$lib/utils/format';
	import { markupParts } from './system';

	const info = new AsyncData<SystemInfo>();
	$effect(() => {
		info.run((signal) => api.get('/api/v1/system/info', { signal }));
		const timer = setInterval(() => info.reload(), 30_000);
		return () => clearInterval(timer);
	});

	const i = $derived(info.data);
	const missing = $derived(Object.entries(i?.missingBinaries ?? {}));
	const browserScheme = typeof window !== 'undefined' ? window.location.protocol.replace(':', '') : '';
	const browserHost = typeof window !== 'undefined' ? window.location.host : '';
	const schemeMismatch = $derived(!!i && !!i.client?.scheme && i.client.scheme !== browserScheme);

	// sentences with code or values in bold: split at their placeholders
	const mismatchText = markupParts(
		t(
			'Der Browser nutzt {browser}, der Server sieht {server}. Hinter einem Reverse-Proxy (z. B. Traefik) muss dessen Adresse unter {setting} stehen, damit {header} übernommen wird – sonst stimmen Deep-Links und Cookie-Flags nicht.'
		)
	);
	const browserText = markupParts(
		t('Browser: {url}. Weichen IP oder Schema hinter einem Reverse-Proxy ab, die Trusted Proxies prüfen.')
	);
	const openApiText = markupParts(t('OpenAPI unter {path}'));

	const tiles = $derived(
		i
			? [
					{ label: 'Version', value: i.version, detail: i.goVersion },
					{
						label: t('Laufzeit'),
						value: formatSeconds(i.uptimeSeconds),
						detail: t('seit {date}', { date: formatDateTime(i.startedAt) })
					},
					{
						label: t('Datenbank'),
						value: formatBytes(i.dbSizeBytes),
						detail: t('Schema {version} · NVD-Spiegel {size} (nicht im Backup)', {
							version: i.schemaVersion,
							size: formatBytes(i.nvdSizeBytes)
						})
					},
					{
						label: t('Speicher'),
						value: formatBytes(i.memoryBytes),
						detail: t('{goroutines} Goroutinen · {plugins} Plugins', {
							goroutines: formatNumber(i.goroutines),
							plugins: formatNumber(i.plugins)
						})
					}
				]
			: []
	);
</script>

{#if info.error && !i}
	<ErrorState error={info.error} onretry={() => info.reload()} />
{:else if !i}
	<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
		{#each Array(4) as _, k (k)}<Card><Skeleton lines={5} /></Card>{/each}
	</div>
{:else}
	<div class="flex flex-col gap-4">
		<section aria-label={t('Kennzahlen')} class="grid grid-cols-2 gap-3 md:grid-cols-4">
			{#each tiles as { label, value, detail } (label)}
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
			<Alert tone="warn" title={t('Fehlende Programme')}>
				{t('Einige Plugins können nicht vollständig arbeiten, weil Programme fehlen:')}
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
			<Card title={t('Umgebung')} icon="server">
				<DescList cols={2}>
					<DescItem label={t('Datenverzeichnis')} mono value={i.dataDir} />
					<DescItem label={t('Konfigurationsdatei')} mono value={i.configPath} />
					<DescItem label={t('Datenbank')} mono value={i.dbPath} />
					<DescItem label="Listen" mono value={i.listen} />
					<DescItem label={t('Zeitzone')} value={i.timeZone} />
					<DescItem label={t('Log-Level / Format')} value="{i.logLevel} / {i.logFormat}" />
					<DescItem label={t('Trusted Proxies')} class="sm:col-span-2">
						{#if i.trustedProxies?.length}
							<span class="flex flex-wrap gap-1">
								{#each i.trustedProxies as p (p)}<Badge variant="outline"><span class="mono">{p}</span></Badge
									>{/each}
							</span>
						{:else}
							<span class="text-fg-subtle">{t('keine – X-Forwarded-* wird ignoriert')}</span>
						{/if}
					</DescItem>
				</DescList>
			</Card>

			<Card title={t('Client (wie vom Server gesehen)')} icon="globe">
				<DescList cols={2}>
					<DescItem label={t('IP-Adresse')} mono value={i.client?.ip} />
					<DescItem label={t('Schema')} value={i.client?.scheme} />
					<DescItem label="Host" mono value={i.client?.host} class="sm:col-span-2" />
				</DescList>
				{#if schemeMismatch}
					<Alert tone="warn" title={t('Schema weicht ab')} class="mt-3">
						{#each mismatchText as part, k (k)}{#if k % 2 === 0}{part}{:else if part === 'browser'}<strong
									>{browserScheme}</strong
								>{:else if part === 'server'}<strong>{i.client.scheme}</strong>{:else}<code class="mono"
									>{part === 'setting' ? 'trusted_proxies' : 'X-Forwarded-Proto'}</code
								>{/if}{/each}
					</Alert>
				{:else}
					<p class="mt-3 text-xs text-fg-subtle">
						{#each browserText as part, k (k)}{#if k % 2}<span class="mono"
									>{browserScheme}://{browserHost}</span
								>{:else}{part}{/if}{/each}
					</p>
				{/if}
			</Card>

			<Card title="Vault" icon="lock">
				{#snippet actions()}
					<Button size="xs" variant="ghost" href="?tab=vault" iconRight="arrow-right">{t('Verwalten')}</Button
					>
				{/snippet}
				<DescList cols={2}>
					<DescItem label={t('Schlüssel-ID')}>
						<span class="mono">{i.vaultKeyId}</span>
						<CopyButton text={i.vaultKeyId} label={t('Schlüssel-ID kopieren')} />
					</DescItem>
					<DescItem label={t('Quelle')} value={i.vaultKeySource} />
				</DescList>
			</Card>

			<Card title={t('Schnittstellen')} icon="link">
				<ul class="flex flex-col gap-2 text-sm">
					<li class="flex items-center gap-2">
						<Icon name="file" size={15} class="text-fg-subtle" />
						<a href="/api/docs" class="link" target="_blank" rel="noopener">{t('API-Dokumentation')}</a>
						<span class="text-xs text-fg-subtle"
							>{#each openApiText as part, k (k)}{#if k % 2}<code class="mono">/api/openapi.json</code
									>{:else}{part}{/if}{/each}</span
						>
					</li>
					<li class="flex items-center gap-2">
						<Icon name="activity" size={15} class="text-fg-subtle" />
						<a href="/metrics" class="link" target="_blank" rel="noopener">{t('Prometheus-Metriken')}</a>
						<span class="text-xs text-fg-subtle"
							>{t('/metrics – Zugriff lt. Einstellung „Metriken öffentlich“')}</span
						>
					</li>
					<li class="flex items-center gap-2">
						<Icon name="health" size={15} class="text-fg-subtle" />
						<a href="/api/v1/health" class="link" target="_blank" rel="noopener">{t('Health-Endpoint')}</a>
						<span class="text-xs text-fg-subtle">{t('ohne Anmeldung, für Monitoring')}</span>
					</li>
				</ul>
			</Card>
		</div>
		<p class="text-xs text-fg-subtle">{t('Aktualisiert sich alle 30 Sekunden.')}</p>
	</div>
{/if}
