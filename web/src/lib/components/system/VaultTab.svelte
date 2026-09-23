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
	import { credentials } from '$lib/stores/catalog.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import StrongConfirm from './StrongConfirm.svelte';

	const info = new AsyncData<SystemInfo>();
	$effect(() => {
		info.run((signal) => api.get('/api/v1/system/info', { signal }));
		credentials.load().catch(() => {});
	});

	const fromEnv = $derived((info.data?.vaultKeySource ?? '').includes('NETSCOPE_MASTER_KEY'));
	const keyFile = $derived(
		info.data && !fromEnv
			? info.data.vaultKeySource.replace(/^Datei\s+/, '')
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
			toast.success('Master-Key rotiert – alle Credentials wurden neu verschlüsselt.');
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
		<Card title="Master-Key" description="Verschlüsselt alle Credentials im Vault (AES-GCM)" icon="lock">
			<DescList cols={2}>
				<DescItem label="Schlüssel-ID">
					<span class="mono">{info.data.vaultKeyId}</span>
					<CopyButton text={info.data.vaultKeyId} label="Schlüssel-ID kopieren" />
				</DescItem>
				<DescItem label="Quelle">
					{info.data.vaultKeySource}
					<Badge tone={fromEnv ? 'info' : 'neutral'} class="ml-1">{fromEnv ? 'Umgebung' : 'Datei'}</Badge>
				</DescItem>
				<DescItem
					label="Gespeicherte Credentials"
					value={credentials.value ? String(credentials.value.length) : '–'}
				/>
			</DescList>
		</Card>

		{#if result}
			<Alert tone="ok" title="Master-Key rotiert">
				Neue Schlüssel-ID <code class="mono">{result.keyId}</code> (vorher
				<code class="mono">{result.previousKeyId}</code>). Die Schlüsseldatei wurde ersetzt – jetzt die neue
				<code class="mono">{keyFile}</code> sichern; ältere Backups der Datenbank benötigen den alten Schlüssel.
			</Alert>
		{/if}
		{#if error}
			<Alert
				tone={error.conflict ? 'warn' : 'danger'}
				title={error.conflict ? 'Rotation über die Oberfläche nicht möglich' : 'Rotation fehlgeschlagen'}
			>
				{error.message}
			</Alert>
		{/if}

		<Card title="Master-Key rotieren" icon="refresh">
			<div class="flex flex-col gap-3 text-sm">
				<p class="text-fg-muted">
					Erzeugt einen neuen Schlüssel und verschlüsselt alle Credentials damit neu. Sinnvoll, wenn der alte
					Schlüssel kompromittiert sein könnte oder nach Personalwechsel.
				</p>
				<Alert tone="warn" title="Vorher sichern">
					Vor der Rotation <code class="mono">{keyFile}</code> und ein aktuelles Datenbank-Backup sichern. Backups,
					die vor der Rotation erstellt wurden, lassen sich nur mit dem alten Schlüssel entschlüsseln.
				</Alert>
				{#if fromEnv}
					<Alert tone="info" title="Schlüssel aus der Umgebung">
						Der Master-Key kommt aus <code class="mono">NETSCOPE_MASTER_KEY</code> und kann nur dort geändert werden.
					</Alert>
				{/if}
				<div>
					<Button variant="danger" icon="refresh" onclick={() => (open = true)}>Master-Key rotieren …</Button>
				</div>
			</div>
		</Card>
	</div>
{/if}

<StrongConfirm
	bind:open
	title="Master-Key rotieren?"
	word="ROTIEREN"
	confirmLabel="Jetzt rotieren"
	{busy}
	onconfirm={rotate}
>
	<Alert tone="danger" title="Nicht rückgängig zu machen">
		Alle {credentials.value?.length ?? ''} Credentials werden mit einem neuen Schlüssel verschlüsselt; der alte
		Schlüssel wird ersetzt. Ohne Sicherung von <code class="mono">{keyFile}</code> sind ältere Backups danach nicht
		mehr entschlüsselbar.
	</Alert>
</StrongConfirm>
