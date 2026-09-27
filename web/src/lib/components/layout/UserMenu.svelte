<script lang="ts">
	import Menu from '$lib/components/ui/Menu.svelte';
	import type { MenuItem } from '$lib/components/ui/Menu.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { t } from '$lib/i18n';

	const name = $derived(
		auth.me?.user?.displayName || auth.me?.user?.username || auth.me?.principal?.username || t('Benutzer')
	);
	const role = $derived(auth.me?.principal?.roleName ?? '');
	const items = $derived<MenuItem[]>([
		{ separator: true, label: role ? `${name} · ${role}` : name },
		{ label: t('Konto & Zwei-Faktor'), icon: 'user', href: '/system?tab=account' },
		{ label: t('API-Tokens'), icon: 'key', href: '/system?tab=tokens' },
		...(auth.can('users.manage')
			? [{ label: t('Benutzer & Rollen'), icon: 'shield', href: '/system?tab=users' } as MenuItem]
			: []),
		{ label: t('API-Dokumentation'), icon: 'external', href: '/api/docs' },
		{ separator: true },
		{ label: t('Abmelden'), icon: 'logout', onclick: () => auth.logout() }
	]);
</script>

<Menu label={t('Benutzermenü ({name})', { name })} icon="user" {items} />
