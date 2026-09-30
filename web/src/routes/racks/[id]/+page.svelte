<script lang="ts">
	import { page } from '$app/state';
	import RackPage from '$lib/components/racks/RackPage.svelte';

	const id = $derived(Number(page.params.id));
</script>

{#key id}
	<RackPage {id} />
{/key}
