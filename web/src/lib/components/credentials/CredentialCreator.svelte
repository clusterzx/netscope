<!--
	Lets credential-ref fields inside create credentials on the spot ("Neu anlegen"): the
	dialog offers the types the field accepts, the new credential is selected at once.
	<CredentialCreator><form>… <SchemaForm …/> …</form></CredentialCreator>
	(The dialog is rendered after the children, so it never ends up inside their form.)
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { api } from '$lib/api';
	import type { Credential, CredentialType } from '$lib/api';
	import { credentials } from '$lib/stores/catalog.svelte';
	import { toast } from '$lib/stores/toast.svelte';
	import CredentialForm from './CredentialForm.svelte';
	import { provideCredentialCreator } from './creator';

	let { children }: { children: Snippet } = $props();

	let all = $state<CredentialType[] | null>(null);
	let wanted = $state<string[]>([]);
	let open = $state(false);
	let done: ((c: Credential) => void) | null = null;

	const types = $derived(
		(all ?? []).filter((ct) => (wanted.length ? wanted.includes(ct.type) : ct.type !== 'wireguard'))
	);

	provideCredentialCreator(async (t, fn) => {
		try {
			all ??= (await api.get('/api/v1/credentials/types')) ?? [];
		} catch (e) {
			toast.error(e);
			return;
		}
		wanted = t;
		done = fn;
		open = true;
	});

	async function saved(c: Credential) {
		await credentials.refresh().catch(() => {});
		done?.(c);
		done = null;
	}
</script>

{@render children()}
<CredentialForm bind:open credential={null} {types} onsaved={saved} />
