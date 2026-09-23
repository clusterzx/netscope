<!-- Theme selection: system / light / dark. -->
<script lang="ts">
	import Menu from '$lib/components/ui/Menu.svelte';
	import { theme } from '$lib/stores/theme.svelte';

	const icon = $derived(theme.mode === 'system' ? 'monitor' : theme.mode === 'dark' ? 'moon' : 'sun');
	const current = $derived(theme.mode === 'system' ? 'System' : theme.mode === 'dark' ? 'Dunkel' : 'Hell');
</script>

<Menu
	label="Darstellung: {current}"
	{icon}
	items={[
		{
			label: 'System',
			icon: 'monitor',
			hint: theme.mode === 'system' ? '✓' : '',
			onclick: () => theme.set('system')
		},
		{
			label: 'Hell',
			icon: 'sun',
			hint: theme.mode === 'light' ? '✓' : '',
			onclick: () => theme.set('light')
		},
		{
			label: 'Dunkel',
			icon: 'moon',
			hint: theme.mode === 'dark' ? '✓' : '',
			onclick: () => theme.set('dark')
		}
	]}
/>
