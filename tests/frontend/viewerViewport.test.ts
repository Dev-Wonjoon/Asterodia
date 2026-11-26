// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { viewportGeometry, mountViewerViewport, zoomSteps } from '../../internal/webserver/web/viewer-viewport.js'

afterEach(() => { document.body.replaceChildren(); vi.useRealTimers(); vi.restoreAllMocks() })

it.each([[800, 450, 2000, 1000], [400, 600, 300, 1800], [300, 200, 4000, 300]])('fits and clamps every zoom for viewport %s×%s and media %s×%s', (w, h, mw, mh) => {
    const fit = viewportGeometry(w, h, mw, mh, { zoom: 1, x: 0, y: 1 })!
    expect(fit.x).toBe(.5); expect(fit.y).toBe(.5)
    expect(fit.visibleX).toBe(1); expect(fit.visibleY).toBe(1)
    for (const zoom of zoomSteps) for (const x of [-20, .5, 20]) for (const y of [-20, .5, 20]) {
        const g = viewportGeometry(w, h, mw, mh, { zoom, x, y })!
        expect(g.x - g.visibleX / 2).toBeGreaterThanOrEqual(-1e-12)
        expect(g.y - g.visibleY / 2).toBeGreaterThanOrEqual(-1e-12)
        expect(g.x + g.visibleX / 2).toBeLessThanOrEqual(1 + 1e-12)
        expect(g.y + g.visibleY / 2).toBeLessThanOrEqual(1 + 1e-12)
        if (g.width >= w) { expect(g.left).toBeLessThanOrEqual(1e-9); expect(g.left + g.width).toBeGreaterThanOrEqual(w - 1e-9) }
        if (g.height >= h) { expect(g.top).toBeLessThanOrEqual(1e-9); expect(g.top + g.height).toBeGreaterThanOrEqual(h - 1e-9) }
    }
})

it('keeps the media point under the viewport center when zooming and resizing', () => {
    const state = { zoom: 4, x: .7, y: .55 }
    for (const [w, h] of [[800, 450], [400, 225], [300, 500]]) {
        const g = viewportGeometry(w, h, 2000, 1000, state)!
        expect((w / 2 - g.left) / g.width).toBeCloseTo(state.x)
        expect((h / 2 - g.top) / g.height).toBeCloseTo(state.y)
    }
    expect(viewportGeometry(0, 450, 2000, 1000, state)).toBeNull()
    expect(viewportGeometry(800, 450, 0, 0, state)).toBeNull()
})

it('waits for image dimensions without losing a saved center, and resets to fit', () => {
    const host = document.createElement('div'); document.body.append(host)
    Object.defineProperties(host, { clientWidth: { value: 800 }, clientHeight: { value: 450 } })
    const img = document.createElement('img'), state = { zoom: 4, x: .8, y: .7 }
    const view = mountViewerViewport(host, { source: img, state })
    expect(state.x).toBe(.8); expect(state.y).toBe(.7)
    expect(host.querySelector<HTMLElement>('.sbv-minimap')!.hidden).toBe(true)
    Object.defineProperties(img, { naturalWidth: { value: 2000 }, naturalHeight: { value: 1000 } }); img.dispatchEvent(new Event('load'))
    expect(host.querySelector<HTMLElement>('.sbv-minimap')!.hidden).toBe(false)
    view.zoom(0); expect(state).toEqual({ zoom: 1, x: .5, y: .5 })
    view.destroy(); expect(host.children).toHaveLength(0)
})

it('fits images through CSS before metadata and resize callbacks, while keeping explicit zoom and reset', () => {
    const host = document.createElement('div'), img = document.createElement('img')
    document.body.append(host)
    Object.defineProperties(host, { clientWidth: { value: 390, configurable: true }, clientHeight: { value: 500, configurable: true } })
    const view = mountViewerViewport(host, { source: img, state: { zoom: 1, x: .5, y: .5 } })
    expect(img.style.width).toBe('100%'); expect(img.style.height).toBe('100%')
    Object.defineProperties(img, { naturalWidth: { value: 4000 }, naturalHeight: { value: 1000 } })
    img.dispatchEvent(new Event('load'))
    expect(img.style.width).toBe('100%'); expect(img.style.left).toBe('0px')
    view.zoom(1); expect(img.style.width).toBe('487.5px')
    view.zoom(0); expect(img.style.width).toBe('100%'); expect(img.style.height).toBe('100%')
    Object.defineProperties(host, { clientWidth: { value: 844 }, clientHeight: { value: 150 } })
    globalThis.dispatchEvent(new Event('resize'))
    expect(img.style.width).toBe('100%'); expect(img.style.height).toBe('100%')
    view.destroy()
})

it('shares one video frame subscription and cancels it after the last pane is released', () => {
    let now = 0; vi.spyOn(performance, 'now').mockImplementation(() => now)
    const video = document.createElement('video'), callbacks = new Map<number, Function>(); let serial = 0
    Object.defineProperties(video, { paused: { value: false }, videoWidth: { value: 2000 }, videoHeight: { value: 1000 }, readyState: { value: 2 } })
    video.requestVideoFrameCallback = vi.fn(callback => { callbacks.set(++serial, callback); return serial })
    video.cancelVideoFrameCallback = vi.fn(id => { callbacks.delete(id) })
    const drawImage = vi.fn(); vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({ setTransform() {}, clearRect() {}, drawImage } as any)
    const create = () => {
        const host = document.createElement('div'); document.body.append(host)
        Object.defineProperties(host, { clientWidth: { value: 800 }, clientHeight: { value: 450 } })
        return mountViewerViewport(host, { source: video, sharedVideo: true, state: { zoom: 2, x: .5, y: .5 } })
    }
    const first = create(), second = create()
    expect(callbacks.size).toBe(1); expect(video.requestVideoFrameCallback).toHaveBeenCalledOnce()
    drawImage.mockClear(); const callback = callbacks.get(serial)!; callbacks.delete(serial); callback()
    expect(drawImage).toHaveBeenCalledTimes(2) // Only the main surfaces repaint at the video frame rate.
    expect(drawImage.mock.calls.every(args => args[0] === video)).toBe(true)
    now = 200; drawImage.mockClear(); const next = callbacks.get(serial)!; callbacks.delete(serial); next()
    expect(drawImage).toHaveBeenCalledTimes(4) // Minimap refreshes are limited to 5 fps.
    first.destroy(); expect(callbacks.size).toBe(1)
    second.destroy(); expect(callbacks.size).toBe(0); expect(video.cancelVideoFrameCallback).toHaveBeenCalledOnce()
    video.dispatchEvent(new Event('play')); expect(callbacks.size).toBe(0)
})

it('displays independent 4K videos directly without frame callbacks or canvas paints at fit', () => {
    const draw = vi.spyOn(HTMLCanvasElement.prototype, 'getContext')
    const frame = vi.spyOn(globalThis, 'requestAnimationFrame')
    const originals: { view: ReturnType<typeof mountViewerViewport>; video: HTMLVideoElement; player: HTMLElement; style: string | null }[] = []
    for (let i = 0; i < 2; i++) {
        const host = document.createElement('div'), player = document.createElement('div'), video = document.createElement('video')
        document.body.append(host, player); player.append(video); video.style.opacity = '0.9'
        const originalStyle = video.getAttribute('style')
        Object.defineProperties(host, { clientWidth: { value: 960 }, clientHeight: { value: 540 } })
        Object.defineProperties(video, { videoWidth: { value: 3840 }, videoHeight: { value: 2160 }, readyState: { value: 2 }, paused: { value: false } })
        video.requestVideoFrameCallback = vi.fn()
        const view = mountViewerViewport(host, { source: video, state: { zoom: 1, x: .5, y: .5 } })
        originals.push({ view, video, player, style: originalStyle })
        expect(view.surface).toBe(video); expect(host.querySelector('video')).toBe(video)
        expect(video.style.width).toBe('960px'); expect(video.style.height).toBe('540px')
        video.dispatchEvent(new Event('play')); video.dispatchEvent(new Event('timeupdate'))
        expect(video.requestVideoFrameCallback).not.toHaveBeenCalled()
    }
    expect(document.querySelectorAll('.sbv-native-video')).toHaveLength(2)
    expect(draw).not.toHaveBeenCalled(); expect(frame).not.toHaveBeenCalled()
    for (const { view, video, player, style } of originals) {
        view.destroy(); expect(video.parentNode).toBe(player); expect(video.getAttribute('style')).toBe(style)
        expect(video.classList.contains('sbv-native-video')).toBe(false)
    }
})

it('refreshes only the visible native minimap at 5 fps and stops it on deselect, pause, fit and destroy', async () => {
    vi.useFakeTimers(); let now = 0; vi.spyOn(performance, 'now').mockImplementation(() => now)
    const host = document.createElement('div'), video = document.createElement('video')
    document.body.append(host)
    const readWidth = vi.fn(() => 800), readHeight = vi.fn(() => 450)
    Object.defineProperties(host, { clientWidth: { get: readWidth }, clientHeight: { get: readHeight } })
    Object.defineProperties(video, { videoWidth: { value: 3840 }, videoHeight: { value: 2160 }, readyState: { value: 2 }, paused: { value: false, configurable: true } })
    const drawImage = vi.fn(); vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({ setTransform() {}, clearRect() {}, drawImage } as any)
    const view = mountViewerViewport(host, { source: video, state: { zoom: 2, x: .5, y: .5 } })
    const map = host.querySelector<HTMLCanvasElement>('.sbv-minimap canvas')!
    expect(drawImage).toHaveBeenCalledOnce(); expect(vi.getTimerCount()).toBe(1)
    drawImage.mockClear(); readWidth.mockClear(); readHeight.mockClear()
    now = 199; await vi.advanceTimersByTimeAsync(199); expect(drawImage).not.toHaveBeenCalled()
    now = 200; await vi.advanceTimersByTimeAsync(1); expect(drawImage).toHaveBeenCalledOnce()
    expect(readWidth).not.toHaveBeenCalled(); expect(readHeight).not.toHaveBeenCalled()
    expect(map.width).toBeLessThanOrEqual(320)
    view.select(false); expect(vi.getTimerCount()).toBe(0)
    view.select(true); expect(vi.getTimerCount()).toBe(1)
    Object.defineProperty(video, 'paused', { value: true }); video.dispatchEvent(new Event('pause')); expect(vi.getTimerCount()).toBe(0)
    drawImage.mockClear(); video.dispatchEvent(new Event('seeked')); expect(drawImage).toHaveBeenCalledOnce()
    Object.defineProperty(video, 'paused', { value: false }); video.dispatchEvent(new Event('play')); expect(vi.getTimerCount()).toBe(1)
    view.zoom(0); expect(vi.getTimerCount()).toBe(0)
    view.zoom(1); expect(vi.getTimerCount()).toBe(1)
    view.destroy(); expect(vi.getTimerCount()).toBe(0)
    video.dispatchEvent(new Event('play')); expect(vi.getTimerCount()).toBe(0)
})

it('updates native geometry on metadata and resize without losing the zoom center', () => {
    const host = document.createElement('div'), video = document.createElement('video'), state = { zoom: 4, x: .7, y: .6 }
    document.body.append(host)
    Object.defineProperties(host, { clientWidth: { value: 800, configurable: true }, clientHeight: { value: 450 } })
    const view = mountViewerViewport(host, { source: video, state })
    expect(state.x).toBe(.7)
    Object.defineProperties(video, { videoWidth: { value: 3840 }, videoHeight: { value: 2160 } })
    video.dispatchEvent(new Event('loadedmetadata'))
    expect(video.style.width).toBe('3200px'); expect(state.x).toBe(.7)
    Object.defineProperty(host, 'clientWidth', { value: 400 }); globalThis.dispatchEvent(new Event('resize'))
    expect(video.style.width).toBe('1600px'); expect(state.x).toBe(.7)
    view.destroy()
})
