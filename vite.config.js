import { resolve } from 'node:path'
import { defineConfig } from 'vite'

const input = {
	backend: resolve(import.meta.dirname, 'assets/js/backend.js'),
	count: resolve(import.meta.dirname, 'assets/js/count.js'),
	styles: resolve(import.meta.dirname, 'assets/css/backend.css'),
}
export default defineConfig({
	publicDir: 'assets/static',
	build: {
		emptyOutDir: true,
		manifest: 'manifest.json',
		outDir: 'public',
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
