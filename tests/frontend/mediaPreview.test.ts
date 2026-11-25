// @vitest-environment happy-dom
import { act, createElement as h, StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, expect, it, vi } from 'vitest'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'
import { cleanupReact, deferred, mount } from './reactHarness'
import { mockImageLoading } from './imageHarness'

afterEach(async () => { await cleanupReact(); vi.unstubAllGlobals() })
const src = () => document.querySelector('img')?.getAttribute('src')
const mediaUrl = (path: string) => '/media?path=' + encodeURIComponent(path)

async function preview() {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
    const host = document.createElement('div'); document.body.append(host)
    const root = createRoot(host)
    return {
        render: async (path: string | null, extra: Partial<Parameters<typeof MediaPreview>[0]> = {}) => {
            await act(async () => root.render(h(MediaPreview, { path, title: '미리보기', ...extra })))
        },
        close: async () => { await act(async () => root.unmount()); host.remove() }
    }
}

it('keeps the same loaded image until its replacement has loaded and decoded', async () => {
    const requests = mockImageLoading(), view = await preview()
    try {
        await view.render('/old.jpg', { draggable: false, className: 'preview-class' })
        const image = document.querySelector('img')!
        expect(image.loading).toBe('lazy')
        await act(async () => image.dispatchEvent(new Event('load')))
        await view.render('/new.jpg', { draggable: false, className: 'preview-class' })
        expect(src()).toBe(mediaUrl('/old.jpg'))
        expect(document.querySelector('img')).toBe(image)
        expect(requests).toHaveLength(1)
        expect(requests[0].src).toBe(mediaUrl('/new.jpg'))
        const decoded = deferred<void>()
        requests[0].decode.mockReturnValueOnce(decoded.promise)
        await act(async () => { void requests[0].onload?.() })
        expect(src()).toBe(mediaUrl('/old.jpg'))
        await act(async () => decoded.resolve())
        expect(src()).toBe(mediaUrl('/new.jpg'))
        expect(document.querySelector('img')).toBe(image)
        expect(image.alt).toBe('미리보기')
        expect(image.getAttribute('draggable')).toBe('false')
        expect(image.className).toBe('preview-class')
    } finally { await view.close() }
})

it('retains the working image when a replacement fails without retrying on every render', async () => {
    for (const failure of ['network', 'decode']) {
        const requests = mockImageLoading(), view = await preview()
        try {
            await view.render('/old.jpg')
            await act(async () => document.querySelector('img')!.dispatchEvent(new Event('load')))
            await view.render('/broken.jpg')
            if (failure === 'network') await act(async () => requests[0].onerror?.())
            else {
                requests[0].decode.mockRejectedValueOnce(new Error('invalid image'))
                await act(async () => { await requests[0].onload?.() })
            }
            await view.render('/broken.jpg')
            expect(src()).toBe(mediaUrl('/old.jpg'))
            expect(requests).toHaveLength(1)
        } finally { await view.close() }
    }
})

it('ignores obsolete replacement loads and detaches handlers on unmount', async () => {
    const requests = mockImageLoading(), view = await preview()
    await view.render('/old.jpg')
    await act(async () => document.querySelector('img')!.dispatchEvent(new Event('load')))
    await view.render('/obsolete.jpg')
    const obsoleteLoad = requests[0].onload!
    await view.render('/latest.jpg')
    expect(requests[0].onload).toBeNull()
    await act(async () => { await obsoleteLoad() })
    expect(src()).toBe(mediaUrl('/old.jpg'))
    await act(async () => { await requests[1].onload?.() })
    expect(src()).toBe(mediaUrl('/latest.jpg'))
    await view.render('/after-unmount.jpg')
    const lateLoad = requests[2].onload!
    await view.close()
    expect(requests[2].onload).toBeNull()
    expect(requests[2].onerror).toBeNull()
    await act(async () => { await lateLoad() })
    expect(src()).toBeUndefined()
})

it('uses fallback images once and resets failure state only for an explicit retry', async () => {
    const view = await preview()
    try {
        await view.render('/bad.jpg', { fallbackPath: '/fallback.jpg' })
        await act(async () => document.querySelector('img')!.dispatchEvent(new Event('error')))
        expect(src()).toBe(mediaUrl('/fallback.jpg'))
        await view.render('/bad.jpg', { fallbackPath: '/another-fallback.jpg' })
        expect(src()).toBe(mediaUrl('/another-fallback.jpg'))
        await act(async () => document.querySelector('img')!.dispatchEvent(new Event('error')))
        expect(src()).toBeUndefined()
        await view.render('/bad.jpg', { fallbackPath: '/another-fallback.jpg', retryKey: 1 })
        expect(src()).toBe(mediaUrl('/bad.jpg'))
    } finally { await view.close() }
})

it('clears images for an empty preview or a different identity and supports HTTP media URLs', async () => {
    const requests = mockImageLoading(), view = await preview()
    const resolveUrl = (path: string | null | undefined) => path ? '/api/media/' + encodeURIComponent(path) : null
    try {
        await view.render('/old.jpg', { identity: 'one', resolveUrl })
        await act(async () => document.querySelector('img')!.dispatchEvent(new Event('load')))
        await view.render('/new.jpg', { identity: 'two', resolveUrl })
        expect(src()).toBe('/api/media/%2Fnew.jpg')
        expect(requests).toHaveLength(0)
        await act(async () => document.querySelector('img')!.dispatchEvent(new Event('load')))
        await view.render(null, { identity: 'two', resolveUrl })
        expect(src()).toBeUndefined()
        await view.render('/third.jpg', { identity: 'two', resolveUrl })
        expect(src()).toBe('/api/media/%2Fthird.jpg')
        expect(requests).toHaveLength(0)
    } finally { await view.close() }
})

it('preserves native initial lazy loading in StrictMode', async () => {
    const requests = mockImageLoading()
    await mount(h(StrictMode, null, h(MediaPreview, { path: '/initial.jpg', title: 'initial' })))
    expect(src()).toBe(mediaUrl('/initial.jpg'))
    expect(requests).toHaveLength(0)
})
