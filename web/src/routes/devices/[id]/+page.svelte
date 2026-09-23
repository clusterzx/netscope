<script lang="ts">
	import { page } from '$app/state';
	import DevicePage from '$lib/components/device/DevicePage.svelte';

	const id = $derived(Number(page.params.id));
</script>

<!-- a fresh component per device: resets all state and aborts pending requests on navigation -->
{#key id}
	<DevicePage {id} />
{/key}
