<!-- Theme selection: system / light / dark. -->
<script lang="ts">
	import Menu from '$lib/components/ui/Menu.svelte';
	import { theme } from '$lib/stores/theme.svelte';
	import { t } from '$lib/i18n';

	const icon = $derived(theme.mode === 'system' ? 'monitor' : theme.mode === 'dark' ? 'moon' : 'sun');
	const current = $derived(
		theme.mode === 'system' ? 'System' : theme.mode === 'dark' ? t('Dunkel') : t('Hell')
	);
</script>

<Menu
	label={t('Darstellung: {mode}', { mode: current })}
	{icon}
	items={[
		{
			label: 'System',
			icon: 'monitor',
			hint: theme.mode === 'system' ? '✓' : '',
			onclick: () => theme.set('system')
		},
		{
			label: t('Hell'),
			icon: 'sun',
			hint: theme.mode === 'light' ? '✓' : '',
			onclick: () => theme.set('light')
		},
		{
			label: t('Dunkel'),
			icon: 'moon',
			hint: theme.mode === 'dark' ? '✓' : '',
			onclick: () => theme.set('dark')
		}
	]}
/>
