<script lang="ts">
	import Menu from '$lib/components/ui/Menu.svelte';
	import { auth } from '$lib/stores/auth.svelte';

	const name = $derived(auth.me?.user?.username ?? auth.me?.principal?.username ?? 'Benutzer');
</script>

<Menu
	label="Benutzermenü ({name})"
	icon="user"
	items={[
		{ separator: true, label: name },
		{ label: 'Passwort & API-Tokens', icon: 'key', href: '/system' },
		{ label: 'API-Dokumentation', icon: 'external', href: '/api/docs' },
		{ separator: true },
		{ label: 'Abmelden', icon: 'logout', onclick: () => auth.logout() }
	]}
/>
