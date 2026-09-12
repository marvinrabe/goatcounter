import { resolve } from 'node:path'
import { defineConfig } from 'vite'

const input = {
	backend: resolve(import.meta.dirname, 'assets/backend.js'),
	count: resolve(import.meta.dirname, 'assets/count.js'),
	styles: resolve(import.meta.dirname, 'assets/backend.css'),
}
export default defineConfig({
	publicDir: false,
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
