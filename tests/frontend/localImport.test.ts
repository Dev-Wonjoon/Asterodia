import { describe, expect, it } from 'vitest'
import type { LocalImportJob } from '@contracts/local-import-api'
import { canResume, importProgress } from '@renderer/features/localimport/presentation'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

function job(overrides: Partial<LocalImportJob> = {}): LocalImportJob {
    return { id: 'job', root: 'C:/fixture', state: 'paused', counts: { discovered: 10, processed: 4, failed: 1, added: 2, updated: 0, unchanged: 0, excluded: 1, scanExcluded: 0, userExcluded: 1, scanErrors: 0, totalBytes: 100, processedBytes: 40, completedFolders: 0, failedFolders: 0 }, cancelRequested: false, pauseRequested: false, scanComplete: true, canResume: true, currentPath: '', error: null, elapsedMillis: 1000, bytesPerSecond: 40, filesPerSecond: 4, largeFolders: 0, revision: 2, ...overrides }
}
describe('folder import progress', () => {
    it('keeps an unknown total indeterminate while enumerating', () => {
        expect(importProgress(job({ state: 'scanning', scanComplete: false }))).toBeUndefined()
    })
    it('counts registered, failed and explicitly excluded files as processed', () => {
        expect(importProgress(job())).toBe(40)
        const current = job(); current.counts.processed = 15
        expect(importProgress(current)).toBe(100)
        current.counts.discovered = 0
        expect(importProgress(current)).toBe(0)
    })
    it('offers resume only for compatible stopped jobs', () => {
        for (const state of ['paused', 'interrupted', 'cancelled', 'failed'] as const) expect(canResume(job({ state }))).toBe(true)
        for (const state of ['scanning', 'running', 'ready', 'completed', 'completed_with_errors'] as const) expect(canResume(job({ state }))).toBe(false)
        expect(canResume(job({ canResume: false }))).toBe(false)
    })
    it('formats bytes and throughput with bounded units', () => {
        expect(formatBytes(0)).toBe('0 B')
        expect(formatBytes(1024)).toBe('1.0 KB')
        expect(formatBytes(3 * 1024 ** 2)).toBe('3.0 MB')
        expect(formatBytes(3 * 1024 ** 4)).toBe('3.0 TB')
    })
})
