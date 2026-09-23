import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// Single page application embedded into the Go binary (internal/webui).
		adapter: adapter({
			pages: '../internal/webui/dist',
			assets: '../internal/webui/dist',
			fallback: 'index.html',
			strict: false
		})
	}
};

export default config;
