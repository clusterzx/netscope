// Markdown → sanitized HTML (marked + DOMPurify).
import DOMPurify from 'dompurify';
import { Marked } from 'marked';

const md = new Marked({ gfm: true, breaks: true, async: false });

let hooked = false;
function hook() {
	if (hooked) return;
	hooked = true;
	DOMPurify.addHook('afterSanitizeAttributes', (node) => {
		if (node.tagName === 'A') {
			const href = node.getAttribute('href') ?? '';
			if (/^https?:\/\//i.test(href)) {
				node.setAttribute('target', '_blank');
				node.setAttribute('rel', 'noopener noreferrer');
			}
		}
	});
}

export function renderMarkdown(source: string): string {
	if (!source) return '';
	hook();
	const html = md.parse(source) as string;
	return DOMPurify.sanitize(html, { USE_PROFILES: { html: true } });
}
