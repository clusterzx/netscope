<!--
	"Aktionen" tab of a plugin: plugin actions with parameter forms (schema), confirmation,
	outcome (message, data, created run with live progress) and – for publishers – a test
	message.
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { ActionOutcome, PluginAction, PluginView, RunView } from '$lib/api';
	import { SchemaForm, schemaInitial, schemaPayload, validateSchema } from '$lib/components/schema';
	import { Alert, Button, Card, EmptyState, JsonView, ProgressBar } from '$lib/components/ui';
	import { confirm } from '$lib/stores/confirm.svelte';
	import { runs } from '$lib/stores/runs.svelte';
	import { formatDateTime } from '$lib/utils/format';
	import RunProgress from './RunProgress.svelte';
	import RunStatusBadge from './RunStatusBadge.svelte';
	import { isActive, isPublisher } from './plugin';

	interface Props {
		plugin: PluginView;
	}

	let { plugin }: Props = $props();

	const id = $derived(plugin.info.id);
	const enabled = $derived(plugin.config?.enabled ?? false);
	const pluginActions = $derived((plugin.actions ?? []).filter((a) => a.scope !== 'device'));
	const deviceActions = $derived((plugin.actions ?? []).filter((a) => a.scope === 'device'));

	type State = {
		values: Record<string, unknown>;
		errors: Record<string, string>;
		busy: boolean;
		error: string;
		outcome: ActionOutcome | null;
		run: RunView | null;
		at: string;
	};

	let states = $state<Record<string, State>>({});
	const subs = new Set<() => void>();
	$effect(() => () => {
		for (const u of subs) u();
		subs.clear();
	});

	function st(a: PluginAction): State {
		let s = states[a.name];
		if (!s) {
			states[a.name] = {
				values: schemaInitial(a.params ?? [], {}),
				errors: {},
				busy: false,
				error: '',
				outcome: null,
				run: null,
				at: ''
			};
			s = states[a.name];
		}
		return s;
	}

	// initialise states for the current actions
	$effect(() => {
		for (const a of pluginActions) st(a);
	});

	async function runAction(a: PluginAction) {
		const s = st(a);
		const fields = a.params ?? [];
		s.errors = validateSchema(fields, s.values);
		if (Object.keys(s.errors).length) return;
		if (a.confirm && !(await confirm({ title: a.label, message: a.confirm, confirmLabel: 'Ausführen' })))
			return;
		s.busy = true;
		s.error = '';
		s.outcome = null;
		s.run = null;
		try {
			// ?wait=0: answer right after queueing; long actions (OUI update, NVD sync) are
			// followed live via the runs store instead of blocking the request
			const out = await api.post('/api/v1/plugins/{id}/actions/{action}', {
				path: { id, action: a.name },
				query: { wait: 0 },
				body: { params: schemaPayload(fields, s.values) }
			});
			s.outcome = out;
			s.at = new Date().toISOString();
			if (!out.finished && out.runId) follow(s, out.runId);
		} catch (e) {
			s.errors = fieldErrors(e);
			s.error = errorMessage(e);
		} finally {
			s.busy = false;
		}
	}

	/** Final outcome of an action run (the result is stored in stats.result). */
	function finish(s: State, r: RunView) {
		s.run = r;
		const res = r.stats?.result as { message?: string; data?: unknown } | undefined;
		s.outcome = {
			runId: r.id,
			status: r.status,
			finished: true,
			error: r.error,
			result: res ? { message: res.message ?? '', data: res.data } : undefined
		};
	}

	/** Follows a queued action run until it finished (live, with a fetch for fast runs). */
	function follow(s: State, runId: number) {
		let done = false;
		const unsub = runs.onFinished((r) => {
			if (r.id !== runId || done) return;
			done = true;
			stop();
			finish(s, r);
		});
		const stop = () => {
			unsub();
			subs.delete(stop);
		};
		subs.add(stop);
		// a quick action may have finished before the subscription existed
		api
			.get('/api/v1/runs/{id}', { path: { id: runId } })
			.then((r) => {
				if (done || isActive(r.status)) return;
				done = true;
				stop();
				finish(s, r);
			})
			.catch(() => {});
	}

	function liveRun(s: State): RunView | undefined {
		if (!s.outcome || s.outcome.finished) return undefined;
		return runs.get(s.outcome.runId);
	}

	// ---------------------------------------------------------------- publisher test
	let testBusy = $state(false);
	let testResult = $state<{ ok: boolean; message: string; at: string } | null>(null);

	async function sendTest() {
		testBusy = true;
		testResult = null;
		try {
			await api.post('/api/v1/plugins/{id}/test', { path: { id } });
			testResult = { ok: true, message: 'Die Testnachricht wurde zugestellt.', at: new Date().toISOString() };
		} catch (e) {
			testResult = { ok: false, message: errorMessage(e), at: new Date().toISOString() };
		} finally {
			testBusy = false;
		}
	}

	const runHref = (runId: number) => `/plugins/${encodeURIComponent(id)}/runs/${runId}`;
</script>

<div class="flex flex-col gap-4">
	{#if isPublisher(plugin)}
		<Card
			title="Testnachricht"
			icon="send"
			description="Prüft die Zustellung mit den gespeicherten Einstellungen."
		>
			<div class="flex flex-col gap-3">
				<p class="text-sm text-fg-muted">
					Sendet eine kurze Testnachricht über {plugin.info.name} – auch wenn der Publisher noch nicht aktiv ist.
					Ungespeicherte Änderungen im Tab „Einstellungen“ werden dabei nicht berücksichtigt.
				</p>
				<div>
					<Button variant="primary" icon="send" loading={testBusy} onclick={sendTest}
						>Testnachricht senden</Button
					>
				</div>
				{#if testResult}
					<Alert
						tone={testResult.ok ? 'ok' : 'danger'}
						title={testResult.ok ? 'Gesendet' : 'Versand fehlgeschlagen'}
					>
						<span class="break-words">{testResult.message}</span>
						<span class="mt-1 block text-xs text-fg-subtle">{formatDateTime(testResult.at, true)}</span>
					</Alert>
				{/if}
			</div>
		</Card>
	{/if}

	{#if pluginActions.length && !enabled}
		<Alert tone="warn" title="Plugin ist inaktiv">
			Aktionen können nur bei aktivem Plugin ausgeführt werden.
		</Alert>
	{/if}

	{#each pluginActions as a (a.name)}
		{@const s = states[a.name]}
		<Card title={a.label} icon="zap" description={a.description}>
			{#if s}
				<form
					class="flex flex-col gap-4"
					novalidate
					onsubmit={(e) => {
						e.preventDefault();
						runAction(a);
					}}
				>
					{#if a.params?.length}
						<SchemaForm
							fields={a.params}
							bind:values={states[a.name].values}
							errors={s.errors}
							idPrefix="act-{id}-{a.name}"
							compact
						/>
					{/if}
					{#if a.confirm}
						<p class="flex items-start gap-1.5 text-xs text-fg-subtle">
							Vor dem Ausführen wird nachgefragt: „{a.confirm}“
						</p>
					{/if}
					<div class="flex flex-wrap items-center gap-2">
						<Button
							type="submit"
							variant="primary"
							icon="play"
							loading={s.busy || (!!s.outcome && !s.outcome.finished)}
							disabled={!enabled}>Ausführen</Button
						>
					</div>

					{#if s.error}
						<Alert tone="danger" title="Aktion fehlgeschlagen"
							><span class="break-words">{s.error}</span></Alert
						>
					{/if}

					{#if s.outcome}
						{@const o = s.outcome}
						{@const lr = liveRun(s)}
						<div
							class="flex flex-col gap-3 rounded-md border border-border bg-surface-2 p-3"
							aria-live="polite"
						>
							<div class="flex flex-wrap items-center gap-2 text-sm">
								<RunStatusBadge status={lr?.status ?? o.status} />
								{#if o.runId}
									<a class="link mono" href={runHref(o.runId)}>Lauf #{o.runId}</a>
								{/if}
								{#if s.at}<span class="text-xs text-fg-subtle">{formatDateTime(s.at, true)}</span>{/if}
							</div>
							{#if !o.finished}
								{#if lr && isActive(lr.status)}
									<RunProgress run={lr} />
								{:else}
									<ProgressBar label="Aktion wird ausgeführt" />
								{/if}
								<p class="text-xs text-fg-subtle">
									Das Ergebnis erscheint hier, sobald die Aktion fertig ist – die Seite kann auch verlassen
									werden; der Lauf ist dann in der Laufhistorie zu finden.
								</p>
							{/if}
							{#if o.error}
								<Alert tone="danger"><span class="break-words">{o.error}</span></Alert>
							{/if}
							{#if o.result?.message}
								<p class="text-sm">{o.result.message}</p>
							{/if}
							{#if o.result?.data !== undefined && o.result?.data !== null}
								<JsonView value={o.result.data} openDepth={1} maxHeight="20rem" />
							{/if}
							{#if o.finished && !o.error && !o.result?.message && (o.result?.data === undefined || o.result?.data === null)}
								<p class="text-sm text-fg-muted">Die Aktion wurde ohne Rückmeldung abgeschlossen.</p>
							{/if}
						</div>
					{/if}
				</form>
			{/if}
		</Card>
	{/each}

	{#if deviceActions.length}
		<Card title="Geräteaktionen" icon="devices">
			<ul class="flex flex-col gap-2">
				{#each deviceActions as a (a.name)}
					<li class="text-sm">
						<span class="font-medium">{a.label}</span>
						{#if a.description}<span class="block text-fg-muted">{a.description}</span>{/if}
					</li>
				{/each}
			</ul>
			<p class="mt-3 text-xs text-fg-subtle">
				Geräteaktionen werden in der Gerätedetailansicht oder als Massenaktion in der
				<a href="/devices" class="link">Geräteliste</a> ausgeführt.
			</p>
		</Card>
	{/if}

	{#if !pluginActions.length && !deviceActions.length && !isPublisher(plugin)}
		<EmptyState icon="zap" title="Keine Aktionen" description="Dieses Plugin bietet keine Aktionen an." />
	{/if}
</div>
