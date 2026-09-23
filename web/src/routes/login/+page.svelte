<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { ApiError, errorMessage } from '$lib/api/client';
	import Button from '$lib/components/ui/Button.svelte';
	import Input from '$lib/components/ui/Input.svelte';
	import Alert from '$lib/components/ui/Alert.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { theme } from '$lib/stores/theme.svelte';

	let username = $state('admin');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	let pwInput: HTMLInputElement | null = $state(null);
	let userInput: HTMLInputElement | null = $state(null);

	/** only local paths are accepted as redirect target */
	function target(): string {
		const next = page.url.searchParams.get('next') ?? '/';
		return next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/login') ? next : '/';
	}

	onMount(async () => {
		try {
			if (await auth.check(true)) {
				goto(target(), { replaceState: true });
				return;
			}
		} catch {
			// backend unreachable – the form shows the error on submit
		}
		(username ? pwInput : userInput)?.focus();
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!username.trim() || !password) {
			error = 'Benutzername und Passwort eingeben';
			return;
		}
		busy = true;
		error = '';
		try {
			await auth.login(username.trim(), password);
			await goto(target(), { replaceState: true });
		} catch (err) {
			error =
				err instanceof ApiError && err.status === 401
					? 'Benutzername oder Passwort falsch'
					: errorMessage(err);
			password = '';
			pwInput?.focus();
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>Anmelden · NetScope</title>
</svelte:head>

<div class="relative flex min-h-dvh items-center justify-center overflow-hidden bg-bg px-4 py-10">
	<!-- subtle radar rings as background texture -->
	<svg class="pointer-events-none absolute inset-0 h-full w-full text-border" aria-hidden="true">
		<defs>
			<pattern id="grid" width="32" height="32" patternUnits="userSpaceOnUse">
				<path d="M32 0H0V32" fill="none" stroke="currentColor" stroke-width="0.6" opacity="0.6" />
			</pattern>
			<radialGradient id="fade" cx="50%" cy="45%" r="60%">
				<stop offset="0%" stop-color="white" stop-opacity="1" />
				<stop offset="100%" stop-color="white" stop-opacity="0" />
			</radialGradient>
			<mask id="m"><rect width="100%" height="100%" fill="url(#fade)" /></mask>
		</defs>
		<rect width="100%" height="100%" fill="url(#grid)" mask="url(#m)" />
	</svg>

	<div class="relative w-full max-w-sm">
		<div class="mb-6 flex flex-col items-center gap-3 text-center">
			<img src="/favicon.svg" alt="" width="52" height="52" class="rounded-xl shadow-md" />
			<div>
				<h1 class="text-2xl font-semibold tracking-tight text-fg">NetScope</h1>
				<p class="mt-1 text-sm text-fg-muted">Netzwerk-Inventar &amp; Asset-Monitoring</p>
			</div>
		</div>

		<form onsubmit={submit} class="rounded-xl border border-border bg-surface p-6 shadow-md" novalidate>
			<div class="flex flex-col gap-4">
				{#if error}
					<Alert tone="danger">{error}</Alert>
				{/if}
				<Input
					label="Benutzername"
					bind:value={username}
					bind:ref={userInput}
					autocomplete="username"
					autocapitalize="none"
					spellcheck={false}
					required
				/>
				<Input
					label="Passwort"
					type="password"
					bind:value={password}
					bind:ref={pwInput}
					autocomplete="current-password"
					required
				/>
				<Button type="submit" variant="primary" full loading={busy}>Anmelden</Button>
			</div>
		</form>
		<p class="mt-5 text-center text-xs text-fg-subtle">
			Darstellung:
			<button
				type="button"
				class="underline-offset-2 hover:text-fg hover:underline"
				onclick={() => theme.cycle()}
			>
				{theme.mode === 'system' ? 'System' : theme.mode === 'dark' ? 'Dunkel' : 'Hell'}
			</button>
		</p>
	</div>
</div>
