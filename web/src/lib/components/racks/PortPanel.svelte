<!--
	A selected port: what is plugged in (cable with its way through patch panels, a device
	set by hand), what other sources detect on it, and actions – set label and device, adopt a
	detected device, draw a cable to another port, pull a cable.
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { RackItem, RackPort, RackTarget } from '$lib/api';
	import DevicePicker from '$lib/components/device/DevicePicker.svelte';
	import { Alert, Badge, Button, Icon, Input, StatusDot } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { cableCss, itemName, portLabel, sourceLabel, speedText } from './rack';

	interface Props {
		item: RackItem;
		port: RackPort;
		rackId: number;
		canEdit?: boolean;
		connecting?: boolean;
		onchanged?: () => void;
		onconnect?: () => void;
		oncablemodal?: () => void;
		onback?: () => void;
	}

	let {
		item,
		port,
		rackId,
		canEdit = false,
		connecting = false,
		onchanged,
		onconnect,
		oncablemodal,
		onback
	}: Props = $props();

	let label = $state('');
	let deviceId = $state<number | null>(null);
	let errors = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);
	let busy = $state(false);

	let lastKey = '';
	$effect(() => {
		const k = `${item.id}/${port.name}/${port.label ?? ''}`;
		if (k !== lastKey) {
			lastKey = k;
			label = port.label ?? '';
			deviceId = null;
			errors = {};
			formError = null;
		}
	});

	const isDevice = $derived(item.kind === 'device');
	// a device port with a cable, or a panel port with both sides cabled, takes no device
	const canPlug = $derived(isDevice ? port.cables.length === 0 : port.cables.length < 2);

	async function setPort(device: number | undefined | null, text = label) {
		busy = true;
		errors = {};
		formError = null;
		try {
			await api.put('/api/v1/rack-items/{id}/port', {
				path: { id: item.id },
				body: { port: port.name, label: text.trim(), deviceId: device ?? undefined }
			});
			deviceId = null;
			onchanged?.();
			return true;
		} catch (e) {
			errors = fieldErrors(e);
			formError = Object.keys(errors).length ? null : errorMessage(e);
			return false;
		} finally {
			busy = false;
		}
	}

	async function pull(id: number) {
		const ok = await confirm({
			title: t('Kabel entfernen?'),
			message: t('Die Verbindung, die dieses Kabel beschreibt, wird aus der Topologie genommen.'),
			confirmLabel: t('Entfernen'),
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/rack-cables/{id}', { path: { id } });
			toast.success(t('Kabel entfernt'));
			onchanged?.();
		} catch (e) {
			toast.error(e);
		}
	}

	function where(tg: RackTarget): string {
		const rack = tg.rackId !== rackId ? ` (${tg.rackName})` : '';
		return `${tg.itemName} · ${portLabel(tg.port)}${tg.label ? ` (${tg.label})` : ''}${rack}`;
	}
</script>

<section class="flex flex-col gap-4" aria-label={portLabel(port.name)}>
	<header class="flex items-start gap-2">
		<Button
			variant="ghost"
			size="sm"
			icon="arrow-left"
			label={t('Zurück zu {name}', { name: itemName(item) })}
			onclick={onback}
		/>
		<div class="min-w-0 flex-1">
			<h2 class="truncate text-base font-semibold text-fg">
				{portLabel(port.name)}
			</h2>
			<p class="truncate text-xs text-fg-muted">{itemName(item)}</p>
		</div>
		<div class="flex flex-wrap justify-end gap-1">
			{#if port.status === 'up'}<Badge tone="ok" dot size="sm">{t('Link aktiv')}</Badge>{/if}
			{#if port.status === 'down'}<Badge size="sm">{t('kein Link')}</Badge>{/if}
			{#if port.status === 'disabled'}<Badge size="sm">{t('deaktiviert')}</Badge>{/if}
			{#if port.speedMbps}<Badge size="sm">{speedText(port.speedMbps)}</Badge>{/if}
			{#if port.media === 'sfp'}<Badge size="sm">SFP</Badge>{/if}
		</div>
	</header>

	{#if formError}<Alert tone="danger">{formError}</Alert>{/if}

	<!-- plugged in -->
	<div class="flex flex-col gap-2">
		<h3 class="text-xs font-semibold tracking-wide text-fg-muted uppercase">{t('Angeschlossen')}</h3>
		{#each port.cables as c (c.id)}
			<div class="flex items-start gap-2 rounded-md border border-border p-2.5 text-sm">
				<span
					class="mt-1 inline-block h-1.5 w-5 shrink-0 rounded-full"
					style="background:{cableCss(c.color)}"
					aria-hidden="true"
				></span>
				<div class="min-w-0 flex-1">
					<p class="text-fg">{t('Kabel zu {target}', { target: where(c.peer) })}</p>
					{#if c.label}<p class="text-xs text-fg-muted">{c.label}</p>{/if}
					{#if c.end}
						<p class="mt-1 flex items-center gap-1 text-xs text-fg-muted">
							<Icon name="arrow-right" size={12} />
							{t('führt zu {target}', { target: where(c.end) })}
						</p>
					{/if}
					{#if (c.end ?? c.peer).device}
						{@const d = (c.end ?? c.peer).device!}
						<a
							href="/devices/{d.id}"
							class="mt-1 inline-flex items-center gap-1.5 text-accent hover:underline"
						>
							<StatusDot status={d.online ? 'online' : 'offline'} pulse={false} />{d.name}
						</a>
					{/if}
				</div>
				{#if canEdit}
					<Button
						variant="ghost"
						size="sm"
						icon="unlink"
						label={t('Kabel entfernen')}
						onclick={() => pull(c.id)}
					/>
				{/if}
			</div>
		{/each}
		{#if port.device}
			<div class="flex items-center gap-2 rounded-md border border-accent/50 bg-accent-soft p-2.5 text-sm">
				<StatusDot status={port.device.online ? 'online' : 'offline'} pulse={false} />
				<a href="/devices/{port.device.id}" class="min-w-0 flex-1 truncate text-accent hover:underline"
					>{port.device.name}</a
				>
				<span class="text-xs text-fg-muted">{t('von Hand')}</span>
				{#if canEdit}
					<Button
						variant="ghost"
						size="sm"
						icon="unlink"
						label={t('Zuordnung lösen')}
						loading={busy}
						onclick={() => setPort(null)}
					/>
				{/if}
			</div>
		{/if}
		{#if !port.cables.length && !port.device}
			<p class="text-sm text-fg-subtle">{t('Nichts eingetragen.')}</p>
		{/if}
	</div>

	<!-- detected -->
	{#if port.detected.length}
		<div class="flex flex-col gap-2">
			<h3 class="text-xs font-semibold tracking-wide text-fg-muted uppercase">{t('Erkannt')}</h3>
			<ul class="flex flex-col gap-1.5">
				{#each port.detected as d (d.device.id)}
					<li class="flex items-start gap-2 rounded-md border border-dashed border-ok/60 p-2 text-sm">
						<StatusDot status={d.device.online ? 'online' : 'offline'} pulse={false} class="mt-1.5" />
						<div class="min-w-0 flex-1">
							<a href="/devices/{d.device.id}" class="text-accent hover:underline">{d.device.name}</a>
							<p class="text-xs text-fg-subtle">
								{sourceLabel(d.source)}{d.remotePort
									? ` · ${t('dort {port}', { port: portLabel(d.remotePort) })}`
									: ''}
							</p>
							{#if d.elsewhere}
								<p class="mt-0.5 flex items-center gap-1 text-xs text-warn">
									<Icon name="alert" size={12} />{t('Im Rack eingetragen an {where}', { where: d.elsewhere })}
								</p>
							{/if}
						</div>
						{#if canEdit && canPlug && !port.device}
							<Button size="xs" icon="check" loading={busy} onclick={() => setPort(d.device.id)}
								>{t('Übernehmen')}</Button
							>
						{/if}
					</li>
				{/each}
			</ul>
			{#if port.detectedMore}
				<p class="text-xs text-fg-subtle">
					{t('… und {n} weitere (vermutlich ein Uplink)', { n: port.detectedMore })}
				</p>
			{/if}
		</div>
	{/if}

	{#if canEdit}
		<div class="flex flex-col gap-3 border-t border-border pt-3">
			<form
				class="flex items-end gap-2"
				onsubmit={(e) => {
					e.preventDefault();
					setPort(port.device?.id, label).then((ok) => ok && toast.success(t('Gespeichert')));
				}}
			>
				<Input
					label={t('Beschriftung')}
					bind:value={label}
					maxlength={100}
					placeholder={isDevice ? t('z. B. Uplink') : t('z. B. Dose Büro 1.04')}
					error={errors.label}
					class="flex-1"
				/>
				<Button
					type="submit"
					icon="save"
					label={t('Beschriftung speichern')}
					loading={busy}
					disabled={label === (port.label ?? '')}
				/>
			</form>
			{#if canPlug}
				<form
					class="flex flex-col gap-2"
					onsubmit={(e) => {
						e.preventDefault();
						if (deviceId) setPort(deviceId).then((ok) => ok && toast.success(t('Gerät zugeordnet')));
					}}
				>
					{#key `${port.name}/${port.device?.id ?? 0}`}
						<DevicePicker
							label={port.device ? t('Anderes Gerät anschließen') : t('Gerät anschließen')}
							bind:value={deviceId}
							exclude={item.deviceId ? [item.deviceId] : []}
							error={errors.deviceId}
						/>
					{/key}
					<div>
						<Button type="submit" size="sm" icon="link" disabled={!deviceId} loading={busy}
							>{t('Anschließen')}</Button
						>
					</div>
				</form>
			{/if}
			<div class="flex flex-wrap gap-2">
				<Button size="sm" icon="link" active={connecting} onclick={onconnect}>
					{connecting ? t('Ziel-Port anklicken …') : t('Kabel ziehen')}
				</Button>
				<Button size="sm" variant="ghost" onclick={oncablemodal}>{t('Ziel auswählen …')}</Button>
			</div>
		</div>
	{/if}
</section>
