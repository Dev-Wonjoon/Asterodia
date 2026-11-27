import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
    base: './',
    publicDir: 'demo-public',
    plugins: [react(), tailwindcss(), {
        name: 'demo-index',
        enforce: 'post',
        generateBundle(_, bundle) {
            const html = bundle['demo.html']
            if (html?.type === 'asset') {
                this.emitFile({ type: 'asset', fileName: 'index.html', source: html.source })
                delete bundle['demo.html']
            }
        },
    }],
    resolve: { alias: [
        { find: /^@renderer\/shared\/api$/, replacement: fileURLToPath(new URL('./src/demo/api.ts', import.meta.url)) },
        { find: /^@renderer\/shared\/utils\/media$/, replacement: fileURLToPath(new URL('./src/demo/media.ts', import.meta.url)) },
        { find: '@renderer', replacement: fileURLToPath(new URL('./src', import.meta.url)) },
        { find: '@contracts', replacement: fileURLToPath(new URL('./src/contracts', import.meta.url)) },
    ] },
    build: { outDir: '../docs/demo', emptyOutDir: true, rollupOptions: { input: 'demo.html' } },
})
