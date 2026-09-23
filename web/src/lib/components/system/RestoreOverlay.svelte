<!--
	Blocking overlay after a restore was accepted (202): the backend restarts its services.
	Polls GET /api/v1/health until it answers "ok" again, then reloads the page.
	<RestoreOverlay active={restoring} source="netscope-20260923.db" />
-->
<script lang="ts">
	import { request } from '$lib/api';
	import type { HealthResponse } from '$lib/api';
	import { Button, Spinner } from '$lib/components/ui';

	interface Props {
		active: boolean;
		source: string;
	}

	let { active, source }: Props = $props();

	let dlg: HTMLDialogElement | null = $state(null);
	let elapsed = $state(0);
	let phase = $state<'waiting' | 'down' | 'ready'>('waiting');
	let lastError = $state('');

	$effect(() => {
		if (!active || !dlg) return;
		if (!dlg.open) dlg.showModal();
		const started = Date.now();
		let stopped = false;
		let sawDown = false;
		let okStreak = 0;
		const tick = setInterval(() => (elapsed = Math.round((Date.now() - started) / 1000)), 1000);

		async function poll() {
			while (!stopped) {
				await new Promise((r) => setTimeout(r, 2000));
				if (stopped) return;
				try {
					const h = await request<HealthResponse>('GET', '/api/v1/health', { auth: false });
					lastError = '';
					if (h.status === 'ok') {
						okStreak++;
						// the server answers briefly before the restart starts: require a gap or some time
						if ((sawDown && okStreak >= 1) || (Date.now() - started > 8000 && okStreak >= 2)) {
							phase = 'ready';
							setTimeout(() => window.location.reload(), 800);
							return;
						}
					} else okStreak = 0;
				} catch (e) {
					sawDown = true;
					okStreak = 0;
					phase = 'down';
					lastError = e instanceof Error ? e.message : String(e);
				}
			}
		}
		poll();
		return () => {
			stopped = true;
			clearInterval(tick);
		};
	});
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dlg}
	aria-labelledby="restore-title"
	aria-describedby="restore-desc"
	oncancel={(e) => e.preventDefault()}
	class="m-auto w-[calc(100%-2rem)] max-w-md rounded-xl border border-border bg-surface p-0 text-fg shadow-lg backdrop:bg-overlay backdrop:backdrop-blur-sm"
>
	{#if active}
		<div class="flex flex-col items-center gap-3 px-6 py-8 text-center" role="status" aria-live="polite">
			<Spinner size={32} class="text-accent" />
			<h2 id="restore-title" class="text-base font-semibold">Datenbank wird wiederhergestellt</h2>
			<p id="restore-desc" class="text-sm text-fg-muted">
				Quelle: <span class="mono">{source}</span><br />
				{#if phase === 'ready'}
					Dienste laufen wieder – die Seite wird neu geladen …
				{:else if phase === 'down'}
					Dienste starten neu …
				{:else}
					Warte auf den Neustart der Dienste …
				{/if}
			</p>
			<p class="text-xs text-fg-subtle tabular">{elapsed} s</p>
			{#if elapsed > 120}
				<p class="text-xs text-warn">
					Das dauert ungewöhnlich lange{lastError ? ` (${lastError})` : ''}. Server-Log prüfen oder die Seite
					manuell neu laden.
				</p>
				<Button size="sm" icon="refresh" onclick={() => window.location.reload()}>Neu laden</Button>
			{/if}
		</div>
	{/if}
</dialog>
