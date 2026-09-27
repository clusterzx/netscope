<!--
	Marks a CVE as irrelevant for a device (with note) or relevant again.
	<IgnoreDialog bind:open target={{ deviceId, deviceName, cve, ignored, note }} ondone={() => reload()} />
	`target.ignored` is the current state; the dialog toggles it.
-->
<script lang="ts" module>
	export interface IgnoreTarget {
		deviceId: number;
		deviceName: string;
		cve: string;
		ignored: boolean;
		note?: string;
	}
</script>

<script lang="ts">
	import { untrack } from 'svelte';
	import { api, errorMessage } from '$lib/api';
	import { Alert, Button, Modal, Textarea } from '$lib/components/ui';
	import { toast } from '$lib/stores/toast.svelte';
	import { t } from '$lib/i18n';

	interface Props {
		open?: boolean;
		target: IgnoreTarget | null;
		ondone?: (ignored: boolean) => void;
	}

	let { open = $bindable(false), target, ondone }: Props = $props();

	let note = $state('');
	let saving = $state(false);
	let err = $state<string | null>(null);

	$effect(() => {
		if (!open) return;
		untrack(() => {
			note = target?.note ?? '';
			err = null;
		});
	});

	const ignore = $derived(!target?.ignored);

	async function submit() {
		if (!target) return;
		saving = true;
		err = null;
		try {
			await api.post('/api/v1/vulnerabilities/ignore', {
				body: {
					deviceId: target.deviceId,
					cve: target.cve,
					ignored: ignore,
					note: ignore ? note.trim() : undefined
				}
			});
			toast.success(
				ignore
					? t('{cve} für {device} als irrelevant markiert', { cve: target.cve, device: target.deviceName })
					: t('{cve} für {device} wieder relevant', { cve: target.cve, device: target.deviceName })
			);
			open = false;
			ondone?.(ignore);
		} catch (e) {
			err = errorMessage(e);
		} finally {
			saving = false;
		}
	}
</script>

<Modal
	bind:open
	title={ignore ? t('Als irrelevant markieren') : t('Wieder als relevant markieren')}
	description={target ? `${target.cve} · ${target.deviceName}` : undefined}
	size="md"
	as="form"
	onsubmit={submit}
	busy={saving}
>
	<div class="flex flex-col gap-3 text-sm">
		{#if err}<Alert tone="danger">{err}</Alert>{/if}
		{#if ignore}
			<p class="text-fg-muted">
				{t(
					'Die CVE wird für dieses Gerät aus Listen, Zählern und Benachrichtigungen ausgeblendet. Die Markierung bleibt bei jedem neuen Abgleich erhalten und kann jederzeit zurückgenommen werden.'
				)}
			</p>
			<Textarea
				label={t('Begründung')}
				bind:value={note}
				rows={3}
				maxlength={500}
				placeholder={t('z. B. Backport im Distributionspaket, Dienst nicht erreichbar …')}
				hint={t('Optional, erscheint im Audit-Log und bei der CVE')}
			/>
		{:else}
			<p class="text-fg-muted">
				{t('Die CVE wird für dieses Gerät wieder in Listen, Zählern und Benachrichtigungen berücksichtigt.')}
			</p>
			{#if target?.note}
				<p class="rounded-md bg-surface-2 px-3 py-2 text-fg-muted">
					<span class="text-xs text-fg-subtle">{t('Bisherige Begründung:')}</span><br />{target.note}
				</p>
			{/if}
		{/if}
	</div>
	{#snippet footer()}
		<Button onclick={() => (open = false)} disabled={saving}>{t('Abbrechen')}</Button>
		<Button type="submit" variant="primary" loading={saving} icon={ignore ? 'eye-off' : 'eye'}>
			{ignore ? t('Als irrelevant markieren') : t('Wieder relevant')}
		</Button>
	{/snippet}
</Modal>
