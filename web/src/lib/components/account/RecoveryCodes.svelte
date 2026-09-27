<!-- Recovery codes shown once after setting up a second factor: copy or download them. -->
<script lang="ts">
	import { Alert, Button, CopyButton } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';

	let { codes }: { codes: string[] } = $props();

	const text = $derived(
		t('NetScope – Wiederherstellungscodes für {user} ({host})', {
			user: auth.me?.user?.username ?? '',
			host: typeof window !== 'undefined' ? window.location.host : ''
		}) +
			'\n' +
			t('Jeder Code funktioniert genau einmal anstelle des zweiten Faktors.') +
			'\n\n' +
			codes.join('\n') +
			'\n'
	);

	function download() {
		const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
		const a = document.createElement('a');
		a.href = url;
		a.download = t('netscope-wiederherstellungscodes.txt');
		a.click();
		URL.revokeObjectURL(url);
	}
</script>

<div class="flex flex-col gap-3">
	<Alert tone="warn" title={t('Nur jetzt sichtbar')}>
		{t(
			'Mit diesen Codes kommst du hinein, wenn Handy oder Passkey fehlen. Sicher ablegen, z. B. im Passwortmanager – jeder Code funktioniert genau einmal.'
		)}
	</Alert>
	<ol
		class="mono grid grid-cols-2 gap-x-6 gap-y-1 rounded-md border border-border bg-surface-2 px-4 py-3 text-sm select-all"
	>
		{#each codes as c (c)}<li>{c}</li>{/each}
	</ol>
	<div class="flex flex-wrap items-center gap-2">
		<Button size="sm" icon="download" onclick={download}>{t('Als Textdatei speichern')}</Button>
		<CopyButton text={codes.join('\n')} label={t('Codes kopieren')} size="sm" />
	</div>
</div>
