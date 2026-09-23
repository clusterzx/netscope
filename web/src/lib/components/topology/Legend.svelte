<!--
  Legend of the topology graph: node colours (status), ring (unknown device), glyphs of the
  device types present, container shape and the edge styles of the kinds present.
-->
<script lang="ts">
	import { Icon } from '$lib/components/ui';
	import { deviceTypeName, relationKindLabel, label } from '$lib/utils/labels';
	import { EDGE_KINDS, edgeStyle, typeIcon } from './graph';

	interface Props {
		/** edge kinds present in the graph */
		kinds: string[];
		/** device types present in the graph */
		types: string[];
		containers?: boolean;
		open?: boolean;
		class?: string;
	}

	let { kinds, types, containers = false, open = $bindable(true), class: klass = '' }: Props = $props();

	const edgeKinds = $derived(
		[...kinds].sort(
			(a, b) => (EDGE_KINDS.indexOf(a) + 1 || 99) - (EDGE_KINDS.indexOf(b) + 1 || 99) || a.localeCompare(b)
		)
	);
	const glyphs = $derived(
		types
			.map((t) => ({ type: t, icon: typeIcon(t) }))
			.filter((g) => g.icon !== null)
			.sort((a, b) => deviceTypeName(a.type).localeCompare(deviceTypeName(b.type), 'de'))
	);
</script>

<div class="rounded-lg border border-border bg-surface/95 text-xs shadow-md backdrop-blur-sm {klass}">
	<button
		type="button"
		class="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left font-medium text-fg hover:bg-surface-2"
		aria-expanded={open}
		onclick={() => (open = !open)}
	>
		<Icon name="info" size={14} class="text-fg-subtle" />
		<span class="flex-1">Legende</span>
		<Icon name={open ? 'chevron-down' : 'chevron-up'} size={14} class="text-fg-subtle" />
	</button>
	{#if open}
		<div
			class="flex max-h-[min(24rem,50dvh)] flex-col gap-3 overflow-y-auto border-t border-border px-3 py-2.5"
		>
			<section>
				<h3 class="mb-1.5 font-semibold tracking-wide text-fg-subtle uppercase">Geräte</h3>
				<ul class="flex flex-col gap-1.5 text-fg-muted">
					<li class="flex items-center gap-2">
						<span class="inline-block size-3 rounded-full bg-online"></span> Online
					</li>
					<li class="flex items-center gap-2">
						<span class="inline-block size-3 rounded-full bg-offline"></span> Offline
					</li>
					<li class="flex items-center gap-2">
						<span
							class="inline-block size-3 rounded-full bg-online ring-2 ring-unknown ring-offset-1 ring-offset-surface"
						></span>
						Unbekanntes Gerät
					</li>
					{#if containers}
						<li class="flex items-center gap-2">
							<span class="inline-block size-3 rounded-[3px] bg-online"></span> Container
						</li>
					{/if}
					<li class="flex items-center gap-2">
						<span class="relative inline-block size-3 rounded-full bg-offline">
							<span
								class="absolute -top-0.5 -right-0.5 size-1.5 rounded-full border border-surface bg-fg-muted"
							></span>
						</span>
						Fixiert (Doppelklick löst)
					</li>
				</ul>
			</section>

			{#if glyphs.length}
				<section>
					<h3 class="mb-1.5 font-semibold tracking-wide text-fg-subtle uppercase">Typen</h3>
					<ul class="grid grid-cols-2 gap-x-3 gap-y-1 text-fg-muted">
						{#each glyphs as g (g.type)}
							<li class="flex min-w-0 items-center gap-1.5">
								<Icon name={g.icon!} size={13} class="shrink-0 text-fg-subtle" />
								<span class="truncate">{deviceTypeName(g.type)}</span>
							</li>
						{/each}
					</ul>
				</section>
			{/if}

			{#if edgeKinds.length}
				<section>
					<h3 class="mb-1.5 font-semibold tracking-wide text-fg-subtle uppercase">Verbindungen</h3>
					<ul class="flex flex-col gap-1 text-fg-muted">
						{#each edgeKinds as k (k)}
							{@const s = edgeStyle(k)}
							<li class="flex items-center gap-2">
								<svg width="28" height="8" viewBox="0 0 28 8" aria-hidden="true" class="shrink-0">
									<line
										x1="1"
										y1="4"
										x2="27"
										y2="4"
										stroke="var({s.color})"
										stroke-width={Math.max(1.2, s.width)}
										stroke-dasharray={s.dash.join(' ')}
										stroke-linecap="round"
									/>
								</svg>
								{label(relationKindLabel, k)}
							</li>
						{/each}
					</ul>
				</section>
			{/if}
		</div>
	{/if}
</div>
