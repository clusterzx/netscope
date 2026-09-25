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
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatBytes, formatDateTime } from '$lib/utils/format';
	import RestoreOverlay from './RestoreOverlay.svelte';
	import StrongConfirm from './StrongConfirm.svelte';

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
			toast.success(`Backup ${b.name} erstellt (${formatBytes(b.size)})`);
			list.reload();
			return true;
		} catch (e) {
			toast.error(e, { title: 'Backup fehlgeschlagen' });
			return false;
		} finally {
			creating = false;
		}
	}

	async function remove(b: BackupInfo) {
		const ok = await confirm({
			title: 'Backup löschen?',
			message: `${b.name} (${formatBytes(b.size)}, ${formatDateTime(b.createdAt)}) wird endgültig gelöscht.`,
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/system/backups/{name}', { path: { name: b.name } });
			toast.success(`Backup ${b.name} gelöscht`);
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

	function askRestore(t: NonNullable<typeof target>) {
		target = t;
		backupFirst = true;
		restoreError = null;
		confirmOpen = true;
	}

	function onFile(e: Event) {
		const f = (e.currentTarget as HTMLInputElement).files?.[0] ?? null;
		fileError = null;
		pickedFile = f;
		if (f && !/\.(db|sqlite3?)$/i.test(f.name))
			fileError = 'Erwartet wird eine SQLite-Datei (.db) aus einem NetScope-Backup.';
	}

	async function doRestore() {
		if (!target) return;
		restoring = true;
		restoreError = null;
		try {
			if (backupFirst && !(await create())) {
				restoreError = 'Das vorherige Backup ist fehlgeschlagen – Wiederherstellung abgebrochen.';
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

	const columns: Column<BackupInfo>[] = [
		{ key: 'name', label: 'Backup' },
		{ key: 'createdAt', label: 'Erstellt' },
		{ key: 'size', label: 'Größe', align: 'right', width: '7rem', hideBelow: 'sm' },
		{ key: 'actions', label: '', align: 'right', width: '3rem' }
	];
</script>

{#if !canManage}
	<Card>
		<EmptyState
			icon="lock"
			title="Keine Berechtigung"
			description="Für Backups fehlt die Berechtigung „Backups verwalten“."
		/>
	</Card>
{:else}
	<div class="flex flex-col gap-4">
		<Card
			title="Backups"
			description="Konsistente Kopie der SQLite-Datenbank im Datenverzeichnis (backups/)"
			icon="disk"
			padding="none"
		>
			{#snippet actions()}
				<Button size="sm" variant="primary" icon="plus" loading={creating} onclick={create}
					>Backup erstellen</Button
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
								label="Aktionen für {b.name}"
								items={[
									{
										// plain link: large backups stream straight to disk (attachment)
										label: 'Herunterladen',
										icon: 'download',
										href: `/api/v1/system/backups/${encodeURIComponent(b.name)}`,
										hint: formatBytes(b.size)
									},
									{
										label: 'Wiederherstellen …',
										icon: 'history',
										onclick: () => askRestore({ kind: 'backup', backup: b })
									},
									{ separator: true },
									{ label: 'Löschen', icon: 'trash', danger: true, onclick: () => remove(b) }
								]}
							/>
						{/if}
					{/snippet}
					{#snippet empty()}
						<EmptyState
							compact
							icon="disk"
							title="Noch keine Backups"
							description="Ein Backup sichert Inventar, Einstellungen, Regeln und verschlüsselte Credentials (ohne Master-Key)."
						/>
					{/snippet}
				</Table>
			{/if}
			{#snippet footer()}
				<p class="text-xs text-fg-subtle">
					Der Vault-Master-Key (<code class="mono">data/master.key</code>) ist nicht Teil des Backups – ohne
					ihn sind gesicherte Credentials nicht lesbar. Den Key separat sichern.
				</p>
			{/snippet}
		</Card>

		<Card
			title="Aus Datei wiederherstellen"
			description="Ein heruntergeladenes Backup hochladen und einspielen"
			icon="upload"
		>
			<div class="flex flex-col gap-3">
				<FormField
					label="Backup-Datei"
					error={fileError}
					hint="SQLite-Datei (.db) aus „Backup herunterladen“"
				>
					{#snippet children(id, describedby)}
						<input
							bind:this={fileInput}
							{id}
							type="file"
							accept=".db,.sqlite,.sqlite3,application/octet-stream"
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
						>Hochladen & wiederherstellen …</Button
					>
				</div>
			</div>
		</Card>
	</div>
{/if}

<StrongConfirm
	bind:open={confirmOpen}
	title="Datenbank wiederherstellen?"
	word="WIEDERHERSTELLEN"
	confirmLabel="Wiederherstellen"
	busy={restoring}
	onconfirm={doRestore}
>
	{#if restoreError}<Alert tone="danger" title="Wiederherstellung nicht möglich">{restoreError}</Alert>{/if}
	<Alert tone="danger" title="Alle aktuellen Daten werden ersetzt">
		Die Datenbank wird durch <strong class="mono">{targetName}</strong>
		{#if target?.kind === 'file'}({formatBytes(target.file.size)}){:else if target?.kind === 'backup'}vom {formatDateTime(
				target.backup.createdAt
			)}{/if} ersetzt. Änderungen seit diesem Stand gehen verloren. Die Dienste starten neu, laufende Scans werden
		abgebrochen und alle Sitzungen können ungültig werden.
	</Alert>
	<Checkbox
		bind:checked={backupFirst}
		label="Vorher ein Backup des aktuellen Stands erstellen"
		description="Empfohlen – so lässt sich die Wiederherstellung rückgängig machen."
	/>
</StrongConfirm>

<RestoreOverlay active={overlay} source={targetName} />
