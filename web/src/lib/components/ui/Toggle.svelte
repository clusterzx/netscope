<!--
	On/off switch (role="switch").
	<Toggle bind:checked={enabled} label="Aktiv" />
	<Toggle checked={p.enabled} onchange={(v) => setEnabled(p, v)} label="Aktiv" hideLabel />
-->
<script lang="ts">
	interface Props {
		checked?: boolean;
		label: string;
		hideLabel?: boolean;
		description?: string;
		disabled?: boolean;
		size?: 'sm' | 'md';
		id?: string;
		class?: string;
		onchange?: (checked: boolean) => void;
	}

	let {
		checked = $bindable(false),
		label,
		hideLabel = false,
		description,
		disabled = false,
		size = 'md',
		id,
		class: klass = '',
		onchange
	}: Props = $props();

	const uid = $props.id();
	const fid = $derived(id ?? `tg-${uid}`);

	function toggle() {
		if (disabled) return;
		checked = !checked;
		onchange?.(checked);
	}
</script>

<div class="inline-flex items-center gap-2.5 {klass}">
	<button
		type="button"
		role="switch"
		id={fid}
		aria-checked={checked}
		aria-label={hideLabel ? label : undefined}
		aria-labelledby={hideLabel ? undefined : `${fid}-l`}
		{disabled}
		onclick={toggle}
		class="relative inline-flex shrink-0 items-center rounded-full border transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-50
			{size === 'sm' ? 'h-4.5 w-8' : 'h-5.5 w-10'}
			{checked ? 'border-accent bg-accent' : 'border-border-strong bg-surface-3'}"
	>
		<span
			class="inline-block rounded-full bg-white shadow-sm transition-transform
				{size === 'sm' ? 'h-3.5 w-3.5' : 'h-4.5 w-4.5'}
				{checked ? (size === 'sm' ? 'translate-x-3.5' : 'translate-x-4.5') : 'translate-x-0.5'}"
		></span>
	</button>
	{#if !hideLabel}
		<span class="flex flex-col">
			<span id="{fid}-l" class="text-sm text-fg">{label}</span>
			{#if description}<span class="text-xs text-fg-subtle">{description}</span>{/if}
		</span>
	{/if}
</div>
