<!-- Own account: user and role, language, password change and the second factor. -->
<script lang="ts">
	import MfaCard from '$lib/components/account/MfaCard.svelte';
	import PasswordForm from '$lib/components/account/PasswordForm.svelte';
	import { Card, DescItem, DescList, Select } from '$lib/components/ui';
	import { errorMessage } from '$lib/api';
	import { LOCALES, browserLocale, t, type LocalePreference } from '$lib/i18n';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import { formatDateTime } from '$lib/utils/format';

	const session = $derived(auth.me?.principal?.kind === 'session');
	const source = $derived(auth.me?.user?.authSource ?? 'local');
	const sourceLabel: Record<string, string> = {
		local: t('NetScope-Konto'),
		ldap: t('Verzeichnis (LDAP)'),
		oidc: t('Identity Provider (SSO)')
	};

	// the language names stay in their own language, so everyone finds theirs
	const browserName = LOCALES.find((l) => l.value === browserLocale())?.label ?? 'Deutsch';
	const languageOptions = [
		{ value: '', label: t('Wie im Browser ({language})', { language: browserName }) },
		...LOCALES
	];
	let language = $state<string>(auth.me?.user?.locale ?? '');
	let savingLanguage = $state(false);

	async function changeLanguage() {
		savingLanguage = true;
		try {
			await auth.setLocale(language as LocalePreference);
		} catch (e) {
			language = auth.me?.user?.locale ?? '';
			toast.error(errorMessage(e));
		} finally {
			savingLanguage = false;
		}
	}
</script>

<div class="grid grid-cols-1 items-start gap-4 xl:grid-cols-[1fr_1.4fr]">
	<div class="flex flex-col gap-4">
		<Card title={t('Benutzer')} icon="user">
			<DescList>
				<DescItem label={t('Benutzername')} value={auth.me?.user?.username} />
				{#if auth.me?.user?.displayName}<DescItem label="Name" value={auth.me.user.displayName} />{/if}
				{#if auth.me?.user?.email}<DescItem label={t('E-Mail')} value={auth.me.user.email} />{/if}
				<DescItem label={t('Rolle')} value={auth.me?.principal?.roleName} />
				<DescItem label={t('Konto')} value={sourceLabel[source] ?? source} />
				<DescItem label={t('Angemeldet über')} value={session ? t('Browser-Sitzung') : t('API-Token')} />
				<DescItem label={t('Letzte Anmeldung')} value={formatDateTime(auth.me?.user?.lastLoginAt)} />
				<DescItem label={t('Konto angelegt')} value={formatDateTime(auth.me?.user?.createdAt)} />
			</DescList>
		</Card>
		{#if session}
			<Card
				title={t('Sprache')}
				description={t('Sprache der Oberfläche und der Texte, die NetScope dir anzeigt')}
				icon="globe"
			>
				<Select
					label={t('Sprache')}
					bind:value={language}
					options={languageOptions}
					disabled={savingLanguage}
					onchange={changeLanguage}
				/>
			</Card>
		{/if}
		{#if source === 'local'}
			<Card
				title={t('Passwort ändern')}
				description={t('Beendet alle anderen Sitzungen; diese bleibt angemeldet')}
				icon="lock"
			>
				<PasswordForm
					onchanged={() => toast.success(t('Passwort geändert. Alle anderen Sitzungen wurden abgemeldet.'))}
				/>
			</Card>
		{:else}
			<Card title={t('Passwort')} icon="lock">
				<p class="text-sm text-fg-muted">
					{source === 'ldap'
						? t('Das Passwort gehört zum Konto im Verzeichnis und wird dort geändert.')
						: t('Die Anmeldung läuft über den Identity Provider; ein NetScope-Passwort gibt es nicht.')}
				</p>
			</Card>
		{/if}
	</div>
	{#if session && source !== 'oidc'}
		<MfaCard />
	{:else if session}
		<Card title={t('Zwei-Faktor-Anmeldung')} icon="shield">
			<p class="text-sm text-fg-muted">
				{t('Über den Identity Provider entscheidet dieser über den zweiten Faktor.')}
			</p>
		</Card>
	{/if}
</div>
