import { expect, it, vi } from 'vitest'
import { createGalleryStore } from '@renderer/features/gallery/galleryStore'
import { deferred } from './reactHarness'

const ok = (data: any) => ({ ok: true as const, data })
const row = (id: number, revision = 4) => ({ id, revision, title: `album ${id}`, fileCount: 2, managedFileCount: 2, isFavorite: false, cover: null })

it('sends tags in one batch without per-album reads, keeps partial failures, and releases controls before reconciliation', async () => {
    const pending = deferred<any>()
    const api = { list: vi.fn().mockResolvedValueOnce(ok({ data: [row(1), row(2)], total: 2, hasNextPage: false })).mockReturnValue(pending.promise),
        get: vi.fn(), edit: vi.fn(), bulkEdit: vi.fn(async () => ok({ updated: [{ mediaId: 1, revision: 5 }], failed: [{ mediaId: 2, error: { code: 'REVISION_CONFLICT', message: 'stale', context: {} } }] })) }
    const store = createGalleryStore(api as any)
    await store.refresh()
    const result = await store.bulk([{ mediaId: 1, revision: 4 }, { mediaId: 2, revision: 4 }], { kind: 'tags', names: ['shared'], remove: false }, vi.fn())
    expect(api.bulkEdit).toHaveBeenCalledExactlyOnceWith({ selection: [{ mediaId: 1, revision: 4 }, { mediaId: 2, revision: 4 }], addTags: ['shared'] })
    expect(api.get).not.toHaveBeenCalled(); expect(api.edit).not.toHaveBeenCalled()
    expect(result?.succeeded).toEqual([1]); expect(result?.failed.map(item => item.mediaId)).toEqual([2])
    expect(store.getSnapshot().list.page.data[0].revision).toBe(5)
    expect(store.getSnapshot().busy).toBe(false)
    pending.resolve(ok({ data: [row(1, 5), row(2, 6)], total: 2, hasNextPage: false }))
})

it('bounds large batches and stops remaining writes after an unknown transport outcome', async () => {
    const api = { list: vi.fn(async () => ok({ data: [], total: 0, hasNextPage: false })),
        bulkEdit: vi.fn(async (request: any) => ok({ updated: request.selection.map((item: any) => ({ mediaId: item.mediaId, revision: item.revision + 1 })), failed: [] })) }
    const store = createGalleryStore(api as any), selection = Array.from({ length: 401 }, (_, i) => ({ mediaId: i + 1, revision: 4 }))
    const progress = vi.fn()
    expect((await store.bulk(selection, { kind: 'tags', names: ['old'], remove: true }, progress))?.succeeded).toHaveLength(401)
    expect(api.bulkEdit.mock.calls.map(([value]) => value.selection.length)).toEqual([200, 200, 1])
    expect(api.bulkEdit.mock.calls[0][0].removeTags).toEqual(['old'])
    api.bulkEdit.mockReset().mockRejectedValueOnce(new Error('lost response'))
    expect((await store.bulk(selection, { kind: 'tags', names: ['new'], remove: false }, vi.fn()))?.failed).toHaveLength(401)
    expect(api.bulkEdit).toHaveBeenCalledOnce(); expect(store.getSnapshot().needsRefresh).toBe(true)
})

it('removes a confirmed file from every visible album before the next list response', async () => {
    const pending = deferred<any>(), file = { id: 10, mediaId: 1, kind: 'image', storageKind: 'app_managed' }
    const api = { list: vi.fn().mockResolvedValueOnce(ok({ data: [
        { ...row(1), galleryFile: file }, { ...row(1), galleryFile: { ...file, id: 11 } }, { ...row(2), isUserAlbum: true, galleryFile: file }
    ], total: 3, hasNextPage: false })).mockReturnValue(pending.promise),
        unregisterFiles: vi.fn(async () => ok({ mediaId: 1, parentRemoved: false, revision: 5, removedFileIds: [10], remainingFileCount: 1, removedManagedFileCount: 1 })) }
    const store = createGalleryStore(api as any)
    await store.refresh()
    expect(await store.unregisterFiles({ mediaId: 1, expectedRevision: 4, fileIds: [10] })).toBe(true)
    const snapshot = store.getSnapshot()
    expect(snapshot.list.page.data.map(item => item.galleryFile?.id)).toEqual([11])
    expect(snapshot.list.page.data[0]).toMatchObject({ fileCount: 1, managedFileCount: 1, revision: 5 })
    expect(snapshot.list.nextOffset).toBe(1); expect(snapshot.busy).toBe(false)
    pending.resolve(ok({ data: snapshot.list.page.data, total: 1, hasNextPage: false }))
})

it('refreshes linked album covers without list reloads and ignores metadata overtaken by a page request', async () => {
    const pending = deferred<any>()
    const api = { list: vi.fn().mockResolvedValueOnce(ok({ data: Array.from({ length: 24 }, (_, i) => row(i + 2)), total: 25, hasNextPage: true }))
        .mockResolvedValueOnce(ok({ data: [row(26)], total: 25, hasNextPage: false })),
        metadata: vi.fn().mockResolvedValueOnce(ok({ media: [{ ...row(2), cover: { fileId: 10, path: 'new-thumb', kind: 'image' } }], files: [] })).mockReturnValue(pending.promise) }
    const store = createGalleryStore(api as any, { expandAlbums: true })
    await store.refresh(); await store.refreshMetadata([1])
    expect(api.metadata).toHaveBeenCalledWith({ mediaIds: Array.from({ length: 24 }, (_, i) => i + 2), fileIds: [], changedMediaIds: [1] })
    expect(api.list).toHaveBeenCalledOnce(); expect(store.getSnapshot().list.page.data[0].cover?.path).toBe('new-thumb')
    const old = store.refreshMetadata([1])
    await store.loadMore()
    pending.resolve(ok({ media: [{ ...row(2), title: 'stale' }], files: [] }))
    expect(await old).toBe(false); expect(store.getSnapshot().list.page.data.map(item => item.id)).toEqual(Array.from({ length: 25 }, (_, i) => i + 2))
    expect(store.getSnapshot().list.page.data[0].title).toBe('album 2')
})
