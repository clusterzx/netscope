<!--
	Application log viewer: GET /api/v1/system/logs + live tail via SSE topic "log".
	Filters (level, plugin, text) in the URL; pause/resume and auto-scroll.
-->
<script lang="ts">
	import { page } from '$app/state';
	import { tick, untrack } from 'svelte';
	import { api } from '$lib/api';
	import type { LogEntry } from '$lib/api';
	import { Badge, Button, Card, Checkbox, ErrorState, Input, Select, Skeleton } from '$lib/components/ui';
	import { live } from '$lib/stores/live.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { formatDate, formatNumber, formatTime } from '$lib/utils/format';
	import { debounce, intParam, setParams } from '$lib/utils/url';
	import { LOG_LEVELS, logLevelLabel, logRank, logTone } from './system';

	const MAX = 5000;
	const LIMITS = [200, 500, 1000, 2000];

	const sp = $derived(page.url.searchParams);
	const level = $derived(sp.get('level') ?? 'info');
	const plugin = $derived(sp.get('plugin') ?? '');
	const q = $derived(sp.get('q') ?? '');
	const limit = $derived(LIMITS.includes(intParam(sp, 'limit', 500)) ? intParam(sp, 'limit', 500) : 500);

	let text = $state(untrack(() => page.url.searchParams.get('q') ?? ''));
	let pluginText = $state(untrack(() => page.url.searchParams.get('plugin') ?? ''));
	const applyText = debounce((v: string) => setParams({ q: v.trim() || null }), 300);
	const applyPlugin = debounce((v: string) => setParams({ plugin: v.trim() || null }), 300);

	const data = new AsyncData<LogEntry[]>();
	let entries = $state<LogEntry[]>([]);
	let paused = $state(false);
	let buffered = $state<LogEntry[]>([]);
	let autoScroll = $state(true);
	let expanded = $state<Record<number, boolean>>({});
	let box: HTMLDivElement | null = $state(null);
	let serverLevel = $state('');

	$effect(() => {
		const query = { level, plugin: plugin || null, q: q || null, limit };
		data
			.run(async (signal) => (await api.get('/api/v1/system/logs', { query, signal })) ?? [])
			.then((list) => {
				if (!list) return;
				entries = list;
				buffered = [];
				scrollDown(true);
			});
	});

	$effect(() => {
		api
			.get('/api/v1/system/info')
			.then((i) => (serverLevel = i.logLevel))
			.catch(() => {});
	});

	function matches(e: LogEntry): boolean {
		if (logRank(e.level) < logRank(level)) return false;
		if (plugin && e.plugin !== plugin) return false;
		if (q && !e.msg.toLowerCase().includes(q.toLowerCase())) return false;
		return true;
	}

	$effect(() =>
		live.on<LogEntry>('log', (m) => {
			const e = m.data;
			if (!e || !matches(e)) return;
			if (paused) {
				buffered = [...buffered, e].slice(-MAX);
				return;
			}
			append([e]);
		})
	);
	$effect(() => live.onReconnect(() => data.reload()));
	$effect(() => () => {
		applyText.cancel();
		applyPlugin.cancel();
	});

	function append(list: LogEntry[]) {
		const last = entries.length ? entries[entries.length - 1].seq : -1;
		const fresh = list.filter((e) => e.seq > last);
		if (!fresh.length) return;
		const next = [...entries, ...fresh];
		entries = next.length > MAX ? next.slice(-MAX) : next;
		scrollDown();
	}

	async function scrollDown(force = false) {
		if (!autoScroll && !force) return;
		await tick();
		if (box) box.scrollTop = box.scrollHeight;
	}

	function resume() {
		paused = false;
		append(buffered);
		buffered = [];
	}

	function onScroll() {
		if (!box) return;
		const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 24;
		if (!atBottom && autoScroll) autoScroll = false;
	}

	const levelText: Record<string, string> = {
		danger: 'text-danger',
		warn: 'text-warn',
		info: 'text-fg',
		neutral: 'text-fg-muted'
	};

	function attrText(v: unknown): string {
		return typeof v === 'string' ? v : JSON.stringify(v);
	}
</script>

<Card padding="none" class="overflow-hidden">
	{#snippet header()}
		<div class="flex w-full flex-col gap-2 py-1">
			<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-end">
				<Select
					label="Level (mindestens)"
					size="sm"
					value={level}
					options={LOG_LEVELS.map((l) => ({ value: l, label: logLevelLabel[l] }))}
					onchange={(e) =>
						setParams({
							level:
								(e.currentTarget as HTMLSelectElement).value === 'info'
									? null
									: (e.currentTarget as HTMLSelectElement).value
						})}
					class="sm:w-36"
				/>
				<Input
					label="Plugin"
					size="sm"
					bind:value={pluginText}
					oninput={() => applyPlugin(pluginText)}
					placeholder="z. B. nmap"
					class="sm:w-36"
				/>
				<Input
					label="Text"
					size="sm"
					type="search"
					icon="search"
					bind:value={text}
					oninput={() => applyText(text)}
					placeholder="in Meldungen suchen"
					class="col-span-2 sm:w-56"
				/>
				<Select
					label="Anzahl"
					size="sm"
					value={String(limit)}
					options={LIMITS.map((l) => ({ value: String(l), label: `letzte ${l}` }))}
					onchange={(e) =>
						setParams({
							limit:
								Number((e.currentTarget as HTMLSelectElement).value) === 500
									? null
									: (e.currentTarget as HTMLSelectElement).value
						})}
					class="sm:w-32"
				/>
			</div>
			<div class="flex flex-wrap items-center gap-2">
				{#if paused}
					<Button size="sm" variant="primary" icon="play" onclick={resume}>
						Fortsetzen{buffered.length ? ` (${formatNumber(buffered.length)} neu)` : ''}
					</Button>
				{:else}
					<Button size="sm" icon="pause" onclick={() => (paused = true)}>Pausieren</Button>
				{/if}
				<Checkbox
					bind:checked={autoScroll}
					label="Automatisch scrollen"
					onchange={() => autoScroll && scrollDown(true)}
				/>
				<span class="flex items-center gap-1.5 text-xs text-fg-subtle">
					<span
						class="inline-block h-2 w-2 rounded-full {live.status === 'open' && !paused
							? 'bg-online'
							: 'bg-offline'}"
						aria-hidden="true"
					></span>
					{live.status === 'open' ? (paused ? 'Live pausiert' : 'Live') : 'Keine Live-Verbindung'}
				</span>
				<span class="ml-auto text-xs text-fg-subtle">
					{formatNumber(entries.length)} Zeilen{#if serverLevel}
						· Server-Level {serverLevel} (<a href="?tab=settings" class="link">ändern</a>){/if}
				</span>
				<Button
					size="sm"
					variant="ghost"
					icon="refresh"
					label="Neu laden"
					loading={data.loading}
					onclick={() => data.reload()}
				/>
			</div>
		</div>
	{/snippet}

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<div class="p-4"><Skeleton rows={10} /></div>
	{:else}
		<!-- the scrollable log must be focusable for keyboard scrolling -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			bind:this={box}
			onscroll={onScroll}
			class="mono h-[calc(100dvh-22rem)] min-h-80 overflow-auto bg-surface-2 text-[0.78rem] leading-relaxed"
			role="log"
			aria-label="Anwendungsprotokoll"
			aria-live="off"
			tabindex="0"
		>
			{#if entries.length === 0}
				<p class="px-4 py-6 text-center font-sans text-sm text-fg-subtle">
					Keine Protokollzeilen für diese Filter.
				</p>
			{/if}
			{#each entries as e (e.seq)}
				{@const tone = logTone(e.level)}
				{@const hasAttrs = !!e.attrs && Object.keys(e.attrs).length > 0}
				<div
					class="border-b border-border/50 px-3 py-0.5 hover:bg-surface-3/60 {tone === 'danger'
						? 'bg-danger-soft'
						: ''}"
				>
					<div class="flex flex-wrap items-start gap-x-2 sm:flex-nowrap">
						<span class="shrink-0 text-fg-subtle tabular" title={formatDate(e.time)}
							>{formatTime(e.time, true)}</span
						>
						<span class="w-11 shrink-0 font-semibold {levelText[tone]}">{e.level.slice(0, 5)}</span>
						{#if e.plugin}<Badge tone="accent" class="shrink-0 font-sans">{e.plugin}</Badge>{/if}
						<span
							class="order-last min-w-0 basis-full break-words whitespace-pre-wrap sm:order-none sm:flex-1 sm:basis-auto {levelText[
								tone
							]}">{e.msg}</span
						>
						{#if hasAttrs}
							<button
								type="button"
								class="ml-auto shrink-0 rounded px-1 font-sans text-xs text-fg-subtle hover:bg-surface-3 hover:text-fg sm:ml-0"
								aria-expanded={!!expanded[e.seq]}
								onclick={() => (expanded[e.seq] = !expanded[e.seq])}
								>{expanded[e.seq] ? 'weniger' : `${Object.keys(e.attrs ?? {}).length} Attribute`}</button
							>
						{/if}
					</div>
					{#if hasAttrs && !expanded[e.seq]}
						<div class="truncate text-fg-subtle sm:pl-[6.5rem]">
							{#each Object.entries(e.attrs ?? {}).slice(0, 6) as [k, v] (k)}<span class="mr-3"
									><span class="text-fg-muted">{k}</span>={attrText(v)}</span
								>{/each}
						</div>
					{:else if hasAttrs}
						<dl class="my-1 grid grid-cols-[auto_1fr] gap-x-3 rounded bg-surface px-2 py-1 sm:ml-[6.5rem]">
							{#each Object.entries(e.attrs ?? {}) as [k, v] (k)}
								<dt class="text-fg-muted">{k}</dt>
								<dd class="break-all whitespace-pre-wrap text-fg">{attrText(v)}</dd>
							{/each}
						</dl>
					{/if}
				</div>
			{/each}
		</div>
		{#if !autoScroll}
			<div class="flex justify-center border-t border-border bg-surface py-1.5">
				<Button
					size="xs"
					variant="ghost"
					icon="arrow-down"
					onclick={() => ((autoScroll = true), scrollDown(true))}>Zum Ende springen</Button
				>
			</div>
		{/if}
	{/if}
</Card>
