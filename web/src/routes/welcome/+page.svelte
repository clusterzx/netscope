<!--
	Setup wizard of a new installation (FR-013). Until it is finished no plugin runs. It
	belongs to whoever proves access to the host with the setup code (log, data/setup-code.txt);
	step 2 creates the administrator, from then on the wizard runs with that session.
	Every step has "Zurück"; the entries stay (sessionStorage, see wizard.svelte.ts).
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api, ApiError, errorMessage, fieldErrors, setSetupCode } from '$lib/api';
	import { Alert, Button, Icon, Input, Spinner } from '$lib/components/ui';
	import StepAccount from '$lib/components/welcome/StepAccount.svelte';
	import StepFinish from '$lib/components/welcome/StepFinish.svelte';
	import StepInstance from '$lib/components/welcome/StepInstance.svelte';
	import StepLanguage from '$lib/components/welcome/StepLanguage.svelte';
	import StepNetworks from '$lib/components/welcome/StepNetworks.svelte';
	import StepScanners from '$lib/components/welcome/StepScanners.svelte';
	import StepSources from '$lib/components/welcome/StepSources.svelte';
	import { STEPS, wizard } from '$lib/components/welcome/wizard.svelte';
	import { t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { setupState } from '$lib/stores/setup.svelte';
	import { toast } from '$lib/stores/toast.svelte';

	const titles: Record<number, string> = {
		1: t('Sprache und Zeitzone'),
		2: t('Konto'),
		3: t('Instanz'),
		4: t('Netze'),
		5: 'Scanner',
		6: t('Quellen'),
		7: t('Abschluss')
	};
	const intros: Record<number, string> = {
		1: t('In welcher Sprache und Zeitzone arbeitet NetScope?'),
		2: t('Das erste Konto verwaltet NetScope als Administrator.'),
		3: t('Läuft diese Instanz allein, als Zentrale oder als Standort einer Zentrale?'),
		4: t('Welche Netze soll NetScope kennen und scannen?'),
		5: t('Welche Scanner sollen von Anfang an laufen? Bis zum Abschluss läuft keiner.'),
		6: t('Welche Systeme soll NetScope mit Zugangsdaten abfragen?'),
		7: t('Alles im Blick – mit „Einrichtung abschließen“ startet NetScope.')
	};

	let phase = $state<'loading' | 'code' | 'login' | 'wizard'>('loading');
	let loadError = $state('');
	let code = $state('');
	let codeError = $state('');
	let busy = $state(false);
	let summary = $state('');
	let stepErrors = $state<Record<string, string>>({});

	let stepLanguage = $state<StepLanguage>();
	let stepAccount = $state<StepAccount>();
	let stepInstance = $state<StepInstance>();
	let stepNetworks = $state<StepNetworks>();

	const step = $derived(wizard.data.step);
	// names of the sources set up in step 6 (for the summary)
	let enabledSources = $state<string[]>([]);
	$effect(() => {
		if (phase === 'wizard' && step === 7) sourceNames().then((v) => (enabledSources = v));
	});

	onMount(async () => {
		try {
			const st = await setupState.check(undefined, true);
			if (st && !st.pending) {
				await goto('/', { replaceState: true });
				return;
			}
			const me = await auth.check(true).catch(() => null);
			if (me?.principal?.admin) {
				await loadOptions();
				return;
			}
			if (st?.account) {
				phase = 'login';
				return;
			}
			if (wizard.data.code) {
				setSetupCode(wizard.data.code);
				try {
					await loadOptions();
					return;
				} catch {
					// a stale code (e.g. from before a restart with a new data directory)
					setSetupCode(null);
					wizard.data.code = '';
					wizard.save();
				}
			}
			phase = 'code';
		} catch (e) {
			loadError = errorMessage(e);
			phase = 'code';
		}
	});

	async function loadOptions() {
		// a wrong code must not lead to the login page (auth: false)
		wizard.init(await api.get('/api/v1/setup/options', { auth: false }));
		phase = 'wizard';
	}

	async function submitCode(e: SubmitEvent) {
		e.preventDefault();
		codeError = '';
		const c = code.trim();
		if (!c) {
			codeError = t('Einrichtungscode eingeben');
			return;
		}
		busy = true;
		try {
			await api.post('/api/v1/setup/verify', { body: { code: c }, auth: false });
			setSetupCode(c);
			wizard.data.code = c;
			wizard.save();
			await loadOptions();
		} catch (err) {
			codeError = errorMessage(err);
		} finally {
			busy = false;
		}
	}

	function go(n: number) {
		summary = '';
		wizard.data.step = n;
		wizard.save();
		window.scrollTo({ top: 0 });
	}

	async function next() {
		summary = '';
		stepErrors = {};
		switch (step) {
			case 1:
				if (!stepLanguage?.validate()) return;
				break;
			case 2: {
				// a new account stays on this step: TOTP can be set up right away
				const created = !wizard.options?.account;
				busy = true;
				try {
					if (!(await stepAccount?.submit())) return;
				} finally {
					busy = false;
				}
				if (created) return;
				break;
			}
			case 3:
				if (!stepInstance?.validate()) return;
				break;
			case 4:
				if (!stepNetworks?.validate()) return;
				break;
			case 7:
				await complete();
				return;
		}
		go(step + 1);
	}

	async function sourceNames(): Promise<string[]> {
		try {
			const ids = new Set((wizard.options?.categories ?? []).flatMap((c) => c.plugins));
			const list = (await api.get('/api/v1/plugins')) ?? [];
			return list.filter((p) => ids.has(p.info.id) && p.config?.enabled).map((p) => p.info.name);
		} catch {
			return [];
		}
	}

	// which step a server field error belongs to
	function stepOf(field: string): number {
		if (field === 'language' || field === 'timezone') return 1;
		if (field === 'publicUrl' || field.startsWith('federation')) return 3;
		if (field.startsWith('subnets') || field.startsWith('scanExclusions') || field.startsWith('dnsServer'))
			return 4;
		if (field === 'scanners') return 5;
		return 7;
	}

	async function complete() {
		const d = wizard.data;
		busy = true;
		try {
			const res = await api.post('/api/v1/setup/complete', {
				body: {
					language: d.language,
					timezone: d.timezone.trim(),
					publicUrl: d.publicUrl.trim(),
					federation: {
						role: d.role,
						localName: d.localName.trim(),
						centralUrl: d.centralUrl.trim(),
						fingerprint: d.fingerprint.trim(),
						token: d.role === 'site' && d.token.trim() ? d.token.trim() : undefined
					},
					subnets: d.subnets
						.filter((s) => s.selected)
						.map((s) => ({
							cidr: s.cidr,
							name: s.name.trim(),
							interface: s.interface,
							gateway: s.gateway,
							access: s.access
						})),
					scanExclusions: d.exclusions.map((x) => x.trim()).filter(Boolean),
					dnsServer: d.dnsServer.trim(),
					scanners: d.scanners,
					firstScan: d.firstScan && (d.scanners.includes('arpscan') || d.scanners.includes('icmp'))
				}
			});
			const account = wizard.options?.account;
			if (account && account.locale !== d.language) {
				await api.put('/api/v1/auth/preferences', { body: { locale: d.language } }).catch(() => {});
			}
			setSetupCode(null);
			wizard.clear();
			setupState.finished();
			await auth.check(true).catch(() => null);
			toast.success(
				res.runs.length
					? t('NetScope ist eingerichtet – der erste Scan läuft.')
					: t('NetScope ist eingerichtet.')
			);
			await goto('/', { replaceState: true });
		} catch (e) {
			const fe = fieldErrors(e);
			const keys = Object.keys(fe);
			summary = errorMessage(e);
			if (keys.length) {
				stepErrors = fe;
				const target = Math.min(...keys.map(stepOf));
				if (target !== 7) {
					wizard.data.step = target;
					wizard.save();
				}
			} else if (e instanceof ApiError && e.code === 'setup_completed') {
				setupState.finished();
				await goto('/', { replaceState: true });
			}
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>{t('NetScope einrichten')}</title>
</svelte:head>

<div class="min-h-dvh bg-bg px-4 py-8">
	<div class="mx-auto flex w-full max-w-4xl flex-col gap-5">
		<header class="flex items-center gap-3">
			<img src="/favicon.svg" alt="" width="36" height="36" class="rounded-lg" />
			<div class="min-w-0 flex-1">
				<h1 class="text-lg font-semibold text-fg">{t('NetScope einrichten')}</h1>
				{#if phase === 'wizard'}
					<p class="text-sm text-fg-muted">
						{t('Schritt {n} von {total}', { n: step, total: STEPS.length })}
					</p>
				{/if}
			</div>
		</header>

		{#if phase === 'loading'}
			<div class="flex justify-center py-16"><Spinner /></div>
		{:else if phase === 'code'}
			<form class="rounded-xl border border-border bg-surface p-6 shadow-md" novalidate onsubmit={submitCode}>
				<h2 class="text-base font-semibold text-fg">{t('Einrichtungscode')}</h2>
				<p class="mt-1 mb-4 text-sm text-fg-muted">
					{t(
						'Diese Instanz ist noch nicht eingerichtet. Damit nur du sie in Besitz nimmst, steht ein Einrichtungscode im Log von NetScope und in der Datei data/setup-code.txt auf dem Host.'
					)}
				</p>
				{#if loadError}<Alert tone="danger">{loadError}</Alert>{/if}
				<Input
					id="welcome-code"
					label={t('Einrichtungscode')}
					bind:value={code}
					placeholder="ABCD-EFGH-JKMN"
					autocomplete="off"
					spellcheck={false}
					mono
					error={codeError}
					required
				/>
				<div class="mt-2 text-xs text-fg-subtle">
					<p>{t('Zum Beispiel:')}</p>
					<code class="mono">docker logs netscope 2>&1 | grep -i setup</code><br />
					<code class="mono">cat data/setup-code.txt</code>
				</div>
				<div class="mt-5 flex justify-end">
					<Button type="submit" variant="primary" iconRight="arrow-right" loading={busy}>{t('Weiter')}</Button
					>
				</div>
			</form>
		{:else if phase === 'login'}
			<div class="rounded-xl border border-border bg-surface p-6 shadow-md">
				<h2 class="text-base font-semibold text-fg">{t('Einrichtung fortsetzen')}</h2>
				<p class="mt-1 mb-4 text-sm text-fg-muted">
					{t(
						'Das Konto des Administrators ist schon angelegt. Melde dich an, um die Einrichtung fortzusetzen.'
					)}
				</p>
				<Button variant="primary" href="/login?next=%2Fwelcome" iconRight="arrow-right"
					>{t('Anmelden')}</Button
				>
			</div>
		{:else}
			<nav aria-label={t('Schritte')}>
				<ol class="flex flex-wrap gap-1">
					{#each STEPS as n (n)}
						<li>
							<button
								type="button"
								class="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm transition-colors
								{n === step
									? 'bg-accent-soft font-medium text-accent'
									: n < step
										? 'cursor-pointer text-fg-muted hover:bg-surface-3 hover:text-fg'
										: 'text-fg-subtle'}"
								disabled={n >= step || busy}
								aria-current={n === step ? 'step' : undefined}
								onclick={() => go(n)}
							>
								<span
									class="flex size-5 items-center justify-center rounded-full text-xs tabular
									{n < step ? 'bg-ok-soft text-ok' : n === step ? 'bg-accent text-white' : 'bg-surface-3'}"
								>
									{#if n < step}<Icon name="check" size={12} />{:else}{n}{/if}
								</span>
								{titles[n]}
							</button>
						</li>
					{/each}
				</ol>
			</nav>

			<section
				class="rounded-xl border border-border bg-surface p-6 shadow-md"
				aria-labelledby="welcome-step-title"
			>
				<h2 id="welcome-step-title" class="text-base font-semibold text-fg">{titles[step]}</h2>
				<p class="mt-1 mb-5 text-sm text-fg-muted">{intros[step]}</p>
				{#if summary}<Alert tone="danger" class="mb-4">{summary}</Alert>{/if}
				{#if step === 1}
					<StepLanguage bind:this={stepLanguage} />
				{:else if step === 2}
					<StepAccount bind:this={stepAccount} />
				{:else if step === 3}
					<StepInstance bind:this={stepInstance} errors={stepErrors} />
				{:else if step === 4}
					<StepNetworks bind:this={stepNetworks} errors={stepErrors} />
				{:else if step === 5}
					<StepScanners />
				{:else if step === 6}
					<StepSources />
				{:else}
					<StepFinish sources={enabledSources} />
				{/if}
			</section>

			<div class="flex flex-wrap items-center gap-2">
				{#if step > 1}
					<Button icon="arrow-left" onclick={() => go(step - 1)} disabled={busy}>{t('Zurück')}</Button>
				{/if}
				<span class="flex-1"></span>
				{#if step === 6}
					<Button variant="ghost" onclick={next} disabled={busy}>{t('Überspringen')}</Button>
				{/if}
				<Button
					variant="primary"
					iconRight={step === 7 ? 'check' : 'arrow-right'}
					loading={busy}
					onclick={next}
				>
					{step === 2 && !wizard.options?.account
						? t('Konto anlegen')
						: step === 7
							? t('Einrichtung abschließen')
							: t('Weiter')}
				</Button>
			</div>
		{/if}
	</div>
</div>
