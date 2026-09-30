<!--
	Step 4: the networks to scan – detected ones to confirm or deselect, further (also
	routed) ones to add –, addresses no scanner probes and the DNS server for reverse DNS.
-->
<script lang="ts">
	import { Badge, Button, Checkbox, Input, Select, TagInput } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { wizard } from './wizard.svelte';

	let { errors = {} }: { errors?: Record<string, string> } = $props();

	let cidr = $state('');
	let name = $state('');
	let access = $state<'direct' | 'routed'>('routed');
	let gateway = $state('');
	let addError = $state('');
	let local = $state<Record<string, string>>({});
	const err = $derived({ ...errors, ...local });

	const accessOptions = [
		{ value: 'direct', label: t('direkt angeschlossen') },
		{ value: 'routed', label: t('über Router erreichbar') }
	];

	const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;
	function validCIDR(v: string): boolean {
		const [ip, bits] = v.split('/');
		const m = IPV4.exec(ip ?? '');
		if (m)
			return (
				m.slice(1).every((x) => Number(x) <= 255) &&
				/^\d+$/.test(bits ?? '') &&
				Number(bits) >= 16 &&
				Number(bits) <= 32
			);
		return (ip ?? '').includes(':') && /^\d+$/.test(bits ?? '') && Number(bits) <= 128;
	}
	function validAddress(v: string): boolean {
		if (v.includes('/')) return validCIDR(v) || /^[0-9a-f:]+\/\d+$/i.test(v);
		const m = IPV4.exec(v);
		return m ? m.slice(1).every((x) => Number(x) <= 255) : /^[0-9a-f:]+$/i.test(v) && v.includes(':');
	}

	function add() {
		const c = cidr.trim();
		if (!validCIDR(c)) {
			addError = t('CIDR erwartet, z. B. 10.20.0.0/24 (höchstens /16)');
			return;
		}
		if (wizard.data.subnets.some((s) => s.cidr === c)) {
			addError = t('Dieses Subnetz ist schon in der Liste');
			return;
		}
		wizard.data.subnets.push({
			cidr: c,
			name: name.trim(),
			interface: '',
			gateway: gateway.trim(),
			access,
			selected: true,
			detected: false
		});
		cidr = name = gateway = addError = '';
		wizard.save();
	}

	function remove(i: number) {
		wizard.data.subnets.splice(i, 1);
		wizard.save();
	}

	/** Checks the step; false keeps the wizard here. */
	export function validate(): boolean {
		const e: Record<string, string> = {};
		wizard.data.exclusions.forEach((x, i) => {
			if (!validAddress(x.trim()))
				e[`scanExclusions.${i}`] = t('„{value}“ ist weder IP-Adresse noch CIDR', { value: x });
		});
		const dns = wizard.data.dnsServer.trim();
		if (dns && /[\s/]/.test(dns)) e.dnsServer = t('Hostname oder IP, optional mit Port');
		local = e;
		return !Object.keys(e).length;
	}

	const exclusionError = $derived(
		Object.entries(err)
			.filter(([k]) => k.startsWith('scanExclusions'))
			.map(([, v]) => v)[0] ?? null
	);
	const subnetError = $derived(
		Object.entries(err)
			.filter(([k]) => k.startsWith('subnets'))
			.map(
				([k, v]) =>
					`${wizard.data.subnets.filter((s) => s.selected)[Number(k.split('.')[1])]?.cidr ?? ''}: ${v}`
			)[0] ?? null
	);
</script>

<div class="flex flex-col gap-6">
	<section class="flex flex-col gap-2">
		<h3 class="text-sm font-medium text-fg">{t('Subnetze')}</h3>
		<p class="text-sm text-fg-muted">
			{t(
				'NetScope scannt nur die ausgewählten Netze. Erkannt wurden die Netze, an die dieser Rechner direkt angeschlossen ist.'
			)}
		</p>
		{#if subnetError}<p class="text-sm text-danger">{subnetError}</p>{/if}
		{#if wizard.data.subnets.length}
			<ul class="divide-y divide-border rounded-lg border border-border">
				{#each wizard.data.subnets as sn, i (sn.cidr)}
					<li class="flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2">
						<Checkbox
							bind:checked={sn.selected}
							label={t('{cidr} scannen', { cidr: sn.cidr })}
							hideLabel
							onchange={() => wizard.save()}
						/>
						<span class="mono w-40 text-sm text-fg">{sn.cidr}</span>
						<Input
							size="sm"
							bind:value={sn.name}
							aria-label={t('Name von {cidr}', { cidr: sn.cidr })}
							placeholder="Name"
							class="min-w-32 flex-1"
							onchange={() => wizard.save()}
						/>
						<span class="flex flex-wrap items-center gap-1 text-xs text-fg-muted">
							{#if sn.detected}<Badge tone="info">{t('erkannt')}</Badge>{/if}
							<Badge>{sn.access === 'routed' ? t('über Router') : t('direkt')}</Badge>
							{#if sn.interface}<span class="mono">{sn.interface}</span>{/if}
							{#if sn.gateway}<span>{t('Gateway {ip}', { ip: sn.gateway })}</span>{/if}
						</span>
						{#if !sn.detected}
							<Button
								size="xs"
								variant="ghost"
								icon="trash"
								label={t('Entfernen')}
								onclick={() => remove(i)}
							/>
						{/if}
					</li>
				{/each}
			</ul>
		{:else}
			<p class="rounded-lg border border-dashed border-border px-3 py-4 text-sm text-fg-muted">
				{t('Kein angeschlossenes Netz erkannt – bitte unten ein Subnetz hinzufügen.')}
			</p>
		{/if}

		<div class="mt-2 grid grid-cols-1 items-end gap-2 sm:grid-cols-[1fr_1fr_auto_1fr_auto]">
			<Input
				id="welcome-add-cidr"
				label={t('Weiteres Subnetz')}
				bind:value={cidr}
				placeholder="10.20.0.0/24"
				mono
				error={addError}
			/>
			<Input id="welcome-add-name" label="Name" bind:value={name} placeholder={t('z. B. Büro')} />
			<Select id="welcome-add-access" label={t('Zugriff')} bind:value={access} options={accessOptions} />
			<Input
				id="welcome-add-gateway"
				label={t('Gateway (optional)')}
				bind:value={gateway}
				placeholder="10.20.0.1"
				mono
			/>
			<Button icon="plus" onclick={add}>{t('Hinzufügen')}</Button>
		</div>
		<p class="text-xs text-fg-subtle">
			{t(
				'Über Router erreichbare Netze scannt NetScope ohne ARP (keine MAC-Adressen). WireGuard-Tunnel richtest du nach der Einrichtung unter System → Subnetze ein.'
			)}
		</p>
	</section>

	<TagInput
		id="welcome-exclusions"
		label={t('Vom Scannen ausgenommen')}
		bind:value={wizard.data.exclusions}
		placeholder="192.168.1.20"
		hint={t(
			'Einzelne Adressen (oder Netze), die kein Scanner abfragt – z. B. empfindliche Geräte wie ältere Drucker oder Steuerungen.'
		)}
		error={exclusionError}
		onchange={() => wizard.save()}
	/>

	<Input
		id="welcome-dns"
		label={t('DNS-Server für Reverse-DNS')}
		bind:value={wizard.data.dnsServer}
		placeholder={t('System-DNS')}
		hint={wizard.options?.dnsServers?.length
			? t('Leer = DNS-Server des Systems ({servers}). Meist kennt der Router die Namen aus DHCP am besten.', {
					servers: wizard.options.dnsServers.join(', ')
				})
			: t('Leer = DNS-Server des Systems. Meist kennt der Router die Namen aus DHCP am besten.')}
		error={err.dnsServer}
		mono
	/>
</div>
