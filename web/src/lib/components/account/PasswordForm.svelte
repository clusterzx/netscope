<!-- Password change of the signed-in user (PUT /api/v1/auth/password). -->
<script lang="ts">
	import { api } from '$lib/api';
	import { apiErrors } from '$lib/components/system/system';
	import { Alert, Button, Input } from '$lib/components/ui';
	import { auth } from '$lib/stores/auth.svelte';

	interface Props {
		/** the current password is the start password an administrator handed out */
		start?: boolean;
		onchanged?: () => void;
	}

	let { start = false, onchanged }: Props = $props();

	const MIN = 10;

	let current = $state('');
	let next = $state('');
	let confirm = $state('');
	let errors = $state<Record<string, string>>({});
	let general = $state<string | null>(null);
	let saving = $state(false);
	let show = $state(false);

	const strength = $derived.by(() => {
		const p = next;
		if (!p) return null;
		let s = 0;
		if (p.length >= MIN) s++;
		if (p.length >= 16) s++;
		if (/[a-z]/.test(p) && /[A-Z]/.test(p)) s++;
		if (/\d/.test(p)) s++;
		if (/[^A-Za-z0-9]/.test(p)) s++;
		return p.length < MIN ? 0 : Math.min(4, s);
	});
	const strengthText = ['zu kurz', 'schwach', 'mittel', 'gut', 'stark'];
	const strengthColor = ['bg-danger', 'bg-danger', 'bg-warn', 'bg-ok', 'bg-ok'];

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		if (!current) e.current = start ? 'Start-Passwort eingeben' : 'Aktuelles Passwort eingeben';
		if (next.length < MIN) e.new = `Mindestens ${MIN} Zeichen`;
		else if (next === current) e.new = 'Das neue Passwort muss sich vom bisherigen unterscheiden';
		if (!e.new && confirm !== next) e.confirm = 'Passwörter stimmen nicht überein';
		return e;
	}

	async function submit() {
		general = null;
		errors = validate();
		if (Object.keys(errors).length) return;
		saving = true;
		try {
			await api.put('/api/v1/auth/password', { body: { current, new: next } });
			current = next = confirm = '';
			onchanged?.();
		} catch (e) {
			// 400 validation: fields "current" (wrong password) and "new" (too short)
			({ errors, general } = apiErrors(e, ['current', 'new']));
		} finally {
			saving = false;
		}
	}
</script>

<form
	novalidate
	class="flex max-w-lg flex-col gap-3"
	onsubmit={(e) => {
		e.preventDefault();
		submit();
	}}
>
	{#if general}<Alert tone="danger">{general}</Alert>{/if}
	<!-- hidden username helps password managers -->
	<input
		type="text"
		name="username"
		autocomplete="username"
		value={auth.me?.user?.username ?? ''}
		hidden
		readonly
	/>
	<Input
		label={start ? 'Start-Passwort' : 'Aktuelles Passwort'}
		type={show ? 'text' : 'password'}
		autocomplete="current-password"
		bind:value={current}
		error={errors.current}
		required
		oninput={() => delete errors.current}
	/>
	<div class="flex flex-col gap-1">
		<Input
			label="Neues Passwort"
			type={show ? 'text' : 'password'}
			autocomplete="new-password"
			bind:value={next}
			error={errors.new}
			hint="Mindestens {MIN} Zeichen"
			required
			oninput={() => delete errors.new}
		/>
		{#if strength !== null}
			<div class="flex items-center gap-2" aria-live="polite">
				<div class="flex h-1.5 flex-1 gap-1">
					{#each [0, 1, 2, 3] as k (k)}
						<span
							class="flex-1 rounded-full {k < Math.max(1, strength)
								? strengthColor[strength]
								: 'bg-surface-3'}"
						></span>
					{/each}
				</div>
				<span class="w-16 text-right text-xs text-fg-subtle">{strengthText[strength]}</span>
			</div>
		{/if}
	</div>
	<Input
		label="Neues Passwort wiederholen"
		type={show ? 'text' : 'password'}
		autocomplete="new-password"
		bind:value={confirm}
		error={errors.confirm}
		required
		oninput={() => delete errors.confirm}
	/>
	<label class="inline-flex items-center gap-2 text-sm text-fg-muted">
		<input type="checkbox" bind:checked={show} class="h-4 w-4 accent-(--accent)" /> Passwörter anzeigen
	</label>
	<div>
		<Button type="submit" variant="primary" icon="save" loading={saving}>Passwort ändern</Button>
	</div>
</form>
