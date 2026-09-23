<!--
	Cron expression input with live German description and the next runs
	(GET /api/v1/cron/describe). Empty = manual only (unless required).
-->
<script lang="ts">
	import { api } from '$lib/api/client';
	import type { CronDescription } from '$lib/api/types';
	import Input from '$lib/components/ui/Input.svelte';
	import Menu from '$lib/components/ui/Menu.svelte';
	import Spinner from '$lib/components/ui/Spinner.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import { formatDateTime } from '$lib/utils/format';

	interface Props {
		value: string;
		label: string;
		hint?: string;
		error?: string | null;
		required?: boolean;
		placeholder?: string;
		disabled?: boolean;
		id: string;
		/** text shown for an empty expression */
		emptyText?: string;
	}

	let {
		value = $bindable(''),
		label,
		hint,
		error,
		required = false,
		placeholder = 'z. B. */15 * * * *',
		disabled = false,
		id,
		emptyText = 'Kein Zeitplan – nur manuelle Ausführung'
	}: Props = $props();

	let desc = $state<CronDescription | null>(null);
	let loading = $state(false);

	const presets: { label: string; expr: string }[] = [
		{ label: 'Alle 5 Minuten', expr: '*/5 * * * *' },
		{ label: 'Alle 15 Minuten', expr: '*/15 * * * *' },
		{ label: 'Stündlich', expr: '0 * * * *' },
		{ label: 'Alle 6 Stunden', expr: '0 */6 * * *' },
		{ label: 'Täglich 03:00', expr: '0 3 * * *' },
		{ label: 'Montags 08:00', expr: '0 8 * * 1' }
	];

	$effect(() => {
		const expr = (value ?? '').trim();
		if (!expr) {
			desc = null;
			loading = false;
			return;
		}
		loading = true;
		const ctrl = new AbortController();
		const t = setTimeout(async () => {
			try {
				desc = await api.get('/api/v1/cron/describe', { query: { expr }, signal: ctrl.signal });
			} catch (e) {
				if (!(e instanceof DOMException)) desc = { valid: false, error: 'Prüfung fehlgeschlagen', next: [] };
			} finally {
				loading = false;
			}
		}, 300);
		return () => {
			clearTimeout(t);
			ctrl.abort();
		};
	});

	const hintText = $derived(error ? undefined : hint);
</script>

<div class="flex flex-col gap-1.5">
	<Input
		{label}
		hint={hintText}
		{error}
		{required}
		{id}
		{placeholder}
		{disabled}
		mono
		bind:value
		spellcheck="false"
		autocomplete="off"
	>
		{#snippet trailing()}
			<Menu
				label="Vorlagen"
				icon="clock"
				size="xs"
				{disabled}
				items={presets.map((p) => ({ label: p.label, hint: p.expr, onclick: () => (value = p.expr) }))}
			/>
		{/snippet}
	</Input>
	<div class="min-h-5 text-xs" aria-live="polite">
		{#if !(value ?? '').trim()}
			<span class="text-fg-subtle">{emptyText}</span>
		{:else if loading && !desc}
			<span class="inline-flex items-center gap-1.5 text-fg-subtle"><Spinner size={12} /> Prüfe …</span>
		{:else if desc && !desc.valid}
			<span class="inline-flex items-center gap-1 text-danger"
				><Icon name="alert" size={13} /> {desc.error}</span
			>
		{:else if desc}
			<div class="text-fg-muted">
				<span class="inline-flex items-center gap-1 font-medium text-fg"
					><Icon name="clock" size={13} class="text-accent" /> {desc.text}</span
				>
				{#if desc.next?.length}
					<span class="ml-1 text-fg-subtle">
						· nächste Läufe: {desc.next
							.slice(0, 3)
							.map((t) => formatDateTime(t))
							.join(', ')}
					</span>
				{/if}
			</div>
		{/if}
	</div>
</div>
