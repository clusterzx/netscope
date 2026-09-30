<!--
	Details of a selected element: device, place, ports as a list (the accessible way to pick
	a port), and actions – edit, move by one unit, adopt detected connections, remove.
-->
<script lang="ts">
	import type { RackItem, RackPort, RackView } from '$lib/api';
	import { Badge, Button, DescItem, DescList, Icon, StatusDot } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { deviceTypeName } from '$lib/utils/labels';
	import { cableCss, fits, itemIcon, itemName, kindLabel, portDevice, portState, unitsText } from './rack';

	interface Props {
		rack: RackView;
		item: RackItem;
		canEdit?: boolean;
		selectedPort?: string | null;
		busy?: boolean;
		onport?: (port: RackPort) => void;
		onedit?: () => void;
		onremove?: () => void;
		onadopt?: () => void;
		onmove?: (position: number) => void;
		onclose?: () => void;
	}

	let {
		rack,
		item,
		canEdit = false,
		selectedPort = null,
		busy = false,
		onport,
		onedit,
		onremove,
		onadopt,
		onmove,
		onclose
	}: Props = $props();

	const adoptable = $derived(
		item.ports.filter((p) => !p.device && !p.cables.length && p.detected.length === 1 && !p.detectedMore)
			.length
	);
	const used = $derived(
		item.ports.filter((p) => portState(p) !== 'free' && portState(p) !== 'disabled').length
	);
	const place = $derived({
		height: item.height,
		face: item.face,
		fullDepth: item.fullDepth,
		col: item.col,
		cols: item.cols
	});
	const canUp = $derived(fits(rack, { ...place, position: item.position + 1 }, item.id));
	const canDown = $derived(fits(rack, { ...place, position: item.position - 1 }, item.id));

	function widthText(cols: number): string {
		return cols === 6 ? t('volle Breite') : cols === 3 ? t('halbe Breite') : t('drittel Breite');
	}
</script>

<section class="flex flex-col gap-4" aria-label={itemName(item)}>
	<header class="flex items-start gap-2">
		<Icon name={itemIcon(item)} size={18} class="mt-0.5 text-fg-muted" />
		<div class="min-w-0 flex-1">
			<h2 class="truncate text-base font-semibold text-fg">{itemName(item)}</h2>
			<p class="text-xs text-fg-muted">{kindLabel(item.kind)}</p>
		</div>
		<Button variant="ghost" size="sm" icon="x" label={t('Auswahl aufheben')} onclick={onclose} />
	</header>

	<DescList>
		{#if item.kind === 'device'}
			<DescItem label={t('Gerät')}>
				{#if item.device}
					<a
						href="/devices/{item.device.id}"
						class="inline-flex items-center gap-1.5 text-accent hover:underline"
					>
						<StatusDot status={item.device.online ? 'online' : 'offline'} pulse={false} />
						{item.device.name}
					</a>
					<span class="text-xs text-fg-subtle">
						{[deviceTypeName(item.device.type), item.device.ip, item.device.model]
							.filter((x) => x && x !== '–')
							.join(' · ')}
					</span>
				{:else}
					<span class="text-fg-muted italic">
						{t('Aus dem Inventar gelöscht (zuletzt: {name})', { name: item.deviceName || '–' })}
					</span>
				{/if}
			</DescItem>
		{/if}
		<DescItem label={t('Platz')}>
			{t('HE {units}', { units: unitsText(rack, item) })} · {item.face === 'front'
				? t('Vorderseite')
				: t('Rückseite')}{item.fullDepth ? ` · ${t('volle Tiefe')}` : ''} · {widthText(item.cols)}
		</DescItem>
		<DescItem label={t('Ports')}>
			{#if item.ports.length}
				{t('{used} von {n} belegt', { used, n: item.ports.length })}
				{#if item.portCount === 0 && item.ports.some((p) => p.source === 'snmp')}
					<Badge size="sm" tone="info">SNMP</Badge>
				{/if}
			{:else}
				<span class="text-fg-muted">{t('keine')}</span>
			{/if}
		</DescItem>
	</DescList>

	{#if canEdit}
		<div class="flex flex-wrap gap-2">
			<Button size="sm" icon="edit" onclick={onedit}>{t('Bearbeiten')}</Button>
			<Button
				size="sm"
				icon="arrow-up"
				label={t('Eine HE höher')}
				disabled={!canUp || busy}
				onclick={() => onmove?.(item.position + 1)}
			/>
			<Button
				size="sm"
				icon="arrow-down"
				label={t('Eine HE tiefer')}
				disabled={!canDown || busy}
				onclick={() => onmove?.(item.position - 1)}
			/>
			{#if adoptable > 0}
				<Button size="sm" icon="check" loading={busy} onclick={onadopt}>
					{t('{n} erkannte Verbindungen übernehmen', { n: adoptable })}
				</Button>
			{/if}
			<Button size="sm" variant="danger" icon="trash" onclick={onremove}>{t('Ausbauen')}</Button>
		</div>
	{/if}

	{#if item.ports.length}
		<div>
			<h3 class="mb-1.5 text-xs font-semibold tracking-wide text-fg-muted uppercase">{t('Ports')}</h3>
			<ul class="max-h-[28rem] divide-y divide-border overflow-auto rounded-md border border-border">
				{#each item.ports as p (p.name)}
					{@const d = portDevice(p)}
					{@const st = portState(p)}
					<li>
						<button
							type="button"
							class="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-sm hover:bg-surface-2 {selectedPort ===
							p.name
								? 'bg-accent-soft'
								: ''}"
							aria-pressed={selectedPort === p.name}
							onclick={() => onport?.(p)}
						>
							<span
								class="inline-block size-2.5 shrink-0 rounded-[2px] border {st === 'device'
									? 'border-accent bg-accent'
									: st === 'detected'
										? 'border-dashed border-ok bg-ok-soft'
										: st === 'up'
											? 'border-ok'
											: 'border-border-strong bg-surface-2'}"
								style={st === 'cable'
									? `background:${cableCss(p.cables[0].color)};border-color:${cableCss(p.cables[0].color)}`
									: ''}
								aria-hidden="true"
							></span>
							<span class="mono w-20 shrink-0 truncate text-xs">{p.name}</span>
							<span class="min-w-0 flex-1 truncate {d ? 'text-fg' : 'text-fg-subtle'}">
								{#if d}
									{d.name}{#if st === 'detected'}<span class="text-xs text-fg-subtle">
											· {t('erkannt')}</span
										>{/if}
								{:else if p.detected.length > 1}
									{t('{n} Geräte erkannt', { n: p.detected.length + (p.detectedMore ?? 0) })}
								{:else if p.cables.length}
									→ {p.cables[0].peer.itemName} {p.cables[0].peer.port}
								{:else}
									{p.label || '–'}
								{/if}
							</span>
						</button>
					</li>
				{/each}
			</ul>
		</div>
	{/if}
</section>
