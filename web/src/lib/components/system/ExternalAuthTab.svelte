<!-- Central sign-in: OIDC (identity provider) and LDAP / Active Directory. -->
<script lang="ts">
	import { api } from '$lib/api';
	import type { ApiExternalAuthResponse } from '$lib/api/generated';
	import { Alert, Card, ErrorState, Skeleton } from '$lib/components/ui';
	import { AsyncData } from '$lib/stores/resource.svelte';
	import LdapForm from './auth/LdapForm.svelte';
	import OidcForm from './auth/OidcForm.svelte';

	const data = new AsyncData<ApiExternalAuthResponse>();
	$effect(() => {
		data.run((signal) => api.get('/api/v1/system/auth', { signal }));
	});
	let roles = $state<{ id: number; name: string }[]>([]);
	$effect(() => {
		api
			.get('/api/v1/roles')
			.then((r) => (roles = (r ?? []).map((x) => ({ id: x.id, name: x.name }))))
			.catch(() => {});
	});
</script>

<div class="flex flex-col gap-4">
	<Alert tone="info" title="Wie Konten entstehen">
		Wer sich zum ersten Mal über LDAP oder OIDC anmeldet, bekommt automatisch ein Konto – mit der Rolle aus
		seinen Gruppen. Solche Konten haben kein NetScope-Passwort und lassen sich unter „Benutzer“ deaktivieren.
		Mindestens ein lokaler Administrator bleibt immer bestehen: Er ist der Notzugang, wenn der
		Verzeichnisdienst ausfällt.
	</Alert>
	{#if data.error && !data.data}
		<ErrorState error={data.error} onretry={() => data.reload()} />
	{:else if !data.data || !roles.length}
		<Card><Skeleton lines={8} /></Card>
	{:else}
		<OidcForm
			config={data.data.oidc}
			redirectUrl={data.data.redirectUrl}
			{roles}
			onsaved={(c) => data.data && data.set({ ...data.data, oidc: c })}
		/>
		<LdapForm
			config={data.data.ldap}
			{roles}
			onsaved={(c) => data.data && data.set({ ...data.data, ldap: c })}
		/>
	{/if}
</div>
