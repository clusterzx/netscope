<!-- Recovery codes shown once after setting up a second factor: copy or download them. -->
<script lang="ts">
	import { Alert, Button, CopyButton } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';

	let { codes }: { codes: string[] } = $props();

	const text = $derived(
		`NetScope – Wiederherstellungscodes für ${auth.me?.user?.username ?? ''} (${typeof window !== 'undefined' ? window.location.host : ''})\n` +
			'Jeder Code funktioniert genau einmal anstelle des zweiten Faktors.\n\n' +
			codes.join('\n') +
			'\n'
	);

	function download() {
		const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
		const a = document.createElement('a');
		a.href = url;
		a.download = 'netscope-wiederherstellungscodes.txt';
		a.click();
		URL.revokeObjectURL(url);
	}
</script>

<div class="flex flex-col gap-3">
	<Alert tone="warn" title="Nur jetzt sichtbar">
		Mit diesen Codes kommst du hinein, wenn Handy oder Passkey fehlen. Sicher ablegen, z. B. im
		Passwortmanager – jeder Code funktioniert genau einmal.
	</Alert>
	<ol
		class="mono grid grid-cols-2 gap-x-6 gap-y-1 rounded-md border border-border bg-surface-2 px-4 py-3 text-sm select-all"
	>
		{#each codes as c (c)}<li>{c}</li>{/each}
	</ol>
	<div class="flex flex-wrap items-center gap-2">
		<Button size="sm" icon="download" onclick={download}>Als Textdatei speichern</Button>
		<CopyButton text={codes.join('\n')} label="Codes kopieren" size="sm" />
	</div>
</div>
