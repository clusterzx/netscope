<!-- Vault: master key id/source and key rotation (POST /api/v1/system/vault/rotate). -->
<script lang="ts">
	import { api, ApiError, errorMessage } from '$lib/api';
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
		Skeleton
	} from '$lib/components/ui';
	import { t, tn } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { credentials } from '$lib/stores/catalog.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import StrongConfirm from './StrongConfirm.svelte';
	import { markupParts } from './system';

	const canView = $derived(auth.can('credentials.view'));
	const canRotate = $derived(auth.can('system.manage'));

	const info = new AsyncData<SystemInfo>();
	$effect(() => {
		info.run((signal) => api.get('/api/v1/system/info', { signal }));
		if (canView) credentials.load().catch(() => {});
	});

	const fromEnv = $derived((info.data?.vaultKeySource ?? '').includes('NETSCOPE_MASTER_KEY'));
	// the server describes the key source as "Datei <path>" (English: "File <path>")
	const keyFile = $derived(
		info.data && !fromEnv
			? info.data.vaultKeySource.replace(/^(?:Datei|File)\s+/, '')
			: `${info.data?.dataDir ?? 'data'}/master.key`
	);

	let open = $state(false);
	let busy = $state(false);
	let error = $state<{ message: string; conflict: boolean } | null>(null);
	let result = $state<{ keyId: string; previousKeyId: string } | null>(null);

	async function rotate() {
		busy = true;
		error = null;
		try {
			const res = await api.post('/api/v1/system/vault/rotate');
			result = { keyId: res.keyId ?? '', previousKeyId: res.previousKeyId ?? '' };
			open = false;
			toast.success(t('Master-Key rotiert – alle Credentials wurden neu verschlüsselt.'));
			info.reload();
		} catch (e) {
			error = { message: errorMessage(e), conflict: e instanceof ApiError && e.status === 409 };
			open = false;
		} finally {
			busy = false;
		}
	}
</script>

{#if info.error && !info.data}
	<ErrorState error={info.error} onretry={() => info.reload()} />
{:else if !info.data}
	<Card><Skeleton lines={5} /></Card>
{:else}
	<div class="flex flex-col gap-4">
		<Card
			title={t('Master-Key')}
			description={t('Verschlüsselt alle Credentials im Vault (AES-GCM)')}
			icon="lock"
		>
			<DescList cols={2}>
				<DescItem label={t('Schlüssel-ID')}>
					<span class="mono">{info.data.vaultKeyId}</span>
					<CopyButton text={info.data.vaultKeyId} label={t('Schlüssel-ID kopieren')} />
				</DescItem>
				<DescItem label={t('Quelle')}>
					{info.data.vaultKeySource}
					<Badge tone={fromEnv ? 'info' : 'neutral'} class="ml-1"
						>{fromEnv ? t('Umgebung') : t('Datei')}</Badge
					>
				</DescItem>
				{#if canView}
					<DescItem
						label={t('Gespeicherte Credentials')}
						value={credentials.value ? String(credentials.value.length) : '–'}
					/>
				{/if}
			</DescList>
		</Card>

		{#if result}
			<Alert tone="ok" title={t('Master-Key rotiert')}>
				{@render withCode(
					t(
						'Neue Schlüssel-ID {keyId} (vorher {previousKeyId}). Die Schlüsseldatei wurde ersetzt – jetzt die neue {file} sichern; ältere Backups der Datenbank benötigen den alten Schlüssel.'
					),
					{ keyId: result.keyId, previousKeyId: result.previousKeyId, file: keyFile }
				)}
			</Alert>
		{/if}
		{#if error}
			<Alert
				tone={error.conflict ? 'warn' : 'danger'}
				title={error.conflict
					? t('Rotation über die Oberfläche nicht möglich')
					: t('Rotation fehlgeschlagen')}
			>
				{error.message}
			</Alert>
		{/if}

		<Card title={t('Master-Key rotieren')} icon="refresh">
			<div class="flex flex-col gap-3 text-sm">
				<p class="text-fg-muted">
					{t(
						'Erzeugt einen neuen Schlüssel und verschlüsselt alle Credentials damit neu. Sinnvoll, wenn der alte Schlüssel kompromittiert sein könnte oder nach Personalwechsel.'
					)}
				</p>
				<Alert tone="warn" title={t('Vorher sichern')}>
					{@render withCode(
						t(
							'Vor der Rotation {file} und ein aktuelles Datenbank-Backup sichern. Backups, die vor der Rotation erstellt wurden, lassen sich nur mit dem alten Schlüssel entschlüsseln.'
						),
						{ file: keyFile }
					)}
				</Alert>
				{#if fromEnv}
					<Alert tone="info" title={t('Schlüssel aus der Umgebung')}>
						{@render withCode(t('Der Master-Key kommt aus {variable} und kann nur dort geändert werden.'), {
							variable: 'NETSCOPE_MASTER_KEY'
						})}
					</Alert>
				{/if}
				{#if canRotate}
					<div>
						<Button variant="danger" icon="refresh" onclick={() => (open = true)}
							>{t('Master-Key rotieren …')}</Button
						>
					</div>
				{:else}
					<p class="text-xs text-fg-subtle">
						{t('Nur lesen – dafür fehlt die Berechtigung „Systemeinstellungen ändern“.')}
					</p>
				{/if}
			</div>
		</Card>
	</div>
{/if}

<StrongConfirm
	bind:open
	title={t('Master-Key rotieren?')}
	word={t('ROTIEREN')}
	confirmLabel={t('Jetzt rotieren')}
	{busy}
	onconfirm={rotate}
>
	<Alert tone="danger" title={t('Nicht rückgängig zu machen')}>
		{@render withCode(
			credentials.value
				? tn(
						credentials.value.length,
						'{n} Credential wird mit einem neuen Schlüssel verschlüsselt; der alte Schlüssel wird ersetzt. Ohne Sicherung von {file} sind ältere Backups danach nicht mehr entschlüsselbar.',
						'Alle {n} Credentials werden mit einem neuen Schlüssel verschlüsselt; der alte Schlüssel wird ersetzt. Ohne Sicherung von {file} sind ältere Backups danach nicht mehr entschlüsselbar.'
					)
				: t(
						'Alle Credentials werden mit einem neuen Schlüssel verschlüsselt; der alte Schlüssel wird ersetzt. Ohne Sicherung von {file} sind ältere Backups danach nicht mehr entschlüsselbar.'
					),
			{ file: keyFile }
		)}
	</Alert>
</StrongConfirm>

<!-- a translated sentence with its placeholders set as code -->
{#snippet withCode(text: string, values: Record<string, string>)}
	{#each markupParts(text) as part, k (k)}{#if k % 2}<code class="mono">{values[part]}</code
			>{:else}{part}{/if}{/each}
{/snippet}
