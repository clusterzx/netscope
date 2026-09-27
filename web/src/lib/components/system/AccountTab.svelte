<!-- Own account: user and role, password change and the second factor. -->
<script lang="ts">
	import MfaCard from '$lib/components/account/MfaCard.svelte';
	import PasswordForm from '$lib/components/account/PasswordForm.svelte';
	import { Card, DescItem, DescList } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDateTime } from '$lib/utils/format';

	const session = $derived(auth.me?.principal?.kind === 'session');
	const source = $derived(auth.me?.user?.authSource ?? 'local');
	const sourceLabel: Record<string, string> = {
		local: 'NetScope-Konto',
		ldap: 'Verzeichnis (LDAP)',
		oidc: 'Identity Provider (SSO)'
	};
</script>

<div class="grid grid-cols-1 items-start gap-4 xl:grid-cols-[1fr_1.4fr]">
	<div class="flex flex-col gap-4">
		<Card title="Benutzer" icon="user">
			<DescList>
				<DescItem label="Benutzername" value={auth.me?.user?.username} />
				{#if auth.me?.user?.displayName}<DescItem label="Name" value={auth.me.user.displayName} />{/if}
				{#if auth.me?.user?.email}<DescItem label="E-Mail" value={auth.me.user.email} />{/if}
				<DescItem label="Rolle" value={auth.me?.principal?.roleName} />
				<DescItem label="Konto" value={sourceLabel[source] ?? source} />
				<DescItem label="Angemeldet über" value={session ? 'Browser-Sitzung' : 'API-Token'} />
				<DescItem label="Letzte Anmeldung" value={formatDateTime(auth.me?.user?.lastLoginAt)} />
				<DescItem label="Konto angelegt" value={formatDateTime(auth.me?.user?.createdAt)} />
			</DescList>
		</Card>
		{#if source === 'local'}
			<Card
				title="Passwort ändern"
				description="Beendet alle anderen Sitzungen; diese bleibt angemeldet"
				icon="lock"
			>
				<PasswordForm
					onchanged={() => toast.success('Passwort geändert. Alle anderen Sitzungen wurden abgemeldet.')}
				/>
			</Card>
		{:else}
			<Card title="Passwort" icon="lock">
				<p class="text-sm text-fg-muted">
					{source === 'ldap'
						? 'Das Passwort gehört zum Konto im Verzeichnis und wird dort geändert.'
						: 'Die Anmeldung läuft über den Identity Provider; ein NetScope-Passwort gibt es nicht.'}
				</p>
			</Card>
		{/if}
	</div>
	{#if session && source !== 'oidc'}
		<MfaCard />
	{:else if session}
		<Card title="Zwei-Faktor-Anmeldung" icon="shield">
			<p class="text-sm text-fg-muted">
				Über den Identity Provider entscheidet dieser über den zweiten Faktor.
			</p>
		</Card>
	{/if}
</div>
