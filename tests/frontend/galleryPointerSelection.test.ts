// @vitest-environment happy-dom
import { act, createElement as h, useRef } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { GalleryCard } from '@renderer/features/gallery/GalleryCard'
import { gallerySelectionKey, useGallerySelection } from '@renderer/features/gallery/useGallerySelection'
import { cleanupReact, mount } from './reactHarness'

const rows = [1, 2, 3, 4, 5].map(id => ({ id, title: `항목 ${id}`, fileCount: 2, revision: 1, createdAt: 0 })) as any[]
let selection: ReturnType<typeof useGallerySelection>
const view = vi.fn(), details = vi.fn(), action = vi.fn()
function Harness({ items = rows, reset = '', disabled = false }: { items?: any[]; reset?: string; disabled?: boolean }) {
    const scroll = useRef<HTMLDivElement>(null), blocked = useRef(disabled); blocked.current = disabled
    selection = useGallerySelection(items, blocked, scroll, reset)
    return h('div', { ref: scroll, 'data-scroll': true },
        h('div', { ref: selection.gridRef, 'data-grid': true }, ...items.map(item => h(GalleryCard, {
            key: `${item.id}:${gallerySelectionKey(item)}`, media: item, previewPath: null, selected: selection.entries.has(gallerySelectionKey(item)),
            selectionKey: gallerySelectionKey(item), onSelect: event => selection.select(item, event), onView: view, onDetails: details,
            selection: h('label', null, h('input', { type: 'checkbox', checked: selection.entries.has(gallerySelectionKey(item)), onChange: () => {}, onClick: event => selection.select(item, event, true) })),
            children: h('div', { 'data-action-boundary': true }, h('button', { onClick: action }, '작업')),
        }))))
}
afterEach(async () => { await cleanupReact(); vi.restoreAllMocks(); vi.unstubAllGlobals(); view.mockClear(); details.mockClear(); action.mockClear() })
const keys = () => [...selection.entries.keys()]
const cardButton = (id: number) => document.querySelector<HTMLButtonElement>(`article[data-album-id="${id}"] button[data-card-select]`)!
async function click(id: number, modifiers = {}) {
    await pointer('pointerdown', cardButton(id), 0, 0, modifiers)
    await pointer('pointerup', cardButton(id), 0, 0, modifiers)
    await act(async () => cardButton(id).dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 1, ...modifiers })))
}
async function pointer(type: string, target: EventTarget, x: number, y: number, modifiers = {}) {
    await act(async () => target.dispatchEvent(new PointerEvent(type, { bubbles: true, cancelable: true, pointerId: 1, pointerType: 'mouse', button: 0, clientX: x, clientY: y, ...modifiers })))
}
function geometry() {
    const grid = document.querySelector<HTMLElement>('[data-grid]')!, root = document.querySelector<HTMLElement>('[data-scroll]')!
    vi.spyOn(root, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 300, 200))
    Object.defineProperties(root, { scrollHeight: { value: 600, configurable: true }, clientHeight: { value: 200, configurable: true } })
    for (const [index, card] of [...grid.querySelectorAll<HTMLElement>('article')].entries()) {
        vi.spyOn(card, 'getBoundingClientRect').mockImplementation(() => new DOMRect(index % 2 * 70 + 10, Math.floor(index / 2) * 80 + 20 - root.scrollTop, 50, 40))
    }
    return { grid, root }
}

it('selects on one click and opens only on double click while preserving action buttons', async () => {
    await mount(h(Harness)); await click(1); expect(keys()).toEqual(['media:1']); expect(view).not.toHaveBeenCalled()
    await click(2); expect(keys()).toEqual(['media:2'])
    await act(async () => cardButton(2).dispatchEvent(new MouseEvent('dblclick', { bubbles: true, detail: 2 })))
    expect(view).toHaveBeenCalledOnce()
    await act(async () => document.querySelector<HTMLButtonElement>('[data-action-boundary] button')!.click())
    expect(action).toHaveBeenCalledOnce(); expect(keys()).toEqual(['media:2'])
})
it('toggles with Ctrl, shrinks Shift ranges and adds ranges with Ctrl+Shift', async () => {
    await mount(h(Harness)); await click(1); await click(3, { ctrlKey: true }); expect(keys()).toEqual(['media:1', 'media:3'])
    await click(5, { shiftKey: true }); expect(keys()).toEqual(['media:3', 'media:4', 'media:5'])
    await click(4, { shiftKey: true }); expect(keys()).toEqual(['media:3', 'media:4'])
    await click(2, { shiftKey: true, ctrlKey: true }); expect(new Set(keys())).toEqual(new Set(['media:1', 'media:2', 'media:3']))
    await click(2, { ctrlKey: true }); expect(keys()).not.toContain('media:2')
})
it('selects mixed groups and expanded files in displayed order and deduplicates shared files', async () => {
    const items = [rows[0], { ...rows[1], galleryFile: { id: 20, title: '공유 파일' } }, { ...rows[2], galleryFile: { id: 20, title: '공유 파일' } }, rows[4]]
    await mount(h(Harness, { items })); await click(1); await click(5, { shiftKey: true })
    expect(keys()).toEqual(['media:1', 'file:20', 'media:5']); expect(selection.fileIds).toEqual([20])
})
it('selects intersecting cards by dragging, adds with Ctrl and cancels without losing the old selection', async () => {
    vi.stubGlobal('requestAnimationFrame', vi.fn(() => 1)); vi.stubGlobal('cancelAnimationFrame', vi.fn())
    await mount(h(Harness)); const { grid } = geometry()
    await pointer('pointerdown', grid, 5, 5); await pointer('pointermove', window, 140, 70)
    expect(keys()).toEqual(['media:1', 'media:2']); expect(selection.dragging).toBe(true)
    expect(selection.groups).toHaveLength(0)
    await pointer('pointerup', window, 140, 70); expect(selection.dragging).toBe(false); expect(selection.groups).toHaveLength(2)
    await click(5); await pointer('pointerdown', grid, 5, 5, { ctrlKey: true }); await pointer('pointermove', window, 140, 70)
    expect(new Set(keys())).toEqual(new Set(['media:1', 'media:2', 'media:5']))
    await act(async () => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(keys()).toEqual(['media:5']); expect(selection.rectangle).toBeNull()
    await act(async () => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(keys()).toEqual([])
})
it('scrolls at the edge, clears a blank click and ignores touch and disabled gestures', async () => {
    let tick: FrameRequestCallback = () => {}
    vi.stubGlobal('requestAnimationFrame', vi.fn(callback => { tick = callback; return 1 })); vi.stubGlobal('cancelAnimationFrame', vi.fn())
    const stop = await mount(h(Harness)); const { grid, root } = geometry()
    await pointer('pointerdown', grid, 5, 5); await pointer('pointermove', window, 140, 195)
    await act(async () => tick(0)); expect(root.scrollTop).toBe(14)
    await pointer('pointerup', window, 140, 195)
    await click(1); await pointer('pointerdown', grid, 150, 90); await pointer('pointerup', window, 150, 90); expect(keys()).toEqual([])
    await pointer('pointerdown', grid, 5, 5, { pointerType: 'touch' }); await pointer('pointermove', window, 140, 70); expect(selection.dragging).toBe(false)
    await stop(); await mount(h(Harness, { disabled: true })); await click(1); expect(keys()).toEqual([])
})
it('drops stale revisions and resets the anchor when the view changes', async () => {
    let update: (items: any[], reset?: string) => void = () => {}
    const { useState } = await import('react')
    function Updating() {
        const [state, setState] = useState({ items: rows, reset: '' })
        update = (items, reset = '') => setState({ items, reset })
        return h(Harness, state)
    }
    await mount(h(Updating)); await click(1)
    await act(async () => update([{ ...rows[0], revision: 2 }, ...rows.slice(1)])); expect(keys()).toEqual([])
    await click(3); await act(async () => update(rows, '다른 검색')); expect(keys()).toEqual([])
    await click(5, { shiftKey: true }); expect(keys()).toEqual(['media:5'])
})

it.each(['thumbnail', 'title', 'body'])('starts a marquee over the card %s and suppresses release clicks', async surface => {
    vi.stubGlobal('requestAnimationFrame', vi.fn(() => 1)); vi.stubGlobal('cancelAnimationFrame', vi.fn())
    await mount(h(Harness)); geometry()
    const card = document.querySelector<HTMLElement>('article[data-album-id="1"]')!
    const origin = surface === 'thumbnail' ? cardButton(1) : card.querySelector(surface === 'title' ? 'h2 button' : 'p')!
    await pointer('pointerdown', origin, 20, 30); await pointer('pointermove', window, 140, 70)
    expect(selection.dragging).toBe(true); expect(keys()).toEqual(['media:1', 'media:2'])
    await pointer('pointerup', window, 140, 70)
    const releaseClick = new MouseEvent('click', { bubbles: true, cancelable: true, detail: 1 })
    await act(async () => origin.dispatchEvent(releaseClick))
    expect(releaseClick.defaultPrevented).toBe(true)
    await act(async () => origin.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true, detail: 2 })))
    expect(view).not.toHaveBeenCalled(); expect(details).not.toHaveBeenCalled(); expect(keys()).toEqual(['media:1', 'media:2'])
    const actionButton = card.querySelector<HTMLButtonElement>('[data-action-boundary] button')!
    await act(async () => actionButton.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, detail: 1 })))
    expect(action).not.toHaveBeenCalled()
    // Browsers can count a release click toward the next double-click sequence.
    await pointer('pointerdown', origin, 20, 30); await pointer('pointerup', origin, 20, 30)
    const repeatedClick = new MouseEvent('click', { bubbles: true, cancelable: true, detail: 2 })
    await act(async () => origin.dispatchEvent(repeatedClick))
    await act(async () => origin.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true, detail: 2 })))
    expect(repeatedClick.defaultPrevented).toBe(true); expect(view).not.toHaveBeenCalled(); expect(keys()).toEqual(['media:1', 'media:2'])
    await click(3); expect(keys()).toEqual(['media:3'])
    await act(async () => cardButton(3).dispatchEvent(new MouseEvent('dblclick', { bubbles: true, detail: 2 })))
    expect(view).toHaveBeenCalledOnce()
})

it('starts a marquee in empty scroll-pane space outside the card grid', async () => {
    vi.stubGlobal('requestAnimationFrame', vi.fn(() => 1)); vi.stubGlobal('cancelAnimationFrame', vi.fn())
    await mount(h(Harness, { items: rows.slice(0, 2) })); const { root } = geometry()
    await pointer('pointerdown', root, 200, 180); await pointer('pointermove', window, 0, 0); await pointer('pointerup', window, 0, 0)
    expect(keys()).toEqual(['media:1', 'media:2']); expect(selection.groups).toHaveLength(2)
})

it('keeps Ctrl additive selection when a drag starts on a card and restores it on cancellation', async () => {
    vi.stubGlobal('requestAnimationFrame', vi.fn(() => 1)); vi.stubGlobal('cancelAnimationFrame', vi.fn())
    await mount(h(Harness)); geometry(); await click(5)
    await pointer('pointerdown', cardButton(1), 20, 30, { ctrlKey: true }); await pointer('pointermove', window, 140, 70)
    expect(new Set(keys())).toEqual(new Set(['media:1', 'media:2', 'media:5']))
    await act(async () => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(keys()).toEqual(['media:5'])
    await pointer('pointerup', window, 140, 70)
    await act(async () => cardButton(1).dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, detail: 1 })))
    expect(keys()).toEqual(['media:5']); expect(view).not.toHaveBeenCalled()
})

it('preserves clicks below the drag threshold without capturing or clearing a Shift anchor', async () => {
    await mount(h(Harness)); const { root } = geometry()
    root.setPointerCapture = vi.fn()
    await click(1)
    await pointer('pointerdown', cardButton(3), 20, 30, { shiftKey: true }); await pointer('pointermove', window, 24, 32); await pointer('pointerup', window, 24, 32)
    expect(root.setPointerCapture).not.toHaveBeenCalled(); expect(selection.dragging).toBe(false)
    await act(async () => cardButton(3).dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 1, shiftKey: true })))
    expect(keys()).toEqual(['media:1', 'media:2', 'media:3'])
})

it('leaves action buttons, checkboxes and protected header areas out of drag selection', async () => {
    await mount(h(Harness)); const { root } = geometry(); await click(2)
    const actionButton = document.querySelector<HTMLButtonElement>('[data-action-boundary] button')!
    const checkbox = document.querySelector<HTMLInputElement>('article input')!
    const header = document.createElement('div'); header.setAttribute('data-action-boundary', ''); root.prepend(header)
    for (const origin of [actionButton, checkbox, header]) {
        await pointer('pointerdown', origin, 20, 30); await pointer('pointermove', window, 140, 70); await pointer('pointerup', window, 140, 70)
        expect(selection.dragging).toBe(false); expect(keys()).toEqual(['media:2'])
    }
    await act(async () => actionButton.click()); expect(action).toHaveBeenCalledOnce()
    await act(async () => checkbox.click()); expect(keys()).toEqual(['media:2', 'media:1'])
})

it('keeps native scrollbars available instead of starting a marquee from them', async () => {
    await mount(h(Harness)); const { root } = geometry(); await click(2)
    Object.defineProperties(root, { clientWidth: { value: 284, configurable: true }, clientHeight: { value: 184, configurable: true } })
    for (const [x, y] of [[292, 50], [150, 194]]) {
        await pointer('pointerdown', root, x, y); await pointer('pointermove', window, 20, 30); await pointer('pointerup', window, 20, 30)
        expect(selection.dragging).toBe(false); expect(keys()).toEqual(['media:2'])
    }
})
