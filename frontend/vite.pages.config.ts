import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
    root: fileURLToPath(new URL('.', import.meta.url)),
    publicDir: false,
    plugins: [tailwindcss()],
    build: {
        outDir: '../docs/assets',
        // Only update the generated stylesheet; retain other documentation assets.
        emptyOutDir: false,
        rollupOptions: {
            input: fileURLToPath(new URL('./src/styles/pages.css', import.meta.url)),
            output: { assetFileNames: 'tailwind.css' },
        },
    },
})
