<!--
	Inline SVG icon from icons.ts. Decorative by default (aria-hidden); pass `label` to make it
	meaningful for screen readers.
	<Icon name="search" size={16} />
-->
<script lang="ts">
	import { icons, type IconName } from './icons';

	interface Props {
		name: IconName;
		size?: number;
		strokeWidth?: number;
		label?: string;
		class?: string;
	}

	let { name, size = 16, strokeWidth = 2, label, class: klass = '' }: Props = $props();
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width={strokeWidth}
	stroke-linecap="round"
	stroke-linejoin="round"
	class="shrink-0 {klass}"
	aria-hidden={label ? undefined : 'true'}
	role={label ? 'img' : undefined}
	aria-label={label}
>
	{#each icons[name] as d, i (i)}
		<path {d} />
	{/each}
</svg>
