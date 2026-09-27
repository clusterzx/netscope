<!-- State badge of a WireGuard tunnel. <TunnelState status={subnet.tunnel} /> -->
<script lang="ts">
	import type { TunnelStatus } from '$lib/api';
	import Badge from '$lib/components/ui/Badge.svelte';
	import { t } from '$lib/i18n';

	interface Props {
		status: TunnelStatus | null | undefined;
	}

	let { status }: Props = $props();

	const view = $derived.by(() => {
		switch (status?.state) {
			case 'up':
				return { tone: 'ok', label: t('Tunnel verbunden') } as const;
			case 'connecting':
				return { tone: 'warn', label: t('Tunnel verbindet …') } as const;
			case 'down':
				return { tone: 'danger', label: t('Tunnel getrennt') } as const;
			case 'error':
				return { tone: 'danger', label: t('Tunnel-Fehler') } as const;
			default:
				return { tone: 'neutral', label: t('Tunnel inaktiv') } as const;
		}
	});
</script>

<Badge tone={view.tone} dot title={status?.error || undefined}>{view.label}</Badge>
