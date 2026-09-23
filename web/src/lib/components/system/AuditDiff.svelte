<!--
	Before/after view of an audit entry (flat key diff + raw JSON).
	<AuditDiff before={entry.before} after={entry.after} />
-->
<script lang="ts">
	import { JsonView } from '$lib/components/ui';
	import { fmtValue, jsonDiff } from './system';

	interface Props {
		before: unknown;
		after: unknown;
	}

	let { before, after }: Props = $props();

	let showAll = $state(false);
	let raw = $state(false);

	const rows = $derived(jsonDiff(before, after));
	const changed = $derived(rows.filter((r) => r.kind !== 'same'));
	const shown = $derived(showAll ? rows : changed);
	const hasBefore = $derived(before !== undefined && before !== null);
	const hasAfter = $derived(after !== undefined && after !== null);

	const kindCls: Record<string, string> = {
		added: 'bg-ok-soft',
		removed: 'bg-danger-soft',
		changed: 'bg-warn-soft',
		same: ''
	};
	const kindLabel: Record<string, string> = {
		added: 'neu',
		removed: 'entfernt',
		changed: 'geändert',
		same: ''
	};
</script>

<div class="flex flex-col gap-2">
	<div class="flex flex-wrap items-center gap-3 text-xs">
		<span class="text-fg-muted">
			{#if hasBefore && hasAfter}{changed.length} geänderte Felder{:else if hasAfter}Neu angelegt{:else}Gelöscht{/if}
		</span>
		{#if hasBefore && hasAfter && rows.length !== changed.length}
			<label class="inline-flex items-center gap-1.5 text-fg-muted">
				<input type="checkbox" bind:checked={showAll} class="h-3.5 w-3.5 accent-(--accent)" /> unveränderte Felder
				zeigen
			</label>
		{/if}
		<label class="inline-flex items-center gap-1.5 text-fg-muted">
			<input type="checkbox" bind:checked={raw} class="h-3.5 w-3.5 accent-(--accent)" /> JSON
		</label>
	</div>
	{#if raw}
		<div class="grid grid-cols-1 gap-2 lg:grid-cols-2">
			{#if hasBefore}<div>
					<div class="mb-1 text-xs font-medium text-fg-muted">Vorher</div>
					<JsonView value={before} openDepth={1} maxHeight="20rem" />
				</div>{/if}
			{#if hasAfter}<div>
					<div class="mb-1 text-xs font-medium text-fg-muted">Nachher</div>
					<JsonView value={after} openDepth={1} maxHeight="20rem" />
				</div>{/if}
		</div>
	{:else if shown.length === 0}
		<p class="text-xs text-fg-subtle">Keine Unterschiede.</p>
	{:else}
		<div class="overflow-x-auto rounded-md border border-border">
			<table class="w-full text-xs">
				<caption class="sr-only">Änderungen</caption>
				<thead class="bg-surface-2 text-fg-muted">
					<tr>
						<th scope="col" class="px-2 py-1 text-left font-medium">Feld</th>
						{#if hasBefore}<th scope="col" class="px-2 py-1 text-left font-medium">Vorher</th>{/if}
						{#if hasAfter}<th scope="col" class="px-2 py-1 text-left font-medium">Nachher</th>{/if}
					</tr>
				</thead>
				<tbody>
					{#each shown as r (r.path)}
						<tr class="border-t border-border align-top {kindCls[r.kind]}">
							<td class="mono px-2 py-1 whitespace-nowrap text-fg">
								{r.path}
								{#if r.kind !== 'same' && hasBefore && hasAfter}<span class="sr-only"
										>({kindLabel[r.kind]})</span
									>{/if}
							</td>
							{#if hasBefore}<td
									class="mono max-w-xs px-2 py-1 break-all {r.kind === 'changed' || r.kind === 'removed'
										? 'text-danger line-through decoration-danger/40'
										: 'text-fg-muted'}">{fmtValue(r.before)}</td
								>{/if}
							{#if hasAfter}<td
									class="mono max-w-xs px-2 py-1 break-all {r.kind === 'same' ? 'text-fg-muted' : 'text-fg'}"
									>{fmtValue(r.after)}</td
								>{/if}
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
