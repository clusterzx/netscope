<!--
	Log of a plugin run (GET /api/v1/runs/{id}/logs) with level filter, search, expandable
	attributes and – while the run is active – a live tail via the SSE topic "run.log".
	Live lines carry their stored id (sent after the database write): duplicates are dropped
	by id and gaps (reconnect, run end) are filled with GET …/logs?after=<last id>.
	<RunLogViewer runId={run.id} active={run.status === 'running'} />
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { RunLog, RunLogMessageData } from '$lib/api';
	import { Button, EmptyState, ErrorState, Icon, Input, Select, Skeleton, Toggle } from '$lib/components/ui';
	import { live } from '$lib/stores/live.svelte';
	import { formatTime, formatDateTime } from '$lib/utils/format';
	import { levelRank, logLevelClass } from './plugin';

	interface Props {
		runId: number;
		/** run is queued/running: follow new lines live */
		active?: boolean;
		/** the run no longer exists on the server (discarded): keep the lines, never refetch */
		frozen?: boolean;
		class?: string;
	}

	let { runId, active = false, frozen = false, class: klass = '' }: Props = $props();

	/** run.log line incl. the stored id (0 = storing failed) */
	type LiveLine = RunLogMessageData & { id?: number };
	type Line = RunLog & { key: string; live?: boolean };

	let lines = $state<Line[]>([]);
	let loading = $state(true);
	let error = $state<unknown>(null);
	let minLevel = $state('');
	let search = $state('');
	let follow = $state(true);
	let expanded = $state<Record<string, boolean>>({});
	let scroller: HTMLDivElement | null = $state(null);
	let seq = 0;
	/** highest stored id we have (continue with ?after=) */
	let lastId = 0;
	/** live lines that arrived while a fetch was running */
	let buffer: LiveLine[] = [];
	let fetching = false;
	let unsaved = 0;

	function toLine(l: RunLog): Line {
		return { ...l, key: `db-${l.id}` };
	}

	/** Appends lines with ids above lastId (and unsaved live lines with id 0). */
	function append(list: (RunLog | LiveLine)[]) {
		const add: Line[] = [];
		for (const l of list) {
			const id = l.id ?? 0;
			if (id > 0) {
				if (id <= lastId) continue;
				lastId = id;
				add.push(toLine({ id, ts: l.ts, level: l.level, msg: l.msg, attrs: l.attrs }));
			} else {
				add.push({
					id: 0,
					ts: l.ts,
					level: l.level,
					msg: l.msg,
					attrs: l.attrs,
					key: `live-${++unsaved}`,
					live: true
				});
			}
		}
		if (add.length) lines.push(...add);
	}

	/** Fetches all stored lines after `after` (paged). */
	async function fetchAfter(after: number): Promise<RunLog[]> {
		const out: RunLog[] = [];
		for (let i = 0; i < 20; i++) {
			const batch =
				(await api.get('/api/v1/runs/{id}/logs', { path: { id: runId }, query: { after, limit: 1000 } })) ??
				[];
			out.push(...batch);
			if (batch.length < 1000) break;
			after = batch[batch.length - 1].id;
		}
		return out;
	}

	/** Loads (full = from the start) or continues after the last known id. */
	async function load(full = false) {
		if (frozen) return;
		const my = ++seq;
		fetching = true;
		error = null;
		try {
			const from = full ? 0 : lastId;
			const got = await fetchAfter(from);
			if (my !== seq) return;
			if (full) {
				lines = [];
				lastId = 0;
			}
			append(got);
			// live lines received meanwhile: those with a higher id are appended, the rest are dupes
			append(buffer.sort((a, b) => (a.id ?? 0) - (b.id ?? 0)));
			buffer = [];
		} catch (e) {
			if (my === seq) error = e;
		} finally {
			if (my === seq) {
				loading = false;
				fetching = false;
			}
		}
	}

	$effect(() => {
		void runId;
		untrack(() => {
			lines = [];
			lastId = 0;
			buffer = [];
			loading = true;
			expanded = {};
			load(true);
		});
	});

	// live tail
	$effect(() => {
		if (!active) return;
		const id = runId;
		return live.on<LiveLine>('run.log', (m) => {
			const d = m.data;
			if (!d || d.runId !== id) return;
			if (fetching) buffer.push(d);
			else append([d]);
		});
	});
	// run ended: pick up anything the stream did not deliver
	let wasActive = untrack(() => active);
	$effect(() => {
		const a = active;
		untrack(() => {
			if (wasActive && !a && !frozen) setTimeout(() => load(), 400);
			wasActive = a;
		});
	});
	$effect(() => live.onReconnect(() => load()));

	const levels = [
		{ value: '', label: 'Alle Stufen' },
		{ value: 'INFO', label: 'Ab Info' },
		{ value: 'WARN', label: 'Ab Warnung' },
		{ value: 'ERROR', label: 'Nur Fehler' }
	];

	const filtered = $derived.by(() => {
		const q = search.trim().toLowerCase();
		const min = minLevel ? levelRank(minLevel) : -1;
		return lines.filter((l) => {
			if (min >= 0 && levelRank(l.level) < min) return false;
			if (!q) return true;
			if (l.msg.toLowerCase().includes(q)) return true;
			return l.attrs ? JSON.stringify(l.attrs).toLowerCase().includes(q) : false;
		});
	});

	const counts = $derived.by(() => {
		let warn = 0;
		let err = 0;
		for (const l of lines) {
			const r = levelRank(l.level);
			if (r === 2) warn++;
			else if (r >= 3) err++;
		}
		return { warn, err };
	});

	// auto scroll to the newest line while following
	$effect(() => {
		void filtered.length;
		if (!follow || !scroller) return;
		const el = scroller;
		requestAnimationFrame(() => (el.scrollTop = el.scrollHeight));
	});

	function onScroll() {
		if (!scroller || !active) return;
		const atBottom = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 24;
		if (!atBottom && follow) follow = false;
	}

	function attrText(v: unknown): string {
		if (v === null || v === undefined) return '';
		if (typeof v === 'string') return v;
		return JSON.stringify(v);
	}

	const plainText = $derived(
		filtered
			.map(
				(l) =>
					`${formatDateTime(l.ts, true)} ${l.level.padEnd(5)} ${l.msg}` +
					(l.attrs && Object.keys(l.attrs).length
						? ' ' +
							Object.entries(l.attrs)
								.map(([k, v]) => `${k}=${attrText(v)}`)
								.join(' ')
						: '')
			)
			.join('\n')
	);

	async function copyAll() {
		try {
			await navigator.clipboard.writeText(plainText);
		} catch {
			// clipboard unavailable (http) – ignore, the text can be selected manually
		}
	}
</script>

<div class="flex min-w-0 flex-col gap-2 {klass}">
	<div class="flex flex-wrap items-end gap-2">
		<Select label="Stufe" size="sm" options={levels} bind:value={minLevel} class="w-36" />
		<Input
			label="Suche"
			size="sm"
			icon="search"
			type="search"
			placeholder="Text oder Attribut …"
			bind:value={search}
			class="min-w-40 flex-1 sm:max-w-72"
		/>
		<div class="ml-auto flex items-center gap-3 pb-1">
			{#if active}
				<Toggle size="sm" label="Automatisch scrollen" bind:checked={follow} />
			{/if}
			<Button
				size="sm"
				icon="copy"
				label="Angezeigte Zeilen kopieren"
				onclick={copyAll}
				disabled={!filtered.length}
			/>
			<Button
				size="sm"
				icon="refresh"
				label="Protokoll neu laden"
				onclick={() => load(true)}
				disabled={frozen}
			/>
		</div>
	</div>

	<div class="flex flex-wrap items-center gap-3 text-xs text-fg-subtle" aria-live="polite">
		<span class="tabular">{filtered.length} von {lines.length} Zeilen</span>
		{#if counts.warn}<span class="text-warn">{counts.warn} Warnungen</span>{/if}
		{#if counts.err}<span class="text-danger">{counts.err} Fehler</span>{/if}
		{#if active}
			<span class="inline-flex items-center gap-1 text-live">
				<span class="h-1.5 w-1.5 animate-pulse rounded-full bg-live"></span> live
			</span>
			{#if !follow}
				<button type="button" class="link" onclick={() => (follow = true)}>
					<Icon name="arrow-down" size={12} class="inline" /> zum Ende springen
				</button>
			{/if}
		{/if}
	</div>

	{#if loading && !lines.length}
		<Skeleton lines={6} />
	{:else if error && !lines.length}
		<ErrorState {error} compact onretry={() => load(true)} />
	{:else if !lines.length}
		<EmptyState
			compact
			icon="terminal"
			title={active ? 'Noch keine Protokollzeilen' : 'Keine Protokollzeilen'}
			description={active
				? 'Neue Zeilen erscheinen hier automatisch.'
				: 'Dieser Lauf hat nichts protokolliert.'}
		/>
	{:else}
		<!-- the scroll container must be reachable by keyboard -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			bind:this={scroller}
			onscroll={onScroll}
			class="max-h-[60vh] min-h-40 overflow-auto rounded-md border border-border bg-surface-2 py-1 font-mono text-[0.78rem] leading-relaxed"
			role="log"
			aria-label="Protokoll von Lauf {runId}"
			aria-live={active && follow ? 'polite' : 'off'}
			tabindex="0"
		>
			{#if !filtered.length}
				<p class="px-3 py-2 font-sans text-sm text-fg-subtle">Keine Zeile passt zum Filter.</p>
			{/if}
			{#each filtered as l (l.key)}
				{@const hasAttrs = !!l.attrs && Object.keys(l.attrs).length > 0}
				{@const open = !!expanded[l.key]}
				<div class="group px-3 py-px hover:bg-surface-3 {l.live ? 'animate-[ns-flash_1.2s_ease-out]' : ''}">
					<div class="flex min-w-0 items-start gap-2">
						<time class="shrink-0 text-fg-subtle tabular" datetime={l.ts} title={formatDateTime(l.ts, true)}
							>{formatTime(l.ts, true)}</time
						>
						<span class="w-11 shrink-0 font-semibold {logLevelClass(l.level)}">{l.level.slice(0, 5)}</span>
						<span class="min-w-0 flex-1 break-words whitespace-pre-wrap text-fg">
							{l.msg}
							{#if hasAttrs && !open}
								<span class="text-fg-subtle">
									{#each Object.entries(l.attrs ?? {}).slice(0, 4) as [k, v] (k)}
										<span class="ml-2"><span class="text-fg-muted">{k}</span>={attrText(v).slice(0, 80)}</span
										>
									{/each}
								</span>
							{/if}
						</span>
						{#if hasAttrs}
							<button
								type="button"
								class="shrink-0 rounded px-1 text-fg-subtle hover:bg-border hover:text-fg"
								aria-expanded={open}
								aria-label={open ? 'Attribute einklappen' : 'Attribute anzeigen'}
								onclick={() => (expanded[l.key] = !open)}
							>
								<Icon name={open ? 'chevron-up' : 'chevron-down'} size={14} />
							</button>
						{/if}
					</div>
					{#if hasAttrs && open}
						<dl
							class="mt-0.5 mb-1 ml-[5.25rem] grid grid-cols-[max-content_1fr] gap-x-3 rounded border border-border bg-surface px-2 py-1"
						>
							{#each Object.entries(l.attrs ?? {}) as [k, v] (k)}
								<dt class="text-fg-muted">{k}</dt>
								<dd class="min-w-0 break-all whitespace-pre-wrap text-fg">{attrText(v)}</dd>
							{/each}
						</dl>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>
