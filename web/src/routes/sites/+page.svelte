<!-- Sites of a central instance: connection state, last report, buffer, plugins with errors;
     create (token shown once), rename, new token, remove. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { Site, SiteCreated } from '$lib/api';
	import StrongConfirm from '$lib/components/system/StrongConfirm.svelte';
	import { apiErrors } from '$lib/components/system/system';
	import {
		Alert,
		Badge,
		Button,
		Card,
		CopyButton,
		DescItem,
		DescList,
		EmptyState,
		ErrorState,
		Icon,
		Input,
		Menu,
		Modal,
		PageHeader,
		RelativeTime,
		Skeleton
	} from '$lib/components/ui';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { federation, siteFilter } from '$lib/stores/federation.svelte';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDateTime, formatNumber, plural } from '$lib/utils/format';

	const list = new AsyncData<Site[]>();
	$effect(() => {
		list.run(async (signal) => (await api.get('/api/v1/sites', { signal })) ?? []);
	});
	$effect(() =>
		live.on('system', (m) => {
			if (m.type === 'sites' || m.type === 'federation') list.reload();
		})
	);
	$effect(() => {
		const t = setInterval(() => list.reload(), 30_000);
		return () => clearInterval(t);
	});

	const isCentral = $derived(federation.role === 'central');
	const origin = typeof window !== 'undefined' ? window.location.origin : '';

	function failedPlugins(s: Site) {
		return (s.status?.plugins ?? []).filter((p) => p.status === 'failed' || p.status === 'timeout');
	}

	// ---------------------------------------------------------------- create / edit
	let editOpen = $state(false);
	let editing = $state<Site | null>(null);
	let name = $state('');
	let url = $state('');
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	let created = $state<SiteCreated | null>(null);

	function openCreate() {
		editing = null;
		name = '';
		url = '';
		errors = {};
		general = null;
		created = null;
		editOpen = true;
	}

	function openEdit(s: Site) {
		editing = s;
		name = s.name;
		url = s.url;
		errors = {};
		general = null;
		created = null;
		editOpen = true;
	}

	async function save() {
		general = null;
		const e: Record<string, string> = {};
		if (!name.trim()) e.name = 'Name erforderlich';
		if (url.trim()) {
			try {
				const u = new URL(url.trim());
				if ((u.protocol !== 'http:' && u.protocol !== 'https:') || !u.host) throw new Error();
			} catch {
				e.url = 'Gültige http(s)-URL erwartet';
			}
		}
		errors = e;
		if (Object.keys(e).length) return;
		saving = true;
		try {
			const body = { name: name.trim(), url: url.trim() };
			if (editing) {
				await api.patch('/api/v1/sites/{id}', { path: { id: editing.id }, body });
				toast.success(`Standort „${body.name}“ gespeichert`);
				editOpen = false;
			} else {
				created = await api.post('/api/v1/sites', { body });
			}
			list.reload();
			federation.refresh().catch(() => {});
		} catch (err) {
			({ errors, general } = apiErrors(err, ['name', 'url']));
		} finally {
			saving = false;
		}
	}

	// ---------------------------------------------------------------- token
	let tokenOpen = $state(false);
	let tokenFor = $state<Site | null>(null);
	let newToken = $state('');

	async function rotate(s: Site) {
		const ok = await confirm({
			title: `Neues Token für „${s.name}“?`,
			message:
				'Das bisherige Token gilt sofort nicht mehr. Bis das neue Token am Standort eingetragen ist, puffert der Standort seine Daten.',
			confirmLabel: 'Neues Token erzeugen',
			danger: true
		});
		if (!ok) return;
		try {
			const res = await api.post('/api/v1/sites/{id}/token', { path: { id: s.id } });
			tokenFor = s;
			newToken = res.token;
			tokenOpen = true;
			list.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	// ---------------------------------------------------------------- delete
	let deleteOpen = $state(false);
	let deleting = $state<Site | null>(null);
	let deleteBusy = $state(false);

	async function remove() {
		if (!deleting) return;
		deleteBusy = true;
		try {
			await api.delete('/api/v1/sites/{id}', { path: { id: deleting.id } });
			toast.success(`Standort „${deleting.name}“ entfernt`);
			if (siteFilter.value === deleting.slug) siteFilter.value = '';
			deleteOpen = false;
			list.reload();
			federation.refresh().catch(() => {});
		} catch (e) {
			toast.error(e);
		} finally {
			deleteBusy = false;
		}
	}

	function setupText(token: string) {
		return `NETSCOPE_CENTRAL_URL=${origin}\nNETSCOPE_CENTRAL_TOKEN=${token}`;
	}
</script>

<PageHeader
	title="Standorte"
	description="NetScope-Instanzen, die ihre Netze an diese Zentrale liefern. Die Zentrale liest nur – gescannt und konfiguriert wird am Standort."
>
	{#snippet actions()}
		{#if isCentral}
			<Button variant="primary" icon="plus" onclick={openCreate}>Standort anlegen</Button>
		{/if}
	{/snippet}
</PageHeader>

{#if !isCentral}
	<Alert tone="info" title="Diese Instanz ist keine Zentrale">
		Standorte können nur an einer Zentrale angelegt werden. Die Rolle wird unter
		<a href="/system?tab=federation" class="text-accent hover:underline">System → Verbund</a> festgelegt.
	</Alert>
{:else if list.error && !list.data}
	<ErrorState error={list.error} onretry={() => list.reload()} />
{:else if !list.data}
	<div class="grid grid-cols-1 gap-4 xl:grid-cols-2"><Card><Skeleton lines={6} /></Card></div>
{:else if list.data.length === 0}
	<Card>
		<EmptyState
			icon="globe"
			title="Noch keine Standorte"
			description="Einen Standort anlegen, das Token am Standort unter System → Verbund eintragen – ab dann liefert er seine Geräte, Events und Zustände hierher."
		>
			{#snippet actions()}
				<Button variant="primary" icon="plus" onclick={openCreate}>Standort anlegen</Button>
			{/snippet}
		</EmptyState>
	</Card>
{:else}
	<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
		{#each list.data as s (s.id)}
			{@const failed = failedPlugins(s)}
			<Card>
				{#snippet header()}
					<div class="flex items-start gap-3">
						<div class="min-w-0 flex-1">
							<h2 class="flex flex-wrap items-center gap-2 text-base font-semibold text-fg">
								{s.name}
								{#if s.connected}
									<Badge tone="ok" dot>verbunden</Badge>
								{:else if s.lastContact}
									<Badge tone="danger" dot>meldet sich nicht</Badge>
								{:else}
									<Badge tone="neutral">wartet auf erste Meldung</Badge>
								{/if}
								{#if s.status?.syncing}<Badge tone="info">Abgleich</Badge>{/if}
							</h2>
							<p class="mono text-xs text-fg-subtle">site:{s.slug} · Token {s.tokenPrefix}…</p>
						</div>
						{#if s.linkUrl}
							<a
								href={s.linkUrl}
								target="_blank"
								rel="noopener"
								class="inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-[0.8125rem] font-medium text-fg-muted hover:bg-surface-3 hover:text-fg"
								title="Oberfläche des Standorts öffnen"><Icon name="external" size={14} />Öffnen</a
							>
						{/if}
						<Menu
							label="Aktionen für {s.name}"
							items={[
								{ label: 'Bearbeiten', icon: 'edit', onclick: () => openEdit(s) },
								{
									label: 'Geräte anzeigen',
									icon: 'devices',
									href: `/devices?q=${encodeURIComponent('site:' + s.slug)}`
								},
								{ label: 'Neues Token', icon: 'key', onclick: () => rotate(s) },
								{ separator: true },
								{
									label: 'Entfernen …',
									icon: 'trash',
									danger: true,
									onclick: () => {
										deleting = s;
										deleteOpen = true;
									}
								}
							]}
						/>
					</div>
				{/snippet}
				<DescList cols={3}>
					<DescItem label="Letzte Meldung">
						{#if s.lastContact}<RelativeTime value={s.lastContact} />{:else}<span class="text-fg-subtle"
								>nie</span
							>{/if}
					</DescItem>
					<DescItem label="Geräte hier" hint={s.devices ? `${formatNumber(s.online)} online` : undefined}>
						<span class="tabular">{formatNumber(s.devices)}</span>
					</DescItem>
					<DescItem label="Im Puffer des Standorts">
						<span class="tabular {s.status?.buffered ? 'text-warn' : ''}"
							>{formatNumber(s.status?.buffered ?? 0)}</span
						>
					</DescItem>
					<DescItem label="Version" value={s.instance?.version} mono />
					<DescItem label="Host" value={s.instance?.hostname} mono />
					<DescItem label="Adresse" value={s.lastIp} mono />
				</DescList>
				{#if s.status?.subnets?.length}
					<div class="mt-4">
						<p class="mb-1 text-xs text-fg-subtle">Subnetze am Standort</p>
						<div class="flex flex-wrap gap-1.5">
							{#each s.status.subnets as sn (sn.cidr)}
								<Badge variant="outline" title={sn.name || undefined}
									><span class="mono">{sn.cidr}</span>{#if sn.name}&nbsp;· {sn.name}{/if}
									· {sn.devices}</Badge
								>
							{/each}
						</div>
					</div>
				{/if}
				{#if failed.length}
					<Alert
						tone="warn"
						class="mt-4"
						title={plural(failed.length, 'Plugin mit Fehler', 'Plugins mit Fehlern')}
					>
						<ul class="flex flex-col gap-1">
							{#each failed as p (p.id)}
								<li>
									<span class="font-medium">{p.name}</span>
									{#if p.lastRun}<span class="text-fg-subtle"> · {formatDateTime(p.lastRun)}</span>{/if}
									{#if p.error}<span class="block text-xs text-fg-muted">{p.error}</span>{/if}
								</li>
							{/each}
						</ul>
					</Alert>
				{/if}
			</Card>
		{/each}
	</div>
{/if}

<Modal
	bind:open={editOpen}
	title={created
		? 'Standort angelegt'
		: editing
			? `Standort „${editing.name}“ bearbeiten`
			: 'Standort anlegen'}
	size="md"
	as="form"
	onsubmit={() => (created ? (editOpen = false) : save())}
	busy={saving}
>
	{#if created}
		<div class="flex flex-col gap-3 text-sm">
			<Alert tone="warn" title="Token nur jetzt sichtbar">
				Das Token am Standort unter System → Verbund eintragen (Rolle „Standort“, Adresse dieser Zentrale). Es
				wird hier nicht noch einmal angezeigt.
			</Alert>
			<div class="flex items-center gap-2 rounded-md border border-border bg-surface-2 py-1.5 pr-1.5 pl-3">
				<code class="mono min-w-0 flex-1 text-[0.8rem] break-all select-all" aria-label="Token des Standorts"
					>{created.token}</code
				>
				<CopyButton text={created.token} label="Token kopieren" size="sm" />
			</div>
			<DescList cols={2}>
				<DescItem label="Adresse der Zentrale" value={origin} mono />
				<DescItem label="Filter" value={created.site ? `site:${created.site.slug}` : ''} mono />
			</DescList>
			<p class="text-fg-muted">
				Ein Standort ohne Oberfläche (reiner Sammler) wird stattdessen per Umgebungsvariablen angebunden:
			</p>
			<pre
				class="mono rounded-md bg-surface-2 px-3 py-2 text-xs break-all whitespace-pre-wrap text-fg-muted">{setupText(
					created.token
				)}
NETSCOPE_UI=false</pre>
		</div>
	{:else}
		<div class="flex flex-col gap-4">
			{#if general}<Alert tone="danger">{general}</Alert>{/if}
			<Input
				label="Name"
				bind:value={name}
				required
				maxlength={64}
				placeholder="z. B. Colo oder Büro Süd"
				hint="Erscheint in der Standort-Auswahl, an Geräten und in Benachrichtigungen."
				error={errors.name}
			/>
			<Input
				label="Adresse der Oberfläche (optional)"
				type="url"
				bind:value={url}
				placeholder="https://colo-netscope.example.org"
				hint="Für Direktlinks. Leer lassen, um die vom Standort gemeldete öffentliche URL zu nutzen."
				error={errors.url}
			/>
		</div>
	{/if}
	{#snippet footer()}
		{#if created}
			<Button type="submit" variant="primary">Fertig</Button>
		{:else}
			<Button onclick={() => (editOpen = false)} disabled={saving}>Abbrechen</Button>
			<Button type="submit" variant="primary" icon={editing ? 'save' : 'plus'} loading={saving}
				>{editing ? 'Speichern' : 'Anlegen'}</Button
			>
		{/if}
	{/snippet}
</Modal>

<Modal bind:open={tokenOpen} title="Neues Token für „{tokenFor?.name}“" size="md">
	<div class="flex flex-col gap-3 text-sm">
		<Alert tone="warn" title="Nur jetzt sichtbar">
			Am Standort unter System → Verbund eintragen. Das alte Token gilt nicht mehr.
		</Alert>
		<div class="flex items-center gap-2 rounded-md border border-border bg-surface-2 py-1.5 pr-1.5 pl-3">
			<code class="mono min-w-0 flex-1 text-[0.8rem] break-all select-all">{newToken}</code>
			<CopyButton text={newToken} label="Token kopieren" size="sm" />
		</div>
	</div>
	{#snippet footer()}
		<Button variant="primary" onclick={() => (tokenOpen = false)}>Fertig</Button>
	{/snippet}
</Modal>

<StrongConfirm
	bind:open={deleteOpen}
	title="Standort „{deleting?.name}“ entfernen?"
	word={deleting?.name ?? ''}
	confirmLabel="Entfernen"
	busy={deleteBusy}
	onconfirm={remove}
>
	<p>
		Entfernt den Standort mit {plural(deleting?.devices ?? 0, 'geliefertem Gerät', 'gelieferten Geräten')} und seinen
		Events aus dieser Zentrale. Am Standort selbst wird nichts gelöscht; er kann danach nichts mehr einliefern.
	</p>
</StrongConfirm>
