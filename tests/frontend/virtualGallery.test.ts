// @vitest-environment happy-dom
import { act, createElement as h, useRef, useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanupReact, mount } from './reactHarness'
import { useVirtualGallery, type GalleryGeometry } from '@renderer/features/gallery/useVirtualGallery'
import { galleryDisplayKey, gallerySelectionKey, useGallerySelection } from '@renderer/features/gallery/useGallerySelection'
import { useGalleryKeyboardFocus } from '@renderer/features/gallery/useGalleryKeyboardFocus'
import type { MediaSummaryDto } from '@contracts/media-v2'

let width: number, cardHeight: number
let frames: Map<number, FrameRequestCallback>, serial: number
let observers: Set<{ callback: ResizeObserverCallback; disconnect: ReturnType<typeof vi.fn> }>
let virtual: ReturnType<typeof useVirtualGallery>, selection: ReturnType<typeof useGallerySelection>
let updateRows: (count: number) => void
let changeRows: (change: (items: MediaSummaryDto[]) => MediaSummaryDto[]) => void
let disableCards: (disabled: boolean) => void
const rows = (count: number) => Array.from({ length: count }, (_, id) => ({ id, revision: 1, title: `Card ${id}`, fileCount: 1 } as MediaSummaryDto))

beforeEach(() => {
    width = 700; cardHeight = 200; frames = new Map(); serial = 0; observers = new Set()
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++serial, callback); return serial })
    vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
    vi.stubGlobal('ResizeObserver', class {
        disconnect = vi.fn(() => observers.delete(this))
        constructor(public callback: ResizeObserverCallback) { observers.add(this) }
        observe() {}
    })
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(function () { return this.hasAttribute('data-scroll') ? 600 : 0 })
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function () {
        return this.hasAttribute('data-scroll') ? 120 + (Number.parseFloat(this.querySelector<HTMLElement>('[data-grid]')?.style.height ?? '') || 0) : 0
    })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
        const root = this.closest<HTMLElement>('[data-scroll]')
        if (this.hasAttribute('data-scroll')) return new DOMRect(0, 0, width, 600)
        if (this.hasAttribute('data-grid')) return new DOMRect(0, 100 - (root?.scrollTop ?? 0), width, Number.parseFloat(this.style.height) || 0)
        if (this.hasAttribute('data-gallery-key')) return new DOMRect(Number.parseFloat(this.style.left), 100 - (root?.scrollTop ?? 0) + Number.parseFloat(this.style.top), Number.parseFloat(this.style.width), cardHeight)
        return new DOMRect()
    })
})
afterEach(async () => { await cleanupReact(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

function Harness({ count = 1000 }: { count?: number }) {
    const [items, setItems] = useState(() => rows(count))
    const [disabled, setDisabled] = useState(false)
    disableCards = setDisabled
    updateRows = count => setItems(rows(count))
    changeRows = change => setItems(change)
    const root = useRef<HTMLDivElement>(null), blocked = useRef(false), geometry = useRef<GalleryGeometry | null>(null)
    selection = useGallerySelection(items, blocked, root, 'same', geometry)
    virtual = useVirtualGallery(selection.gridRef, root, items.map(galleryDisplayKey), geometry)
    useGalleryKeyboardFocus(selection.gridRef, items, virtual.indices.join(','))
    return h('div', { ref: root, 'data-scroll': true },
        h('div', { ref: selection.gridRef, 'data-grid': true, style: virtual.style }, virtual.indices.map(index => {
            const item = items[index]
            return h('div', { key: item.id, 'data-gallery-key': galleryDisplayKey(item), style: virtual.slotStyle(index) },
                h('article', { 'data-album-id': item.id, 'data-selection-key': gallerySelectionKey(item), 'data-selected': selection.entries.has(gallerySelectionKey(item)) || undefined },
                    h('button', { 'data-card-focus': true, disabled, onClick: event => selection.select(item, event) }, item.title),
                    h('img', { src: `/thumbnail/${item.id}` })))
        })))
}
async function tick() {
    await act(async () => { const pending = [...frames.values()]; frames.clear(); pending.forEach(callback => callback(0)) })
}
async function scroll(top: number) {
    const root = document.querySelector<HTMLElement>('[data-scroll]')!
    await act(async () => { root.scrollTop = top; root.dispatchEvent(new Event('scroll')) })
    await tick()
}
const cards = () => [...document.querySelectorAll<HTMLElement>('article')]
const button = (id: number) => document.querySelector<HTMLButtonElement>(`[data-album-id="${id}"] button`)!
async function pointer(type: string, target: EventTarget, x: number, y: number) {
    await act(async () => target.dispatchEvent(new PointerEvent(type, { bubbles: true, button: 0, pointerId: 1, pointerType: 'mouse', clientX: x, clientY: y })))
}

it('bounds mounted cards and thumbnails across repeated scrolling through 10,000 items', async () => {
    await mount(h(Harness, { count: 10000 }))
    const firstImage = document.querySelector('img')!
    expect(cards().length).toBeLessThan(20)
    for (const top of [8000, 40000, 120000, 500000, 8000, 0]) {
        await scroll(top)
        expect(cards().length).toBeLessThan(20)
        expect(document.querySelectorAll('img').length).toBe(cards().length)
    }
    expect(firstImage.isConnected).toBe(false)
    expect(cards()[0].dataset.albumId).toBe('0')
})
it('preserves offscreen selections and Shift ranges while remounting cards', async () => {
    await mount(h(Harness))
    await act(async () => button(0).click())
    await scroll(16000)
    const last = Number(cards().at(-1)!.dataset.albumId)
    await act(async () => button(last).dispatchEvent(new MouseEvent('click', { bubbles: true, shiftKey: true })))
    expect(selection.entries.size).toBe(last + 1)
    expect(selection.entries.has('media:0')).toBe(true)
    await scroll(0)
    expect(cards()[0].dataset.selected).toBe('true')
})
it('mounts and focuses an offscreen keyboard target and preserves a roving Tab stop', async () => {
    await mount(h(Harness))
    await act(async () => button(0).focus())
    await act(async () => virtual.focus('999:media:999'))
    expect(document.activeElement).toBe(button(999))
    await tick()
    expect(cards().length).toBeLessThan(20)
    expect(button(999).tabIndex).toBe(0)
    expect(document.querySelectorAll('button[tabindex="0"]')).toHaveLength(1)
    await act(async () => virtual.focus('0:media:0'))
    expect(document.activeElement).toBe(button(0))
})
it('retains only focused and native drag source cards outside the visible range', async () => {
    await mount(h(Harness))
    await act(async () => button(0).focus())
    await act(async () => button(1).dispatchEvent(new Event('dragstart', { bubbles: true })))
    await scroll(30000)
    expect(button(0)).not.toBeNull(); expect(button(1)).not.toBeNull()
    await act(async () => { button(0).blur(); window.dispatchEvent(new Event('dragend')) })
    await tick()
    expect(button(0)).toBeNull(); expect(button(1)).toBeNull()
    expect(cards().length).toBeLessThan(20)
})
it('keeps the same offscreen focused card when refresh prepends or removes earlier rows', async () => {
    await mount(h(Harness))
    const original = button(0)
    await act(async () => original.focus())
    await scroll(20000)
    await act(async () => changeRows(items => [{ ...items[0], id: -1 }, ...items]))
    expect(document.activeElement).toBe(original)
    expect(original.isConnected).toBe(true)
    await act(async () => changeRows(items => items.filter(item => item.id !== -1)))
    expect(document.activeElement).toBe(original)
    expect(cards().length).toBeLessThan(20)
})
it('keeps focus when reordering moves the focused card out of the mounted window', async () => {
    await mount(h(Harness))
    const last = button(Number(cards().at(-1)!.dataset.albumId))
    await act(async () => last.focus())
    await act(async () => changeRows(items => [...items.slice(100), ...items.slice(0, 100)]))
    expect(document.activeElement).toBe(last)
    expect(last.isConnected).toBe(true)
    expect(cards().length).toBeLessThan(20)
})
it('restores keyboard focus and a Tab stop to a visible card if the focused item is removed', async () => {
    await mount(h(Harness))
    await act(async () => button(0).focus())
    await scroll(20000)
    await act(async () => changeRows(items => items.filter(item => item.id !== 0)))
    expect(document.activeElement?.matches('[data-card-focus]')).toBe(true)
    expect(document.activeElement?.closest('article')?.getAttribute('data-album-id')).not.toBe('0')
    expect((document.activeElement as HTMLElement).tabIndex).toBe(0)
    expect(document.querySelectorAll('button[tabindex="0"]')).toHaveLength(1)
})
it('does not steal focus from an input outside the gallery when the old active card is removed', async () => {
    await mount(h(Harness))
    const input = document.createElement('input'); document.body.append(input)
    await act(async () => { button(0).focus(); input.focus() })
    await act(async () => changeRows(items => items.filter(item => item.id !== 0)))
    expect(document.activeElement).toBe(input)
    input.remove()
})
it('restores a card shortcut target after refresh temporarily disables the focused button', async () => {
    const setAttribute = HTMLElement.prototype.setAttribute
    // Model the browser moving focus away when a focused button is disabled.
    vi.spyOn(HTMLElement.prototype, 'setAttribute').mockImplementation(function(name, value) {
        if (name === 'disabled' && document.activeElement === this) (this as HTMLElement).blur()
        setAttribute.call(this, name, value)
    })
    await mount(h(Harness))
    const original = button(0)
    await act(async () => original.focus())
    await act(async () => disableCards(true))
    expect(document.activeElement).toBe(document.body)
    await act(async () => disableCards(false))
    expect(document.activeElement).toBe(original)
    expect(original.tabIndex).toBe(0)
})
it('cancels pending focus restoration when the user moves to an external input during refresh', async () => {
    const setAttribute = HTMLElement.prototype.setAttribute
    vi.spyOn(HTMLElement.prototype, 'setAttribute').mockImplementation(function(name, value) {
        if (name === 'disabled' && document.activeElement === this) (this as HTMLElement).blur()
        setAttribute.call(this, name, value)
    })
    await mount(h(Harness))
    const input = document.createElement('input'); document.body.append(input)
    await act(async () => button(0).focus())
    await act(async () => disableCards(true))
    await act(async () => input.focus())
    await act(async () => disableCards(false))
    expect(document.activeElement).toBe(input)
    input.remove()
})
it('selects cards crossed by a marquee even after they have scrolled out of the DOM', async () => {
    await mount(h(Harness))
    const grid = document.querySelector('[data-grid]')!
    await pointer('pointerdown', grid, 1, 90)
    await pointer('pointermove', window, 310, 590)
    await scroll(2500)
    await pointer('pointermove', window, 310, 590)
    await pointer('pointerup', window, 310, 590)
    expect(document.querySelector('[data-album-id="0"]')).toBeNull()
    expect(selection.entries.has('media:0')).toBe(true)
    expect(selection.entries.has('media:20')).toBe(true)
})
it('updates responsive columns and card heights, handles shrinking lists and releases observers and animation frames', async () => {
    const stop = await mount(h(Harness))
    await scroll(15000)
    const firstVisible = Number(cards()[4].dataset.albumId)
    width = 300; cardHeight = 280
    await act(async () => { for (const observer of [...observers]) observer.callback([], {} as ResizeObserver) })
    await tick()
    expect(virtual.columns).toBe(1)
    expect(cards().length).toBeLessThan(15)
    expect(cards().some(card => Number(card.dataset.albumId) === firstVisible)).toBe(true)
    await act(async () => updateRows(2))
    await scroll(0)
    expect(cards()).toHaveLength(2)
    await act(async () => updateRows(0))
    expect(cards()).toHaveLength(0)
    await stop()
    expect(observers.size).toBe(0)
    expect(frames.size).toBe(0)
})
