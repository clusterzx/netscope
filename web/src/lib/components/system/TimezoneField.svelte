<!--
	Time zone (IANA name) with a searchable list of the browser's time zones.
	<TimezoneField bind:value={tz} error={errors.timezone} />
-->
<script lang="ts">
	import { Input } from '$lib/components/ui';
	import { t } from '$lib/i18n';
	import { timezones } from './timezones';

	interface Props {
		value: string;
		label?: string;
		hint?: string;
		error?: string | null;
		id?: string;
		class?: string;
	}

	let {
		value = $bindable(),
		label = t('Zeitzone'),
		hint,
		error,
		id = 'timezone',
		class: klass = ''
	}: Props = $props();
</script>

<Input
	{id}
	{label}
	{hint}
	{error}
	class={klass}
	bind:value
	list="{id}-list"
	autocomplete="off"
	spellcheck={false}
	placeholder="Europe/Berlin"
	required
/>
<datalist id="{id}-list">
	{#each timezones as z (z)}<option value={z}></option>{/each}
</datalist>
