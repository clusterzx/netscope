<!-- Step 7: summary, backup note for master.key, optional first ARP/ping run. -->
<script lang="ts">
	import { Alert, Checkbox, CodeBlock, DescItem, DescList } from '$lib/components/ui';
	import { LOCALES, t, tn } from '$lib/i18n';
	import { wizard } from './wizard.svelte';

	let { sources }: { sources: string[] } = $props();

	const d = $derived(wizard.data);
	const o = $derived(wizard.options);
	const roleLabel = $derived(
		d.role === 'central'
			? t('Zentrale')
			: d.role === 'site'
				? t('Standort von {url}', {
						url: o?.federation.managed ? (o.federation.centralUrl ?? '') : d.centralUrl
					})
				: t('Allein')
	);
	const subnets = $derived(d.subnets.filter((s) => s.selected));
	const scannerNames = $derived(
		(o?.scanners ?? []).filter((s) => d.scanners.includes(s.id)).map((s) => s.name)
	);
	const canFirstScan = $derived(d.scanners.includes('arpscan') || d.scanners.includes('icmp'));
</script>

<div class="flex flex-col gap-5">
	<DescList>
		<DescItem label={t('Sprache')} value={LOCALES.find((l) => l.value === d.language)?.label ?? d.language} />
		<DescItem label={t('Zeitzone')} value={d.timezone} mono />
		<DescItem label={t('Konto')} value={o?.account?.displayName || o?.account?.username || '–'} />
		<DescItem label={t('Rolle')} value={roleLabel} />
		<DescItem label={t('Öffentliche URL')} value={d.publicUrl || '–'} mono />
		<DescItem label={t('Subnetze')}>
			{#if subnets.length}
				<span class="mono">{subnets.map((s) => s.cidr).join(', ')}</span>
			{:else}
				<span class="text-warn"
					>{t('keines – Scanner finden ohne Subnetz nur die angeschlossenen Netze')}</span
				>
			{/if}
		</DescItem>
		<DescItem label={t('Ausgenommen')} value={d.exclusions.length ? d.exclusions.join(', ') : '–'} mono />
		<DescItem label={t('DNS-Server')} value={d.dnsServer || t('System-DNS')} mono />
		<DescItem label="Scanner">
			{#if scannerNames.length}{scannerNames.join(', ')}{:else}<span class="text-warn">{t('keiner')}</span
				>{/if}
		</DescItem>
		<DescItem label={t('Quellen')} value={sources.length ? sources.join(', ') : '–'} />
	</DescList>

	<Alert tone="warn" title={t('Master-Key sichern')}>
		{#if o?.masterKeyFile}
			<p>
				{t(
					'Zugangsdaten verschlüsselt NetScope mit dem Master-Key in dieser Datei. Ohne sie sind gespeicherte Zugangsdaten nach einer Wiederherstellung verloren – bitte sicher aufbewahren:'
				)}
			</p>
			<CodeBlock code={o.masterKeyFile} />
		{:else}
			<p>
				{t(
					'Der Master-Key kommt aus der Umgebungsvariable NETSCOPE_MASTER_KEY. Ohne ihn sind gespeicherte Zugangsdaten verloren – bitte sicher aufbewahren.'
				)}
			</p>
		{/if}
	</Alert>

	<Checkbox
		bind:checked={wizard.data.firstScan}
		disabled={!canFirstScan}
		label={t('Gleich einen ersten ARP-Scan und Ping starten')}
		description={canFirstScan
			? t('Findet die Geräte in den gewählten Netzen sofort, statt auf den ersten Zeitplan zu warten.')
			: t('Dafür ARP-Scan oder ICMP-Ping bei den Scannern auswählen.')}
		onchange={() => wizard.save()}
	/>
	<p class="text-xs text-fg-subtle">
		{tn(
			d.scanners.length,
			'Nach dem Abschluss läuft {n} Scanner nach Zeitplan; alles lässt sich später unter Plugins und System ändern.',
			'Nach dem Abschluss laufen {n} Scanner nach Zeitplan; alles lässt sich später unter Plugins und System ändern.'
		)}
	</p>
</div>
