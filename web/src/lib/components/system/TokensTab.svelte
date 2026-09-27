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
	import { t } from '$lib/i18n';
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
	const expired = (tok: ApiToken) => !!tok.expiresAt && new Date(tok.expiresAt).getTime() < now;

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
		{ value: '7', label: t('{n} Tage', { n: 7 }) },
		{ value: '30', label: t('{n} Tage', { n: 30 }) },
		{ value: '90', label: t('{n} Tage', { n: 90 }) },
		{ value: '365', label: t('1 Jahr') },
		{ value: 'never', label: t('Läuft nie ab') },
		{ value: 'custom', label: t('Datum wählen …') }
	];

	const scopeLabel = (s: string | undefined) => (s === 'write' ? t('Lesen & Schreiben') : t('Nur lesen'));
	const SCOPES = [
		{ value: 'read', label: t('Nur lesen'), description: t('GET-Zugriffe, keine Änderungen') },
		{ value: 'write', label: t('Lesen & Schreiben'), description: t('Alles, was deine Rolle darf') }
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
		if (!name.trim()) e.name = t('Name erforderlich');
		else if (name.trim().length > 100) e.name = t('Maximal 100 Zeichen');
		const exp = expiresAt();
		if (exp === null) e.expiry = t('Ablaufdatum wählen');
		else if (exp && new Date(exp).getTime() < Date.now())
			e.expiry = t('Das Datum liegt in der Vergangenheit');
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

	async function revoke(tok: ApiToken) {
		const ok = await confirm({
			title: t('API-Token widerrufen?'),
			message: t('„{name}“ ({prefix}…) wird sofort ungültig. Skripte, die ihn nutzen, erhalten danach 401.', {
				name: tok.name,
				prefix: tok.prefix
			}),
			confirmLabel: t('Widerrufen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/tokens/{id}', { path: { id: tok.id } });
			toast.success(t('Token „{name}“ wurde widerrufen', { name: tok.name }));
			list.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	const all = $derived(auth.can('users.manage'));
	const columns: Column<ApiToken>[] = $derived([
		{ key: 'name', label: 'Name' },
		...(all ? [{ key: 'owner', label: t('Benutzer'), hideBelow: 'md' as const }] : []),
		{ key: 'scope', label: t('Berechtigung'), width: '8rem' },
		{ key: 'createdAt', label: t('Erstellt'), hideBelow: 'md' },
		{ key: 'expiresAt', label: t('Läuft ab') },
		{ key: 'lastUsedAt', label: t('Zuletzt benutzt'), hideBelow: 'lg' },
		{ key: 'actions', label: '', align: 'right', width: '3rem' }
	]);
</script>

<Card
	title={t('API-Tokens')}
	description={t(
		'Für Skripte: Header „Authorization: Bearer <token>“ – mit höchstens den Rechten deiner Rolle'
	)}
	icon="key"
	padding="none"
>
	{#snippet actions()}
		{#if auth.can('tokens.create')}
			<Button size="sm" variant="primary" icon="plus" onclick={openCreate}>{t('Token erstellen')}</Button>
		{/if}
	{/snippet}
	{#if list.error && !list.data}
		<ErrorState error={list.error} onretry={() => list.reload()} />
	{:else}
		<Table
			{columns}
			rows={tokens}
			key={(tok) => tok.id}
			loading={list.loading && !list.data}
			class="rounded-none border-0"
			caption={t('API-Tokens')}
		>
			{#snippet cell(tok, col)}
				{#if col.key === 'name'}
					<span class="font-medium {expired(tok) ? 'text-fg-subtle line-through' : ''}">{tok.name}</span>
					<span class="mono block text-xs text-fg-subtle">{tok.prefix}…</span>
				{:else if col.key === 'owner'}
					<span class="text-fg-muted">{tok.owner}</span>
				{:else if col.key === 'scope'}
					<Badge tone={tok.scope === 'write' ? 'warn' : 'info'}>{scopeLabel(tok.scope)}</Badge>
				{:else if col.key === 'createdAt'}
					<span class="text-fg-muted">{formatDate(tok.createdAt)}</span>
				{:else if col.key === 'expiresAt'}
					{#if !tok.expiresAt}
						<span class="text-fg-muted">{t('nie')}</span>
					{:else if expired(tok)}
						<Badge tone="neutral" title={formatDateTime(tok.expiresAt)}>{t('abgelaufen')}</Badge>
					{:else}
						<RelativeTime value={tok.expiresAt} class="text-fg-muted" />
					{/if}
				{:else if col.key === 'lastUsedAt'}
					{#if tok.lastUsedAt}
						<RelativeTime value={tok.lastUsedAt} class="text-fg-muted" />
						{#if tok.lastUsedIp}<span class="mono block text-xs text-fg-subtle">{tok.lastUsedIp}</span>{/if}
					{:else}
						<span class="text-fg-subtle">{t('noch nie')}</span>
					{/if}
				{:else if col.key === 'actions'}
					<Button
						size="sm"
						variant="ghost"
						icon="trash"
						label={t('Token „{name}“ widerrufen', { name: tok.name })}
						onclick={() => revoke(tok)}
					/>
				{/if}
			{/snippet}
			{#snippet empty()}
				<EmptyState
					compact
					icon="key"
					title={t('Keine API-Tokens')}
					description={t('Tokens erlauben Skripten den Zugriff auf die API – wahlweise nur lesend.')}
				/>
			{/snippet}
		</Table>
	{/if}
</Card>

<Modal
	bind:open
	title={created ? t('Token erstellt') : t('API-Token erstellen')}
	size="md"
	as="form"
	onsubmit={() => (created ? (open = false) : create())}
	busy={saving}
>
	{#if created}
		<div class="flex flex-col gap-3 text-sm">
			<Alert tone="warn" title={t('Nur jetzt sichtbar')}>
				{t('Den Token jetzt kopieren und sicher ablegen – er wird nicht noch einmal angezeigt.')}
			</Alert>
			<div class="flex items-center gap-2 rounded-md border border-border bg-surface-2 py-1.5 pr-1.5 pl-3">
				<code class="mono min-w-0 flex-1 text-[0.8rem] break-all select-all" aria-label={t('Neuer Token')}
					>{created.token}</code
				>
				<CopyButton text={created.token} label={t('Token kopieren')} size="sm" />
			</div>
			<p class="text-fg-muted">
				{t('„{name}“ · {scope} · {validity}', {
					name: created.info?.name ?? '',
					scope: scopeLabel(created.info?.scope),
					validity: created.info?.expiresAt
						? t('gültig bis {date}', { date: formatDateTime(created.info.expiresAt) })
						: t('läuft nie ab')
				})}
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
				placeholder={t('z. B. home-assistant')}
				error={errors.name}
			/>
			<fieldset>
				<legend class="mb-1.5 text-[0.8125rem] font-medium text-fg">{t('Berechtigung')}</legend>
				<div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
					{#each SCOPES as s (s.value)}
						<label
							class="flex cursor-pointer items-start gap-2 rounded-md border px-3 py-2 text-sm has-focus-visible:ring-2 has-focus-visible:ring-focus
								{scope === s.value ? 'border-accent bg-accent-soft' : 'border-border hover:bg-surface-2'}"
						>
							<input
								type="radio"
								name="token-scope"
								value={s.value}
								bind:group={scope}
								class="mt-0.5 accent-(--accent)"
							/>
							<span>
								<span class="block font-medium">{s.label}</span>
								<span class="block text-xs text-fg-subtle">{s.description}</span>
							</span>
						</label>
					{/each}
				</div>
				{#if errors.scope}<p class="mt-1 text-xs text-danger" role="alert">{errors.scope}</p>{/if}
			</fieldset>
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<Select
					label={t('Gültigkeit')}
					bind:value={expiry}
					options={EXPIRY}
					error={expiry === 'custom' ? undefined : errors.expiry}
				/>
				{#if expiry === 'custom'}
					<Input label={t('Gültig bis')} type="date" bind:value={customDate} error={errors.expiry} required />
				{/if}
			</div>
		</div>
	{/if}
	{#snippet footer()}
		{#if created}
			<Button type="submit" variant="primary">{t('Fertig')}</Button>
		{:else}
			<Button onclick={() => (open = false)} disabled={saving}>{t('Abbrechen')}</Button>
			<Button type="submit" variant="primary" icon="key" loading={saving}>{t('Erstellen')}</Button>
		{/if}
	{/snippet}
</Modal>
