<!--
	"Scan jetzt": pick scanners (meta.scanners) and run them for one device
	(POST /api/v1/devices/{id}/scan). The started runs are handed to the page (ScanStrip).
-->
<script lang="ts" module>
	export interface StartedScan {
		plugin: string;
		name: string;
		runId: number;
		/** final status once the run finished (else taken live from the runs store) */
		status?: string;
		error?: string;
		durationMs?: number;
	}
</script>

<script lang="ts">
	import { api, errorMessage } from '$lib/api';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import { meta } from '$lib/stores/catalog.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { loadPref, savePref } from '$lib/utils/url';

	interface Props {
		open: boolean;
		deviceId: number;
		deviceName: string;
		onstarted: (runs: StartedScan[]) => void;
	}

	let { open = $bindable(false), deviceId, deviceName, onstarted }: Props = $props();

	const scanners = $derived(
		[...(meta.value?.scanners ?? [])].sort(
			(a, b) => Number(b.enabled) - Number(a.enabled) || a.name.localeCompare(b.name, 'de')
		)
	);
	const slow = new Set(['nmap', 'nmap_udp']);

	let selected = $state<string[]>([]);
	let error = $state('');
	let busy = $state(false);

	$effect(() => {
		if (!open) return;
		meta.load().catch(() => {});
		error = '';
		const pref = loadPref<string[]>('device.scanners', ['icmp']);
		selected = pref;
	});

	function toggle(id: string, on: boolean) {
		selected = on ? [...new Set([...selected, id])] : selected.filter((x) => x !== id);
	}

	const valid = $derived(selected.filter((id) => scanners.some((s) => s.id === id && s.enabled)));

	async function submit() {
		if (!valid.length) {
			error = 'Mindestens einen aktiven Scanner wählen';
			return;
		}
		busy = true;
		error = '';
		try {
			const res = await api.post('/api/v1/devices/{id}/scan', {
				path: { id: deviceId },
				body: { plugins: valid }
			});
			savePref('device.scanners', valid);
			const started: StartedScan[] = Object.entries(res.runs ?? {}).map(([plugin, runId]) => ({
				plugin,
				runId,
				name: scanners.find((s) => s.id === plugin)?.name ?? plugin
			}));
			onstarted(started);
			toast.info(
				started.length === 1 ? `${started[0].name} gestartet` : `${started.length} Scanner gestartet`,
				{ title: deviceName }
			);
			open = false;
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

<Modal
	bind:open
	title="Scan jetzt"
	description="Ausgewählte Scanner sofort nur für „{deviceName}“ ausführen."
	size="sm"
	as="form"
	onsubmit={submit}
	{busy}
>
	<div class="flex flex-col gap-3">
		{#if error}<Alert tone="danger">{error}</Alert>{/if}
		{#if !meta.value && meta.loading}
			<p class="text-sm text-fg-muted">Scanner werden geladen …</p>
		{:else if !scanners.length}
			<p class="text-sm text-fg-muted">Keine Scanner verfügbar.</p>
		{:else}
			<fieldset class="flex flex-col gap-1">
				<legend class="mb-1.5 text-[0.8125rem] font-medium">Scanner</legend>
				{#each scanners as s (s.id)}
					<label
						class="flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm {s.enabled
							? 'cursor-pointer hover:bg-surface-2'
							: 'cursor-not-allowed opacity-55'}"
					>
						<input
							type="checkbox"
							class="h-4 w-4 accent-(--accent)"
							checked={selected.includes(s.id) && s.enabled}
							disabled={!s.enabled}
							onchange={(e) => toggle(s.id, (e.currentTarget as HTMLInputElement).checked)}
						/>
						<span class="min-w-0 flex-1">{s.name}</span>
						{#if !s.enabled}
							<span class="text-xs text-fg-subtle">inaktiv</span>
						{:else if slow.has(s.id)}
							<span class="text-xs text-fg-subtle">dauert länger</span>
						{/if}
					</label>
				{/each}
			</fieldset>
			<p class="text-xs text-fg-subtle">
				Inaktive Scanner lassen sich unter <a href="/plugins" class="link">Plugins</a> aktivieren. Fortschritt und
				Ergebnis erscheinen oben auf der Geräteseite.
			</p>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="radar" loading={busy} disabled={!valid.length}>
			{valid.length > 1 ? `${valid.length} Scanner starten` : 'Scan starten'}
		</Button>
	{/snippet}
</Modal>
