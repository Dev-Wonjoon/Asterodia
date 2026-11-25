// @vitest-environment happy-dom
import { act, createElement as h, StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DownloadThumbnail } from '@renderer/features/download/components/DownloadThumbnail'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'
import { mockImageLoading } from './imageHarness'

const api = vi.hoisted(() => ({ getDownloadPreview: vi.fn(), onMediaThumbnailUpdated: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => api }))
const unsubscribe = vi.fn()
let updated: (mediaId: number) => void
beforeEach(() => {
    vi.resetAllMocks()
    api.getDownloadPreview.mockResolvedValue(null)
    api.onMediaThumbnailUpdated.mockImplementation((listener) => { updated = listener; return unsubscribe })
})
afterEach(async () => { await cleanupReact(); vi.unstubAllGlobals() })
const src = () => document.querySelector('img')?.getAttribute('src')

describe('download local thumbnails', () => {
    it('keeps a decoded image and its DOM node during empty refresh responses and polling gaps', async () => {
        const host = document.createElement('div'); document.body.append(host)
        const root = createRoot(host)
        try {
            const props = { url: 'https://example.invalid/a', completed: true, localPath: '/local.jpg' }
            await act(async () => root.render(h(DownloadThumbnail, props)))
            const image = document.querySelector('img')!
            await act(async () => image.dispatchEvent(new Event('load')))
            await act(async () => updated(1))
            expect(src()).toBe('/media?path=%2Flocal.jpg')
            expect(document.querySelector('img')).toBe(image)
            await act(async () => root.render(h(DownloadThumbnail, { ...props, localPath: null })))
            expect(src()).toBe('/media?path=%2Flocal.jpg')
            expect(document.querySelector('img')).toBe(image)
            expect(document.body.textContent).not.toContain('썸네일 없음')
        } finally { await act(async () => root.unmount()); host.remove() }
    })

    it('loads the generated thumbnail before replacing the cached image and retains it on later refreshes', async () => {
        const requests = mockImageLoading()
        await mount(h(DownloadThumbnail, { url: 'https://example.invalid/a', completed: true, localPath: '/cached.jpg' }))
        const image = document.querySelector('img')!
        await act(async () => image.dispatchEvent(new Event('load')))
        api.getDownloadPreview.mockResolvedValue('/generated.jpg')
        await act(async () => updated(1))
        expect(src()).toBe('/media?path=%2Fcached.jpg')
        expect(requests).toHaveLength(1)
        await act(async () => { await requests[0].onload?.() })
        expect(src()).toBe('/media?path=%2Fgenerated.jpg')
        expect(document.querySelector('img')).toBe(image)
        api.getDownloadPreview.mockResolvedValue(null)
        await act(async () => updated(2))
        expect(src()).toBe('/media?path=%2Fgenerated.jpg')
        expect(requests).toHaveLength(1)
    })

    it('does not repeatedly retry a broken image on unrelated updates but allows an explicit retry', async () => {
        await mount(h(DownloadThumbnail, { url: 'https://example.invalid/a', completed: true, localPath: '/broken.jpg' }))
        await act(async () => document.querySelector('img')!.dispatchEvent(new Event('error')))
        api.getDownloadPreview.mockResolvedValue('/broken.jpg')
        for (let id = 1; id <= 3; id++) {
            await act(async () => updated(id))
            expect(src()).toBeUndefined()
            expect(document.body.textContent).toContain('썸네일 없음')
        }
        const pending = deferred<string | null>()
        api.getDownloadPreview.mockReturnValueOnce(pending.promise)
        await clickButton('썸네일 다시 불러오기')
        expect(src()).toBeUndefined()
        await act(async () => pending.resolve('/broken.jpg'))
        expect(src()).toBe('/media?path=%2Fbroken.jpg')
    })

    it('does not show a loaded image from another URL while the new preview is pending', async () => {
        const host = document.createElement('div'); document.body.append(host)
        const root = createRoot(host)
        try {
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/old', localPath: '/old.jpg' })))
            await act(async () => document.querySelector('img')!.dispatchEvent(new Event('load')))
            const pending = deferred<string | null>(); api.getDownloadPreview.mockReturnValueOnce(pending.promise)
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/new' })))
            expect(src()).toBeUndefined()
            await act(async () => pending.resolve('/new.jpg'))
            expect(src()).toBe('/media?path=%2Fnew.jpg')
        } finally { await act(async () => root.unmount()); host.remove() }
    })

    it('renders an initial local path immediately and reuses it on screen re-entry without a preview request', async () => {
        const pending = deferred<string | null>(); api.getDownloadPreview.mockReturnValue(pending.promise)
        const props = { url: 'https://example.invalid/a', completed: true, localPath: '/local.jpg' }
        const unmount = await mount(h(DownloadThumbnail, props))
        expect(src()).toBe('/media?path=%2Flocal.jpg')
        expect(document.body.textContent).not.toContain('미리보기 준비 중')
        expect(api.getDownloadPreview).not.toHaveBeenCalled()
        await unmount(); await mount(h(StrictMode, null, h(DownloadThumbnail, props)))
        expect(src()).toBe('/media?path=%2Flocal.jpg')
        expect(api.getDownloadPreview).not.toHaveBeenCalled()
    })

    it('applies path changes from list polling without fetching a separate preview', async () => {
        const host = document.createElement('div'); document.body.append(host)
        const root = createRoot(host)
        try {
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/a', localPath: '/cached.jpg' })))
            expect(src()).toBe('/media?path=%2Fcached.jpg')
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/a', localPath: '/generated.jpg', completed: true })))
            expect(src()).toBe('/media?path=%2Fgenerated.jpg')
            expect(api.getDownloadPreview).not.toHaveBeenCalled()
        } finally { await act(async () => root.unmount()); host.remove() }
    })

    it('keeps a supplied local image during event refresh and transient lookup failures', async () => {
        await mount(h(DownloadThumbnail, { url: 'https://example.invalid/a', localPath: '/local.jpg', completed: true }))
        const pending = deferred<string | null>(); api.getDownloadPreview.mockReturnValueOnce(pending.promise)
        await act(async () => updated(1))
        expect(src()).toBe('/media?path=%2Flocal.jpg')
        expect(document.body.textContent).not.toContain('미리보기 준비 중')
        await act(async () => pending.reject(new Error('temporary')))
        expect(src()).toBe('/media?path=%2Flocal.jpg')
        api.getDownloadPreview.mockResolvedValue('/generated.jpg')
        await act(async () => updated(1))
        expect(src()).toBe('/media?path=%2Fgenerated.jpg')
    })

    it('recovers an unreadable supplied path through manual retry without restoring the stale hint', async () => {
        await mount(h(DownloadThumbnail, { url: 'https://example.invalid/a', localPath: '/missing.jpg' }))
        await act(async () => document.querySelector('img')!.dispatchEvent(new Event('error')))
        expect(api.getDownloadPreview).not.toHaveBeenCalled()
        await clickButton('썸네일 다시 불러오기')
        expect(src()).toBeUndefined()
        expect(document.body.textContent).toContain('썸네일 없음')
        api.getDownloadPreview.mockResolvedValue('/restored.jpg')
        await clickButton('썸네일 다시 불러오기')
        expect(src()).toBe('/media?path=%2Frestored.jpg')
    })

    it('does not overwrite a newly supplied local path with an older lookup response', async () => {
        const pending = deferred<string | null>(); api.getDownloadPreview.mockReturnValueOnce(pending.promise)
        const host = document.createElement('div'); document.body.append(host)
        const root = createRoot(host)
        try {
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/a' })))
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/a', localPath: '/new.jpg' })))
            await act(async () => pending.resolve('/old.jpg'))
            expect(src()).toBe('/media?path=%2Fnew.jpg')
            expect(api.getDownloadPreview).toHaveBeenCalledTimes(1)
        } finally { await act(async () => root.unmount()); host.remove() }
    })

    it('shows cached local media and refreshes after generation with subscription cleanup', async () => {
        api.getDownloadPreview.mockResolvedValue('/cache/previews/a.jpg')
        const unmount = await mount(h(DownloadThumbnail, { url: 'https://example.invalid/a', completed: true }))
        expect(src()).toBe('/media?path=%2Fcache%2Fpreviews%2Fa.jpg')
        const generated = deferred<string | null>()
        api.getDownloadPreview.mockReturnValueOnce(generated.promise)
        await act(async () => updated(12))
        expect(src()).toBe('/media?path=%2Fcache%2Fpreviews%2Fa.jpg')
        await act(async () => generated.resolve('/cache/thumbnails/generated.jpg'))
        expect(src()).toBe('/media?path=%2Fcache%2Fthumbnails%2Fgenerated.jpg')
        expect(api.getDownloadPreview).toHaveBeenCalledTimes(2)
        await unmount()
        expect(unsubscribe).toHaveBeenCalledTimes(1)
    })

    it('switches to the library thumbnail on completion without remounting the card', async () => {
        const host = document.createElement('div'); document.body.append(host)
        const root = createRoot(host)
        try {
            api.getDownloadPreview.mockResolvedValue('/cached.jpg')
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/a' })))
            expect(api.onMediaThumbnailUpdated).not.toHaveBeenCalled()
            api.getDownloadPreview.mockResolvedValue('/local.jpg')
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/a', completed: true })))
            expect(src()).toBe('/media?path=%2Flocal.jpg')
            expect(api.onMediaThumbnailUpdated).toHaveBeenCalledTimes(1)
        } finally { await act(async () => root.unmount()); host.remove() }
    })

    it('supports retry for missing and unreadable previews', async () => {
        await mount(h(DownloadThumbnail, { url: 'https://example.invalid/a' }))
        expect(document.body.textContent).toContain('썸네일 없음')
        api.getDownloadPreview.mockResolvedValue('/cached.jpg')
        await clickButton('썸네일 다시 불러오기')
        expect(src()).toBe('/media?path=%2Fcached.jpg')
        await act(async () => document.querySelector('img')!.dispatchEvent(new Event('error')))
        expect(document.body.textContent).toContain('썸네일 없음')
        api.getDownloadPreview.mockRejectedValueOnce(new Error('offline'))
        await clickButton('썸네일 다시 불러오기')
        expect(src()).toBeUndefined()
        await clickButton('썸네일 다시 불러오기')
        expect(src()).toBe('/media?path=%2Fcached.jpg')
    })

    it('ignores late responses after URL changes and unmounting', async () => {
        const old = deferred<string | null>()
        api.getDownloadPreview.mockReturnValueOnce(old.promise).mockResolvedValue('/new.jpg')
        const host = document.createElement('div'); document.body.append(host)
        const root = createRoot(host)
        try {
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/old' })))
            expect(document.body.textContent).toContain('미리보기 준비 중')
            await act(async () => root.render(h(DownloadThumbnail, { url: 'https://example.invalid/new' })))
            await act(async () => old.resolve('/old.jpg'))
            expect(src()).toBe('/media?path=%2Fnew.jpg')
        } finally { await act(async () => root.unmount()); host.remove() }
        const late = deferred<string | null>(); api.getDownloadPreview.mockReturnValueOnce(late.promise)
        const unmount = await mount(h(DownloadThumbnail, { url: 'https://example.invalid/late' }))
        await unmount(); await act(async () => late.resolve('/late.jpg'))
        expect(src()).toBeUndefined()
    })

    it('keeps one live event subscription after StrictMode initialization', async () => {
        api.getDownloadPreview.mockResolvedValue('/cached.jpg')
        const unmount = await mount(h(StrictMode, null, h(DownloadThumbnail, { url: 'https://example.invalid/a', completed: true })))
        expect(src()).toBe('/media?path=%2Fcached.jpg')
        expect(api.onMediaThumbnailUpdated.mock.calls.length - unsubscribe.mock.calls.length).toBe(1)
        await unmount()
        expect(api.onMediaThumbnailUpdated.mock.calls.length).toBe(unsubscribe.mock.calls.length)
    })
})
