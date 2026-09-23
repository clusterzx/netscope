<!-- Copies text to the clipboard. <CopyButton text={mac} /> -->
<script lang="ts">
	import Button from './Button.svelte';
	import { toast } from '$lib/stores/toast.svelte';

	interface Props {
		text: string;
		label?: string;
		size?: 'xs' | 'sm';
		class?: string;
	}

	let { text, label = 'Kopieren', size = 'xs', class: klass = '' }: Props = $props();
	let done = $state(false);

	async function copy() {
		try {
			await navigator.clipboard.writeText(text);
		} catch {
			// clipboard API unavailable (http): fall back to a hidden textarea
			const ta = document.createElement('textarea');
			ta.value = text;
			ta.style.position = 'fixed';
			ta.style.opacity = '0';
			document.body.appendChild(ta);
			ta.select();
			const ok = document.execCommand('copy');
			ta.remove();
			if (!ok) {
				toast.error('Kopieren nicht möglich');
				return;
			}
		}
		done = true;
		setTimeout(() => (done = false), 1200);
	}
</script>

<Button
	variant="ghost"
	{size}
	icon={done ? 'check' : 'copy'}
	label={done ? 'Kopiert' : label}
	onclick={copy}
	class={klass}
/>
