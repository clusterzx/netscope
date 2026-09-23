<!--
	Renders Markdown safely (marked + DOMPurify, links open in a new tab).
	<MarkdownView source={device.notes} />
-->
<script lang="ts">
	import { renderMarkdown } from '$lib/utils/markdown';

	interface Props {
		source: string;
		class?: string;
	}

	let { source, class: klass = '' }: Props = $props();
	const html = $derived(renderMarkdown(source ?? ''));
</script>

<div class="prose-ns text-sm text-fg {klass}">
	<!-- sanitized by DOMPurify (utils/markdown.ts) -->
	{@html html}
</div>
