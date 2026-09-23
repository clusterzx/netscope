<!-- Split MAC addresses off into a new device (POST /api/v1/devices/{id}/split {macs}). -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, errorMessage } from '$lib/api';
	import type { DeviceDetail } from '$lib/api/types';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import { toast } from '$lib/stores/toast.svelte';

	interface Props {
		open: boolean;
		device: DeviceDetail;
		onsplit: (newId: number) => void;
	}

	let { open = $bindable(false), device, onsplit }: Props = $props();

	let selected = $state<string[]>([]);
	let error = $state('');
	let busy = $state(false);

	$effect(() => {
		if (open) {
			selected = [];
			error = '';
		}
	});

	const macs = $derived(device.macList ?? []);
	const ipsOf = (mac: string) =>
		(device.ipHistory ?? []).filter((i) => i.mac === mac && !i.goneAt).map((i) => i.ip);

	function toggle(mac: string, on: boolean) {
		selected = on ? [...selected, mac] : selected.filter((m) => m !== mac);
	}

	async function submit() {
		error = '';
		if (!selected.length) {
			error = 'Mindestens eine MAC-Adresse auswählen';
			return;
		}
		if (selected.length >= macs.length) {
			error = 'Mindestens eine MAC-Adresse muss beim Gerät bleiben';
			return;
		}
		busy = true;
		try {
			const res = await api.post('/api/v1/devices/{id}/split', {
				path: { id: device.id },
				body: { macs: selected }
			});
			open = false;
			onsplit(res.id);
			toast.success(`${selected.length === 1 ? 'MAC-Adresse' : 'MAC-Adressen'} in neues Gerät abgespalten`, {
				timeout: 10000,
				action: { label: 'Neues Gerät öffnen', onClick: () => goto(`/devices/${res.id}`) }
			});
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

<Modal
	bind:open
	title="MAC-Adressen abspalten"
	description="Die gewählten MACs samt ihrer IPs, Ports, Zertifikate und Web-Dienste werden zu einem neuen Gerät – z. B. wenn zwei Geräte fälschlich zusammengefasst wurden."
	as="form"
	onsubmit={submit}
	{busy}
>
	<div class="flex flex-col gap-3">
		{#if error}<Alert tone="danger">{error}</Alert>{/if}
		<fieldset class="flex flex-col gap-1.5">
			<legend class="mb-1 text-[0.8125rem] font-medium">MAC-Adressen für das neue Gerät</legend>
			{#each macs as m (m.mac)}
				<label
					class="flex cursor-pointer items-center gap-2.5 rounded-md border border-border px-3 py-2 text-sm hover:bg-surface-2 has-checked:border-accent has-checked:bg-accent-soft"
				>
					<input
						type="checkbox"
						class="h-4 w-4 accent-(--accent)"
						checked={selected.includes(m.mac)}
						onchange={(e) => toggle(m.mac, (e.currentTarget as HTMLInputElement).checked)}
					/>
					<span class="min-w-0 flex-1">
						<span class="mono">{m.mac}</span>
						{#if m.randomized}<Badge tone="warn">zufällig</Badge>{/if}
						<span class="block truncate text-xs text-fg-subtle">
							{m.vendor || 'Hersteller unbekannt'}
							{#if ipsOf(m.mac).length}
								&middot; <span class="mono">{ipsOf(m.mac).join(', ')}</span>
							{/if}
						</span>
					</span>
					<RelativeTime value={m.lastSeen} class="text-xs text-fg-subtle" />
				</label>
			{/each}
		</fieldset>
		<p class="text-xs text-fg-subtle">
			Manuelle Angaben, Tags und Notizen bleiben beim bisherigen Gerät. Rückgängig machen lässt sich die
			Aufteilung per „Zusammenführen“ in der Geräteliste.
		</p>
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="split" loading={busy}>Abspalten</Button>
	{/snippet}
</Modal>
