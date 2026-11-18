import { beforeEach, describe, expect, it, vi } from 'vitest'

const bindings = vi.hoisted(() => ({
    list: vi.fn(), edit: vi.fn(), enqueue: vi.fn(), run: vi.fn(), preview: vi.fn(),
    thumbnail: vi.fn(), path: vi.fn(), on: vi.fn()
}))
vi.mock('@wailsio/runtime', () => ({ Events: { On: bindings.on } }))
vi.mock('@bindings/download', () => ({ Service: { Enqueue: bindings.enqueue, Run: bindings.run, PreviewEngine: bindings.preview } }))
vi.mock('@bindings/media', () => ({ Service: { List: bindings.list, Edit: bindings.edit } }))
vi.mock('@bindings/thumbnail', () => ({ Service: { Get: bindings.thumbnail, GetDownloadPreview: vi.fn() } }))
vi.mock('@bindings/settings', () => ({ Service: { GetDownloadPath: bindings.path } }))
vi.mock('@bindings/desktop', () => ({ SystemService: { GetDoubleClickInterval: vi.fn() } }))
vi.mock('@bindings/engine', () => ({ Manager: {} }))
import { getApi } from '@renderer/shared/api'

describe('Wails API boundary', () => {
    beforeEach(() => vi.clearAllMocks())
    it('passes pagination and filters without changing the contract', async () => {
        const expected = { ok: true, data: { data: [], total: 0, hasNextPage: false } }
        bindings.list.mockResolvedValue(expected)
        const request = { offset: 50, filter: { tags: ['여행'], tagMode: 'and' as const } }
        expect(await getApi().media.list(request)).toEqual(expected)
        expect(bindings.list).toHaveBeenCalledWith(request)
    })
    it('preserves explicit title reset and transport error envelopes', async () => {
        const expected = { ok: false, error: { code: 'REVISION_CONFLICT', message: 'changed', context: {} } }
        bindings.edit.mockResolvedValue(expected)
        const request = { mediaId: 4, expectedRevision: 2, customTitle: null }
        expect(await getApi().media.edit(request)).toEqual(expected)
        expect(bindings.edit).toHaveBeenCalledWith(request)
    })
    it('rejects malformed service responses', async () => {
        bindings.list.mockResolvedValue({ data: [] })
        await expect(getApi().media.list()).rejects.toThrow('응답 형식')
    })
    it('forwards replacement identity and options', async () => {
        const request = { mode: 'replace' as const, url: 'https://example.test/item', mediaId: 5, expectedRevision: 7, options: { type: 'audio' as const, extension: 'mp3' as const } }
        bindings.enqueue.mockResolvedValue({ ok: true, data: { id: 'job' } })
        await getApi().downloads.enqueue(request)
        expect(bindings.enqueue).toHaveBeenCalledWith(request)
    })
    it('subscribes to actual Wails thumbnail events and exposes unsubscribe', () => {
        const off = vi.fn(), listener = vi.fn()
        bindings.on.mockReturnValue(off)
        expect(getApi().onMediaThumbnailUpdated(listener)).toBe(off)
        expect(bindings.on.mock.calls[0][0]).toBe('media:thumbnail-updated')
        bindings.on.mock.calls[0][1]({ data: 9 })
        expect(listener).toHaveBeenCalledWith(9)
    })
})
