<!-- Step 1: language (of the first user and of notifications) and time zone. -->
<script lang="ts">
	import TimezoneField from '$lib/components/system/TimezoneField.svelte';
	import { knownTimezone } from '$lib/components/system/timezones';
	import { applyPreference, LOCALES, t, type Locale } from '$lib/i18n';
	import { wizard } from './wizard.svelte';

	let error = $state('');

	function choose(l: Locale) {
		wizard.data.language = l;
		wizard.save();
		// the page shows one language for its lifetime: reload to switch
		if (applyPreference(l)) window.location.reload();
	}

	/** Checks the step; false keeps the wizard here. */
	export function validate(): boolean {
		error = knownTimezone(wizard.data.timezone) ? '' : t('Unbekannte Zeitzone');
		return !error;
	}
</script>

<div class="flex flex-col gap-6">
	<fieldset>
		<legend class="mb-2 text-sm font-medium text-fg">{t('Sprache')}</legend>
		<div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
			{#each LOCALES as l (l.value)}
				<label
					class="flex cursor-pointer items-center gap-3 rounded-lg border px-4 py-3 transition-colors
					{wizard.data.language === l.value
						? 'border-accent bg-accent-soft'
						: 'border-border hover:border-border-strong'}"
				>
					<input
						type="radio"
						name="language"
						value={l.value}
						checked={wizard.data.language === l.value}
						onchange={() => choose(l.value)}
						class="accent-accent"
					/>
					<!-- the language names stay in their own language -->
					<span class="font-medium text-fg">{l.label}</span>
				</label>
			{/each}
		</div>
		<p class="mt-2 text-xs text-fg-subtle">
			{t(
				'Sprache der Oberfläche für dein Konto sowie der Benachrichtigungen und Berichte. Beides lässt sich später ändern.'
			)}
		</p>
	</fieldset>

	<TimezoneField
		id="welcome-timezone"
		bind:value={wizard.data.timezone}
		{error}
		hint={t('Für Zeitpläne, Berichte und Datumsangaben. Vorschlag: die Zeitzone dieses Browsers.')}
	/>
</div>
