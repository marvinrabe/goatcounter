import { resolve } from 'node:path'
import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'

// The frontend lives with the Go package that embeds it. Paths are relative to
// root, which is also what the manifest keys are relative to.
const root = resolve(import.meta.dirname, 'internal/web')
const input = {
	backend: resolve(root, 'assets/js/backend.js'),
	count: resolve(root, 'assets/js/count.js'),
	styles: resolve(root, 'assets/css/backend.css'),
}
export default defineConfig({
	root,
	plugins: [tailwindcss()],
	publicDir: 'assets/static',
	build: {
		emptyOutDir: true,
		manifest: 'manifest.json',
		outDir: 'dist',
		rollupOptions: {
			input,
			output: {
				assetFileNames: 'assets/[name]-[hash][extname]',
				entryFileNames: (chunk) => chunk.name === 'count'
					? '[name].js'
					: 'assets/[name]-[hash].js',
			},
		},
	},
})
