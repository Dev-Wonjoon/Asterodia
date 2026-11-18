import { afterEach, describe, expect, it, vi } from 'vitest'
import { createDownloadStore } from '@renderer/features/download/downloadStore'
import type { DownloadApi, DownloadJobDto } from '@contracts/download-api'

const row: DownloadJobDto = { id: 'new', url: 'https://example.test/item', title: null, state: 'running', phase: 'discovering', progress: 0, options: {}, cleanupRequired: false, currentAttemptId: 'attempt', errorCode: null }
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })

describe('confirmed replacement requests', () => {
    async function fixture(confirmed: boolean) {
        vi.useFakeTimers()
        const confirm = vi.fn(() => confirmed)
        vi.stubGlobal('window', { confirm })
        const enqueue = vi.fn<DownloadApi['enqueue']>()
            .mockResolvedValueOnce({ ok: false, error: { code: 'REDOWNLOAD_CONFIRMATION_REQUIRED', message: 'exists', context: { mediaId: 17, expectedRevision: 8 } } })
            .mockResolvedValue({ ok: true, data: row })
        const api: DownloadApi = {
            enqueue,
            list: vi.fn(async () => ({ ok: true, data: { data: [], limit: 50, offset: 0, hasNextPage: false } })),
            get: vi.fn(async () => ({ ok: true, data: row })),
            run: vi.fn(async () => ({ ok: true, data: { status: 'blocked', jobId: row.id, reason: 'fixture' } })),
            cancel: vi.fn(async () => ({ ok: true, data: { accepted: true } })),
            previewEngine: vi.fn(async () => 'yt-dlp')
        }
        const store = createDownloadStore(api)
        const stop = store.connect(() => {})
        await store.refresh()
        return { api, store, stop, confirm }
    }
    it('keeps the selected options and the revision shown at confirmation', async () => {
        const f = await fixture(true)
        try {
            await f.store.startDownload(row.url, { type: 'audio', extension: 'mp3' })
            expect(f.confirm).toHaveBeenCalledOnce()
            expect(f.api.enqueue).toHaveBeenLastCalledWith({ mode: 'replace', url: row.url, options: { type: 'audio', extension: 'mp3' }, mediaId: 17, expectedRevision: 8 })
        } finally { f.stop() }
    })
    it('does not enqueue or run replacement after the user declines', async () => {
        const f = await fixture(false)
        try {
            await f.store.startDownload(row.url)
            expect(f.api.enqueue).toHaveBeenCalledOnce()
            expect(f.api.run).not.toHaveBeenCalled()
        } finally { f.stop() }
    })
})
