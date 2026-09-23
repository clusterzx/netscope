<!--
	Editor for one rule action: publisher, priority, mode (immediate/batch), throttle, quiet
	hours and escalation. Error keys: action.<i>.publisher|batchMinutes|throttle|quiet|escalate.
-->
<script lang="ts">
	import type { PublisherInfo, RuleAction } from '$lib/api';
	import { Alert, Button, Checkbox, Input, Select } from '$lib/components/ui';
	import { PRIORITIES, durationText, fieldMessage, priorityLabel } from './rule';

	interface Props {
		action: RuleAction;
		index: number;
		publishers: PublisherInfo[];
		errors?: Record<string, string>;
		canRemove?: boolean;
		onremove?: () => void;
		onmove?: (dir: -1 | 1) => void;
		count?: number;
	}

	let {
		action = $bindable(),
		index,
		publishers,
		errors = {},
		canRemove = true,
		onremove,
		onmove,
		count = 1
	}: Props = $props();

	const idp = $derived(`act-${index}`);
	/** error of a field of this action (keys are the backend JSON paths, e.g. actions.0.throttle) */
	const err = (k: string) => fieldMessage(errors[`actions.${index}.${k}`]);

	const publisherOptions = $derived([
		...publishers.map((p) => ({ value: p.id, label: p.enabled ? p.name : `${p.name} (inaktiv)` })),
		...(action.publisher && !publishers.some((p) => p.id === action.publisher)
			? [{ value: action.publisher, label: `${action.publisher} (unbekannt)` }]
			: [])
	]);
	const selected = $derived(publishers.find((p) => p.id === action.publisher));
	const priorityOptions = PRIORITIES.map((p) => ({ value: p, label: priorityLabel[p] }));

	const quiet = $derived(!!action.quietHours);
	const escalate = $derived(action.escalateAfterMinutes !== undefined);

	function setQuiet(on: boolean) {
		action.quietHours = on
			? (action.quietHours ?? { from: '22:00', to: '07:00', behavior: 'delay', allowUrgent: true })
			: undefined;
	}

	function setEscalate(on: boolean) {
		if (on) action.escalateAfterMinutes = action.escalateAfterMinutes || 60;
		else {
			action.escalateAfterMinutes = undefined;
			action.escalatePublisher = undefined;
			action.escalatePriority = undefined;
		}
	}

	function setMode(m: string) {
		action.mode = m;
		if (m === 'batch' && !action.batchMinutes) action.batchMinutes = 60;
	}
</script>

<fieldset class="flex flex-col gap-4 rounded-lg border border-border bg-surface-2/40 p-3.5">
	<legend class="sr-only">Aktion {index + 1}</legend>
	<div class="flex items-center gap-2">
		<span
			class="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-accent-soft text-xs font-semibold text-accent"
			aria-hidden="true">{index + 1}</span
		>
		<span class="flex-1 text-sm font-medium">Aktion {index + 1}</span>
		{#if count > 1 && onmove}
			<Button
				size="xs"
				variant="ghost"
				icon="arrow-up"
				label="Aktion nach oben"
				disabled={index === 0}
				onclick={() => onmove(-1)}
			/>
			<Button
				size="xs"
				variant="ghost"
				icon="arrow-down"
				label="Aktion nach unten"
				disabled={index === count - 1}
				onclick={() => onmove(1)}
			/>
		{/if}
		{#if canRemove}
			<Button
				size="xs"
				variant="ghost"
				icon="trash"
				label="Aktion {index + 1} entfernen"
				onclick={onremove}
			/>
		{/if}
	</div>

	<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
		<Select
			id="{idp}-publisher"
			label="Publisher"
			required
			options={publisherOptions}
			placeholder="Publisher wählen …"
			bind:value={action.publisher}
			error={err('publisher')}
		/>
		<Select
			id="{idp}-priority"
			label="Priorität"
			options={priorityOptions}
			bind:value={action.priority}
			error={err('priority')}
		/>
	</div>
	{#if selected && !selected.enabled}
		<Alert tone="warn">
			Der Publisher „{selected.name}“ ist inaktiv – Benachrichtigungen dieser Aktion werden übersprungen.
			<a href="/plugins/{encodeURIComponent(selected.id)}" class="link">Publisher einrichten</a>
		</Alert>
	{/if}

	<fieldset class="flex flex-col gap-2">
		<legend class="mb-1 text-[0.8125rem] font-medium text-fg">Zustellung</legend>
		<div class="flex flex-wrap gap-x-5 gap-y-2">
			<label class="flex cursor-pointer items-start gap-2 text-sm">
				<input
					type="radio"
					name="{idp}-mode"
					class="mt-0.5 accent-(--accent)"
					checked={action.mode !== 'batch'}
					onchange={() => setMode('immediate')}
				/>
				<span
					>Sofort <span class="block text-xs text-fg-subtle">Events eines Laufs werden gebündelt</span></span
				>
			</label>
			<label class="flex cursor-pointer items-start gap-2 text-sm">
				<input
					type="radio"
					name="{idp}-mode"
					class="mt-0.5 accent-(--accent)"
					checked={action.mode === 'batch'}
					onchange={() => setMode('batch')}
				/>
				<span
					>Sammeln <span class="block text-xs text-fg-subtle">über einen Zeitraum zusammenfassen</span></span
				>
			</label>
		</div>
		{#if err('mode')}<p class="text-xs text-danger">{err('mode')}</p>{/if}
		{#if action.mode === 'batch'}
			<Input
				id="{idp}-batch"
				type="number"
				label="Sammelzeitraum (Minuten)"
				min={1}
				max={1440}
				bind:value={action.batchMinutes}
				error={err('batchMinutes')}
				hint="1–1440 Minuten"
				class="sm:w-64"
			/>
		{/if}
	</fieldset>

	<Input
		id="{idp}-throttle"
		label="Drosselung"
		placeholder="z. B. 24h"
		bind:value={action.throttle}
		error={err('throttle')}
		hint={action.throttle?.trim()
			? `Höchstens eine Benachrichtigung je Event-Typ und Gerät alle ${durationText(action.throttle.trim())}.`
			: 'Leer = keine Drosselung. Dauer wie 30m, 6h, 24h.'}
		class="sm:w-80"
		mono
	/>

	<div class="flex flex-col gap-2">
		<Checkbox
			id="{idp}-quiet"
			checked={quiet}
			onchange={(e) => setQuiet((e.currentTarget as HTMLInputElement).checked)}
			label="Ruhezeiten"
			description="In diesem Zeitraum wird nicht sofort benachrichtigt."
		/>
		{#if quiet && action.quietHours}
			<div class="grid grid-cols-2 gap-3 sm:ml-6 sm:grid-cols-4">
				<Input
					id="{idp}-qfrom"
					type="time"
					label="Von"
					bind:value={action.quietHours.from}
					error={err('quietHours.from')}
				/>
				<Input
					id="{idp}-qto"
					type="time"
					label="Bis"
					bind:value={action.quietHours.to}
					error={err('quietHours.to')}
				/>
				<Select
					id="{idp}-qbehavior"
					label="Verhalten"
					options={[
						{ value: 'delay', label: 'Bis zum Ende verzögern' },
						{ value: 'drop', label: 'Verwerfen' }
					]}
					bind:value={action.quietHours.behavior}
					error={err('quietHours.behavior')}
					class="col-span-2"
				/>
				<Checkbox
					id="{idp}-qurgent"
					bind:checked={action.quietHours.allowUrgent}
					label="Dringende trotzdem sofort senden"
					class="col-span-2 sm:col-span-4"
				/>
			</div>
		{/if}
	</div>

	<div class="flex flex-col gap-2">
		<Checkbox
			id="{idp}-esc"
			checked={escalate}
			onchange={(e) => setEscalate((e.currentTarget as HTMLInputElement).checked)}
			label="Eskalation"
			description="Erneut benachrichtigen, wenn das Event nicht rechtzeitig quittiert wird."
		/>
		{#if escalate}
			<div class="grid grid-cols-1 gap-3 sm:ml-6 sm:grid-cols-3">
				<Input
					id="{idp}-escmin"
					type="number"
					label="Nach (Minuten)"
					min={1}
					max={10080}
					bind:value={action.escalateAfterMinutes}
					error={err('escalateAfterMinutes')}
				/>
				<Select
					id="{idp}-escpub"
					label="An Publisher"
					options={publishers.map((p) => ({
						value: p.id,
						label: p.enabled ? p.name : `${p.name} (inaktiv)`
					}))}
					placeholder="Gleicher Publisher"
					bind:value={action.escalatePublisher}
					error={err('escalatePublisher')}
				/>
				<Select
					id="{idp}-escprio"
					label="Priorität"
					options={priorityOptions}
					placeholder="Dringend (Standard)"
					bind:value={action.escalatePriority}
					error={err('escalatePriority')}
				/>
			</div>
		{/if}
	</div>
</fieldset>
