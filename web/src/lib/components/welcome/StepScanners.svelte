<!--
	Step 5: the scanners active from the start, each with its load and schedule; presets as
	shortcuts. Evaluating plugins (CVE, OUI, changes, topology …) stay on and are not asked
	for; SSH and SNMP need credentials and come in the sources step.
-->
<script lang="ts">
	import type { SetupScanner } from '$lib/api';
	import { loadLabel, loadTone } from '$lib/components/plugins/plugin';
	import { Badge, Button, Icon, Toggle } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { PRESETS, wizard } from './wizard.svelte';

	const LOAD_ORDER: Record<string, number> = { low: 0, medium: 1, high: 2 };
	const list = $derived(
		[...(wizard.options?.scanners ?? [])].sort(
			(a, b) => LOAD_ORDER[a.load] - LOAD_ORDER[b.load] || a.name.localeCompare(b.name)
		)
	);

	const presets: { id: keyof typeof PRESETS; label: string; text: string }[] = [
		{ id: 'presence', label: t('Nur Anwesenheit'), text: t('ARP und Ping') },
		{ id: 'gentle', label: t('Ohne belastende Scans'), text: t('alles außer hoher Belastung') },
		{ id: 'full', label: t('Vollständig'), text: t('alle Scanner') }
	];

	function same(a: string[], b: string[]) {
		return a.length === b.length && a.every((x) => b.includes(x));
	}
	const activePreset = $derived(
		presets.find((p) => same(PRESETS[p.id](list), wizard.data.scanners))?.id ?? ''
	);

	function apply(id: keyof typeof PRESETS) {
		wizard.data.scanners = PRESETS[id](list);
		wizard.save();
	}

	function toggle(s: SetupScanner, on: boolean) {
		const set = new Set(wizard.data.scanners);
		if (on) set.add(s.id);
		else set.delete(s.id);
		wizard.data.scanners = list.map((x) => x.id).filter((id) => set.has(id));
		wizard.save();
	}
</script>

<div class="flex flex-col gap-5">
	<div class="flex flex-col gap-2">
		<span class="text-sm font-medium text-fg">{t('Vorlagen')}</span>
		<div class="flex flex-wrap gap-2">
			{#each presets as p (p.id)}
				<Button variant={activePreset === p.id ? 'primary' : 'secondary'} onclick={() => apply(p.id)}>
					{p.label}
					<span class="text-xs opacity-75">· {p.text}</span>
				</Button>
			{/each}
		</div>
	</div>

	<ul class="divide-y divide-border rounded-lg border border-border">
		{#each list as s (s.id)}
			{@const on = wizard.data.scanners.includes(s.id)}
			<li class="flex items-start gap-3 px-4 py-3">
				<Toggle checked={on} onchange={(v) => toggle(s, v)} label={s.name} hideLabel class="mt-0.5" />
				<div class="min-w-0 flex-1">
					<div class="flex flex-wrap items-center gap-2">
						<span class="font-medium text-fg">{s.name}</span>
						<Badge tone={loadTone[s.load]}>{loadLabel[s.load]}</Badge>
						{#if s.missingBinaries?.length}
							<Badge tone="warn">{t('fehlt: {list}', { list: s.missingBinaries.join(', ') })}</Badge>
						{/if}
					</div>
					<p class="mt-0.5 text-sm text-fg-muted">{s.description}</p>
					<p class="mt-1 flex items-center gap-1.5 text-xs text-fg-subtle" title={s.schedule}>
						<Icon name="clock" size={12} />{s.scheduleText || s.schedule}
					</p>
				</div>
			</li>
		{/each}
	</ul>
	<p class="text-xs text-fg-subtle">
		{t(
			'Auswertende Plugins (CVE-Abgleich, OUI-Hersteller, Änderungen, Topologie, Aufräumen …) bleiben aktiv. SSH-Inventar und SNMP brauchen Zugangsdaten und folgen im nächsten Schritt. Zeitpläne und Umfang lassen sich später je Plugin ändern.'
		)}
	</p>
</div>
