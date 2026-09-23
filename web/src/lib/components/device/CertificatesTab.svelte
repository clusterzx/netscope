<!--
	"Zertifikate" tab: TLS certificates per endpoint with days left (coloured by urgency),
	subject/issuer, SANs, validity, self-signed, chain validity and weak protocols/ciphers.
-->
<script lang="ts">
	import { api } from '$lib/api';
	import type { CertView } from '$lib/api/types';
	import Badge from '$lib/components/ui/Badge.svelte';
	import CopyButton from '$lib/components/ui/CopyButton.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import ErrorState from '$lib/components/ui/ErrorState.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import RelativeTime from '$lib/components/ui/RelativeTime.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import Toggle from '$lib/components/ui/Toggle.svelte';
	import { formatDate, formatDateTime } from '$lib/utils/format';
	import { daysLeftText, daysLeftTone, LazyData, sourceName } from './util';

	interface Props {
		deviceId: number;
		version: number;
		active: boolean;
	}

	let { deviceId, version, active }: Props = $props();

	let history = $state(false);
	const data = new LazyData<CertView[]>();

	$effect(() => {
		if (!active) return;
		const h = history;
		data.ensure(
			`${version}|${h}`,
			async (signal) =>
				((await api.get('/api/v1/devices/{id}/certificates', {
					path: { id: deviceId },
					query: { history: h || undefined },
					signal
				})) ?? []) as CertView[]
		);
	});
	$effect(() => () => data.abort());

	const certs = $derived(
		[...(data.data ?? [])].sort(
			(a, b) => Number(!!a.goneAt) - Number(!!b.goneAt) || a.daysLeft - b.daysLeft || a.port - b.port
		)
	);

	/** share of the validity period that has passed (0..100) */
	function elapsed(c: CertView): number {
		const from = new Date(c.notBefore).getTime();
		const to = new Date(c.notAfter).getTime();
		if (!(to > from)) return 100;
		return Math.max(0, Math.min(100, ((Date.now() - from) / (to - from)) * 100));
	}

	const bar: Record<string, string> = {
		critical: 'bg-sev-critical',
		high: 'bg-sev-high',
		medium: 'bg-sev-medium',
		ok: 'bg-ok'
	};
</script>

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-center gap-3">
		<h2 class="flex-1 text-sm font-semibold">TLS-Zertifikate</h2>
		<Toggle bind:checked={history} label="Frühere Zertifikate anzeigen" size="sm" />
	</div>

	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data}
		<Skeleton rows={4} />
	{:else if !certs.length}
		<div class="rounded-lg border border-border bg-surface">
			<EmptyState
				icon="lock"
				title="Keine Zertifikate"
				description="Der TLS-Scanner liest Zertifikate auf allen TLS-Ports (z. B. 443, 8443) aus."
			/>
		</div>
	{:else}
		{#each certs as c (c.id)}
			{@const tone = daysLeftTone(c.daysLeft)}
			<article
				class="rounded-lg border border-border bg-surface shadow-sm {c.goneAt ? 'opacity-70' : ''}"
				aria-label="Zertifikat {c.subjectCn} auf Port {c.port}"
			>
				<header class="flex flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-border px-4 py-2.5">
					<Icon name="lock" size={16} class="text-fg-subtle" />
					<div class="min-w-0 flex-1">
						<h3 class="truncate text-sm font-semibold" title={c.subjectCn}>{c.subjectCn || '(ohne CN)'}</h3>
						<p class="mono truncate text-xs text-fg-subtle">
							{c.ip}:{c.port}{#if c.serverName}<span class="font-sans"> · SNI {c.serverName}</span>{/if}
						</p>
					</div>
					{#if c.goneAt}
						<Badge tone="neutral">ersetzt/entfernt {formatDate(c.goneAt)}</Badge>
					{/if}
					<Badge {tone} size="md" title="Gültig bis {formatDateTime(c.notAfter)}">
						{daysLeftText(c.daysLeft)}
					</Badge>
				</header>
				<div class="grid grid-cols-1 gap-4 px-4 py-3 lg:grid-cols-2">
					<dl class="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1.5 text-sm">
						<dt class="text-xs text-fg-subtle">Gültigkeit</dt>
						<dd>
							{formatDate(c.notBefore)} – {formatDate(c.notAfter)}
							<span
								class="mt-1 block h-1.5 overflow-hidden rounded-full bg-surface-3"
								role="meter"
								aria-label="Abgelaufener Anteil der Laufzeit"
								aria-valuenow={Math.round(elapsed(c))}
								aria-valuemin={0}
								aria-valuemax={100}
							>
								<span class="block h-full rounded-full {bar[tone] ?? 'bg-ok'}" style="width:{elapsed(c)}%"
								></span>
							</span>
						</dd>
						<dt class="text-xs text-fg-subtle">Aussteller</dt>
						<dd class="break-words">
							{c.issuerCn || '–'}
							{#if c.issuer && c.issuer !== `CN=${c.issuerCn}`}
								<span class="mono block text-xs break-all text-fg-subtle">{c.issuer}</span>
							{/if}
						</dd>
						<dt class="text-xs text-fg-subtle">Vertrauen</dt>
						<dd class="flex flex-wrap items-center gap-1.5">
							{#if c.selfSigned}<Badge tone="warn">selbstsigniert</Badge>{/if}
							{#if c.chainValid}
								<Badge tone="ok">Kette gültig</Badge>
							{:else}
								<Badge tone="danger" title={c.chainError}>Kette ungültig</Badge>
							{/if}
							{#if !c.chainValid && c.chainError}
								<span class="mono block w-full text-xs break-all text-fg-subtle">{c.chainError}</span>
							{/if}
						</dd>
						<dt class="text-xs text-fg-subtle">Alternative Namen</dt>
						<dd class="flex flex-wrap gap-1">
							{#each c.sans ?? [] as s (s)}
								<span class="mono rounded bg-surface-3 px-1.5 text-xs break-all">{s}</span>
							{:else}<span class="text-fg-subtle">–</span>{/each}
						</dd>
					</dl>
					<dl class="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1.5 text-sm">
						<dt class="text-xs text-fg-subtle">Protokolle</dt>
						<dd class="flex flex-wrap gap-1">
							{#each c.versions ?? [] as v (v)}
								<Badge tone={(c.weakProtocols ?? []).includes(v) ? 'danger' : 'neutral'}>{v}</Badge>
							{:else}<span class="text-fg-subtle">–</span>{/each}
							{#each (c.weakProtocols ?? []).filter((w) => !(c.versions ?? []).includes(w)) as w (w)}
								<Badge tone="danger">{w}</Badge>
							{/each}
						</dd>
						<dt class="text-xs text-fg-subtle">Cipher</dt>
						<dd class="mono text-xs break-all">{c.cipher || '–'}</dd>
						<dt class="text-xs text-fg-subtle">Schwache Cipher</dt>
						<dd>
							{#if c.weakCiphers?.length}
								<span class="flex flex-col gap-0.5">
									{#each c.weakCiphers as w (w)}<span class="mono text-xs break-all text-danger">{w}</span
										>{/each}
								</span>
							{:else}
								<span class="text-xs text-ok">keine gefunden</span>
							{/if}
						</dd>
						<dt class="text-xs text-fg-subtle">Schlüssel</dt>
						<dd>
							{c.keyType}{c.keyBits ? ` ${c.keyBits} Bit` : ''}
							<span class="text-xs text-fg-subtle">· {c.signatureAlg}</span>
						</dd>
						<dt class="text-xs text-fg-subtle">Fingerprint</dt>
						<dd class="flex items-start gap-1">
							<span class="mono min-w-0 text-xs break-all text-fg-muted">SHA-256 {c.fingerprint}</span>
							<CopyButton text={c.fingerprint} label="Fingerprint kopieren" />
						</dd>
						<dt class="text-xs text-fg-subtle">Seriennummer</dt>
						<dd class="mono text-xs break-all text-fg-muted">{c.serial || '–'}</dd>
						<dt class="text-xs text-fg-subtle">Gesehen</dt>
						<dd class="text-xs text-fg-muted">
							seit {formatDateTime(c.firstSeen)} · zuletzt <RelativeTime value={c.lastSeen} /> ({sourceName(
								c.source
							)})
						</dd>
					</dl>
				</div>
			</article>
		{/each}
	{/if}
</div>
