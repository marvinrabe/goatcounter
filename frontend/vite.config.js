import { resolve } from 'node:path'
import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'

// Builds into the Go package that embeds the files. The manifest maps the
// sources, relative to this directory, to the built files.
const input = {
	app: resolve(import.meta.dirname, 'js/app.js'),
	count: resolve(import.meta.dirname, 'js/count.js'),
	styles: resolve(import.meta.dirname, 'css/app.css'),
}
export default defineConfig({
	root: import.meta.dirname,
	plugins: [tailwindcss()],
	publicDir: 'static',
	build: {
		emptyOutDir: true,
		manifest: 'manifest.json',
		outDir: resolve(import.meta.dirname, '../internal/web/dist'),
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
