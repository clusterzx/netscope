<!--
  Result of GET /api/v1/diff: summary per kind (clickable filters), change/text filters and the
  differences grouped by device (added / removed / changed with old → new values).
-->
<script lang="ts">
	import type { DiffItem, DiffResult } from '$lib/api';
	import { Button, EmptyState, Icon, Input } from '$lib/components/ui';
	import { formatNumber, plural } from '$lib/utils/format';
	import { deviceTypeName, diffChangeLabel, diffKindLabel, label } from '$lib/utils/labels';

	interface Props {
		result: DiffResult;
		/** active kind filter */
		kinds: string[];
		/** active change filter ('' = all) */
		change: string;
		text: string;
		onkinds: (kinds: string[]) => void;
		onchange: (change: string) => void;
		ontext: (text: string) => void;
		/** clear kind, change and text filter at once */
		onreset: () => void;
	}

	let { result, kinds, change, text = $bindable(''), onkinds, onchange, ontext, onreset }: Props = $props();

	const KIND_ORDER = [
		'device',
		'ip',
		'mac',
		'hostname',
		'os',
		'vendor',
		'type',
		'port',
		'cert',
		'http',
		'container',
		'package'
	];
	const CHANGES = ['added', 'removed', 'changed'] as const;
	const GROUP_LIMIT = 40;
	const PAGE_GROUPS = 60;

	const items = $derived<DiffItem[]>(result.items ?? []);

	// summary "kind.change" → per kind
	const perKind = $derived.by(() => {
		const m = new Map<string, Record<string, number>>();
		for (const [k, n] of Object.entries(result.summary ?? {})) {
			const [kind, ch] = k.split('.');
			const e = m.get(kind) ?? { added: 0, removed: 0, changed: 0 };
			e[ch] = (e[ch] ?? 0) + n;
			m.set(kind, e);
		}
		return [...m.entries()]
			.sort((a, b) => (KIND_ORDER.indexOf(a[0]) + 1 || 99) - (KIND_ORDER.indexOf(b[0]) + 1 || 99))
			.map(([kind, c]) => ({ kind, added: c.added ?? 0, removed: c.removed ?? 0, changed: c.changed ?? 0 }));
	});
	const totals = $derived.by(() => {
		const t: Record<string, number> = { added: 0, removed: 0, changed: 0 };
		for (const it of items) t[it.change] = (t[it.change] ?? 0) + 1;
		return t;
	});

	const filtered = $derived.by(() => {
		const q = text.trim().toLowerCase();
		const ks = new Set(kinds);
		return items.filter(
			(it) =>
				(!ks.size || ks.has(it.kind)) &&
				(!change || it.change === change) &&
				(!q ||
					it.deviceName.toLowerCase().includes(q) ||
					it.key.toLowerCase().includes(q) ||
					(it.old ?? '').toLowerCase().includes(q) ||
					(it.new ?? '').toLowerCase().includes(q) ||
					label(diffKindLabel, it.kind).toLowerCase().includes(q))
		);
	});

	const groups = $derived.by(() => {
		// by device id (devices can share a name, so the API order may interleave them)
		const byId = new Map<
			number,
			{ id: number; name: string; items: DiffItem[]; c: Record<string, number> }
		>();
		for (const it of filtered) {
			let g = byId.get(it.deviceId);
			if (!g) {
				g = { id: it.deviceId, name: it.deviceName, items: [], c: { added: 0, removed: 0, changed: 0 } };
				byId.set(it.deviceId, g);
			}
			g.items.push(it);
			g.c[it.change]++;
		}
		return [...byId.values()];
	});

	let expanded = $state(new Set<number>());
	let groupLimit = $state(PAGE_GROUPS);
	$effect(() => {
		void result;
		expanded = new Set();
		groupLimit = PAGE_GROUPS;
	});

	function toggleKind(k: string) {
		onkinds(kinds.includes(k) ? kinds.filter((x) => x !== k) : [...kinds, k]);
	}

	const marker: Record<string, { sign: string; cls: string; row: string }> = {
		added: { sign: '+', cls: 'text-ok', row: 'bg-ok-soft' },
		removed: { sign: '−', cls: 'text-danger', row: 'bg-danger-soft' },
		changed: { sign: '~', cls: 'text-warn', row: 'bg-warn-soft' }
	};

	/** device items: the value is only "gesehen"/"vorhanden" – describe the change instead */
	function deviceText(it: DiffItem): string {
		if (it.change === 'added') return result.a.kind === 'run' ? 'im neueren Lauf gesehen' : 'neu im Inventar';
		if (it.change === 'removed')
			return result.a.kind === 'run' ? 'im neueren Lauf nicht mehr gesehen' : 'nicht mehr im Inventar';
		return 'geändert';
	}

	/** display value (device types are translated) */
	const val = (it: DiffItem, v: string | undefined) =>
		it.kind === 'type' && v ? deviceTypeName(v) : v || '–';

	const deleted = (name: string) => /\(gelöscht\)$/.test(name);
	const filterActive = $derived(kinds.length > 0 || !!change || !!text.trim());
</script>

{#if items.length === 0}
	<EmptyState
		icon="check-circle"
		title="Keine Änderungen"
		description={result.a.kind === 'run'
			? 'Beide Läufe haben dieselben Daten geliefert.'
			: 'Zwischen den beiden Zeitpunkten hat sich im Inventar nichts geändert.'}
	/>
{:else}
	<div class="flex flex-col gap-4">
		<!-- summary per kind -->
		<section aria-label="Zusammenfassung" class="flex flex-wrap gap-2">
			{#each perKind as k (k.kind)}
				{@const on = kinds.includes(k.kind)}
				<button
					type="button"
					aria-pressed={on}
					class="flex items-center gap-2 rounded-lg border px-3 py-1.5 text-left text-sm shadow-sm transition-colors
						{on ? 'border-accent bg-accent-soft' : 'border-border bg-surface hover:border-border-strong'}"
					onclick={() => toggleKind(k.kind)}
				>
					<span class="font-medium text-fg">{label(diffKindLabel, k.kind)}</span>
					<span class="flex items-center gap-1.5 text-xs tabular">
						{#if k.added}<span class="text-ok" title="neu">+{formatNumber(k.added)}</span>{/if}
						{#if k.removed}<span class="text-danger" title="entfernt">−{formatNumber(k.removed)}</span>{/if}
						{#if k.changed}<span class="text-warn" title="geändert">~{formatNumber(k.changed)}</span>{/if}
					</span>
				</button>
			{/each}
		</section>

		<!-- filters -->
		<div class="flex flex-wrap items-center gap-2">
			<div
				class="inline-flex rounded-md border border-border bg-surface p-0.5 shadow-sm"
				role="group"
				aria-label="Art der Änderung"
			>
				<button
					type="button"
					aria-pressed={!change}
					class="rounded px-2.5 py-1 text-sm {!change
						? 'bg-surface-3 font-medium text-fg'
						: 'text-fg-muted hover:text-fg'}"
					onclick={() => onchange('')}
				>
					Alle <span class="text-xs text-fg-subtle tabular">{formatNumber(items.length)}</span>
				</button>
				{#each CHANGES as c (c)}
					<button
						type="button"
						aria-pressed={change === c}
						disabled={!totals[c]}
						class="rounded px-2.5 py-1 text-sm disabled:opacity-40 {change === c
							? 'bg-surface-3 font-medium text-fg'
							: 'text-fg-muted hover:text-fg'}"
						onclick={() => onchange(change === c ? '' : c)}
					>
						<span class={marker[c].cls}>{marker[c].sign}</span>
						{diffChangeLabel[c]}
						<span class="text-xs text-fg-subtle tabular">{formatNumber(totals[c] ?? 0)}</span>
					</button>
				{/each}
			</div>
			<Input
				type="search"
				size="sm"
				icon="search"
				bind:value={text}
				oninput={() => ontext(text)}
				placeholder="Gerät, Port, Wert …"
				aria-label="Änderungen durchsuchen"
				class="w-full sm:w-64"
			/>
			<span class="text-xs text-fg-subtle" aria-live="polite">
				{plural(filtered.length, 'Änderung', 'Änderungen')} auf {plural(groups.length, 'Gerät', 'Geräten')}
			</span>
			{#if filterActive}
				<Button
					size="sm"
					variant="ghost"
					icon="x"
					onclick={() => {
						text = '';
						onreset();
					}}>Filter zurücksetzen</Button
				>
			{/if}
		</div>

		{#if groups.length === 0}
			<EmptyState
				compact
				icon="filter"
				title="Keine Treffer"
				description="Keine Änderung passt zu den Filtern."
			/>
		{:else}
			<div class="flex flex-col gap-3">
				{#each groups.slice(0, groupLimit) as g (g.id)}
					{@const open = expanded.has(g.id)}
					{@const shown = open ? g.items : g.items.slice(0, GROUP_LIMIT)}
					<section
						class="overflow-hidden rounded-lg border border-border bg-surface shadow-sm"
						aria-label="Änderungen an {g.name}"
					>
						<header
							class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border bg-surface-2 px-4 py-2"
						>
							<Icon name="devices" size={15} class="text-fg-subtle" />
							{#if deleted(g.name)}
								<span class="font-medium text-fg-muted">{g.name}</span>
							{:else}
								<a class="font-medium text-fg hover:text-accent hover:underline" href="/devices/{g.id}"
									>{g.name}</a
								>
							{/if}
							<span class="flex items-center gap-2 text-xs tabular">
								{#if g.c.added}<span class="text-ok">+{formatNumber(g.c.added)}</span>{/if}
								{#if g.c.removed}<span class="text-danger">−{formatNumber(g.c.removed)}</span>{/if}
								{#if g.c.changed}<span class="text-warn">~{formatNumber(g.c.changed)}</span>{/if}
							</span>
						</header>
						<ul class="divide-y divide-border text-sm">
							{#each shown as it, i (i)}
								{@const m = marker[it.change] ?? marker.changed}
								<li
									class="grid grid-cols-[1.25rem_minmax(0,1fr)] items-start gap-x-2 px-4 py-1.5 sm:grid-cols-[1.25rem_8.5rem_minmax(0,1fr)]"
								>
									<span
										class="mt-px flex size-4.5 items-center justify-center rounded font-mono text-xs font-bold {m.cls} {m.row}"
										title={diffChangeLabel[it.change] ?? it.change}
										aria-label={diffChangeLabel[it.change] ?? it.change}>{m.sign}</span
									>
									<span class="text-xs leading-5 text-fg-subtle sm:text-sm sm:leading-normal">
										{label(diffKindLabel, it.kind)}
									</span>
									<div class="col-start-2 min-w-0 sm:col-start-3">
										{#if it.kind === 'device'}
											<span class="text-fg-muted">{deviceText(it)}</span>
										{:else}
											{#if it.key}<span class="mono text-fg">{it.key}</span>{/if}
											{#if it.change === 'changed'}
												<span class="block break-words sm:inline">
													{#if it.key}<span class="mx-1.5 hidden text-fg-subtle sm:inline">·</span>{/if}
													<del class="text-danger decoration-danger/60">{val(it, it.old)}</del>
													<span class="mx-1 text-fg-subtle" aria-label="geändert zu">→</span>
													<ins class="text-ok no-underline">{val(it, it.new)}</ins>
												</span>
											{:else if it.change === 'added' && it.new}
												<span class="block break-words text-ok sm:inline">
													{#if it.key}<span class="mx-1.5 hidden text-fg-subtle sm:inline">·</span>{/if}{val(
														it,
														it.new
													)}
												</span>
											{:else if it.change === 'removed' && it.old}
												<span
													class="block break-words text-danger line-through decoration-danger/50 sm:inline"
												>
													{#if it.key}<span class="mx-1.5 inline-block text-fg-subtle no-underline">·</span
														>{/if}{val(it, it.old)}
												</span>
											{/if}
										{/if}
									</div>
								</li>
							{/each}
						</ul>
						{#if g.items.length > GROUP_LIMIT}
							<div class="border-t border-border px-4 py-1.5">
								<Button
									size="xs"
									variant="ghost"
									icon={open ? 'chevron-up' : 'chevron-down'}
									onclick={() => {
										const next = new Set(expanded);
										if (open) next.delete(g.id);
										else next.add(g.id);
										expanded = next;
									}}
								>
									{open ? 'Weniger anzeigen' : `Alle ${formatNumber(g.items.length)} anzeigen`}
								</Button>
							</div>
						{/if}
					</section>
				{/each}
				{#if groups.length > groupLimit}
					<Button onclick={() => (groupLimit += PAGE_GROUPS)} class="self-center">
						Weitere {formatNumber(Math.min(PAGE_GROUPS, groups.length - groupLimit))} von {formatNumber(
							groups.length - groupLimit
						)} Geräten anzeigen
					</Button>
				{/if}
			</div>
		{/if}
	</div>
{/if}
