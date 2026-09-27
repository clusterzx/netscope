<!-- Backups: list, create, download, delete, restore (existing backup or uploaded file). -->
<script lang="ts">
	import { api, errorMessage } from '$lib/api';
	import type { BackupInfo } from '$lib/api';
	import {
		Alert,
		Button,
		Card,
		Checkbox,
		EmptyState,
		ErrorState,
		FormField,
		Menu,
		Table
	} from '$lib/components/ui';
	import type { Column } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatBytes, formatDateTime } from '$lib/utils/format';
	import RestoreOverlay from './RestoreOverlay.svelte';
	import StrongConfirm from './StrongConfirm.svelte';
	import { markupParts } from './system';

	const canManage = $derived(auth.can('backups.manage'));

	const list = new AsyncData<BackupInfo[]>();
	$effect(() => {
		if (!canManage) return;
		list.run(async (signal) => (await api.get('/api/v1/system/backups', { signal })) ?? []);
	});

	let creating = $state(false);
	async function create(): Promise<boolean> {
		creating = true;
		try {
			const b = await api.post('/api/v1/system/backups');
			toast.success(t('Backup {name} erstellt ({size})', { name: b.name, size: formatBytes(b.size) }));
			list.reload();
			return true;
		} catch (e) {
			toast.error(e, { title: t('Backup fehlgeschlagen') });
			return false;
		} finally {
			creating = false;
		}
	}

	async function remove(b: BackupInfo) {
		const ok = await confirm({
			title: t('Backup löschen?'),
			message: t('{name} ({size}, {date}) wird endgültig gelöscht.', {
				name: b.name,
				size: formatBytes(b.size),
				date: formatDateTime(b.createdAt)
			}),
			confirmLabel: t('Löschen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/system/backups/{name}', { path: { name: b.name } });
			toast.success(t('Backup {name} gelöscht', { name: b.name }));
			list.reload();
		} catch (e) {
			toast.error(e);
		}
	}

	// ---------------------------------------------------------------- restore
	let confirmOpen = $state(false);
	let target = $state<{ kind: 'backup'; backup: BackupInfo } | { kind: 'file'; file: File } | null>(null);
	let backupFirst = $state(true);
	let restoring = $state(false);
	let overlay = $state(false);
	let restoreError = $state<string | null>(null);
	let fileInput: HTMLInputElement | null = $state(null);
	let pickedFile = $state<File | null>(null);
	let fileError = $state<string | null>(null);

	const targetName = $derived(
		!target ? '' : target.kind === 'backup' ? target.backup.name : target.file.name
	);

	function askRestore(next: NonNullable<typeof target>) {
		target = next;
		backupFirst = true;
		restoreError = null;
		confirmOpen = true;
	}

	function onFile(e: Event) {
		const f = (e.currentTarget as HTMLInputElement).files?.[0] ?? null;
		fileError = null;
		pickedFile = f;
		if (f && !/\.(db|sqlite3?)(\.gz)?$/i.test(f.name))
			fileError = t('Erwartet wird ein NetScope-Backup (.db.gz oder .db).');
	}

	async function doRestore() {
		if (!target) return;
		restoring = true;
		restoreError = null;
		try {
			if (backupFirst && !(await create())) {
				restoreError = t('Das vorherige Backup ist fehlgeschlagen – Wiederherstellung abgebrochen.');
				return;
			}
			if (target.kind === 'backup') {
				await api.raw.post('/api/v1/system/restore', { name: target.backup.name });
			} else {
				const fd = new FormData();
				fd.append('file', target.file, target.file.name);
				await api.raw.post('/api/v1/system/restore', fd);
			}
			confirmOpen = false;
			overlay = true;
		} catch (e) {
			restoreError = errorMessage(e);
		} finally {
			restoring = false;
		}
	}

	// the path of the master key is set as code
	const footerText = markupParts(
		t(
			'Backups sind gzip-komprimiert und enthalten den NVD-Spiegel nicht – der wird bei Bedarf neu geladen. Der Vault-Master-Key ({file}) ist nicht Teil des Backups – ohne ihn sind gesicherte Credentials nicht lesbar. Den Key separat sichern.'
		)
	);

	// the restore warning: the backup name is set in bold
	const restoreText = $derived(
		markupParts(
			target?.kind === 'file'
				? t('Die Datenbank wird durch {name} ({size}) ersetzt.', { size: formatBytes(target.file.size) })
				: target?.kind === 'backup'
					? t('Die Datenbank wird durch {name} vom {date} ersetzt.', {
							date: formatDateTime(target.backup.createdAt)
						})
					: ''
		)
	);

	const columns: Column<BackupInfo>[] = [
		{ key: 'name', label: 'Backup' },
		{ key: 'createdAt', label: t('Erstellt') },
		{ key: 'size', label: t('Größe'), align: 'right', width: '7rem', hideBelow: 'sm' },
		{ key: 'actions', label: '', align: 'right', width: '3rem' }
	];
</script>

{#if !canManage}
	<Card>
		<EmptyState
			icon="lock"
			title={t('Keine Berechtigung')}
			description={t('Für Backups fehlt die Berechtigung „Backups verwalten“.')}
		/>
	</Card>
{:else}
	<div class="flex flex-col gap-4">
		<Card
			title="Backups"
			description={t('Konsistente Kopie der SQLite-Datenbank im Datenverzeichnis (backups/)')}
			icon="disk"
			padding="none"
		>
			{#snippet actions()}
				<Button size="sm" variant="primary" icon="plus" loading={creating} onclick={create}
					>{t('Backup erstellen')}</Button
				>
			{/snippet}
			{#if list.error && !list.data}
				<ErrorState error={list.error} onretry={() => list.reload()} />
			{:else}
				<Table
					{columns}
					rows={list.data ?? []}
					key={(b) => b.name}
					loading={list.loading && !list.data}
					class="rounded-none border-0"
					caption="Backups"
				>
					{#snippet cell(b, col)}
						{#if col.key === 'name'}
							<span class="mono break-all">{b.name}</span>
						{:else if col.key === 'createdAt'}
							<span class="text-fg-muted">{formatDateTime(b.createdAt)}</span>
						{:else if col.key === 'size'}
							<span class="tabular text-fg-muted">{formatBytes(b.size)}</span>
						{:else if col.key === 'actions'}
							<Menu
								label={t('Aktionen für {name}', { name: b.name })}
								items={[
									{
										// plain link: large backups stream straight to disk (attachment)
										label: t('Herunterladen'),
										icon: 'download',
										href: `/api/v1/system/backups/${encodeURIComponent(b.name)}`,
										hint: formatBytes(b.size)
									},
									{
										label: t('Wiederherstellen …'),
										icon: 'history',
										onclick: () => askRestore({ kind: 'backup', backup: b })
									},
									{ separator: true },
									{ label: t('Löschen'), icon: 'trash', danger: true, onclick: () => remove(b) }
								]}
							/>
						{/if}
					{/snippet}
					{#snippet empty()}
						<EmptyState
							compact
							icon="disk"
							title={t('Noch keine Backups')}
							description={t(
								'Ein Backup sichert Inventar, Einstellungen, Regeln und verschlüsselte Credentials (ohne Master-Key).'
							)}
						/>
					{/snippet}
				</Table>
			{/if}
			{#snippet footer()}
				<p class="text-xs text-fg-subtle">
					{#each footerText as part, k (k)}{#if k % 2}<code class="mono">data/master.key</code
							>{:else}{part}{/if}{/each}
				</p>
			{/snippet}
		</Card>

		<Card
			title={t('Aus Datei wiederherstellen')}
			description={t('Ein heruntergeladenes Backup hochladen und einspielen')}
			icon="upload"
		>
			<div class="flex flex-col gap-3">
				<FormField
					label={t('Backup-Datei')}
					error={fileError}
					hint={t('Datei (.db.gz) aus „Herunterladen“ – ältere unkomprimierte .db-Backups gehen auch')}
				>
					{#snippet children(id, describedby)}
						<input
							bind:this={fileInput}
							{id}
							type="file"
							accept=".gz,.db,.sqlite,.sqlite3,application/gzip,application/octet-stream"
							aria-describedby={describedby}
							onchange={onFile}
							class="block w-full text-sm text-fg-muted file:mr-3 file:h-8 file:cursor-pointer file:rounded-md file:border file:border-border file:bg-surface file:px-3 file:text-sm file:font-medium file:text-fg hover:file:bg-surface-2"
						/>
					{/snippet}
				</FormField>
				<div>
					<Button
						variant="danger"
						icon="upload"
						disabled={!pickedFile || !!fileError}
						onclick={() => pickedFile && askRestore({ kind: 'file', file: pickedFile })}
						>{t('Hochladen & wiederherstellen …')}</Button
					>
				</div>
			</div>
		</Card>
	</div>
{/if}

<StrongConfirm
	bind:open={confirmOpen}
	title={t('Datenbank wiederherstellen?')}
	word={t('WIEDERHERSTELLEN')}
	confirmLabel={t('Wiederherstellen')}
	busy={restoring}
	onconfirm={doRestore}
>
	{#if restoreError}<Alert tone="danger" title={t('Wiederherstellung nicht möglich')}>{restoreError}</Alert
		>{/if}
	<Alert tone="danger" title={t('Alle aktuellen Daten werden ersetzt')}>
		{#each restoreText as part, k (k)}{#if k % 2}<strong class="mono">{targetName}</strong
				>{:else}{part}{/if}{/each}
		{t(
			'Änderungen seit diesem Stand gehen verloren. Die Dienste starten neu, laufende Scans werden abgebrochen und alle Sitzungen können ungültig werden.'
		)}
	</Alert>
	<Checkbox
		bind:checked={backupFirst}
		label={t('Vorher ein Backup des aktuellen Stands erstellen')}
		description={t('Empfohlen – so lässt sich die Wiederherstellung rückgängig machen.')}
	/>
</StrongConfirm>

<RestoreOverlay active={overlay} source={targetName} />
