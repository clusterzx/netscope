<script lang="ts">
	import { api, ApiError, errorMessage } from '$lib/api';
	import type { Credential, CredentialType } from '$lib/api';
	import type { ApiCredentialUse as CredentialUse } from '$lib/api/generated';
	import {
		Alert,
		Badge,
		Button,
		EmptyState,
		ErrorState,
		Icon,
		Menu,
		PageHeader,
		RelativeTime,
		Table
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import CredentialForm from '$lib/components/credentials/CredentialForm.svelte';
	import { credentialScopeLevel, credentialScopeSummary } from '$lib/components/plugins/plugin';
	import { credentials as credentialCatalog, groups } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDateTime } from '$lib/utils/format';

	type Data = { list: Credential[]; types: CredentialType[] };

	const data = new AsyncData<Data>();
	$effect(() => {
		data.run(async (signal) => {
			const [list, types] = await Promise.all([
				api.get('/api/v1/credentials', { signal }),
				api.get('/api/v1/credentials/types', { signal })
			]);
			return { list: list ?? [], types: types ?? [] };
		});
	});

	$effect(() => {
		groups.load().catch(() => {});
	});
	const groupName = (id: number) => groups.value?.find((g) => g.id === id)?.name ?? `#${id}`;
	const levelTone = { device: 'accent', selection: 'accent', subnet: 'info', everywhere: 'neutral' } as const;
	const levelIcon = { device: 'server', selection: 'tag', subnet: 'network', everywhere: 'globe' } as const;

	const list = $derived(data.data?.list ?? []);
	const types = $derived(data.data?.types ?? []);
	const typeOf = (t: string) => types.find((x) => x.type === t);

	const pluginHref = (id: string) => `/plugins/${encodeURIComponent(id)}`;
	const useHref = (u: CredentialUse) => (u.kind === 'subnet' ? '/system?tab=subnets' : pluginHref(u.id));
	const usedNames = (c: Credential) => (c.usedBy ?? []).map((u) => u.name).join(', ');

	function fieldLabel(type: string, key: string): string {
		return typeOf(type)?.schema?.fields?.find((f) => f.key === key)?.label ?? key;
	}

	/** public fields in schema order */
	function publicFields(c: Credential): [string, string][] {
		const fields = typeOf(c.type)?.schema?.fields ?? [];
		const pub = c.public ?? {};
		const keys = [
			...fields.map((f) => f.key).filter((k) => k in pub),
			...Object.keys(pub).filter((k) => !fields.some((f) => f.key === k))
		];
		return keys.filter((k) => pub[k] !== '').map((k) => [k, pub[k]]);
	}

	// ---------------------------------------------------------------- create / edit
	let formOpen = $state(false);
	let editing = $state<Credential | null>(null);

	function openCreate() {
		editing = null;
		formOpen = true;
	}
	function openEdit(c: Credential) {
		editing = c;
		formOpen = true;
	}
	function onsaved(c: Credential) {
		toast.success(editing ? 'Credential gespeichert' : 'Credential angelegt', { title: c.name });
		data.reload();
		credentialCatalog.refresh().catch(() => {});
	}

	// ---------------------------------------------------------------- delete
	let blocked = $state<{ name: string; message: string; usedBy: CredentialUse[] } | null>(null);

	async function remove(c: Credential) {
		const used = c.usedBy ?? [];
		const ok = await confirm({
			title: `Credential „${c.name}“ löschen?`,
			message: used.length
				? `Achtung: Es wird noch verwendet von ${usedNames(c)}. Es muss zuerst dort entfernt werden.`
				: 'Die verschlüsselten Zugangsdaten werden endgültig gelöscht.',
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		blocked = null;
		try {
			await api.delete('/api/v1/credentials/{id}', { path: { id: c.id } });
			toast.success(`Credential „${c.name}“ gelöscht`);
			data.reload();
			credentialCatalog.refresh().catch(() => {});
		} catch (e) {
			if (e instanceof ApiError && e.status === 409) {
				blocked = { name: c.name, message: e.message, usedBy: used };
				requestAnimationFrame(() =>
					document.getElementById('cred-blocked')?.scrollIntoView({ block: 'nearest' })
				);
			} else toast.error(errorMessage(e), { title: 'Löschen fehlgeschlagen' });
		}
	}

	const columns: Column<Credential>[] = [
		{ key: 'name', label: 'Name' },
		{ key: 'type', label: 'Typ', hideBelow: 'sm' },
		{ key: 'scope', label: 'Gilt für', hideBelow: 'md' },
		{ key: 'public', label: 'Angaben', hideBelow: 'lg' },
		{ key: 'usedBy', label: 'Verwendet von', hideBelow: 'sm' },
		{ key: 'lastUsed', label: 'Zuletzt verwendet', hideBelow: 'lg' },
		{ key: 'updated', label: 'Geändert', hideBelow: 'xl' },
		{ key: 'actions', label: 'Aktionen', align: 'right', width: '3.5rem' }
	];
</script>

<PageHeader title="Credentials" description="Zugangsdaten für Scanner, Importer und Publisher (Vault)">
	{#snippet actions()}
		<Button variant="primary" icon="plus" onclick={openCreate} disabled={!data.data}
			>Credential anlegen</Button
		>
	{/snippet}
</PageHeader>

<div class="flex flex-col gap-4">
	<Alert tone="info" title="Verschlüsselt gespeichert">
		Secrets (Passwörter, private Schlüssel, Tokens, Communities) werden mit AES-256-GCM verschlüsselt abgelegt
		und nach dem Speichern nie wieder angezeigt – weder hier noch über die API. Plugins verweisen nur auf das
		Credential. Der Master-Schlüssel wird unter <a href="/system?tab=vault" class="link">System → Vault</a>
		rotiert.
		<span class="mt-1 block">
			<strong class="font-medium">Automatische Auswahl:</strong> Plugins ohne eigene Auswahl nehmen pro Gerät die
			Zugangsdaten, deren „Gilt für“ passt – das spezifischste zuerst (Gerät, dann Gruppe/Tag/Filter, dann Subnetz,
			zuletzt überall). Welche für ein Gerät gelten, zeigt die Geräteseite.
		</span>
	</Alert>

	{#if blocked}
		<div id="cred-blocked">
			<Alert tone="danger" title="„{blocked.name}“ kann nicht gelöscht werden">
				{blocked.message}
				{#if blocked.usedBy.length}
					<span class="mt-1 block">
						Zuerst dort entfernen:
						{#each blocked.usedBy as u, i (u.id)}
							<a class="link" href={useHref(u)}>{u.name}</a>{i < blocked.usedBy.length - 1 ? ', ' : ''}
						{/each}
					</span>
				{/if}
				{#snippet actions()}
					<Button
						size="xs"
						variant="ghost"
						icon="x"
						label="Hinweis schließen"
						onclick={() => (blocked = null)}
					/>
				{/snippet}
			</Alert>
		</div>
	{/if}

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else}
		<Table {columns} rows={list} key={(c) => c.id} loading={data.loading && !data.data} caption="Credentials">
			{#snippet cell(c, col)}
				{#if col.key === 'name'}
					<div class="min-w-0">
						<button
							type="button"
							class="text-left font-medium text-fg hover:text-accent hover:underline"
							onclick={() => openEdit(c)}>{c.name}</button
						>
						{#if c.description}<span class="block text-xs text-fg-muted">{c.description}</span>{/if}
						<span class="mt-1 block text-xs text-fg-subtle sm:hidden"
							>{typeOf(c.type)?.label ?? c.type}{(c.usedBy ?? []).length
								? ` · verwendet von ${usedNames(c)}`
								: ''}</span
						>
					</div>
				{:else if col.key === 'type'}
					<Badge tone="neutral" title={typeOf(c.type)?.description}>
						<Icon name="key" size={12} class="-mt-px mr-0.5 inline align-middle" />
						{typeOf(c.type)?.label ?? c.type}
					</Badge>
				{:else if col.key === 'scope' && c.type === 'wireguard'}
					<span class="text-xs text-fg-subtle" title="Tunnel-Konfigurationen werden beim Subnetz ausgewählt"
						>Tunnel für Subnetze</span
					>
				{:else if col.key === 'scope'}
					{@const level = credentialScopeLevel(c.scope)}
					<Badge
						tone={levelTone[level]}
						title="Geltungsbereich – bestimmt, für welche Ziele Plugins dieses Credential automatisch wählen"
					>
						<Icon name={levelIcon[level]} size={12} class="-mt-px mr-0.5 inline align-middle" />
						<span class="inline-block max-w-56 truncate align-bottom"
							>{credentialScopeSummary(c.scope, groupName)}</span
						>
					</Badge>
				{:else if col.key === 'public'}
					{@const pub = publicFields(c)}
					<div class="flex flex-col gap-1">
						{#if pub.length}
							<dl class="flex flex-col gap-0.5 text-xs">
								{#each pub as [k, v] (k)}
									<div class="flex gap-1.5">
										<dt class="text-fg-subtle">{fieldLabel(c.type, k)}:</dt>
										<dd class="mono truncate text-fg">{v}</dd>
									</div>
								{/each}
							</dl>
						{/if}
						<span class="flex flex-wrap gap-1">
							{#each c.secretsSet ?? [] as s (s)}
								<Badge tone="ok" title="Secret ist gespeichert (wird nicht angezeigt)">
									<Icon name="lock" size={11} class="-mt-px mr-0.5 inline align-middle" />
									{fieldLabel(c.type, s)}
								</Badge>
							{:else}
								{#if !pub.length}<span class="text-fg-subtle">–</span>{/if}
							{/each}
						</span>
					</div>
				{:else if col.key === 'usedBy'}
					<span class="flex flex-wrap gap-1">
						{#each c.usedBy ?? [] as u (u.id)}
							<a
								href={useHref(u)}
								class="rounded focus-visible:outline-2"
								title={u.kind === 'subnet' ? 'Subnetz öffnen' : 'Plugin öffnen'}
							>
								<Badge tone="accent">{u.name}</Badge>
							</a>
						{:else}
							<span
								class="text-xs text-fg-subtle"
								title="Kein Plugin hat es fest ausgewählt. Plugins ohne eigene Auswahl verwenden es automatisch, wenn „Gilt für“ passt."
								>nicht fest zugewiesen</span
							>
						{/each}
					</span>
				{:else if col.key === 'lastUsed'}
					{#if c.lastUsedAt}<RelativeTime value={c.lastUsedAt} />{:else}<span class="text-fg-subtle">nie</span
						>{/if}
				{:else if col.key === 'updated'}
					<span title="Angelegt {formatDateTime(c.createdAt)}"><RelativeTime value={c.updatedAt} /></span>
				{:else if col.key === 'actions'}
					<Menu
						label="Aktionen für „{c.name}“"
						items={[
							{ label: 'Bearbeiten', icon: 'edit', onclick: () => openEdit(c) },
							{ separator: true },
							{
								label: 'Löschen',
								icon: 'trash',
								danger: true,
								hint: (c.usedBy ?? []).length ? 'in Verwendung' : undefined,
								onclick: () => remove(c)
							}
						]}
					/>
				{/if}
			{/snippet}
			{#snippet empty()}
				<EmptyState
					icon="key"
					title="Noch keine Credentials"
					description="Zugangsdaten (SSH-Schlüssel, Passwort, SNMP-Community, API-Token) hier anlegen und anschließend in den Plugin-Einstellungen auswählen."
				>
					{#snippet actions()}
						<Button variant="primary" icon="plus" onclick={openCreate}>Credential anlegen</Button>
					{/snippet}
				</EmptyState>
			{/snippet}
		</Table>
	{/if}
</div>

<CredentialForm bind:open={formOpen} credential={editing} {types} {onsaved} />
