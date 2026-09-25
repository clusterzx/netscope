<!-- TOTP setup: QR code for the authenticator app, activated by its first code. Replacing an
     active TOTP asks for the password first. ondone receives new recovery codes (may be empty). -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { TotpSetup } from '$lib/api';
	import { errorMessage, fieldErrors } from '$lib/api/client';
	import { Alert, Button, CopyButton, Input, Spinner } from '$lib/components/ui';

	interface Props {
		replacing?: boolean;
		ondone: (recoveryCodes: string[]) => void;
		oncancel?: () => void;
	}

	let { replacing = false, ondone, oncancel }: Props = $props();

	let setup = $state<TotpSetup | null>(null);
	let password = $state('');
	let code = $state('');
	let error = $state<string | null>(null);
	let busy = $state(false);

	const grouped = $derived(setup?.secret.replace(/(.{4})/g, '$1 ').trim() ?? '');

	async function start() {
		busy = true;
		error = null;
		try {
			setup = await api.post('/api/v1/auth/2fa/totp', { body: replacing ? { password } : {} });
		} catch (e) {
			error = fieldErrors(e).password ?? errorMessage(e);
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		if (!replacing) start();
	});

	async function confirm() {
		if (!/^\d{6}$/.test(code.replace(/\s/g, ''))) {
			error = 'Den 6-stelligen Code aus der App eingeben';
			return;
		}
		busy = true;
		error = null;
		try {
			const res = await api.post('/api/v1/auth/2fa/totp/confirm', { body: { code } });
			ondone(res.recoveryCodes ?? []);
		} catch (e) {
			error = fieldErrors(e).code ?? errorMessage(e);
			code = '';
		} finally {
			busy = false;
		}
	}
</script>

<form
	novalidate
	class="flex flex-col gap-4 text-sm"
	onsubmit={(e) => {
		e.preventDefault();
		if (setup) confirm();
		else start();
	}}
>
	{#if error}<Alert tone="danger">{error}</Alert>{/if}
	{#if !setup}
		{#if replacing}
			<p class="text-fg-muted">
				Die bisherige Verbindung zur Authenticator-App wird ersetzt, sobald der neue Code bestätigt ist. Zur
				Sicherheit dein Passwort:
			</p>
			<Input
				label="Passwort"
				type="password"
				autocomplete="current-password"
				bind:value={password}
				required
			/>
			<div class="flex gap-2">
				<Button type="submit" variant="primary" loading={busy}>Weiter</Button>
				{#if oncancel}<Button onclick={oncancel} disabled={busy}>Abbrechen</Button>{/if}
			</div>
		{:else if busy}
			<div class="flex items-center gap-2 text-fg-muted"><Spinner size={16} /> Schlüssel wird erzeugt …</div>
		{:else if oncancel}
			<div><Button onclick={oncancel}>Schließen</Button></div>
		{/if}
	{:else}
		<ol class="flex list-decimal flex-col gap-4 pl-5">
			<li>
				<p>
					Authenticator-App öffnen (z. B. Aegis, Google Authenticator, Microsoft Authenticator, 1Password) und
					den QR-Code scannen:
				</p>
				<img
					src={setup.qr}
					alt="QR-Code für die Authenticator-App"
					width="176"
					height="176"
					class="mt-2 h-44 w-44 rounded-md border border-border bg-white p-1"
				/>
				<p class="mt-2 text-xs text-fg-subtle">Scannen nicht möglich? Schlüssel von Hand eingeben:</p>
				<div class="mt-1 flex items-center gap-2">
					<code class="mono rounded bg-surface-2 px-2 py-1 text-xs break-all select-all">{grouped}</code>
					<CopyButton text={setup.secret} label="Schlüssel kopieren" />
				</div>
			</li>
			<li>
				<Input
					label="Code aus der App"
					bind:value={code}
					inputmode="numeric"
					autocomplete="one-time-code"
					maxlength={7}
					placeholder="123456"
					class="w-40"
					mono
					required
				/>
			</li>
		</ol>
		<div class="flex gap-2">
			<Button type="submit" variant="primary" icon="check" loading={busy}>Aktivieren</Button>
			{#if oncancel}<Button onclick={oncancel} disabled={busy}>Abbrechen</Button>{/if}
		</div>
	{/if}
</form>
