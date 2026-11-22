import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
    base: '/ui/',
    plugins: [react(), tailwindcss()],
    resolve: { alias: [
        { find: '@renderer/shared/api', replacement: fileURLToPath(new URL('./src/web/api.ts', import.meta.url)) },
        { find: '@renderer/shared/utils/media', replacement: fileURLToPath(new URL('./src/web/media.ts', import.meta.url)) },
        { find: '@renderer', replacement: fileURLToPath(new URL('./src', import.meta.url)) },
        { find: '@contracts', replacement: fileURLToPath(new URL('./src/contracts', import.meta.url)) },
    ] },
    build: { outDir: '../internal/webserver/web/dist', emptyOutDir: true, rollupOptions: { input: 'web.html' } },
})
