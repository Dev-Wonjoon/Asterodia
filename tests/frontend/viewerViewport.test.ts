// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { viewportGeometry, mountViewerViewport, zoomSteps } from '../../internal/webserver/web/viewer-viewport.js'

afterEach(() => { document.body.replaceChildren(); vi.restoreAllMocks() })

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

it('shares one video frame subscription and cancels it after the last pane is released', () => {
    const video = document.createElement('video'), callbacks = new Map<number, Function>(); let serial = 0
    Object.defineProperties(video, { paused: { value: false }, videoWidth: { value: 2000 }, videoHeight: { value: 1000 }, readyState: { value: 2 } })
    video.requestVideoFrameCallback = vi.fn(callback => { callbacks.set(++serial, callback); return serial })
    video.cancelVideoFrameCallback = vi.fn(id => { callbacks.delete(id) })
    const drawImage = vi.fn(); vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({ setTransform() {}, clearRect() {}, drawImage } as any)
    const create = () => {
        const host = document.createElement('div'); document.body.append(host)
        Object.defineProperties(host, { clientWidth: { value: 800 }, clientHeight: { value: 450 } })
        return mountViewerViewport(host, { source: video, state: { zoom: 2, x: .5, y: .5 } })
    }
    const first = create(), second = create()
    expect(callbacks.size).toBe(1); expect(video.requestVideoFrameCallback).toHaveBeenCalledOnce()
    drawImage.mockClear(); const callback = callbacks.get(serial)!; callbacks.delete(serial); callback()
    expect(drawImage).toHaveBeenCalledTimes(4) // Each pane and its minimap use the same source frame.
    expect(drawImage.mock.calls.every(args => args[0] === video)).toBe(true)
    first.destroy(); expect(callbacks.size).toBe(1)
    second.destroy(); expect(callbacks.size).toBe(0); expect(video.cancelVideoFrameCallback).toHaveBeenCalledOnce()
    video.dispatchEvent(new Event('play')); expect(callbacks.size).toBe(0)
})
