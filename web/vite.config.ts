import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

// Backend for `npm run dev` (make dev): a local Go server or the LXC instance.
const target = process.env.NETSCOPE_API ?? 'http://127.0.0.1:18080';

export default defineConfig({
	// separate dep caches when several dev servers run side by side
	cacheDir: process.env.VITE_CACHE_DIR ?? 'node_modules/.vite',
	plugins: [tailwindcss(), sveltekit()],
	server: {
		proxy: {
			'/api': { target, changeOrigin: false },
			'/metrics': { target, changeOrigin: false }
		}
	}
});
