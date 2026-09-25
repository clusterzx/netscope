<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import type { ApiCveDetail, CveDeviceCVE } from '$lib/api/generated';
	import CvssCard from '$lib/components/vulnerabilities/CvssCard.svelte';
	import Disclaimer from '$lib/components/vulnerabilities/Disclaimer.svelte';
	import IgnoreDialog, { type IgnoreTarget } from '$lib/components/vulnerabilities/IgnoreDialog.svelte';
	import {
		cweUrl,
		matchTypeHint,
		matchTypeLabel,
		matchTypeTone,
		nvdStatusLabel,
		versionRange
	} from '$lib/components/vulnerabilities/cve';
	import {
		Alert,
		Badge,
		Button,
		Card,
		CopyButton,
		EmptyState,
		ErrorState,
		Icon,
		PageHeader,
		RelativeTime,
		Skeleton
	} from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { formatDate, formatDateTime, formatNumber, plural } from '$lib/utils/format';

	const id = $derived((page.params.cve ?? '').toUpperCase());
	const highlight = $derived(Number(page.url.searchParams.get('device') ?? 0));

	const data = new AsyncData<ApiCveDetail>();
	$effect(() => {
		const cve = id;
		data.run((signal) => api.get('/api/v1/vulnerabilities/{cve}', { path: { cve }, signal }), true);
	});
	$effect(() => runs.onFinished((r) => r.pluginId === 'cve' && data.reload()));

	const info = $derived(data.data?.cve);
	const devices = $derived(data.data?.devices ?? []);
	const activeDevices = $derived(devices.filter((d) => !d.ignored).length);
	const notFound = $derived(data.error instanceof ApiError && data.error.status === 404);

	// first device row carries CVE data too (for CVEs missing in the mirror)
	const first = $derived(devices[0]);
	const cvss = $derived(info?.cvss ?? first?.cvss);
	const vector = $derived(info?.vector || first?.vector || '');
	const version = $derived(info?.cvssVersion || first?.cvssVersion || '');
	const severity = $derived(
		info?.severity && info.severity !== 'unknown' ? info.severity : (first?.severity ?? '')
	);
	const description = $derived(info?.description || first?.description || '');
	const refs = $derived(info?.refs?.length ? info.refs : (first?.refs ?? []));
	const cwes = $derived(info?.cwes?.length ? info.cwes : (first?.cwes ?? []));

	let showAllCpe = $state(false);
	const cpeShown = $derived(showAllCpe ? (info?.cpeMatches ?? []) : (info?.cpeMatches ?? []).slice(0, 12));

	const canManage = $derived(auth.can('vulns.manage'));
	let ignoreOpen = $state(false);
	let ignoreTarget = $state<IgnoreTarget | null>(null);
	function openIgnore(d: CveDeviceCVE) {
		ignoreTarget = {
			deviceId: d.deviceId,
			deviceName: d.deviceName,
			cve: d.cve,
			ignored: d.ignored,
			note: d.ignoreNote
		};
		ignoreOpen = true;
	}

	function host(url: string): string {
		try {
			return new URL(url).host;
		} catch {
			return url;
		}
	}
</script>

<PageHeader title={id} docTitle="{id} · Schwachstellen">
	{#snippet breadcrumb()}
		<a href="/vulnerabilities" class="link">Schwachstellen</a> <span aria-hidden="true">/</span> {id}
	{/snippet}
	{#snippet meta()}
		{#if data.data}
			{#if info?.status}
				<Badge tone={info.status === 'Rejected' ? 'danger' : 'neutral'} title="NVD-Status"
					>{nvdStatusLabel[info.status] ?? info.status}</Badge
				>
			{/if}
			{#if info?.published}<span>Veröffentlicht {formatDate(info.published)}</span>{/if}
			{#if info?.lastModified}<span class="text-fg-subtle">Geändert {formatDate(info.lastModified)}</span
				>{/if}
		{/if}
	{/snippet}
	{#snippet actions()}
		<CopyButton text={id} label="CVE-ID kopieren" size="sm" />
		<a
			href={info?.url || `https://nvd.nist.gov/vuln/detail/${id}`}
			target="_blank"
			rel="noopener noreferrer"
			class="inline-flex h-8.5 items-center gap-1.5 rounded-md border border-border bg-surface px-3.5 text-sm font-medium text-fg shadow-sm transition-colors hover:border-border-strong hover:bg-surface-2"
		>
			In der NVD öffnen <Icon name="external" size={15} />
		</a>
	{/snippet}
</PageHeader>

{#if notFound}
	<EmptyState
		icon="search"
		title="CVE nicht gefunden"
		description="{id} ist weder in der lokalen NVD-Kopie noch bei einem Gerät bekannt."
	>
		{#snippet actions()}
			<Button href="/vulnerabilities" icon="arrow-left">Zur Liste</Button>
		{/snippet}
	</EmptyState>
{:else if data.error}
	<ErrorState error={data.error} onretry={() => data.reload()} />
{:else if !data.data}
	<div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
		<div class="flex flex-col gap-4 lg:col-span-2">
			<Card><Skeleton lines={4} /></Card>
			<Card><Skeleton rows={4} /></Card>
		</div>
		<Card><Skeleton lines={8} /></Card>
	</div>
{:else}
	<div class="flex flex-col gap-4">
		<Disclaimer text={data.data.disclaimer} compact />
		{#if info && !info.inMirror}
			<Alert tone="info" title="Nicht in der lokalen NVD-Kopie">
				Die CVE ist nur aus Geräte-Treffern bekannt – z. B. weil die NVD sie inzwischen zurückgezogen hat oder
				noch nicht analysiert wurde. Die Angaben stammen aus dem letzten Abgleich.
			</Alert>
		{/if}

		<div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
			<div class="flex min-w-0 flex-col gap-4 lg:col-span-2">
				<Card title="Beschreibung" icon="note">
					{#if description}
						<p class="text-sm leading-relaxed break-words whitespace-pre-line text-fg">{description}</p>
					{:else}
						<p class="text-sm text-fg-subtle">Keine Beschreibung vorhanden.</p>
					{/if}
				</Card>

				<Card
					title="Betroffene Geräte"
					description="{formatNumber(activeDevices)} relevant{devices.length !== activeDevices
						? `, ${devices.length - activeDevices} als irrelevant markiert`
						: ''}"
					icon="devices"
					padding="none"
				>
					{#if devices.length === 0}
						<EmptyState
							compact
							icon="check-circle"
							title="Kein Gerät betroffen"
							description="Der letzte Abgleich hat diese CVE bei keinem Gerät gefunden."
						/>
					{:else}
						<ul class="divide-y divide-border">
							{#each devices as d (d.id)}
								<li
									class="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-start {d.deviceId === highlight
										? 'bg-accent-soft'
										: ''}"
								>
									<div class="min-w-0 flex-1 {d.ignored ? 'opacity-70' : ''}">
										<div class="flex flex-wrap items-center gap-1.5">
											<a href="/devices/{d.deviceId}" class="link font-medium">{d.deviceName}</a>
											<Badge tone={matchTypeTone(d.matchType)} title={matchTypeHint[d.matchType]}>
												{matchTypeLabel[d.matchType] ?? d.matchType}
											</Badge>
											{#if d.ignored}<Badge tone="neutral">irrelevant</Badge>{/if}
										</div>
										<dl class="mt-1 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
											<dt class="text-fg-subtle">Produkt</dt>
											<dd class="min-w-0 break-words text-fg">
												{d.product || '–'}{#if d.version}{' '}<span class="mono text-fg-muted"
														>{d.version}</span
													>{/if}
											</dd>
											{#if d.cpe}
												<dt class="text-fg-subtle">CPE</dt>
												<dd class="mono min-w-0 break-all text-fg-muted">{d.cpe}</dd>
											{/if}
											<dt class="text-fg-subtle">Quelle</dt>
											<dd class="text-fg-muted">
												{d.source || '–'} · gefunden {formatDateTime(d.firstSeen)} · zuletzt
												<RelativeTime value={d.lastSeen} />
											</dd>
											{#if d.ignored}
												<dt class="text-fg-subtle">Markiert</dt>
												<dd class="text-fg-muted">
													{d.ignoredBy || '–'}{#if d.ignoredAt}, {formatDateTime(
															d.ignoredAt
														)}{/if}{#if d.ignoreNote}{' '}– „{d.ignoreNote}“{/if}
												</dd>
											{/if}
										</dl>
									</div>
									{#if canManage}
										<Button
											size="sm"
											variant={d.ignored ? 'secondary' : 'ghost'}
											icon={d.ignored ? 'eye' : 'eye-off'}
											class="self-start"
											onclick={() => openIgnore(d)}
										>
											{d.ignored ? 'Wieder relevant' : 'Als irrelevant markieren'}
										</Button>
									{/if}
								</li>
							{/each}
						</ul>
					{/if}
				</Card>

				{#if info?.inMirror}
					<Card
						title="Betroffene Konfigurationen (CPE)"
						description="{plural(info.cpeMatchesTotal, 'Eintrag', 'Einträge')} laut NVD{info.cpeMatchesTotal >
						(info.cpeMatches?.length ?? 0)
							? `, die ersten ${info.cpeMatches?.length ?? 0} angezeigt`
							: ''}"
						icon="package"
						padding="none"
					>
						{#if (info.cpeMatches ?? []).length === 0}
							<p class="px-4 py-3 text-sm text-fg-subtle">Keine CPE-Angaben vorhanden.</p>
						{:else}
							<div class="overflow-x-auto">
								<table class="w-full text-sm">
									<caption class="sr-only">CPE-Kriterien</caption>
									<thead class="bg-surface-2 text-xs text-fg-muted">
										<tr>
											<th scope="col" class="px-4 py-2 text-left font-semibold">Hersteller / Produkt</th>
											<th scope="col" class="px-4 py-2 text-left font-semibold">Versionen</th>
											<th scope="col" class="hidden px-4 py-2 text-left font-semibold md:table-cell"
												>Kriterium</th
											>
										</tr>
									</thead>
									<tbody>
										{#each cpeShown as m, i (i)}
											<tr class="border-t border-border align-top">
												<td class="px-4 py-1.5">
													<span class="text-fg">{m.vendor}</span>
													<span class="text-fg-subtle">/</span>
													<span class="font-medium">{m.product}</span>
													{#if m.part === 'o'}<Badge class="ml-1">OS</Badge>{:else if m.part === 'h'}<Badge
															class="ml-1">Hardware</Badge
														>{/if}
												</td>
												<td class="mono px-4 py-1.5 text-xs whitespace-nowrap text-fg-muted"
													>{versionRange(m)}</td
												>
												<td class="mono hidden px-4 py-1.5 text-xs break-all text-fg-subtle md:table-cell"
													>{m.criteria}</td
												>
											</tr>
										{/each}
									</tbody>
								</table>
							</div>
							{#if (info.cpeMatches?.length ?? 0) > 12}
								<div class="border-t border-border px-4 py-2">
									<Button size="xs" variant="ghost" onclick={() => (showAllCpe = !showAllCpe)}>
										{showAllCpe ? 'Weniger anzeigen' : `Alle ${info.cpeMatches.length} anzeigen`}
									</Button>
								</div>
							{/if}
						{/if}
					</Card>
				{/if}
			</div>

			<div class="flex min-w-0 flex-col gap-4">
				<CvssCard {cvss} {vector} {version} {severity} />

				<Card title="Schwachstellentyp (CWE)" icon="bug">
					{#if cwes.length === 0}
						<p class="text-sm text-fg-subtle">Keine CWE angegeben.</p>
					{:else}
						<ul class="flex flex-wrap gap-1.5">
							{#each cwes as w (w)}
								{@const url = cweUrl(w)}
								<li>
									{#if url}
										<a
											href={url}
											target="_blank"
											rel="noopener noreferrer"
											class="mono inline-flex items-center gap-1 rounded bg-surface-3 px-1.5 py-0.5 text-xs text-accent hover:underline"
											>{w}<Icon name="external" size={11} /></a
										>
									{:else}
										<span class="mono rounded bg-surface-3 px-1.5 py-0.5 text-xs text-fg-muted">{w}</span>
									{/if}
								</li>
							{/each}
						</ul>
					{/if}
				</Card>

				<Card
					title="Referenzen"
					description={plural(refs.length, 'Link', 'Links')}
					icon="link"
					padding="none"
				>
					{#if refs.length === 0}
						<p class="px-4 py-3 text-sm text-fg-subtle">Keine Referenzen vorhanden.</p>
					{:else}
						<ul class="max-h-[32rem] divide-y divide-border overflow-auto">
							{#each refs as r, i (i)}
								<li class="px-4 py-2">
									<a
										href={r.url}
										target="_blank"
										rel="noopener noreferrer"
										class="link block truncate text-sm"
										title={r.url}>{host(r.url)}</a
									>
									<span class="block truncate text-xs text-fg-subtle" title={r.url}>{r.url}</span>
									{#if r.tags?.length}
										<span class="mt-1 flex flex-wrap gap-1">
											{#each r.tags as t (t)}<Badge
													tone={t.includes('Advisory') || t === 'Patch' ? 'accent' : 'neutral'}>{t}</Badge
												>{/each}
										</span>
									{/if}
								</li>
							{/each}
						</ul>
					{/if}
				</Card>
			</div>
		</div>
	</div>
{/if}

<IgnoreDialog bind:open={ignoreOpen} target={ignoreTarget} ondone={() => data.reload()} />
