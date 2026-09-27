<!--
  Acknowledge events with an optional note: POST /api/v1/events/ack with {ids, note} or
  {filter, note} (all open events matching the filter) → {acknowledged}.
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { AckRequest } from '$lib/api';
	import { Alert, Button, Modal, Textarea } from '$lib/components/ui';
	import { toast } from '$lib/stores/toast.svelte';
	import { t, tn } from '$lib/i18n';

	interface Props {
		open?: boolean;
		/** ack these events … */
		ids?: number[];
		/** … or all open events matching this filter */
		filter?: AckRequest['filter'] | null;
		/** number of events affected (for the text; -1 = unknown) */
		count: number;
		/** short description of the filter (filter mode) */
		summary?: string;
		ondone?: (acknowledged: number) => void;
	}

	let { open = $bindable(false), ids = [], filter = null, count, summary = '', ondone }: Props = $props();

	let note = $state('');
	let error = $state<string | null>(null);
	let noteError = $state<string | null>(null);
	let busy = $state(false);

	let wasOpen = false;
	$effect(() => {
		if (open && !wasOpen) {
			note = '';
			error = noteError = null;
		}
		wasOpen = open;
	});

	const byFilter = $derived(!!filter && !ids.length);
	const title = $derived(
		byFilter
			? count < 0
				? t('Alle passenden offenen Events quittieren')
				: tn(count, '{n} passende Event quittieren', '{n} passende Events quittieren')
			: tn(count, '{n} Event quittieren', '{n} Events quittieren')
	);

	async function submit() {
		error = noteError = null;
		if (note.length > 1000) {
			noteError = t('Höchstens 1000 Zeichen');
			return;
		}
		busy = true;
		try {
			const body: AckRequest = byFilter ? { filter: filter!, note: note.trim() } : { ids, note: note.trim() };
			const res = await api.post('/api/v1/events/ack', { body });
			const n = res.acknowledged;
			toast.success(
				n === 0 ? t('Keine offenen Events betroffen') : tn(n, '{n} Event quittiert', '{n} Events quittiert')
			);
			open = false;
			ondone?.(n);
		} catch (e) {
			const f = fieldErrors(e);
			if (f.note) noteError = f.note;
			else error = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

<Modal bind:open {title} as="form" onsubmit={submit} {busy} size="md">
	<div class="flex flex-col gap-3">
		{#if byFilter}
			<Alert tone="warn">
				{t('Quittiert alle')}
				<strong>{t('offenen')}</strong>
				{t('Events, die zum Filter passen – auch solche auf anderen Seiten der Liste.')}
				{#if summary}<span class="mt-1 block text-fg-muted">Filter: {summary}</span>{/if}
			</Alert>
		{/if}
		{#if error}<Alert tone="danger">{error}</Alert>{/if}
		<Textarea
			label={t('Notiz')}
			hint={t('Optional – z. B. warum das Event erwartet war. Wird beim Event gespeichert.')}
			bind:value={note}
			rows={3}
			maxlength={1000}
			error={noteError}
		/>
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={busy}>{t('Abbrechen')}</Button>
		<Button type="submit" variant="primary" icon="check" loading={busy}>{t('Quittieren')}</Button>
	{/snippet}
</Modal>
