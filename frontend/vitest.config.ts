import { defineConfig } from 'vitest/config'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../', import.meta.url))
export default defineConfig({
    root,
    cacheDir: root + '/.test-data/frontend-cache',
    resolve: { alias: {
        'vitest': root + '/frontend/node_modules/vitest/dist/index.js',
        '@wailsio/runtime': root + '/frontend/node_modules/@wailsio/runtime/dist/index.js',
        '@renderer': root + '/frontend/src',
        '@contracts': root + '/frontend/src/contracts',
        '@bindings': root + '/frontend/bindings/github.com/Dev-Wonjoon/StorageBucket/internal'
    } },
    test: { include: ['tests/frontend/**/*.test.ts'], environment: 'node', watch: false }
})
