<!--
	Credentials whose scope covers the device, in the order plugins try them (most specific
	first), with the reason and the plugins that use them.
	<CredentialsCard deviceId={d.id} version={version} active={active} />
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { ApiDeviceCredential } from '$lib/api/generated';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { t, tn } from '$lib/i18n';
	import { LazyData } from './util';

	interface Props {
		deviceId: number;
		version: number;
		active: boolean;
	}

	let { deviceId, version, active }: Props = $props();

	const data = new LazyData<ApiDeviceCredential[]>();
	$effect(() => {
		if (!active) return;
		data.ensure(
			`${deviceId}:${version}`,
			async (signal) =>
				(await api.get('/api/v1/devices/{id}/credentials', { path: { id: deviceId }, signal })) ?? []
		);
	});
	$effect(() => () => data.abort());

	const typeLabel: Record<string, string> = {
		ssh: 'SSH',
		password: t('Passwort'),
		snmp_v2c: 'SNMP v2c',
		snmp_v3: 'SNMP v3',
		api_token: t('API-Token')
	};
	// the sentence links to the credentials page: split at the placeholder
	const noneText = t(
		'Für dieses Gerät gelten keine Zugangsdaten. Unter {credentials} bei „Gilt für“ das Gerät, eine Gruppe, einen Tag oder ein Subnetz eintragen.'
	).split('{credentials}');
	const rankTone = (rank: number) => (rank >= 3 ? 'accent' : rank === 2 ? 'info' : 'neutral');

	const used = $derived((data.data ?? []).filter((c) => c.usedBy?.length));
	const unused = $derived((data.data ?? []).filter((c) => !c.usedBy?.length));
</script>

{#snippet item(c: ApiDeviceCredential)}
	<li class="flex flex-col gap-1 px-4 py-2 text-sm">
		<div class="flex min-w-0 items-center gap-2">
			<span class="min-w-0 flex-1 truncate font-medium" title={c.name}>{c.name}</span>
			<Badge tone="neutral">{typeLabel[c.type] ?? c.type}</Badge>
		</div>
		<div class="flex flex-wrap items-center gap-1.5 text-xs">
			<Badge tone={rankTone(c.rank)} title={t('Warum es für dieses Gerät gilt')}>{c.reason}</Badge>
			{#each c.usedBy ?? [] as u (u.id)}
				<a href="/plugins/{encodeURIComponent(u.id)}" class="link">{u.name}</a>
			{/each}
		</div>
	</li>
{/snippet}

<Card
	title={t('Zugangsdaten')}
	icon="key"
	padding="none"
	description={t('Pro Plugin in dieser Reihenfolge probiert')}
>
	{#snippet actions()}
		<Button size="xs" variant="ghost" href="/credentials" iconRight="arrow-right">{t('Verwalten')}</Button>
	{/snippet}
	{#if !data.data}
		<p class="px-4 py-3 text-sm text-fg-subtle">{data.error ? t('Nicht verfügbar') : t('Wird geladen …')}</p>
	{:else if !data.data.length}
		<p class="px-4 py-3 text-sm text-fg-subtle">
			{noneText[0]}<a href="/credentials" class="link">Credentials</a>{noneText[1] ?? ''}
		</p>
	{:else}
		{#if used.length}
			<ol class="divide-y divide-border">
				{#each used as c (c.id)}{@render item(c)}{/each}
			</ol>
		{:else}
			<p class="px-4 py-3 text-sm text-fg-subtle">
				{t('Kein Plugin greift mit Zugangsdaten auf dieses Gerät zu.')}
			</p>
		{/if}
		{#if unused.length}
			<details class="border-t border-border">
				<summary class="cursor-pointer px-4 py-2 text-xs text-fg-subtle hover:text-fg">
					{tn(
						unused.length,
						'{n} weiteres gilt hier, wird aber von keinem Plugin genutzt',
						'{n} weitere gelten hier, werden aber von keinem Plugin genutzt'
					)}
				</summary>
				<ol class="divide-y divide-border">
					{#each unused as c (c.id)}{@render item(c)}{/each}
				</ol>
			</details>
		{/if}
	{/if}
</Card>
