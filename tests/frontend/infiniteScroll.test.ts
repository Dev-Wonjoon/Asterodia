// @vitest-environment happy-dom
import { act, createElement as h, useRef } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useInfiniteScroll } from '@renderer/shared/hooks/useInfiniteScroll'
import { cleanupReact, deferred, mount } from './reactHarness'

const observers = new Set<{ callback: IntersectionObserverCallback; disconnect(): void }>()
let observerOptions: IntersectionObserverInit | undefined
beforeEach(() => {
    observers.clear()
    vi.stubGlobal('IntersectionObserver', class {
        constructor(public callback: IntersectionObserverCallback, options: IntersectionObserverInit) { observerOptions = options }
        observe() { observers.add(this) }
        disconnect() { observers.delete(this) }
    })
})
afterEach(async () => { await cleanupReact(); vi.unstubAllGlobals() })

function Harness({ load, enabled = true }: { load(): Promise<unknown>; enabled?: boolean }) {
    const root = useRef<HTMLDivElement>(null), target = useRef<HTMLDivElement>(null)
    useInfiniteScroll({ rootRef: root, targetRef: target, enabled, onLoadMore: load })
    return h('div', { ref: root, 'data-scroll-root': true }, h('div', { ref: target }))
}

function metrics(remaining: number, height = 400) {
    const root = document.querySelector<HTMLElement>('[data-scroll-root]')!
    Object.defineProperties(root, {
        clientHeight: { configurable: true, value: height },
        scrollHeight: { configurable: true, value: 1000 },
        scrollTop: { configurable: true, writable: true, value: 1000 - height - remaining },
    })
    return root
}
async function intersect() {
    await act(async () => {
        for (const observer of [...observers]) observer.callback([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    })
}
async function scroll(root: HTMLElement) {
    await act(async () => { root.dispatchEvent(new Event('scroll')) })
}

it('does not preload while more than 20px remains, even if the sentinel is visible', async () => {
    const load = vi.fn().mockResolvedValue(undefined)
    await mount(h(Harness, { load }))
    expect(observerOptions?.rootMargin).toBe('0px')
    for (const distance of [300, 21, 20.01]) {
        const root = metrics(distance)
        await intersect(); await scroll(root)
    }
    expect(load).not.toHaveBeenCalled()
})

it.each([20, 19.5, 0])('loads when the remaining scroll distance is %spx', async remaining => {
    const load = vi.fn().mockResolvedValue(undefined)
    await mount(h(Harness, { load })); metrics(remaining)
    await intersect()
    expect(load).toHaveBeenCalledOnce()
})

it('loads on entering the last 20px without another intersection notification', async () => {
    const load = vi.fn().mockResolvedValue(undefined)
    await mount(h(Harness, { load }))
    const root = metrics(80)
    await intersect(); expect(load).not.toHaveBeenCalled()
    root.scrollTop += 60
    await scroll(root)
    expect(load).toHaveBeenCalledOnce()
})

it('does not overlap requests and waits for the new bottom after appending items', async () => {
    const pending = deferred<unknown>(), load = vi.fn().mockReturnValueOnce(pending.promise).mockResolvedValue(undefined)
    await mount(h(Harness, { load }))
    const root = metrics(20)
    await intersect(); await scroll(root); await intersect()
    expect(load).toHaveBeenCalledOnce()
    Object.defineProperty(root, 'scrollHeight', { configurable: true, value: 1400 })
    await act(async () => { pending.resolve(undefined) })
    await intersect(); await scroll(root)
    expect(load).toHaveBeenCalledOnce()
    root.scrollTop = 980
    await scroll(root)
    expect(load).toHaveBeenCalledTimes(2)
})

it('ignores hidden or disabled lists and removes listeners when unmounted', async () => {
    const load = vi.fn().mockResolvedValue(undefined)
    const stop = await mount(h(Harness, { load }))
    const root = metrics(0, 0)
    await intersect(); await scroll(root)
    expect(load).not.toHaveBeenCalled()
    metrics(0)
    await stop(); await scroll(root); await intersect()
    expect(load).not.toHaveBeenCalled(); expect(observers.size).toBe(0)
    await mount(h(Harness, { load, enabled: false })); const disabled = metrics(0)
    await scroll(disabled); await intersect()
    expect(load).not.toHaveBeenCalled()
})
