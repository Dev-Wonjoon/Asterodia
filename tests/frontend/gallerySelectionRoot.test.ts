// @vitest-environment happy-dom
import { createElement as h, useRef, useState, act } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { mount, cleanupReact } from './reactHarness'
import { useGallerySelection } from '@renderer/features/gallery/useGallerySelection'

afterEach(async () => { await cleanupReact(); vi.restoreAllMocks() })

it('keeps marquee selection connected when the card scroll element is replaced', async () => {
    let replaceScroller!: () => void
    const items = [{ id: 1, revision: 1 }] as any
    function Gallery() {
        const [version, setVersion] = useState(0), root = useRef<HTMLDivElement>(null), blocked = useRef(false)
        const selection = useGallerySelection(items, blocked, root, 'same gallery')
        replaceScroller = () => setVersion(value => value + 1)
        return h('div', { key: version, ref: root, 'data-scroll-pane': true },
            h('div', { ref: selection.gridRef }, h('article', { 'data-selection-key': 'media:1', 'data-selected': selection.entries.has('media:1') || undefined })))
    }
    vi.spyOn(globalThis, 'requestAnimationFrame').mockReturnValue(0)
    await mount(h(Gallery))
    const previous = document.querySelector('[data-scroll-pane]')!
    await act(async () => replaceScroller())
    const root = document.querySelector('[data-scroll-pane]')!, card = root.querySelector('article')!
    expect(root).not.toBe(previous)
    vi.spyOn(card, 'getBoundingClientRect').mockReturnValue(new DOMRect(10, 10, 100, 100))
    const pointer = async (type: string, target: EventTarget, x: number, y: number) => act(async () => {
        target.dispatchEvent(new PointerEvent(type, { bubbles: true, cancelable: true, pointerId: 1, pointerType: 'mouse', button: 0, clientX: x, clientY: y }))
    })
    await pointer('pointerdown', root, 0, 0)
    await pointer('pointermove', window, 150, 150)
    await pointer('pointerup', window, 150, 150)
    expect(card.getAttribute('data-selected')).toBe('true')
})
