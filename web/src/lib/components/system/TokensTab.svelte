<!-- API tokens: list, create (plain token shown once), revoke. A token never has more rights than
     the role of its user; with user management all tokens are listed. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { ApiToken, TokenCreated } from '$lib/api';
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
		RelativeTime,
		Select,
		Table
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDate, formatDateTime } from '$lib/utils/format';
	import { apiErrors } from './system';

	const list = new AsyncData<ApiToken[]>();
	$effect(() => {
		list.run(async (signal) => (await api.get('/api/v1/tokens', { signal })) ?? []);
	});
	const now = Date.now();
	const tokens = $derived(
		[...(list.data ?? [])].sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
	);
	const expired = (t: ApiToken) => !!t.expiresAt && new Date(t.expiresAt).getTime() < now;

	// ---------------------------------------------------------------- create
	let open = $state(false);
	let name = $state('');
	let scope = $state<'read' | 'write'>('read');
	let expiry = $state('90');
	let customDate = $state('');
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	let created = $state<TokenCreated | null>(null);

	const EXPIRY = [
		{ value: '7', label: '7 Tage' },
		{ value: '30', label: '30 Tage' },
		{ value: '90', label: '90 Tage' },
		{ value: '365', label: '1 Jahr' },
		{ value: 'never', label: 'Läuft nie ab' },
		{ value: 'custom', label: 'Datum wählen …' }
	];

	function openCreate() {
		name = '';
		scope = 'read';
		expiry = '90';
		customDate = '';
		errors = {};
		general = null;
		created = null;
		open = true;
	}

	function expiresAt(): string | undefined | null {
		if (expiry === 'never') return undefined;
		if (expiry === 'custom') {
			if (!customDate) return null;
			const d = new Date(customDate + 'T23:59:59');
			return isNaN(d.getTime()) ? null : d.toISOString();
		}
		return new Date(Date.now() + Number(expiry) * 86400_000).toISOString();
	}

	async function create() {
		general = null;
		const e: Record<string, string> = {};
		if (!name.trim()) e.name = 'Name erforderlich';
		else if (name.trim().length > 100) e.name = 'Maximal 100 Zeichen';
		const exp = expiresAt();
		if (exp === null) e.expiry = 'Ablaufdatum wählen';
		else if (exp && new Date(exp).getTime() < Date.now()) e.expiry = 'Das Datum liegt in der Vergangenheit';
		errors = e;
		if (Object.keys(e).length) return;
		saving = true;
		try {
			created = await api.post('/api/v1/tokens', {
				body: { name: name.trim(), scope, expiresAt: exp ?? undefined }
			});
			list.reload();
		} catch (err) {
			({ errors, general } = apiErrors(err, ['name', 'scope', 'expiry'], { expiresAt: 'expiry' }));
		} finally {
			saving = false;
		}
	}

	async function revoke(t: ApiToken) {
		const ok = await confirm({
			title: 'API-Token widerrufen?',
			message: `„${t.name}“ (${t.prefix}…) wird sofort ungültig. Skripte, die ihn nutzen, erhalten danach 401.`,
			confirmLabel: 'Widerrufen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/tokens/{id}', { path: { id: t.id } });
			toast.success(`Token „${t.name}“ widerrufen`);
			list.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	const all = $derived(auth.can('users.manage'));
	const columns: Column<ApiToken>[] = $derived([
		{ key: 'name', label: 'Name' },
		...(all ? [{ key: 'owner', label: 'Benutzer', hideBelow: 'md' as const }] : []),
		{ key: 'scope', label: 'Berechtigung', width: '8rem' },
		{ key: 'createdAt', label: 'Erstellt', hideBelow: 'md' },
		{ key: 'expiresAt', label: 'Läuft ab' },
		{ key: 'lastUsedAt', label: 'Zuletzt benutzt', hideBelow: 'lg' },
		{ key: 'actions', label: '', align: 'right', width: '3rem' }
	]);
</script>

<Card
	title="API-Tokens"
	description="Für Skripte: Header „Authorization: Bearer &lt;token&gt;“ – mit höchstens den Rechten deiner Rolle"
	icon="key"
	padding="none"
>
	{#snippet actions()}
		{#if auth.can('tokens.create')}
			<Button size="sm" variant="primary" icon="plus" onclick={openCreate}>Token erstellen</Button>
		{/if}
	{/snippet}
	{#if list.error && !list.data}
		<ErrorState error={list.error} onretry={() => list.reload()} />
	{:else}
		<Table
			{columns}
			rows={tokens}
			key={(t) => t.id}
			loading={list.loading && !list.data}
			class="rounded-none border-0"
			caption="API-Tokens"
		>
			{#snippet cell(t, col)}
				{#if col.key === 'name'}
					<span class="font-medium {expired(t) ? 'text-fg-subtle line-through' : ''}">{t.name}</span>
					<span class="mono block text-xs text-fg-subtle">{t.prefix}…</span>
				{:else if col.key === 'owner'}
					<span class="text-fg-muted">{t.owner}</span>
				{:else if col.key === 'scope'}
					<Badge tone={t.scope === 'write' ? 'warn' : 'info'}
						>{t.scope === 'write' ? 'Lesen & Schreiben' : 'Nur lesen'}</Badge
					>
				{:else if col.key === 'createdAt'}
					<span class="text-fg-muted">{formatDate(t.createdAt)}</span>
				{:else if col.key === 'expiresAt'}
					{#if !t.expiresAt}
						<span class="text-fg-muted">nie</span>
					{:else if expired(t)}
						<Badge tone="neutral" title={formatDateTime(t.expiresAt)}>abgelaufen</Badge>
					{:else}
						<RelativeTime value={t.expiresAt} class="text-fg-muted" />
					{/if}
				{:else if col.key === 'lastUsedAt'}
					{#if t.lastUsedAt}
						<RelativeTime value={t.lastUsedAt} class="text-fg-muted" />
						{#if t.lastUsedIp}<span class="mono block text-xs text-fg-subtle">{t.lastUsedIp}</span>{/if}
					{:else}
						<span class="text-fg-subtle">noch nie</span>
					{/if}
				{:else if col.key === 'actions'}
					<Button
						size="sm"
						variant="ghost"
						icon="trash"
						label="Token „{t.name}“ widerrufen"
						onclick={() => revoke(t)}
					/>
				{/if}
			{/snippet}
			{#snippet empty()}
				<EmptyState
					compact
					icon="key"
					title="Keine API-Tokens"
					description="Tokens erlauben Skripten den Zugriff auf die API – wahlweise nur lesend."
				/>
			{/snippet}
		</Table>
	{/if}
</Card>

<Modal
	bind:open
	title={created ? 'Token erstellt' : 'API-Token erstellen'}
	size="md"
	as="form"
	onsubmit={() => (created ? (open = false) : create())}
	busy={saving}
>
	{#if created}
		<div class="flex flex-col gap-3 text-sm">
			<Alert tone="warn" title="Nur jetzt sichtbar">
				Den Token jetzt kopieren und sicher ablegen – er wird nicht noch einmal angezeigt.
			</Alert>
			<div class="flex items-center gap-2 rounded-md border border-border bg-surface-2 py-1.5 pr-1.5 pl-3">
				<code class="mono min-w-0 flex-1 text-[0.8rem] break-all select-all" aria-label="Neuer Token"
					>{created.token}</code
				>
				<CopyButton text={created.token} label="Token kopieren" size="sm" />
			</div>
			<p class="text-fg-muted">
				„{created.info?.name}“ · {created.info?.scope === 'write' ? 'Lesen & Schreiben' : 'Nur lesen'} · {created
					.info?.expiresAt
					? `gültig bis ${formatDateTime(created.info.expiresAt)}`
					: 'läuft nie ab'}
			</p>
			<pre
				class="mono rounded-md bg-surface-2 px-3 py-2 text-xs break-all whitespace-pre-wrap text-fg-muted">curl -H "Authorization: Bearer {created.token.slice(
					0,
					10
				)}…" {typeof window !== 'undefined' ? window.location.origin : ''}/api/v1/devices</pre>
		</div>
	{:else}
		<div class="flex flex-col gap-4">
			{#if general}<Alert tone="danger">{general}</Alert>{/if}
			<Input
				label="Name"
				bind:value={name}
				required
				maxlength={100}
				placeholder="z. B. home-assistant"
				error={errors.name}
			/>
			<fieldset>
				<legend class="mb-1.5 text-[0.8125rem] font-medium text-fg">Berechtigung</legend>
				<div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
					{#each [['read', 'Nur lesen', 'GET-Zugriffe, keine Änderungen'], ['write', 'Lesen & Schreiben', 'Alles, was deine Rolle darf']] as [v, l, d] (v)}
						<label
							class="flex cursor-pointer items-start gap-2 rounded-md border px-3 py-2 text-sm has-focus-visible:ring-2 has-focus-visible:ring-focus
								{scope === v ? 'border-accent bg-accent-soft' : 'border-border hover:bg-surface-2'}"
						>
							<input
								type="radio"
								name="token-scope"
								value={v}
								bind:group={scope}
								class="mt-0.5 accent-(--accent)"
							/>
							<span>
								<span class="block font-medium">{l}</span>
								<span class="block text-xs text-fg-subtle">{d}</span>
							</span>
						</label>
					{/each}
				</div>
				{#if errors.scope}<p class="mt-1 text-xs text-danger" role="alert">{errors.scope}</p>{/if}
			</fieldset>
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<Select
					label="Gültigkeit"
					bind:value={expiry}
					options={EXPIRY}
					error={expiry === 'custom' ? undefined : errors.expiry}
				/>
				{#if expiry === 'custom'}
					<Input label="Gültig bis" type="date" bind:value={customDate} error={errors.expiry} required />
				{/if}
			</div>
		</div>
	{/if}
	{#snippet footer()}
		{#if created}
			<Button type="submit" variant="primary">Fertig</Button>
		{:else}
			<Button onclick={() => (open = false)} disabled={saving}>Abbrechen</Button>
			<Button type="submit" variant="primary" icon="key" loading={saving}>Erstellen</Button>
		{/if}
	{/snippet}
</Modal>
