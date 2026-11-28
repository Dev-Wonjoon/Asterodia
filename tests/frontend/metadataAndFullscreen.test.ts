// @vitest-environment happy-dom
import { expect, it, vi } from 'vitest'
import { createGalleryStore } from '@renderer/features/gallery/galleryStore'
import { createDemoModel } from '@renderer/demo/model'
import { createFullscreenController } from '../../internal/webserver/web/fullscreen.js'

it('updates tags and platform classifications without refetching album files or the unfiltered gallery', async () => {
    const model = createDemoModel(), api = model.media
    const list = vi.spyOn(api, 'list'), get = vi.spyOn(api, 'get'), edit = vi.spyOn(api, 'edit')
    const store = createGalleryStore(api)
    try {
        await store.refresh(); const id = store.getSnapshot().list.page.data[0].id
        await store.openDetail(id)
        const before = store.getSnapshot().detail.data!, files = before.files
        list.mockClear(); get.mockClear()
        expect(await store.edit({ mediaId: id, expectedRevision: before.revision, tagChange: { addNames: ['quick tag'] } })).toBe(true)
        const tagged = store.getSnapshot().detail.data!
        expect(tagged.tags.some(tag => tag.name === 'quick tag')).toBe(true)
        expect(tagged.files).toBe(files); expect(store.getSnapshot().busy).toBe(false)
        expect(await store.edit({ mediaId: id, expectedRevision: tagged.revision, platformChange: { addNames: ['Archive'] } })).toBe(true)
        expect(store.getSnapshot().detail.data?.platforms?.map(value => value.name)).toEqual(['Archive'])
        expect(list).not.toHaveBeenCalled(); expect(get).not.toHaveBeenCalled()
        // The mock backend implements bulk writes using its in-memory edit.
        expect(edit).toHaveBeenCalledTimes(2)
        list.mockClear(); get.mockClear()
        const result = await store.bulk([{ mediaId: id, revision: store.getSnapshot().detail.data!.revision }], { kind: 'tags', names: ['bulk tag'], remove: false }, vi.fn())
        expect(result?.succeeded).toEqual([id])
        expect(store.getSnapshot().detail.data?.tags.some(tag => tag.name === 'bulk tag')).toBe(true)
        expect(store.getSnapshot().detail.data?.files).toBe(files)
        expect(store.getSnapshot().detail.data?.revision).toBe(store.getSnapshot().list.page.data.find(row => row.id === id)?.revision)
        expect(list).not.toHaveBeenCalled(); expect(get).not.toHaveBeenCalled()
    } finally { model.dispose() }
})

it('requests browser fullscreen on the document and restores only fullscreen it owns', async () => {
    const doc: any = new EventTarget(), exited = vi.fn()
    doc.fullscreenElement = null
    doc.documentElement = { requestFullscreen: vi.fn(async () => { doc.fullscreenElement = doc.documentElement }) }
    doc.exitFullscreen = vi.fn(async () => { doc.fullscreenElement = null; doc.dispatchEvent(new Event('fullscreenchange')) })
    const controller = createFullscreenController(doc, undefined, exited)
    await controller.enter(); expect(doc.documentElement.requestFullscreen).toHaveBeenCalledOnce()
    doc.fullscreenElement = null; doc.dispatchEvent(new Event('fullscreenchange'))
    expect(exited).toHaveBeenCalledOnce(); controller.destroy()
})

it('restores the desktop window when a pending fullscreen entry finishes after close', async () => {
    let finish!: (owned: boolean) => void
    const native = { enter: vi.fn(() => new Promise<boolean>(resolve => { finish = resolve })), exit: vi.fn(async () => {}), onExit: vi.fn(() => vi.fn()) }
    const controller = createFullscreenController(document, native, vi.fn())
    const pending = controller.enter(); controller.destroy(); finish(true); await pending
    expect(native.exit).toHaveBeenCalledOnce()
})

it('does not exit a desktop window that was already fullscreen before the viewer opened', async () => {
    const native = { enter: vi.fn(async () => false), exit: vi.fn(async () => {}), onExit: vi.fn(() => vi.fn()) }
    const controller = createFullscreenController(document, native, vi.fn())
    await controller.enter(); controller.destroy(); expect(native.exit).not.toHaveBeenCalled()
})
