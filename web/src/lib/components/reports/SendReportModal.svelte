<!--
	Sends the change report of a period through publishers (POST /api/v1/reports/send).
	<SendReportModal bind:open from={iso} to={iso} rangeText="16.09. – 23.09.2026" />
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import type { PublisherInfo } from '$lib/api';
	import { Alert, Button, Checkbox, Modal, Skeleton } from '$lib/components/ui';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import { toast } from '$lib/stores/toast.svelte';

	interface Props {
		open?: boolean;
		from: string;
		to?: string;
		rangeText: string;
	}

	let { open = $bindable(false), from, to, rangeText }: Props = $props();

	const publishers = new AsyncData<PublisherInfo[]>();
	let selected = $state<string[]>([]);
	let sending = $state(false);
	let err = $state<string | null>(null);

	$effect(() => {
		if (!open) return;
		untrack(() => {
			err = null;
			publishers
				.run(async (signal) => (await api.get('/api/v1/publishers', { signal })) ?? [])
				.then((list) => {
					selected = (list ?? []).filter((p) => p.enabled && selected.includes(p.id)).map((p) => p.id);
					if (!selected.length) selected = (list ?? []).filter((p) => p.enabled).map((p) => p.id);
				});
		});
	});

	const list = $derived(publishers.data ?? []);
	const enabled = $derived(list.filter((p) => p.enabled));

	function toggle(id: string, on: boolean) {
		selected = on ? [...selected.filter((x) => x !== id), id] : selected.filter((x) => x !== id);
		err = null;
	}

	async function send() {
		if (!selected.length) {
			err = 'Mindestens einen Publisher wählen.';
			return;
		}
		sending = true;
		err = null;
		try {
			await api.post('/api/v1/reports/send', { body: { publishers: selected, from, to: to || undefined } });
			toast.success(
				`Änderungsbericht (${rangeText}) wird über ${selected.length === 1 ? 'einen Publisher' : `${selected.length} Publisher`} versendet.`,
				{
					title: 'Versand eingeplant'
				}
			);
			open = false;
		} catch (e) {
			err = errorMessage(e);
		} finally {
			sending = false;
		}
	}
</script>

<Modal
	bind:open
	title="Änderungsbericht versenden"
	description={rangeText}
	size="md"
	as="form"
	onsubmit={send}
	busy={sending}
>
	<div class="flex flex-col gap-3 text-sm">
		{#if err}<Alert tone="danger" title="Versand nicht möglich">{err}</Alert>{/if}
		{#if publishers.error}
			<Alert tone="danger" title="Publisher konnten nicht geladen werden"
				>{errorMessage(publishers.error)}</Alert
			>
		{:else if !publishers.data}
			<Skeleton lines={3} />
		{:else if enabled.length === 0}
			<Alert tone="warn" title="Kein Publisher aktiv">
				Um Berichte zu versenden, zuerst einen Publisher (z. B. Telegram, E-Mail oder ntfy) unter Plugins
				konfigurieren und aktivieren.
				{#snippet actions()}
					<Button size="xs" href="/plugins" iconRight="arrow-right">Plugins</Button>
				{/snippet}
			</Alert>
		{:else}
			<p class="text-fg-muted">
				Der Bericht wird als Markdown-Nachricht an die gewählten Publisher übergeben.
			</p>
		{/if}
		{#if list.length}
			<fieldset class="flex flex-col gap-2 rounded-md border border-border px-3 py-2.5">
				<legend class="px-1 text-xs font-medium text-fg-muted">Publisher</legend>
				{#each list as p (p.id)}
					<Checkbox
						checked={selected.includes(p.id)}
						disabled={!p.enabled}
						class={p.enabled ? '' : 'cursor-not-allowed opacity-60'}
						label={p.name}
						description={p.enabled ? undefined : 'inaktiv – unter Plugins aktivieren'}
						onchange={(e) => toggle(p.id, (e.currentTarget as HTMLInputElement).checked)}
					/>
				{/each}
			</fieldset>
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={sending}>Abbrechen</Button>
		<Button type="submit" variant="primary" icon="send" loading={sending} disabled={!enabled.length}
			>Jetzt versenden</Button
		>
	{/snippet}
</Modal>
