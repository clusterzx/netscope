<!--
  Side panel of the topology page: details of the selected node (with its connections) or of
  the selected edge. Manual edges can be deleted here.
-->
<script lang="ts">
	import type { GraphEdge, GraphNode } from '$lib/api';
	import { Badge, Button, Icon, StatusDot } from '$lib/components/ui';
	import { deviceTypeName, label, relationKindLabel, stateLabel, stateTone } from '$lib/utils/labels';
	import { edgeLabel, edgeStyle, typeIcon } from './graph';

	interface Props {
		node: GraphNode | null;
		edge: GraphEdge | null;
		/** all edges of the graph */
		edges: GraphEdge[];
		nodesById: Map<string, GraphNode>;
		pinned: boolean;
		canWrite: boolean;
		class?: string;
		onclose: () => void;
		onselectnode: (id: string) => void;
		onselectedge: (id: string) => void;
		oncenter: (id: string) => void;
		onunpin: (id: string) => void;
		onconnect: (id: string) => void;
		ondeleteedge: (edge: GraphEdge) => void;
	}

	let {
		node,
		edge,
		edges,
		nodesById,
		pinned,
		canWrite,
		class: klass = '',
		onclose,
		onselectnode,
		onselectedge,
		oncenter,
		onunpin,
		onconnect,
		ondeleteedge
	}: Props = $props();

	const name = (id: string) => nodesById.get(id)?.label ?? id;

	const nodeEdges = $derived.by(() => {
		if (!node) return [];
		const id = node.id;
		return edges
			.filter((e) => e.source === id || e.target === id)
			.map((e) => ({ edge: e, other: e.source === id ? e.target : e.source, up: e.target === id }))
			.sort(
				(a, b) =>
					Number(b.edge.origin === 'manual') - Number(a.edge.origin === 'manual') ||
					a.edge.kind.localeCompare(b.edge.kind) ||
					name(a.other).localeCompare(name(b.other), 'de')
			);
	});

	const host = $derived.by(() => {
		if (!node || node.kind !== 'container') return null;
		const e = edges.find((x) => x.target === node.id && x.kind === 'container');
		return e ? (nodesById.get(e.source) ?? null) : null;
	});

	/** ports of an edge from the point of view of the selected node: own port ↔ port of the other end */
	function portText(e: GraphEdge, up: boolean): string {
		const own = up ? e.childPort : e.parentPort;
		const other = up ? e.parentPort : e.childPort;
		if (!own && !other) return '';
		return `${own || '–'} ↔ ${other || '–'}`;
	}

	const deletable = (e: GraphEdge) => e.origin === 'manual' && !!e.relationId;
	const icon = $derived(node ? typeIcon(node.type) : null);
</script>

{#snippet swatch(kind: string)}
	{@const s = edgeStyle(kind)}
	<svg width="22" height="8" viewBox="0 0 22 8" aria-hidden="true" class="shrink-0">
		<line
			x1="1"
			y1="4"
			x2="21"
			y2="4"
			stroke="var({s.color})"
			stroke-width={Math.max(1.4, s.width)}
			stroke-dasharray={s.dash.join(' ')}
			stroke-linecap="round"
		/>
	</svg>
{/snippet}

<section
	class="flex flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-lg {klass}"
	aria-label={node ? `Details zu ${node.label}` : 'Details zur Verbindung'}
>
	{#if node}
		<header class="flex items-start gap-2.5 border-b border-border px-4 py-3">
			<span
				class="mt-0.5 flex size-8 shrink-0 items-center justify-center {node.kind === 'container'
					? 'rounded-md'
					: 'rounded-full'} {node.online ? 'bg-online' : 'bg-offline'} text-surface"
			>
				{#if icon}<Icon name={icon} size={16} />{/if}
			</span>
			<div class="min-w-0 flex-1">
				<h2 class="truncate text-sm font-semibold text-fg" title={node.label}>{node.label}</h2>
				<div class="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-muted">
					<StatusDot
						status={node.online ? 'online' : 'offline'}
						label={node.kind === 'container'
							? node.online
								? 'läuft'
								: 'gestoppt'
							: node.online
								? 'Online'
								: 'Offline'}
					/>
					{#if node.state && node.kind !== 'container'}
						<Badge tone={stateTone(node.state)}>{label(stateLabel, node.state)}</Badge>
					{/if}
					{#if pinned}<Badge tone="neutral">Fixiert</Badge>{/if}
				</div>
			</div>
			<Button variant="ghost" size="sm" icon="x" label="Auswahl schließen" onclick={onclose} />
		</header>

		<div class="min-h-0 flex-1 overflow-y-auto px-4 py-3">
			<dl class="grid grid-cols-[6.5rem_1fr] gap-x-3 gap-y-1.5 text-sm">
				{#if node.kind === 'container'}
					<dt class="text-fg-subtle">Image</dt>
					<dd class="mono min-w-0 break-all text-fg">{node.image || '–'}</dd>
					<dt class="text-fg-subtle">Host</dt>
					<dd class="min-w-0 text-fg">
						{#if host}
							<button type="button" class="link truncate text-left" onclick={() => onselectnode(host.id)}>
								{host.label}
							</button>
						{:else}–{/if}
					</dd>
				{:else}
					<dt class="text-fg-subtle">IP</dt>
					<dd class="mono text-fg">{node.ip || '–'}</dd>
					<dt class="text-fg-subtle">Typ</dt>
					<dd class="text-fg">{deviceTypeName(node.type)}</dd>
					<dt class="text-fg-subtle">Hersteller</dt>
					<dd class="min-w-0 break-words text-fg">{node.vendor || '–'}</dd>
					<dt class="text-fg-subtle">Subnetz</dt>
					<dd class="mono text-fg">{node.subnet || '–'}</dd>
					{#if node.tags?.length}
						<dt class="text-fg-subtle">Tags</dt>
						<dd class="flex flex-wrap gap-1">
							{#each node.tags as t (t)}<Badge tone="accent">{t}</Badge>{/each}
						</dd>
					{/if}
				{/if}
			</dl>

			<div class="mt-3 flex flex-wrap gap-2">
				{#if node.deviceId}
					<Button variant="primary" size="sm" icon="external" href="/devices/{node.deviceId}"
						>Gerät öffnen</Button
					>
				{:else if host?.deviceId}
					<Button variant="primary" size="sm" icon="external" href="/devices/{host.deviceId}"
						>Host öffnen</Button
					>
				{/if}
				<Button size="sm" icon="crosshair" onclick={() => oncenter(node.id)}>Zentrieren</Button>
				{#if pinned}
					<Button size="sm" icon="pin" onclick={() => onunpin(node.id)}>Fixierung lösen</Button>
				{/if}
				{#if canWrite && node.deviceId}
					<Button size="sm" icon="link" onclick={() => onconnect(node.id)}>Verbindung anlegen</Button>
				{/if}
			</div>

			<h3 class="mt-4 mb-1.5 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
				Verbindungen ({nodeEdges.length})
			</h3>
			{#if nodeEdges.length === 0}
				<p class="text-sm text-fg-muted">Keine Verbindungen im aktuellen Ausschnitt.</p>
			{:else}
				<ul class="-mx-2 flex flex-col">
					{#each nodeEdges as { edge: e, other, up } (e.id)}
						<li class="group flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-surface-2">
							{@render swatch(e.kind)}
							<div class="min-w-0 flex-1">
								<div class="flex items-center gap-1 text-sm">
									<Icon
										name={up ? 'arrow-up' : 'arrow-down'}
										size={12}
										class="text-fg-subtle"
										label={up ? 'übergeordnet' : 'untergeordnet'}
									/>
									<button
										type="button"
										class="min-w-0 truncate text-left text-fg hover:text-accent hover:underline"
										onclick={() => onselectnode(other)}
									>
										{name(other)}
									</button>
								</div>
								<div class="truncate text-xs text-fg-subtle">
									<button
										type="button"
										class="hover:text-fg hover:underline"
										onclick={() => onselectedge(e.id)}
									>
										{label(relationKindLabel, e.kind)}
									</button>{#if portText(e, up)}{' · '}<span class="mono">{portText(e, up)}</span
										>{/if}{#if edgeLabel(e)}{' · '}{edgeLabel(
											e
										)}{/if}{#if e.origin === 'manual' && e.kind !== 'manual'}{' · manuell'}{/if}
								</div>
							</div>
							{#if canWrite && deletable(e)}
								<Button
									variant="ghost"
									size="xs"
									icon="trash"
									label="Verbindung zu {name(other)} löschen"
									onclick={() => ondeleteedge(e)}
								/>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{:else if edge}
		<header class="flex items-start gap-2.5 border-b border-border px-4 py-3">
			<span class="mt-1.5">{@render swatch(edge.kind)}</span>
			<div class="min-w-0 flex-1">
				<h2 class="truncate text-sm font-semibold text-fg">{label(relationKindLabel, edge.kind)}</h2>
				<div class="mt-0.5 flex flex-wrap gap-1.5 text-xs">
					<Badge tone={edge.origin === 'manual' ? 'ok' : 'neutral'}>
						{edge.origin === 'manual' ? 'Manuell angelegt' : 'Automatisch erkannt'}
					</Badge>
					{#if edge.protected}<Badge tone="accent">Geschützt</Badge>{/if}
				</div>
			</div>
			<Button variant="ghost" size="sm" icon="x" label="Auswahl schließen" onclick={onclose} />
		</header>
		<div class="min-h-0 flex-1 overflow-y-auto px-4 py-3">
			<dl class="grid grid-cols-[6.5rem_1fr] gap-x-3 gap-y-1.5 text-sm">
				<dt class="text-fg-subtle">Übergeordnet</dt>
				<dd class="min-w-0">
					<button type="button" class="link truncate text-left" onclick={() => onselectnode(edge.source)}>
						{name(edge.source)}
					</button>
					{#if edge.parentPort}
						<span class="block text-xs text-fg-muted">Port <span class="mono">{edge.parentPort}</span></span>
					{/if}
				</dd>
				<dt class="text-fg-subtle">Untergeordnet</dt>
				<dd class="min-w-0">
					<button type="button" class="link truncate text-left" onclick={() => onselectnode(edge.target)}>
						{name(edge.target)}
					</button>
					{#if edge.childPort}
						<span class="block text-xs text-fg-muted">Port <span class="mono">{edge.childPort}</span></span>
					{/if}
				</dd>
				<dt class="text-fg-subtle">Beschriftung</dt>
				<dd class="min-w-0 break-words text-fg">{edgeLabel(edge) || '–'}</dd>
				<dt class="text-fg-subtle">Quelle</dt>
				<dd class="text-fg">{edge.origin === 'manual' ? 'Manuell angelegt' : edge.origin}</dd>
			</dl>
			{#if deletable(edge)}
				{#if canWrite}
					<div class="mt-4">
						<Button variant="danger" size="sm" icon="trash" onclick={() => ondeleteedge(edge)}>
							Verbindung löschen
						</Button>
					</div>
				{/if}
			{:else}
				<p class="mt-4 text-xs text-fg-muted">
					Automatisch erkannte Verbindung – sie wird bei jedem Lauf von „{edge.origin}“ neu bestimmt und kann
					hier nicht gelöscht werden.
				</p>
			{/if}
		</div>
	{/if}
</section>
