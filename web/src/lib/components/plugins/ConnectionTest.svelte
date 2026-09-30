<!--
	Connection test of a plugin that reads systems with credentials (POST
	/api/v1/plugins/{id}/connection-test): tests the settings of the form without saving them.
	Plugins that work on devices (SSH, SNMP) ask for the address of a device.
	<ConnectionTest plugin={p} settings={() => schemaPayload(fields, values)} />
-->
<script lang="ts">
	import { api, errorMessage, fieldErrors } from '$lib/api';
	import type { ConnectionResult, PluginView } from '$lib/api';
	import { Button, Icon, Input } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { formatMs } from '$lib/utils/format';

	interface Props {
		plugin: PluginView;
		/** the settings to test (form values, not saved) */
		settings: () => Record<string, unknown>;
		/** field errors of the settings go back to the form (keys without prefix) */
		onfielderrors?: (errors: Record<string, string>) => void;
		disabled?: boolean;
	}

	let { plugin, settings, onfielderrors, disabled = false }: Props = $props();

	const needsTarget = $derived(plugin.info.targets === 'devices');
	const idp = $derived(`ct-${plugin.info.id}`);
	let target = $state('');
	let busy = $state(false);
	let results = $state<ConnectionResult[] | null>(null);
	let error = $state('');

	async function run() {
		busy = true;
		error = '';
		results = null;
		try {
			const res = await api.post('/api/v1/plugins/{id}/connection-test', {
				path: { id: plugin.info.id },
				body: { settings: settings(), target: needsTarget ? target.trim() : undefined }
			});
			results = res.results;
		} catch (e) {
			const fe = fieldErrors(e);
			if (Object.keys(fe).length) onfielderrors?.(fe);
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

<div class="flex flex-col gap-2">
	<div class="flex flex-wrap items-end gap-2">
		{#if needsTarget}
			<Input
				id="{idp}-target"
				label={t('Testadresse')}
				bind:value={target}
				placeholder="192.168.1.10"
				hint={t('Ein Gerät, an dem die Zugangsdaten funktionieren sollen (optional mit :Port)')}
				mono
				class="w-64"
			/>
		{/if}
		<Button icon="zap" onclick={run} loading={busy} disabled={disabled || (needsTarget && !target.trim())}>
			{t('Verbindung testen')}
		</Button>
	</div>
	<div aria-live="polite" class="flex flex-col gap-1.5">
		{#if error}
			<p class="flex items-start gap-1.5 text-sm text-danger">
				<Icon name="x-circle" size={16} class="mt-0.5 shrink-0" />{error}
			</p>
		{/if}
		{#each results ?? [] as r (r.target)}
			<p class="flex items-start gap-1.5 text-sm {r.ok ? 'text-ok' : 'text-danger'}">
				<Icon name={r.ok ? 'check-circle' : 'x-circle'} size={16} class="mt-0.5 shrink-0" />
				<span class="min-w-0">
					<span class="mono text-fg">{r.target}</span>
					<span class="text-fg-muted"> · {formatMs(r.durationMs)}</span><br />
					<span class="break-words">{r.message}</span>
				</span>
			</p>
		{/each}
	</div>
</div>
