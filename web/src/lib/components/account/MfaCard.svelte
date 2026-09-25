<!-- Second factor of the signed-in user: TOTP (authenticator app), passkeys and recovery codes. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { MfaStatus, Passkey } from '$lib/api';
	import { errorMessage, fieldErrors } from '$lib/api/client';
	import {
		Alert,
		Badge,
		Button,
		Card,
		ErrorState,
		Input,
		Modal,
		RelativeTime,
		Skeleton
	} from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDate } from '$lib/utils/format';
	import { createPasskey, passkeysSupported } from '$lib/utils/webauthn';
	import RecoveryCodes from './RecoveryCodes.svelte';
	import TotpSetup from './TotpSetup.svelte';

	const data = new AsyncData<MfaStatus>();
	$effect(() => {
		data.run((signal) => api.get('/api/v1/auth/2fa', { signal }));
	});
	const st = $derived(data.data);
	const passkeys = $derived(st?.passkeys ?? []);
	const factors = $derived((st?.totp ? 1 : 0) + passkeys.length);
	const browserPasskeys = passkeysSupported();

	async function changed() {
		data.reload();
		await auth.check(true);
	}

	// ---------------------------------------------------------------- recovery codes shown once
	let codes = $state<string[]>([]);
	let codesOpen = $state(false);
	function showCodes(list: string[] | undefined) {
		if (!list?.length) return;
		codes = list;
		codesOpen = true;
	}

	// ---------------------------------------------------------------- TOTP
	let totpOpen = $state(false);
	async function totpDone(list: string[]) {
		totpOpen = false;
		toast.success('TOTP ist eingerichtet');
		await changed();
		showCodes(list);
	}

	// ---------------------------------------------------------------- password-confirmed actions
	let pwAction = $state<'disable' | 'codes' | null>(null);
	let pw = $state('');
	let pwError = $state<string | null>(null);
	let pwBusy = $state(false);
	function askPassword(action: 'disable' | 'codes') {
		pwAction = action;
		pw = '';
		pwError = null;
	}
	async function runPasswordAction() {
		if (!pw) {
			pwError = 'Passwort eingeben';
			return;
		}
		pwBusy = true;
		pwError = null;
		try {
			if (pwAction === 'disable') {
				await api.post('/api/v1/auth/2fa/totp/disable', { body: { password: pw } });
				toast.success('TOTP abgeschaltet');
				pwAction = null;
				await changed();
			} else {
				const res = await api.post('/api/v1/auth/2fa/recovery', { body: { password: pw } });
				pwAction = null;
				await changed();
				showCodes(res.recoveryCodes);
			}
		} catch (e) {
			pwError = fieldErrors(e).password ?? errorMessage(e);
		} finally {
			pwBusy = false;
		}
	}

	// ---------------------------------------------------------------- passkeys
	let pkName = $state('');
	let pkBusy = $state(false);
	async function addPasskey() {
		pkBusy = true;
		try {
			const options = await api.post('/api/v1/auth/passkeys/options');
			const credential = await createPasskey(options);
			const res = await api.post('/api/v1/auth/passkeys', {
				body: { name: pkName.trim() || defaultName(), credential }
			});
			toast.success(`Passkey „${res.passkey?.name}“ registriert`);
			pkName = '';
			await changed();
			showCodes(res.recoveryCodes);
		} catch (e) {
			toast.error(e);
		} finally {
			pkBusy = false;
		}
	}
	function defaultName(): string {
		const ua = navigator.userAgent;
		const os = /Windows/.test(ua)
			? 'Windows'
			: /iPhone|iPad/.test(ua)
				? 'iPhone'
				: /Mac/.test(ua)
					? 'Mac'
					: /Android/.test(ua)
						? 'Android'
						: /Linux/.test(ua)
							? 'Linux'
							: 'Gerät';
		return `Passkey (${os})`;
	}

	let renaming = $state<Passkey | null>(null);
	let newName = $state('');
	let renameBusy = $state(false);
	async function rename() {
		if (!renaming) return;
		renameBusy = true;
		try {
			await api.patch('/api/v1/auth/passkeys/{id}', { path: { id: renaming.id }, body: { name: newName } });
			renaming = null;
			data.reload();
		} catch (e) {
			toast.error(e);
		} finally {
			renameBusy = false;
		}
	}

	async function removePasskey(p: Passkey) {
		const ok = await confirm({
			title: 'Passkey entfernen?',
			message: `„${p.name}“ kann danach nicht mehr zur Anmeldung genutzt werden.`,
			confirmLabel: 'Entfernen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/auth/passkeys/{id}', { path: { id: p.id } });
			toast.success('Passkey entfernt');
			await changed();
		} catch (e) {
			toast.error(e);
		}
	}
</script>

<Card title="Zwei-Faktor-Anmeldung" description="Schützt das Konto zusätzlich zum Passwort" icon="shield">
	{#if data.error && !st}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !st}
		<Skeleton rows={4} />
	{:else}
		<div class="flex flex-col gap-5 text-sm">
			{#if st.required}
				<Alert tone="info">
					Deine Rolle „{auth.me?.principal?.roleName}“ verlangt einen zweiten Faktor – der letzte lässt sich
					nicht abschalten.
				</Alert>
			{/if}

			<section class="flex flex-col gap-2">
				<div class="flex flex-wrap items-center gap-2">
					<h3 class="font-medium text-fg">Authenticator-App (TOTP)</h3>
					{#if st.totp}<Badge tone="ok">aktiv</Badge>{:else}<Badge tone="neutral">aus</Badge>{/if}
				</div>
				<p class="text-fg-muted">
					6-stelliger Code aus einer App wie Aegis, Google oder Microsoft Authenticator.
				</p>
				<div class="flex flex-wrap gap-2">
					<Button
						size="sm"
						variant={st.totp ? 'secondary' : 'primary'}
						icon="phone"
						onclick={() => (totpOpen = true)}>{st.totp ? 'Neu einrichten' : 'Einrichten'}</Button
					>
					{#if st.totp}
						<Button size="sm" variant="ghost" onclick={() => askPassword('disable')}>Abschalten</Button>
					{/if}
				</div>
			</section>

			<section class="flex flex-col gap-2">
				<div class="flex flex-wrap items-center gap-2">
					<h3 class="font-medium text-fg">Passkeys</h3>
					<Badge tone={passkeys.length ? 'ok' : 'neutral'}>{passkeys.length}</Badge>
				</div>
				<p class="text-fg-muted">
					Fingerabdruck, Gesichtserkennung, Geräte-PIN oder Sicherheitsschlüssel. Ein Passkey gilt nur für die
					Adresse, unter der er eingerichtet wurde{st.rpId ? ` (hier: ${st.rpId})` : ''}.
				</p>
				{#if passkeys.length}
					<ul class="divide-y divide-border rounded-md border border-border">
						{#each passkeys as p (p.id)}
							<li class="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
								<div class="min-w-0 flex-1">
									<span class="font-medium">{p.name}</span>
									<span class="mono ml-1 text-xs text-fg-subtle">{p.rpId}</span>
									<span class="block text-xs text-fg-subtle">
										angelegt {formatDate(p.createdAt)} · {#if p.lastUsedAt}zuletzt <RelativeTime
												value={p.lastUsedAt}
											/>{:else}noch nicht benutzt{/if}
									</span>
								</div>
								<Button
									size="xs"
									variant="ghost"
									icon="edit"
									label="„{p.name}“ umbenennen"
									onclick={() => {
										renaming = p;
										newName = p.name;
									}}
								/>
								<Button
									size="xs"
									variant="ghost"
									icon="trash"
									label="„{p.name}“ entfernen"
									onclick={() => removePasskey(p)}
								/>
							</li>
						{/each}
					</ul>
				{/if}
				{#if st.passkeysAvailable && browserPasskeys}
					<form
						class="flex flex-wrap items-end gap-2"
						onsubmit={(e) => {
							e.preventDefault();
							addPasskey();
						}}
					>
						<Input
							label="Name (optional)"
							bind:value={pkName}
							placeholder="z. B. Laptop, YubiKey"
							class="w-56"
						/>
						<Button type="submit" size="sm" icon="plus" loading={pkBusy}>Passkey hinzufügen</Button>
					</form>
				{:else}
					<p class="text-xs text-fg-subtle">
						Passkeys funktionieren nur über HTTPS mit einem Hostnamen (z. B. https://netscope.example.lan
						hinter einem Reverse-Proxy), nicht über eine IP-Adresse wie hier.
					</p>
				{/if}
			</section>

			{#if factors > 0}
				<section class="flex flex-col gap-2">
					<h3 class="font-medium text-fg">Wiederherstellungscodes</h3>
					<p class="text-fg-muted">
						{st.recoveryCodes} von 10 unbenutzt – für den Fall, dass kein zweiter Faktor zur Hand ist.
						{#if st.recoveryCodes <= 3}<span class="text-warn">Bald neue erzeugen.</span>{/if}
					</p>
					<div>
						<Button size="sm" variant="ghost" icon="refresh" onclick={() => askPassword('codes')}
							>Neue Codes erzeugen</Button
						>
					</div>
				</section>
			{/if}
		</div>
	{/if}
</Card>

<Modal bind:open={totpOpen} title="Authenticator-App einrichten" size="md">
	{#if totpOpen}
		<TotpSetup replacing={!!st?.totp} ondone={totpDone} oncancel={() => (totpOpen = false)} />
	{/if}
</Modal>

<Modal bind:open={codesOpen} title="Wiederherstellungscodes" size="md">
	<RecoveryCodes {codes} />
	{#snippet footer()}
		<Button variant="primary" onclick={() => (codesOpen = false)}>Gespeichert</Button>
	{/snippet}
</Modal>

<Modal
	open={pwAction !== null}
	onclose={() => (pwAction = null)}
	title={pwAction === 'disable' ? 'TOTP abschalten' : 'Neue Wiederherstellungscodes'}
	description={pwAction === 'disable'
		? 'Die Anmeldung braucht danach keinen Code aus der App mehr.'
		: 'Die bisherigen Codes werden ungültig.'}
	size="sm"
	as="form"
	onsubmit={runPasswordAction}
	busy={pwBusy}
>
	<div class="flex flex-col gap-3">
		{#if pwError}<Alert tone="danger">{pwError}</Alert>{/if}
		<Input label="Passwort" type="password" autocomplete="current-password" bind:value={pw} required />
	</div>
	{#snippet footer()}
		<Button onclick={() => (pwAction = null)} disabled={pwBusy}>Abbrechen</Button>
		<Button type="submit" variant={pwAction === 'disable' ? 'danger' : 'primary'} loading={pwBusy}
			>{pwAction === 'disable' ? 'Abschalten' : 'Erzeugen'}</Button
		>
	{/snippet}
</Modal>

<Modal
	open={renaming !== null}
	onclose={() => (renaming = null)}
	title="Passkey umbenennen"
	size="sm"
	as="form"
	onsubmit={rename}
	busy={renameBusy}
>
	<Input label="Name" bind:value={newName} maxlength={64} required />
	{#snippet footer()}
		<Button onclick={() => (renaming = null)} disabled={renameBusy}>Abbrechen</Button>
		<Button type="submit" variant="primary" loading={renameBusy}>Speichern</Button>
	{/snippet}
</Modal>
