<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import type { Rule } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		EmptyState,
		ErrorState,
		Icon,
		Menu,
		PageHeader,
		Skeleton,
		Tabs,
		Toggle
	} from '$lib/components/ui';
	import NotificationHistory from '$lib/components/rules/NotificationHistory.svelte';
	import { loadPublishers, publishers } from '$lib/components/rules/publishers.svelte';
	import { actionSummary, conditionSummary } from '$lib/components/rules/rule';
	import { auth } from '$lib/stores/auth.svelte';
	import { eventTypes, groups } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { federation } from '$lib/stores/federation.svelte';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { setParams } from '$lib/utils/url';

	onMount(() => {
		loadPublishers();
		eventTypes.load().catch(() => {});
		groups.load().catch(() => {});
	});

	const tab = $derived(page.url.searchParams.get('tab') === 'notifications' ? 'notifications' : 'rules');
	let activeTab = $state('rules');
	$effect(() => {
		activeTab = tab;
	});

	const data = new AsyncData<Rule[]>();
	$effect(() => {
		data.run(async (signal) => (await api.get('/api/v1/rules', { signal })) ?? []);
	});

	const rules = $derived([...(data.data ?? [])].sort((a, b) => a.sortOrder - b.sortOrder || a.id - b.id));
	const pubs = $derived(publishers.value ?? []);
	const catalog = $derived(eventTypes.value ?? []);
	const groupName = (id: number) => groups.value?.find((g) => g.id === id)?.name ?? `#${id}`;
	const siteName = (id: number) =>
		id === 0 ? federation.localName : (federation.site(id)?.name ?? `#${id}`);
	const anyPublisher = $derived(pubs.some((p) => p.enabled));
	const canManage = $derived(auth.can('rules.manage'));

	// ---------------------------------------------------------------- enable
	let busy = $state<Record<number, boolean>>({});

	async function setEnabled(r: Rule, v: boolean) {
		const prev = r.enabled;
		r.enabled = v;
		busy[r.id] = true;
		try {
			const saved = await api.put('/api/v1/rules/{id}', { path: { id: r.id }, body: { ...r, enabled: v } });
			Object.assign(r, saved);
			toast.success(`Regel ${v ? 'aktiviert' : 'deaktiviert'}`, { title: r.name });
		} catch (e) {
			r.enabled = prev;
			toast.error(errorMessage(e), { title: `${r.name}: Umschalten fehlgeschlagen` });
		} finally {
			busy[r.id] = false;
		}
	}

	// ---------------------------------------------------------------- reorder
	let reordering = $state(false);

	/** Persists the new evaluation order in one atomic call (PUT /api/v1/rules/order). */
	async function persistOrder(list: Rule[]) {
		list.forEach((r, i) => (r.sortOrder = (i + 1) * 10)); // optimistic, same numbering as the server
		reordering = true;
		try {
			const saved = await api.put('/api/v1/rules/order', { body: { ids: list.map((r) => r.id) } });
			data.set(saved ?? []);
			toast.success('Reihenfolge gespeichert');
		} catch (e) {
			toast.error(errorMessage(e), { title: 'Reihenfolge konnte nicht gespeichert werden' });
			data.reload();
		} finally {
			reordering = false;
		}
	}

	function move(index: number, dir: -1 | 1) {
		const j = index + dir;
		if (j < 0 || j >= rules.length) return;
		const list = [...rules];
		[list[index], list[j]] = [list[j], list[index]];
		persistOrder(list).then(() => {
			// keep keyboard focus on the moved rule's button
			requestAnimationFrame(() =>
				document.getElementById(`rule-${list[j].id}-${dir < 0 ? 'up' : 'down'}`)?.focus()
			);
		});
	}

	// drag & drop (mouse); the arrow buttons are the keyboard alternative
	let dragId = $state<number | null>(null);
	let overId = $state<number | null>(null);

	function onDrop(target: Rule) {
		const from = rules.findIndex((r) => r.id === dragId);
		const to = rules.findIndex((r) => r.id === target.id);
		dragId = null;
		overId = null;
		if (from < 0 || to < 0 || from === to) return;
		const list = [...rules];
		const [item] = list.splice(from, 1);
		list.splice(to, 0, item);
		persistOrder(list);
	}

	// ---------------------------------------------------------------- delete
	async function remove(r: Rule) {
		const ok = await confirm({
			title: `Regel „${r.name}“ löschen?`,
			message: r.builtin
				? 'Das ist eine mitgelieferte Standardregel. Sie wird nicht automatisch wiederhergestellt.'
				: 'Bereits verschickte Benachrichtigungen bleiben im Verlauf erhalten.',
			confirmLabel: 'Löschen',
			danger: true
		});
		if (!ok) return;
		try {
			await api.delete('/api/v1/rules/{id}', { path: { id: r.id } });
			toast.success(`Regel „${r.name}“ gelöscht`);
			data.reload();
		} catch (e) {
			toast.error(errorMessage(e), { title: 'Löschen fehlgeschlagen' });
		}
	}

	function disabledPublishers(r: Rule): string[] {
		return (r.actions ?? [])
			.map((a) => pubs.find((p) => p.id === a.publisher))
			.filter((p) => p && !p.enabled)
			.map((p) => p!.name);
	}
</script>

<PageHeader title="Regeln" description="Welche Events wann über welchen Publisher gemeldet werden">
	{#snippet actions()}
		{#if tab === 'rules' && canManage}
			<Button variant="primary" icon="plus" href="/rules/new">Neue Regel</Button>
		{/if}
	{/snippet}
</PageHeader>

<Tabs
	items={[
		{ id: 'rules', label: 'Regeln', icon: 'rules', count: data.data ? rules.length : null },
		{ id: 'notifications', label: 'Benachrichtigungen', icon: 'bell' }
	]}
	bind:active={activeTab}
	onchange={(t) =>
		setParams(
			{ tab: t === 'rules' ? null : t, status: null, publisher: null, rule: null, offset: null, limit: null },
			{ push: true }
		)}
	label="Regel-Bereiche"
	class="mb-4"
/>

{#if tab === 'notifications'}
	<div role="tabpanel" id="panel-notifications" aria-labelledby="tab-notifications">
		<NotificationHistory />
	</div>
{:else}
	<div role="tabpanel" id="panel-rules" aria-labelledby="tab-rules" class="flex flex-col gap-3">
		{#if publishers.value && !anyPublisher}
			<Alert tone="warn" title="Kein Publisher aktiv">
				Regeln werden ausgewertet, aber Benachrichtigungen erst verschickt, wenn ein Publisher eingerichtet
				und aktiviert ist.
				{#snippet actions()}
					{#if auth.can('plugins.manage')}
						<Button size="xs" href="/plugins#kind-publisher">Publisher einrichten</Button>
					{/if}
				{/snippet}
			</Alert>
		{/if}

		{#if data.error && !data.data}
			<ErrorState error={data.error} onretry={() => data.reload()} />
		{:else if !data.data}
			<div class="rounded-lg border border-border bg-surface p-4"><Skeleton rows={4} /></div>
		{:else if rules.length === 0}
			{#snippet create()}
				<Button variant="primary" icon="plus" href="/rules/new">Neue Regel</Button>
			{/snippet}
			<EmptyState
				icon="rules"
				title="Keine Regeln"
				description="Ohne Regeln werden Events zwar erfasst, aber niemand benachrichtigt."
				actions={canManage ? create : undefined}
			/>
		{:else}
			<p class="text-sm text-fg-muted">
				Regeln werden von oben nach unten ausgewertet; eine Regel mit „Stopp“ beendet die Auswertung, wenn sie
				greift.{#if canManage}
					Reihenfolge per Ziehen oder mit den Pfeil-Schaltflächen ändern.{/if}
			</p>
			<ol class="flex flex-col gap-2" aria-label="Regeln in Auswertungsreihenfolge" aria-busy={reordering}>
				{#each rules as r, i (r.id)}
					{@const off = disabledPublishers(r)}
					<li
						class="group flex items-stretch gap-2 rounded-lg border bg-surface shadow-sm transition-colors
							{overId === r.id && dragId !== r.id ? 'border-accent' : 'border-border'}
							{dragId === r.id ? 'opacity-50' : ''} {r.enabled ? '' : 'opacity-70'}"
						ondragover={(e) => {
							if (dragId === null) return;
							e.preventDefault();
							overId = r.id;
						}}
						ondragleave={() => {
							if (overId === r.id) overId = null;
						}}
						ondrop={(e) => {
							e.preventDefault();
							onDrop(r);
						}}
					>
						<!-- order handle -->
						<div
							class="flex w-10 shrink-0 flex-col items-center justify-center gap-0.5 border-r border-border py-2"
						>
							{#if canManage}
								<span
									draggable="true"
									role="presentation"
									title="Ziehen zum Verschieben"
									class="hidden cursor-grab text-fg-subtle hover:text-fg active:cursor-grabbing sm:block"
									ondragstart={(e) => {
										dragId = r.id;
										e.dataTransfer?.setData('text/plain', String(r.id));
										if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
									}}
									ondragend={() => {
										dragId = null;
										overId = null;
									}}
								>
									<Icon name="grip" size={16} />
								</span>
								<Button
									id="rule-{r.id}-up"
									size="xs"
									variant="ghost"
									icon="chevron-up"
									label="„{r.name}“ nach oben"
									disabled={i === 0 || reordering}
									onclick={() => move(i, -1)}
								/>
							{/if}
							<span class="text-xs font-semibold text-fg-subtle tabular" aria-hidden="true">{i + 1}</span>
							{#if canManage}
								<Button
									id="rule-{r.id}-down"
									size="xs"
									variant="ghost"
									icon="chevron-down"
									label="„{r.name}“ nach unten"
									disabled={i === rules.length - 1 || reordering}
									onclick={() => move(i, 1)}
								/>
							{/if}
						</div>

						<div class="flex min-w-0 flex-1 flex-col gap-1.5 py-3 pr-1">
							<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
								<a href="/rules/{r.id}" class="font-medium text-fg hover:text-accent hover:underline"
									>{r.name}</a
								>
								{#if r.builtin}<Badge tone="accent" title="Mitgelieferte Standardregel">Standard</Badge>{/if}
								{#if r.stop}<Badge
										tone="warn"
										title="Greift diese Regel, werden nachfolgende Regeln nicht ausgewertet">Stopp</Badge
									>{/if}
								{#if !r.enabled}<Badge>inaktiv</Badge>{/if}
							</div>
							{#if r.description}<p class="text-sm text-fg-muted">{r.description}</p>{/if}
							<dl class="grid grid-cols-[3.5rem_1fr] gap-x-2 gap-y-0.5 text-sm">
								<dt class="text-fg-subtle">Wenn</dt>
								<dd class="min-w-0">
									{conditionSummary(r.conditions, catalog, groupName, siteName).join(' · ')}
								</dd>
								<dt class="text-fg-subtle">Dann</dt>
								<dd class="min-w-0">
									{#each r.actions ?? [] as a, ai (ai)}
										<span class="block">{actionSummary(a, pubs)}</span>
									{/each}
									{#if off.length}
										<span class="mt-0.5 flex items-center gap-1 text-xs text-warn">
											<Icon name="alert" size={13} /> Publisher inaktiv: {off.join(', ')} – wird übersprungen
										</span>
									{/if}
								</dd>
							</dl>
						</div>

						<div
							class="flex shrink-0 flex-col items-end justify-between gap-2 py-3 pr-3 sm:flex-row sm:items-center"
						>
							{#if canManage}
								<Toggle
									checked={r.enabled}
									onchange={(v) => setEnabled(r, v)}
									disabled={busy[r.id]}
									label="„{r.name}“ aktiv"
									hideLabel
									size="sm"
								/>
							{/if}
							<Menu
								label="Aktionen für „{r.name}“"
								items={canManage
									? [
											{ label: 'Bearbeiten', icon: 'edit', href: `/rules/${r.id}` },
											{ label: 'Duplizieren', icon: 'copy', href: `/rules/new?from=${r.id}` },
											{
												label: 'Benachrichtigungen',
												icon: 'bell',
												href: `/rules?tab=notifications&rule=${r.id}`
											},
											{ separator: true },
											{ label: 'Löschen', icon: 'trash', danger: true, onclick: () => remove(r) }
										]
									: [
											{ label: 'Anzeigen', icon: 'eye', href: `/rules/${r.id}` },
											{
												label: 'Benachrichtigungen',
												icon: 'bell',
												href: `/rules?tab=notifications&rule=${r.id}`
											}
										]}
							/>
						</div>
					</li>
				{/each}
			</ol>
		{/if}
	</div>
{/if}
