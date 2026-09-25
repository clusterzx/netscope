<script lang="ts">
	import Menu from '$lib/components/ui/Menu.svelte';
	import type { MenuItem } from '$lib/components/ui/Menu.svelte';
	import { auth } from '$lib/stores/auth.svelte';

	const name = $derived(
		auth.me?.user?.displayName || auth.me?.user?.username || auth.me?.principal?.username || 'Benutzer'
	);
	const role = $derived(auth.me?.principal?.roleName ?? '');
	const items = $derived<MenuItem[]>([
		{ separator: true, label: role ? `${name} · ${role}` : name },
		{ label: 'Konto & Zwei-Faktor', icon: 'user', href: '/system?tab=account' },
		{ label: 'API-Tokens', icon: 'key', href: '/system?tab=tokens' },
		...(auth.can('users.manage')
			? [{ label: 'Benutzer & Rollen', icon: 'shield', href: '/system?tab=users' } as MenuItem]
			: []),
		{ label: 'API-Dokumentation', icon: 'external', href: '/api/docs' },
		{ separator: true },
		{ label: 'Abmelden', icon: 'logout', onclick: () => auth.logout() }
	]);
</script>

<Menu label="Benutzermenü ({name})" icon="user" {items} />
