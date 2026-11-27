import { afterEach, describe, expect, it, vi } from 'vitest'
import { createDemoModel } from '@renderer/demo/model'
import type { MediaResult } from '@contracts/media-v2'

function data<T>(result: MediaResult<T>): T {
    if (!result.ok) throw new Error(result.error.message)
    return result.data
}
const models: ReturnType<typeof createDemoModel>[] = []
function model() { const value = createDemoModel(); models.push(value); return value }
afterEach(() => { models.splice(0).forEach(value => value.dispose()); vi.useRealTimers() })

describe('Pages demo uses an isolated media backend', () => {
    it('applies batched tags to current albums while preserving a conflicted album and unrelated tags', async () => {
        const { media } = model()
        const first = data(await media.get({ mediaId: 2 })), second = data(await media.get({ mediaId: 7 }))
        const result = data(await media.bulkEdit!({ selection: [{ mediaId: 2, revision: first.revision }, { mediaId: 7, revision: second.revision + 1 }], addTags: ['batch'] }))
        expect(result.updated.map(row => row.mediaId)).toEqual([2])
        expect(result.failed.map(row => row.mediaId)).toEqual([7])
        expect(data(await media.get({ mediaId: 2 })).tags.map(tag => tag.name)).toEqual([...first.tags.map(tag => tag.name), 'batch'])
        expect(data(await media.get({ mediaId: 7 })).tags).toEqual(second.tags)
        data(await media.bulkEdit!({ selection: [{ mediaId: 2, revision: result.updated[0].revision }], removeTags: ['batch'] }))
        expect(data(await media.get({ mediaId: 2 })).tags).toEqual(first.tags)
    })
    it('keeps favorite and tag edits searchable, rejects stale revisions, and returns detached values', async () => {
        const { media } = model()
        const initial = data(await media.get({ mediaId: 2 }))
        const edited = data(await media.edit({ mediaId: 2, expectedRevision: initial.revision, isFavorite: true, tagChange: { addNames: ['my-picks'] } }))
        expect(edited.revision).toBe(initial.revision + 1)
        expect((await media.edit({ mediaId: 2, expectedRevision: initial.revision, isFavorite: false })).ok).toBe(false)
        const matches = data(await media.list({ favoritesOnly: true, filter: { tags: ['my-picks'] } }))
        expect(matches.data.map(item => item.id)).toEqual([2])
        matches.data[0].title = 'external mutation'
        expect(data(await media.get({ mediaId: 2 })).title).toBe(initial.title)
    })

    it('shares references without duplicating files and keeps source files after deleting a user album', async () => {
        const { media } = model()
        let album = data(await media.saveAlbum({ title: 'My picks', addFileIds: [1, 1, 6] }))
        expect(album.fileCount).toBe(2)
        album = data(await media.saveAlbum({ albumId: album.id, expectedRevision: album.revision, addMediaIds: [7] }))
        expect(album.fileCount).toBe(4)
        expect(data(await media.sequence({ mediaId: album.id })).ids).toEqual([1, 6, 2, 3])
        data(await media.saveAlbum({ albumId: album.id, expectedRevision: album.revision, delete: true }))
        expect(data(await media.readFile({ fileId: 1 })).kind).toBe('image')
        expect(data(await media.readFile({ fileId: 6 })).kind).toBe('video')
    })

    it('sorts album files and applies filters and pagination', async () => {
        const { media } = model()
        const first = data(await media.exploreAlbum({ mediaId: 7, sort: 'name', descending: false, limit: 1 }))
        expect(first.data[0].kind === 'file' && first.data[0].title).toContain('Blue hour')
        expect(first.hasNextPage).toBe(true)
        const filtered = data(await media.exploreAlbum({ mediaId: 7, filter: { tags: ['nature'] } }))
        expect(filtered.total).toBe(2)
    })

    it('simulates progress then appends one file to the Instagram profile album', async () => {
        vi.useFakeTimers()
        const { downloads, media } = model()
        const job = data(await downloads.enqueue({ mode: 'create', url: 'https://www.instagram.com/p/sample/' }))
        const running = downloads.run({ jobId: job.id, trigger: 'manual_initial' })
        await vi.advanceTimersByTimeAsync(2600)
        expect(data(await downloads.get({ jobId: job.id })).progress).toBe(40)
        expect(data(await media.sequence({ mediaId: 7 })).ids).toHaveLength(3)
        await vi.advanceTimersByTimeAsync(4000)
        expect(data(await running).status).toBe('committed')
        expect(data(await downloads.get({ jobId: job.id })).state).toBe('completed')
        expect(data(await media.sequence({ mediaId: 7 })).ids).toHaveLength(4)
        expect(data(await media.list()).total).toBe(9)
        expect((await downloads.run({ jobId: job.id, trigger: 'manual_restart' })).ok).toBe(false)
    })

    it('cancels and resets active timers without adding delayed files or retaining edits', async () => {
        vi.useFakeTimers()
        const demo = model()
        const job = data(await demo.downloads.enqueue({ mode: 'create', url: 'https://example.com/sample' }))
        const running = demo.downloads.run({ jobId: job.id, trigger: 'manual_initial' })
        await vi.advanceTimersByTimeAsync(1000)
        await demo.downloads.cancel({ jobId: job.id })
        expect((await running).ok).toBe(false)
        await vi.advanceTimersByTimeAsync(10000)
        expect(data(await demo.media.sequence({ mediaId: 7 })).ids).toHaveLength(3)
        await demo.media.edit({ mediaId: 1, expectedRevision: 1, isFavorite: false })
        const second = data(await demo.downloads.enqueue({ mode: 'create', url: 'https://example.com/reset' }))
        const pending = demo.downloads.run({ jobId: second.id, trigger: 'manual_initial' })
        demo.reset()
        expect((await pending).ok).toBe(false)
        await vi.advanceTimersByTimeAsync(10000)
        expect(data(await demo.media.get({ mediaId: 1 })).isFavorite).toBe(true)
        expect(data(await demo.downloads.list()).data).toHaveLength(0)
        expect(data(await demo.media.list()).total).toBe(8)
    })

    it('rejects non-http input without fetching any submitted URL', async () => {
        const demo = model()
        const fetch = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('Unexpected network request'))
        try {
            expect((await demo.downloads.enqueue({ mode: 'create', url: 'file:///private' })).ok).toBe(false)
            expect((await demo.downloads.enqueue({ mode: 'create', url: 'https://example.com/sample' })).ok).toBe(true)
            expect(fetch).not.toHaveBeenCalled()
        } finally { fetch.mockRestore() }
    })
})
