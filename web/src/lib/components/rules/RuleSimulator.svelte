<!--
	"Event simulieren": tests a (possibly unsaved) rule against a synthetic event with
	POST /api/v1/rules/test {rule, event}. Shows match result, each condition, the action
	plans and the notification preview. "Test wirklich senden" delivers through the publishers.
-->
<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { api, ApiError, errorMessage } from '$lib/api';
	import { SEVERITIES } from '$lib/api';
	import type { Rule, RuleSimInput, RuleSimResult } from '$lib/api';
	import {
		Alert,
		Badge,
		Button,
		CodeBlock,
		FormField,
		Icon,
		Input,
		MarkdownView,
		RelativeTime,
		Select,
		SeverityBadge
	} from '$lib/components/ui';
	import DevicePicker from '$lib/components/plugins/DevicePicker.svelte';
	import { eventTypes } from '$lib/stores/catalog.svelte';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { formatDateTime } from '$lib/utils/format';
	import { eventCategoryLabel, severityLabel } from '$lib/utils/labels';
	import { priorityLabel, priorityTone } from './rule';

	interface Props {
		/** the rule as it would be saved (rulePayload of the editor state) */
		rule: Rule;
		/** why the rule cannot be tested yet (client side validation), empty = ok */
		blocked?: string;
	}

	let { rule, blocked = '' }: Props = $props();

	onMount(() => {
		eventTypes.load().catch(() => {});
	});

	const catalog = $derived(eventTypes.value ?? []);
	const categories = $derived([...new Set(catalog.map((e) => e.category))]);

	let type = $state('');
	let severity = $state('');
	let deviceIds = $state<number[]>([]);
	let title = $state('');
	let message = $state('');
	let payload = $state<Record<string, string | number | null>>({});
	let busy = $state<'' | 'check' | 'send'>('');
	let error = $state('');
	/** validation messages of the rule (the test validates like saving) */
	let errorList = $state<string[]>([]);
	let result = $state<RuleSimResult | null>(null);
	let resultFor = $state<{ type: string; deliver: boolean; at: string } | null>(null);

	// default event type: the first concrete type of the rule, else the first catalog entry
	$effect(() => {
		const types = rule.conditions.eventTypes ?? [];
		const cat = catalog;
		untrack(() => {
			if (type && cat.some((e) => e.type === type)) return;
			const concrete = types.find((t) => cat.some((e) => e.type === t));
			const byPattern = types
				.filter((t) => t.endsWith('.*'))
				.map((p) => cat.find((e) => e.type.startsWith(p.slice(0, -1))))
				.find(Boolean);
			type = concrete ?? byPattern?.type ?? cat[0]?.type ?? '';
		});
	});

	const spec = $derived(catalog.find((e) => e.type === type));
	/** payload fields that are not filled from the device */
	const fields = $derived((spec?.payload ?? []).filter((f) => !f.key.startsWith('device_')));

	// keep the payload editor in sync with the chosen type (values of shared keys survive)
	$effect(() => {
		const keys = fields.map((f) => f.key);
		untrack(() => {
			const next: Record<string, string | number | null> = {};
			for (const k of keys) next[k] = payload[k] ?? '';
			payload = next;
		});
	});

	function payloadValue(t: string, raw: string): unknown {
		const v = raw.trim();
		if (t === 'number') {
			const n = Number(v.replace(',', '.'));
			return isFinite(n) ? n : v;
		}
		if (t === 'list')
			return v
				.split(',')
				.map((s) => s.trim())
				.filter(Boolean);
		if (t === 'bool') return v === 'true' || v === 'ja' || v === '1';
		return v;
	}

	function buildEvent(deliver: boolean): RuleSimInput {
		const p: Record<string, unknown> = {};
		for (const f of fields) {
			const v = payload[f.key];
			const raw = v === null || v === undefined ? '' : String(v);
			if (raw.trim() !== '') p[f.key] = payloadValue(f.type, raw);
		}
		const ev: RuleSimInput = { type, payload: p, deliver };
		if (severity) ev.severity = severity;
		if (deviceIds[0]) ev.deviceId = deviceIds[0];
		if (title.trim()) ev.title = title.trim();
		if (message.trim()) ev.message = message.trim();
		return ev;
	}

	async function run(deliver: boolean) {
		if (!type) return;
		if (deliver) {
			const names = [...new Set(rule.actions.map((a) => a.publisher))].join(', ');
			const ok = await confirm({
				title: 'Testnachricht wirklich senden?',
				message: `Wenn die Regel greift, wird für jede Aktion eine als Test markierte Nachricht über ${names || 'die Publisher'} verschickt.`,
				confirmLabel: 'Senden'
			});
			if (!ok) return;
		}
		busy = deliver ? 'send' : 'check';
		error = '';
		errorList = [];
		try {
			result = await api.post('/api/v1/rules/test', { body: { rule, event: buildEvent(deliver) } });
			resultFor = { type, deliver, at: new Date().toISOString() };
		} catch (e) {
			error = errorMessage(e);
			errorList = e instanceof ApiError ? e.fields.map((f) => f.message) : [];
			result = null;
		} finally {
			busy = '';
		}
	}

	const severityOptions = SEVERITIES.map((s) => ({ value: s, label: severityLabel[s] }));
</script>

<div class="flex flex-col gap-4">
	<form
		class="flex flex-col gap-3"
		novalidate
		onsubmit={(e) => {
			e.preventDefault();
			run(false);
		}}
	>
		<FormField label="Event-Typ" required>
			{#snippet children(fid, describedby)}
				<div class="relative flex items-center">
					<select
						id={fid}
						aria-describedby={describedby}
						bind:value={type}
						class="h-8.5 w-full appearance-none rounded-md border border-border bg-surface pr-8 pl-2.5 text-sm shadow-sm hover:border-border-strong focus:border-accent focus:ring-2 focus:ring-focus focus:outline-none"
					>
						{#each categories as c (c)}
							<optgroup label={eventCategoryLabel[c] ?? c}>
								{#each catalog.filter((e) => e.category === c) as e (e.type)}
									<option value={e.type}>{e.label} ({e.type})</option>
								{/each}
							</optgroup>
						{/each}
					</select>
					<Icon name="chevron-down" size={14} class="pointer-events-none absolute right-2.5 text-fg-subtle" />
				</div>
			{/snippet}
		</FormField>
		{#if spec?.description}<p class="-mt-2 text-xs text-fg-subtle">{spec.description}</p>{/if}

		<Select
			label="Schweregrad"
			options={severityOptions}
			placeholder={spec
				? `Standard (${severityLabel[spec.defaultSeverity] ?? spec.defaultSeverity})`
				: 'Standard'}
			bind:value={severity}
		/>

		<DevicePicker
			label="Gerät"
			single
			bind:value={deviceIds}
			hint="Optional. Name, IP, MAC und Zustand des Geräts werden ins Event übernommen."
		/>

		{#if fields.length}
			<fieldset class="flex flex-col gap-2">
				<legend class="mb-1 text-[0.8125rem] font-medium text-fg">Payload</legend>
				{#each fields as f (f.key)}
					<Input
						label={f.key}
						size="sm"
						mono
						type={f.type === 'number' ? 'number' : 'text'}
						step="any"
						bind:value={payload[f.key]}
						placeholder={f.type === 'list' ? 'kommagetrennt' : f.type === 'number' ? 'Zahl' : ''}
						hint={f.description || undefined}
					/>
				{/each}
			</fieldset>
		{/if}

		<details class="group rounded-md border border-border">
			<summary
				class="flex cursor-pointer list-none items-center gap-1.5 px-3 py-2 text-[0.8125rem] font-medium text-fg-muted select-none hover:text-fg [&::-webkit-details-marker]:hidden"
			>
				<Icon name="chevron-right" size={14} class="transition-transform group-open:rotate-90" />
				Titel und Nachricht
			</summary>
			<div class="flex flex-col gap-3 border-t border-border px-3 py-3">
				<Input label="Titel" bind:value={title} placeholder="{spec?.label ?? 'Event'} (Simulation)" />
				<Input label="Nachricht" bind:value={message} />
			</div>
		</details>

		{#if blocked}
			<Alert tone="warn" title="Regel noch unvollständig">{blocked}</Alert>
		{/if}

		<div class="flex flex-wrap gap-2">
			<Button
				type="submit"
				variant="primary"
				icon="play"
				loading={busy === 'check'}
				disabled={!type || !!blocked || !!busy}
			>
				Nur prüfen
			</Button>
			<Button
				icon="send"
				loading={busy === 'send'}
				disabled={!type || !!blocked || !!busy}
				onclick={() => run(true)}
			>
				Test wirklich senden
			</Button>
		</div>
	</form>

	{#if error}
		<Alert tone="danger" title={errorList.length ? 'Die Regel ist ungültig' : 'Test fehlgeschlagen'}>
			{#if errorList.length}
				<ul class="list-disc pl-4">
					{#each errorList as msg, i (i)}<li class="break-words">{msg}</li>{/each}
				</ul>
			{:else}
				<span class="break-words">{error}</span>
			{/if}
		</Alert>
	{/if}

	{#if result && resultFor}
		<section class="flex flex-col gap-3" aria-live="polite" aria-label="Testergebnis">
			<div
				class="flex items-center gap-3 rounded-lg border px-3.5 py-3 {result.matched
					? 'border-ok/35 bg-ok-soft'
					: 'border-border bg-surface-2'}"
			>
				<Icon
					name={result.matched ? 'check-circle' : 'x-circle'}
					size={22}
					class={result.matched ? 'text-ok' : 'text-fg-subtle'}
				/>
				<div class="min-w-0 flex-1">
					<p class="font-semibold">{result.matched ? 'Regel greift' : 'Regel greift nicht'}</p>
					<p class="text-xs text-fg-muted">
						{resultFor.type} · {resultFor.deliver ? 'mit Versand' : 'nur geprüft'} · {formatDateTime(
							resultFor.at,
							true
						)}
					</p>
				</div>
			</div>
			{#if result.note}<Alert tone="info">{result.note}</Alert>{/if}

			<div>
				<h3 class="mb-1.5 text-xs font-semibold tracking-wide text-fg-subtle uppercase">Bedingungen</h3>
				{#if !result.conditions?.length}
					<p class="text-sm text-fg-muted">Keine Bedingungen – jedes Event passt.</p>
				{:else}
					<ul class="flex flex-col gap-1">
						{#each result.conditions as c, i (i)}
							<li class="flex items-start gap-2 text-sm">
								<Icon
									name={c.ok ? 'check' : 'x'}
									size={16}
									class="mt-0.5 shrink-0 {c.ok ? 'text-ok' : 'text-danger'}"
									label={c.ok ? 'erfüllt' : 'nicht erfüllt'}
								/>
								<span class="min-w-0">
									<span class="font-medium">{c.condition}</span>
									<span class="block text-xs break-words text-fg-muted">{c.detail}</span>
								</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			{#if result.matched}
				<div>
					<h3 class="mb-1.5 text-xs font-semibold tracking-wide text-fg-subtle uppercase">Aktionen</h3>
					<ul class="flex flex-col gap-2">
						{#each result.actions ?? [] as a, i (i)}
							{@const delivery = result.delivery?.[a.publisher]}
							<li class="flex flex-col gap-1.5 rounded-md border border-border px-3 py-2 text-sm">
								<div class="flex flex-wrap items-center gap-2">
									<span class="font-medium">{a.publisherName || a.publisher}</span>
									{#if !a.publisherEnabled}<Badge tone="warn">inaktiv</Badge>{/if}
									<Badge tone={priorityTone(a.priority)}>{priorityLabel[a.priority] ?? a.priority}</Badge>
									<span class="text-xs text-fg-muted">{a.mode === 'batch' ? 'gesammelt' : 'sofort'}</span>
								</div>
								{#if a.skipped}
									<p class="text-xs text-warn">Übersprungen: {a.skipped}</p>
								{:else if a.throttled}
									<p class="text-xs text-warn">
										Gedrosselt – innerhalb der Drosselzeit wurde bereits benachrichtigt.
									</p>
								{:else}
									<p class="text-xs text-fg-muted">
										Zustellung {formatDateTime(a.deliverAt, true)} (<RelativeTime value={a.deliverAt} />)
										{#if a.quiet === 'delayed'}
											· wegen Ruhezeit verzögert{/if}
									</p>
								{/if}
								{#if a.quiet === 'dropped'}<p class="text-xs text-warn">In der Ruhezeit verworfen.</p>{/if}
								{#if a.escalateAt}
									<p class="text-xs text-fg-muted">
										Eskalation {formatDateTime(a.escalateAt)}, falls nicht quittiert
									</p>
								{/if}
								{#if delivery}
									{#if delivery === 'ok'}
										<p class="flex items-center gap-1 text-xs text-ok">
											<Icon name="check" size={13} /> Testnachricht zugestellt
										</p>
									{:else}
										<p class="text-xs break-words text-danger">Versand fehlgeschlagen: {delivery}</p>
									{/if}
								{/if}
							</li>
						{:else}
							<li class="text-sm text-fg-muted">Keine Aktionen.</li>
						{/each}
					</ul>
				</div>
			{/if}

			{#if result.preview}
				<div>
					<h3 class="mb-1.5 text-xs font-semibold tracking-wide text-fg-subtle uppercase">
						Vorschau der Benachrichtigung
					</h3>
					<div class="flex flex-col gap-2 rounded-md border border-border bg-surface-2 p-3">
						<p class="font-medium">{result.preview.title}</p>
						{#if result.preview.body}<MarkdownView source={result.preview.body} />{/if}
						{#each result.preview.events ?? [] as ev, i (i)}
							<div class="flex items-start gap-2 text-sm">
								<SeverityBadge severity={ev.severity} />
								<span class="min-w-0">
									{ev.title}
									{#if ev.message}<span class="block text-xs text-fg-muted">{ev.message}</span>{/if}
								</span>
							</div>
						{/each}
					</div>
					{#if result.previewText}
						<CodeBlock code={result.previewText} label="Als Text" wrap maxHeight="14rem" class="mt-2" />
					{/if}
				</div>
			{/if}
		</section>
	{/if}
</div>
