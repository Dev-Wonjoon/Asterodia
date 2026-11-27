// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { createGalleryStore } from '@renderer/features/gallery/galleryStore'
import type { MediaApi } from '@contracts/media-api'
import { readExpandedAlbums } from '../../internal/webserver/web/album-state.js'
afterEach(() => { localStorage.clear() })
const ok = (data: unknown) => ({ ok: true, data })
it.each(['all','albums','files'] as const)('sends %s view and hidden-file visibility on every gallery page', async galleryView => {
    const list=vi.fn(async(req:any)=>ok({data:Array.from({length:24},(_,i)=>({id:req.offset+i+1})),total:48,offset:req.offset,hasNextPage:req.offset===0}))
    const store=createGalleryStore({list} as unknown as MediaApi,{expandAlbums:true,galleryView,showAlbumFiles:false})
    await store.refresh();await store.loadMore()
    expect(list).toHaveBeenCalledTimes(2)
    for(const [request] of list.mock.calls)expect(request).toMatchObject({gallery:true,galleryView,showAlbumFiles:false})
    expect(store.getSnapshot().list.page.data).toHaveLength(48)
    const visible=createGalleryStore({list} as unknown as MediaApi,{expandAlbums:true,galleryView,showAlbumFiles:true})
    await visible.refresh()
    expect(list.mock.lastCall![0].showAlbumFiles).toBe(true)
})
it('keeps the same original file visible in two different expanded albums', async () => {
    const rows = [{ id: 1, galleryFile: { id: 7 } }, { id: 2, galleryFile: { id: 7 } }]
    const store = createGalleryStore({ list: vi.fn(async () => ok({ data: rows, total: 2, hasNextPage: false })) } as unknown as MediaApi, { expandAlbums: true })
    await store.refresh()
    expect(store.getSnapshot().list.page.data.map(item => item.id)).toEqual([1, 2])
})
function fixture() {
    const list = vi.fn(async (req: any) => {
        const rows = [{ id: 3 }, ...(req.expandedMediaIds.includes(1)
            ? Array.from({ length: 53 }, (_, i) => ({ id: 1, galleryFileOffset: i, galleryFile: { id: 100 + i } }))
            : [{ id: 1 }]), { id: 2 }]
        return ok({ data: rows.slice(req.offset, req.offset + req.limit), total: rows.length, hasNextPage: req.offset + req.limit < rows.length, offset: req.offset })
    })
    const get = vi.fn(async (req: any) => ok({ id: req.mediaId, files: { data: [], total: 102 } }))
    const store = createGalleryStore({ list, get } as unknown as MediaApi, { expandAlbums: true })
    return { store, list, get }
}
it('starts explorer entry cards grouped despite saved inline expansions and still permits explicit expansion', async () => {
    localStorage.setItem('asterodia:expanded-albums:v1', JSON.stringify([1, 2]))
    const { list, get } = fixture()
    const api = { list, get } as unknown as MediaApi
    const store = createGalleryStore(api, { expandAlbums: true, albumCards: true })
    await store.refresh()
    expect(list.mock.lastCall![0]).toMatchObject({ gallery: true, albumCards: true, expandedMediaIds: [] })
    expect(store.getSnapshot().list.page.data).toHaveLength(3)
    expect(store.getSnapshot().list.page.data.every(row => !row.galleryFile)).toBe(true)
    await store.toggleAlbum(1)
    expect(store.getSnapshot().list.page.data.some(row => row.galleryFile)).toBe(true)
    expect(readExpandedAlbums()).toEqual([1, 2])
    const reopened = createGalleryStore(api, { expandAlbums: true, albumCards: true })
    await reopened.refresh()
    expect(reopened.getSnapshot().list.page.data.every(row => !row.galleryFile)).toBe(true)
})
it('appends expanded files and other albums to one infinite list, then collapses without skipping the prefix', async () => {
    const { store, list, get } = fixture()
    await store.refresh(); await store.toggleAlbum(1)
    expect(list.mock.lastCall![0]).toMatchObject({ gallery: true, expandedMediaIds: [1], limit: 24, offset: 0 })
    expect(list.mock.lastCall![0].anchorMediaId).toBeUndefined()
    expect(store.getSnapshot().list.page.data).toHaveLength(24)
    expect(readExpandedAlbums()).toEqual([1])
    await store.loadMore()
    expect(list.mock.lastCall![0]).toMatchObject({ offset: 24, expandedMediaIds: [1] })
    expect(store.getSnapshot().list.page.data).toHaveLength(48)
    expect(store.getSnapshot().list.page.data[0].id).toBe(3)
    expect(store.getSnapshot().list.page.data[47].galleryFileOffset).toBe(46)
    await store.loadMore()
    expect(store.getSnapshot().list.page.data).toHaveLength(55)
    expect(store.getSnapshot().list.page.hasNextPage).toBe(false)
    expect(store.getSnapshot().list.page.data.at(-1)?.id).toBe(2)
    await store.openDetail(1, 51); expect(get.mock.lastCall![0]).toMatchObject({ mediaId: 1, fileOffset: 51 })
    await store.toggleAlbum(1)
    expect(list.mock.lastCall![0]).toMatchObject({ offset: 0, expandedMediaIds: [] })
    expect(store.getSnapshot().list.page.data.map(item => item.id)).toEqual([3, 1, 2])
    expect(store.getSnapshot().list.nextOffset).toBe(3)
    expect(readExpandedAlbums()).toEqual([])
})
it('keeps loaded rows on an append failure, retries at the same offset, and ignores overlapping loads', async () => {
    const { store, list } = fixture()
    await store.refresh(); await store.toggleAlbum(1)
    list.mockResolvedValueOnce({ ok: false, error: { code: 'INTERNAL_ERROR', message: '오류' } } as any)
    expect(await store.loadMore()).toBe(false)
    expect(store.getSnapshot().list.page.data).toHaveLength(24)
    expect(store.getSnapshot().list.nextOffset).toBe(24)
    expect(store.getSnapshot().list.moreError?.message).toBe('오류')
    await store.loadMore()
    expect(list.mock.lastCall![0].offset).toBe(24)
    expect(store.getSnapshot().list.page.data).toHaveLength(48)
    let resolve!: (value: any) => void
    list.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const pending = store.loadMore(), calls = list.mock.calls.length
    expect(await store.loadMore()).toBe(false); expect(list).toHaveBeenCalledTimes(calls)
    resolve(ok({ data: [], total: 48, hasNextPage: false, offset: 48 })); await pending
    expect(store.getSnapshot().list.page.data).toHaveLength(48)
})
it('preserves the loaded range on refresh, rolls back failed expansion, and rejects stale search responses', async () => {
    const { store, list } = fixture()
    await store.refresh(); await store.toggleAlbum(1); await store.loadMore()
    list.mockClear(); await store.refresh()
    expect(list.mock.calls.map(([req]) => req.offset)).toEqual([0, 24])
    expect(store.getSnapshot().list.page.data).toHaveLength(48)
    list.mockResolvedValueOnce({ ok: false, error: { code: 'INTERNAL_ERROR', message: '오류' } } as any)
    expect(await store.toggleAlbum(1)).toBe(false)
    expect(store.getSnapshot().expandedMediaIds).toEqual([1])
    expect(store.getSnapshot().list.page.data).toHaveLength(48)
    await store.refresh()
    let resolve!: (value: any) => void
    list.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const pending = store.loadMore()
    store.setFilter({ keyword: 'new' })
    list.mockResolvedValueOnce(ok({ data: [{ id: 8 }], total: 1, offset: 0, hasNextPage: false }))
    await store.refresh()
    resolve(ok({ data: [{ id: 7 }], total: 49, offset: 48, hasNextPage: false })); await pending
    expect(store.getSnapshot().list.page.data).toEqual([{ id: 8 }])
})
