<!--
	Role of external users: ordered group → role list (first match wins), default role and
	whether the directory/provider decides at every sign-in.
	<Provisioning bind:mappings bind:defaultRoleId bind:syncRole {roles} groupHint="…" />
-->
<script lang="ts">
	import type { AuthRoleMapping } from '$lib/api/generated';
	import { Button, Input, Select, Toggle } from '$lib/components/ui';

	interface Props {
		mappings: AuthRoleMapping[];
		defaultRoleId: number;
		syncRole: boolean;
		roles: { id: number; name: string }[];
		/** what a group is for this method (claim value, DN or CN) */
		groupHint: string;
		groupPlaceholder: string;
		errors?: Record<string, string>;
	}

	let {
		mappings = $bindable(),
		defaultRoleId = $bindable(),
		syncRole = $bindable(),
		roles,
		groupHint,
		groupPlaceholder,
		errors = {}
	}: Props = $props();

	const roleOptions = $derived(roles.map((r) => ({ value: String(r.id), label: r.name })));

	function add() {
		mappings = [
			...mappings,
			{ group: '', roleId: roles.find((r) => r.name !== 'Administrator')?.id ?? roles[0]?.id ?? 0 }
		];
	}
	function remove(i: number) {
		mappings = mappings.filter((_, j) => j !== i);
	}
	function move(i: number, d: -1 | 1) {
		const j = i + d;
		if (j < 0 || j >= mappings.length) return;
		const next = [...mappings];
		[next[i], next[j]] = [next[j], next[i]];
		mappings = next;
	}
</script>

<div class="flex flex-col gap-3">
	<div>
		<p class="text-sm font-medium text-fg">Rollen aus Gruppen</p>
		<p class="text-xs text-fg-subtle">
			Von oben nach unten: Die erste Gruppe, der ein Konto angehört, bestimmt die Rolle. {groupHint}
		</p>
	</div>
	{#if mappings.length}
		<ol class="flex flex-col gap-2">
			{#each mappings as m, i (i)}
				<li class="grid grid-cols-[1fr_12rem_auto] items-start gap-2 max-sm:grid-cols-1">
					<Input
						aria-label="Gruppe {i + 1}"
						bind:value={m.group}
						placeholder={groupPlaceholder}
						error={errors[`mappings.${i}.group`]}
						mono
					/>
					<Select
						aria-label="Rolle für Gruppe {i + 1}"
						value={String(m.roleId)}
						options={roleOptions}
						error={errors[`mappings.${i}.roleId`]}
						onchange={(e) => (m.roleId = Number((e.currentTarget as HTMLSelectElement).value))}
					/>
					<div class="flex gap-1">
						<Button
							size="sm"
							variant="ghost"
							icon="arrow-up"
							label="Nach oben"
							disabled={i === 0}
							onclick={() => move(i, -1)}
						/>
						<Button
							size="sm"
							variant="ghost"
							icon="arrow-down"
							label="Nach unten"
							disabled={i === mappings.length - 1}
							onclick={() => move(i, 1)}
						/>
						<Button size="sm" variant="ghost" icon="trash" label="Entfernen" onclick={() => remove(i)} />
					</div>
				</li>
			{/each}
		</ol>
	{/if}
	<div>
		<Button size="sm" icon="plus" onclick={add}>Gruppe zuordnen</Button>
	</div>
	<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
		<Select
			label="Ohne passende Gruppe"
			value={String(defaultRoleId)}
			options={[{ value: '0', label: 'Kein Zugriff' }, ...roleOptions]}
			hint="Rolle für Konten, die keiner Gruppe oben angehören"
			error={errors.defaultRoleId}
			onchange={(e) => (defaultRoleId = Number((e.currentTarget as HTMLSelectElement).value))}
		/>
		<Toggle
			bind:checked={syncRole}
			label="Rolle bei jeder Anmeldung übernehmen"
			description="Aus: Die Gruppen bestimmen die Rolle nur bei der ersten Anmeldung, danach gilt, was in NetScope eingestellt ist."
		/>
	</div>
</div>
