<!--
	A port drawn like the real thing: RJ45 socket with latch notch and link LED, SFP cage,
	keystone of a patch panel or a PDU outlet. A plug sits in it in the colour of its cable
	(white for a device set by hand, translucent for a detected one). Interactive sockets are
	buttons; the legend and lists use the same drawing as a plain span.
-->
<script lang="ts" module>
	export type SocketKind = 'rj45' | 'sfp' | 'keystone' | 'outlet';
</script>

<script lang="ts">
	import type { RackPort } from '$lib/api';
	import { cableCss, portState, portTitle } from './rack';

	interface Props {
		/** the port (omit for a legend sample with `state`) */
		port?: RackPort | null;
		state?: 'cable' | 'device' | 'detected' | 'up' | 'free' | 'disabled';
		kind?: SocketKind;
		/** width in px (height follows the kind) */
		width?: number;
		/** row of the socket: the latch of the top row points down, of the bottom row up */
		row?: 'top' | 'bottom';
		selected?: boolean;
		connecting?: boolean;
		color?: string;
		onclick?: () => void;
	}

	let {
		port = null,
		state,
		kind = 'rj45',
		width = 14,
		row = 'top',
		selected = false,
		connecting = false,
		color,
		onclick
	}: Props = $props();

	const st = $derived(state ?? (port ? portState(port) : 'free'));
	const plug = $derived.by(() => {
		if (st === 'cable') return color ?? (port?.cables[0] ? cableCss(port.cables[0].color) : '#3b82f6');
		if (st === 'device') return '#e5e7eb';
		return null;
	});
	const led = $derived(
		st === 'cable' || st === 'device' || st === 'up' ? 'on' : st === 'detected' ? 'warn' : 'off'
	);
	const height = $derived(
		kind === 'sfp'
			? Math.round(width * 0.55)
			: kind === 'keystone'
				? Math.round(width * 0.95)
				: kind === 'outlet'
					? width
					: Math.round(width * 0.8)
	);
	const title = $derived(port ? portTitle(port) : undefined);
</script>

<svelte:element
	this={onclick ? 'button' : 'span'}
	type={onclick ? 'button' : undefined}
	class="socket {kind} {row} led-{led} {st === 'detected' ? 'detected' : ''} {st === 'disabled'
		? 'disabled'
		: ''} {selected ? 'selected' : ''} {connecting ? 'connecting' : ''}"
	style="width:{width}px;height:{height}px;--plug:{plug ?? 'transparent'}"
	{title}
	aria-label={onclick ? title : undefined}
	aria-pressed={onclick ? selected : undefined}
	aria-hidden={onclick ? undefined : 'true'}
	onpointerdown={onclick ? (e: PointerEvent) => e.stopPropagation() : undefined}
	onclick={onclick
		? (e: MouseEvent) => {
				e.stopPropagation();
				onclick();
			}
		: undefined}
>
	{#if plug}<span class="plug"></span>{/if}
	{#if kind === 'rj45'}<span class="led"></span>{/if}
</svelte:element>

<style>
	.socket {
		position: relative;
		display: inline-block;
		flex: none;
		padding: 0;
		border-radius: 1.5px;
		cursor: default;
		outline: none;
		transition:
			box-shadow 120ms,
			transform 120ms;
	}
	button.socket {
		cursor: pointer;
	}
	button.socket:hover {
		box-shadow: 0 0 0 1.5px rgb(148 163 184 / 0.9);
	}
	button.socket.connecting:hover {
		box-shadow:
			0 0 0 2px #60a5fa,
			0 0 8px #60a5fa;
	}
	button.socket:focus-visible,
	.socket.selected {
		box-shadow:
			0 0 0 2px #60a5fa,
			0 0 10px rgb(96 165 250 / 0.8);
		z-index: 1;
	}

	/* RJ45: dark hole with a lighter rim, latch notch towards the middle of the pair */
	.rj45 {
		background: linear-gradient(180deg, #030405 0%, #14171b 100%);
		border: 1px solid #4b525c;
		box-shadow: inset 0 1px 2px rgb(0 0 0 / 0.9);
	}
	.rj45::after {
		content: '';
		position: absolute;
		left: 32%;
		right: 32%;
		height: 2px;
		background: #4b525c;
	}
	.rj45.top::after {
		bottom: -1px;
	}
	.rj45.bottom::after {
		top: -1px;
	}
	.rj45 .led {
		position: absolute;
		top: 1px;
		left: 1px;
		width: 3px;
		height: 2px;
		border-radius: 0.5px;
		background: #2a2f36;
	}
	.rj45.bottom .led {
		top: auto;
		bottom: 1px;
	}
	.led-on .led {
		background: #4ade80;
		box-shadow: 0 0 4px #22c55e;
	}
	.led-warn .led {
		background: #fbbf24;
		box-shadow: 0 0 4px #f59e0b;
		animation: blink 1.6s ease-in-out infinite;
	}

	/* SFP: metal cage */
	.sfp {
		background: linear-gradient(180deg, #0b0c0e, #1b1e23);
		border: 1.5px solid #a3acb8;
		border-radius: 1px;
		box-shadow:
			inset 0 0 0 1px #4b525c,
			0 0 0 0.5px #2a2f36;
	}
	.sfp.led-on {
		border-color: #c9d1db;
	}

	/* keystone of a patch panel: white frame */
	.keystone {
		background: radial-gradient(ellipse at 50% 60%, #050607 55%, #1f2328 100%);
		border: 2px solid #d9dde3;
		border-radius: 2px;
		box-shadow: 0 0.5px 0 #6b7280;
	}

	/* PDU outlet (C13) */
	.outlet {
		background: #e5e7eb;
		border-radius: 3px 3px 5px 5px;
		border: 1px solid #9ca3af;
	}
	.outlet::before,
	.outlet::after {
		content: '';
		position: absolute;
		top: 35%;
		width: 2px;
		height: 30%;
		background: #1f2937;
		border-radius: 1px;
	}
	.outlet::before {
		left: 28%;
	}
	.outlet::after {
		right: 28%;
	}
	.outlet.led-on {
		background: #f8fafc;
	}

	/* plug in the socket, in the colour of its cable */
	.plug {
		position: absolute;
		inset: 1px 1.5px;
		border-radius: 1.5px;
		background: linear-gradient(
			180deg,
			color-mix(in srgb, var(--plug) 75%, white) 0%,
			var(--plug) 45%,
			color-mix(in srgb, var(--plug) 70%, black) 100%
		);
		box-shadow:
			0 0 0 0.5px rgb(0 0 0 / 0.5),
			0 1px 2px rgb(0 0 0 / 0.6);
	}
	.outlet .plug {
		inset: 0;
		border-radius: 3px;
	}
	.detected::before {
		content: '';
		position: absolute;
		inset: 1px 1.5px;
		border: 1px dashed rgb(251 191 36 / 0.8);
		border-radius: 1.5px;
	}
	.disabled {
		opacity: 0.45;
	}

	@keyframes blink {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.35;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.led-warn .led {
			animation: none;
		}
	}
</style>
