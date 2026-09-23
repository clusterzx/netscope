<!-- Create a device manually (POST /api/v1/devices {name, ip, mac}). -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, errorMessage } from '$lib/api';
	import Alert from '$lib/components/ui/Alert.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import { toast } from '$lib/stores/toast.svelte';

	let { open = $bindable(false) }: { open: boolean } = $props();

	let name = $state('');
	let ip = $state('');
	let mac = $state('');
	let error = $state('');
	let errors = $state<{ ip?: string; mac?: string }>({});
	let busy = $state(false);

	$effect(() => {
		if (open) {
			name = ip = mac = error = '';
			errors = {};
		}
	});

	function validate(): boolean {
		errors = {};
		if (ip && !/^(\d{1,3}\.){3}\d{1,3}$/.test(ip.trim()) && !ip.includes(':'))
			errors.ip = 'Ungültige IP-Adresse';
		if (mac && !/^([0-9a-fA-F]{2}[:-]?){5}[0-9a-fA-F]{2}$/.test(mac.trim()))
			errors.mac = 'Ungültige MAC-Adresse';
		if (!name.trim() && !ip.trim() && !mac.trim()) error = 'Name, IP oder MAC angeben';
		else error = '';
		return !error && !errors.ip && !errors.mac;
	}

	async function submit() {
		if (!validate()) return;
		busy = true;
		try {
			const res = await api.post('/api/v1/devices', {
				body: { name: name.trim(), ip: ip.trim(), mac: mac.trim() }
			});
			toast.success('Gerät angelegt');
			open = false;
			goto(`/devices/${res.id}`);
		} catch (e) {
			const msg = errorMessage(e);
			if (/IP/i.test(msg)) errors = { ip: msg };
			else if (/MAC/i.test(msg)) errors = { mac: msg };
			else error = msg;
		} finally {
			busy = false;
		}
	}
</script>

<Modal
	bind:open
	title="Gerät anlegen"
	description="Für Geräte, die kein Scanner findet (z. B. hinter einer Firewall). Mindestens ein Feld ausfüllen."
	size="sm"
	as="form"
	onsubmit={submit}
	{busy}
>
	<div class="flex flex-col gap-3">
		{#if error}<Alert tone="danger">{error}</Alert>{/if}
		<Input label="Name" bind:value={name} placeholder="z. B. Drucker Büro" />
		<Input label="IP-Adresse" bind:value={ip} error={errors.ip} mono placeholder="192.168.8.50" />
		<Input label="MAC-Adresse" bind:value={mac} error={errors.mac} mono placeholder="aa:bb:cc:dd:ee:ff" />
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="plus" loading={busy}>Anlegen</Button>
	{/snippet}
</Modal>
