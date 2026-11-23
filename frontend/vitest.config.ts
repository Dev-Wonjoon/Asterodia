import { defineConfig } from 'vitest/config'
import { fileURLToPath } from 'node:url'
import { randomUUID } from 'node:crypto'
import { existsSync, mkdirSync, realpathSync } from 'node:fs'
import path from 'node:path'

const root = path.resolve(fileURLToPath(new URL('../', import.meta.url)))
const runDirectory = path.join(root, '.test-data', 'frontend', randomUUID())
let existingParent = path.dirname(runDirectory)
while (!existsSync(existingParent)) existingParent = path.dirname(existingParent)
const actualParent = realpathSync(existingParent)
if (actualParent !== realpathSync(root) && !actualParent.startsWith(realpathSync(root) + path.sep)) throw new Error('테스트 부모 경로가 저장소 밖을 가리킵니다.')
mkdirSync(runDirectory, { recursive: true })
if (!realpathSync(runDirectory).startsWith(realpathSync(root) + path.sep)) throw new Error('테스트 경로가 저장소 밖을 가리킵니다.')
export default defineConfig({
    root,
    server: { fs: { allow: [root] } },
    cacheDir: path.join(runDirectory, 'cache'),
    resolve: { alias: {
        'vitest': root + '/frontend/node_modules/vitest/dist/index.js',
        'react': root + '/frontend/node_modules/react',
        'react-dom': root + '/frontend/node_modules/react-dom',
        '@wailsio/runtime': root + '/frontend/node_modules/@wailsio/runtime/dist/index.js',
        '@renderer': root + '/frontend/src',
        '@contracts': root + '/frontend/src/contracts',
        '@bindings': root + '/frontend/bindings/github.com/Dev-Wonjoon/StorageBucket/internal'
    } },
    test: {
        include: ['tests/frontend/**/*.test.ts'], environment: 'node', watch: false,
        setupFiles: ['tests/frontend/i18nSetup.ts'],
        coverage: {
            provider: 'v8',
            include: ['frontend/src/**/*.{ts,tsx}'],
            exclude: ['frontend/src/contracts/**', '**/*.d.ts'],
            reportsDirectory: path.join(runDirectory, 'coverage'),
            reporter: ['text-summary', 'json-summary', 'html'],
            thresholds: {
                'frontend/src/app/providers/ConfirmProvider.tsx': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/shared/components/ui/ConfirmDialog.tsx': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/shared/hooks/useLeaveGuard.ts': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/gallery/FileMovePanel.tsx': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/settings/WebServerSettings.tsx': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/settings/useWebServerSettings.ts': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/download/downloadStore.ts': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/download/downloadPresentation.ts': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/settings/engineInstall.ts': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/localimport/panelVisibility.ts': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/shared/utils/formatBytes.ts': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/settings/useEngineSettingsViewModel.ts': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/settings/components/EngineInstallProgress.tsx': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/settings/components/EngineSetupModal.tsx': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/settings/EngineSettings.tsx': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/shared/components/ui/ProgressBar.tsx': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/download/downloadLogs.ts': { branches: 90, functions: 90, lines: 90, statements: 90 },
                'frontend/src/features/download/components/DownloadLogPane.tsx': { branches: 90, functions: 90, lines: 90, statements: 90 },
            },
        },
    }
})
