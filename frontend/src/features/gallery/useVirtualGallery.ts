import { useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type RefObject } from 'react'
import { flushSync } from 'react-dom'

const GAP = 16
const OVERSCAN = 2

export interface GalleryGeometry {
    bounds(key: string, origin?: DOMRect): DOMRect | null
}

type Layout = { width: number; columns: number; pitch: number; start: number; end: number; retained: string[] }

/** Keep card components/images near the viewport. List metadata remains available to selection. */
export function useVirtualGallery(
    gridRef: RefObject<HTMLDivElement | null>,
    scrollRef: RefObject<HTMLDivElement | null>,
    keys: string[],
    geometryRef?: RefObject<GalleryGeometry | null>
) {
    const [layout, setLayout] = useState<Layout>({ width: 260, columns: 1, pitch: 320, start: 0, end: 8, retained: [] })
    const current = useRef(layout)
    const latestKeys = useRef(keys); latestKeys.current = keys
    const heights = useRef(new Map<string, number>())
    const dragging = useRef<string | null>(null)
    const focusKey = useRef<string | undefined>(undefined)
    const committingFocus = useRef(false)
    committingFocus.current = gridRef.current?.contains(document.activeElement) ?? false
    const frame = useRef(0)
    const positions = useMemo(() => new Map(keys.map((key, index) => [key, index])), [keys])
    const positionsRef = useRef(positions); positionsRef.current = positions

    function measure() {
        const grid = gridRef.current, root = scrollRef.current
        if (!grid || !root) return
        const width = grid.getBoundingClientRect().width || grid.clientWidth
        // Hidden galleries retain their geometry and scroll position for explorer return.
        if (width <= 0) return
        const columns = Math.max(1, Math.floor((width + GAP) / (260 + GAP)))
        const cardWidth = Math.min(320, (width - (columns - 1) * GAP) / columns)
        const previous = current.current
        const resized = Math.abs(width - previous.width) > 0.5
        if (resized) heights.current.clear()
        let height = resized ? 0 : previous.pitch - GAP
        for (const slot of grid.querySelectorAll<HTMLElement>(':scope > [data-gallery-key]')) {
            const value = slot.getBoundingClientRect().height
            if (value > 0) { height = Math.max(height, value); heights.current.set(slot.dataset.galleryKey!, value) }
        }
        const pitch = (height || cardWidth * 9 / 16 + 156) + GAP
        const gridTop = grid.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop - root.clientTop
        // Preserve the visible card when width, language or card height changes.
        if (root.scrollTop > gridTop && (resized || Math.abs(pitch - previous.pitch) > 0.5)) {
            const row = Math.floor((root.scrollTop - gridTop) / previous.pitch)
            const index = row * previous.columns
            root.scrollTop = gridTop + Math.floor(index / columns) * pitch + (root.scrollTop - gridTop) % previous.pitch
        }
        const top = Math.max(0, root.scrollTop - gridTop)
        const rowCount = Math.ceil(latestKeys.current.length / columns)
        const start = Math.max(0, Math.min(rowCount - 1, Math.floor(top / pitch)) - OVERSCAN) * columns
        const end = Math.min(latestKeys.current.length, (Math.ceil((top + (root.clientHeight || 600)) / pitch) + OVERSCAN) * columns)
        const retained = new Set<string>()
        const focused = grid.contains(document.activeElement) ? (document.activeElement as Element).closest<HTMLElement>('[data-gallery-key]')?.dataset.galleryKey
            : document.activeElement === document.body ? focusKey.current : undefined
        for (const key of [focused, dragging.current]) {
            const index = key ? positionsRef.current.get(key) : undefined
            if (key && index !== undefined && (index < start || index >= end)) retained.add(key)
        }
        const next = { width, columns, pitch, start, end, retained: [...retained] }
        if (next.width !== previous.width || next.columns !== previous.columns || next.pitch !== previous.pitch ||
            next.start !== previous.start || next.end !== previous.end || next.retained.join(',') !== previous.retained.join(',')) {
            current.current = next
            setLayout(next)
        }
    }

    function bounds(key: string, origin = gridRef.current?.getBoundingClientRect()) {
        const grid = gridRef.current, index = positionsRef.current.get(key)
        if (!grid || !origin || index === undefined) return null
        const { width, columns, pitch } = current.current
        const slotWidth = (width + GAP) / columns
        return new DOMRect(origin.left + index % columns * slotWidth, origin.top + Math.floor(index / columns) * pitch,
            Math.min(320, slotWidth - GAP), heights.current.get(key) ?? pitch - GAP)
    }
    if (geometryRef) geometryRef.current = { bounds }

    // Capture the live focus key before React removes/reorders children. A data
    // refresh can move a visible card out of the window before measure runs.
    if (gridRef.current?.contains(document.activeElement)) focusKey.current = (document.activeElement as Element).closest<HTMLElement>('[data-gallery-key]')?.dataset.galleryKey
    const focusedKey = document.activeElement === document.body || gridRef.current?.contains(document.activeElement) ? focusKey.current : undefined
    const retainedKeys = [...layout.retained, focusedKey, dragging.current]
    const indices = [...new Set([
        ...Array.from({ length: Math.max(0, Math.min(keys.length, layout.end) - layout.start) }, (_, i) => layout.start + i),
        ...retainedKeys.flatMap(key => {
            const index = key ? positions.get(key) : undefined
            return index === undefined ? [] : [index]
        })
    ])].sort((a, b) => a - b)
    const renderedKey = indices.join(',')
    useLayoutEffect(() => {
        const present = new Set(keys)
        for (const key of heights.current.keys()) if (!present.has(key)) heights.current.delete(key)
    }, [keys])
    useLayoutEffect(() => { committingFocus.current = false; measure() })
    useLayoutEffect(() => {
        const grid = gridRef.current, root = scrollRef.current
        if (!grid || !root) return
        const schedule = () => {
            cancelAnimationFrame(frame.current)
            frame.current = requestAnimationFrame(measure)
        }
        const dragStart = (event: DragEvent) => {
            dragging.current = (event.target as Element).closest<HTMLElement>('[data-gallery-key]')?.dataset.galleryKey ?? null
        }
        const dragEnd = () => { dragging.current = null; schedule() }
        const rememberFocus = (event: FocusEvent) => {
            const target = event.target instanceof Element ? event.target : null
            focusKey.current = target && grid.contains(target) ? target.closest<HTMLElement>('[data-gallery-key]')?.dataset.galleryKey : undefined
            schedule()
        }
        const leaveFocus = (event: FocusEvent) => {
            const target = event.target instanceof HTMLElement ? event.target : null
            // A deliberate blur releases the extra card. React removing,
            // moving or disabling the focused control keeps its logical key.
            if (!event.relatedTarget && target?.isConnected && !target.matches(':disabled') && !committingFocus.current) focusKey.current = undefined
            schedule()
        }
        const observer = new ResizeObserver(schedule)
        observer.observe(root); observer.observe(grid)
        for (const slot of grid.querySelectorAll(':scope > [data-gallery-key]')) observer.observe(slot)
        root.addEventListener('scroll', schedule, { passive: true })
        document.addEventListener('focusin', rememberFocus); grid.addEventListener('focusout', leaveFocus)
        grid.addEventListener('dragstart', dragStart, true)
        window.addEventListener('dragend', dragEnd)
        return () => {
            cancelAnimationFrame(frame.current); observer.disconnect()
            root.removeEventListener('scroll', schedule)
            document.removeEventListener('focusin', rememberFocus); grid.removeEventListener('focusout', leaveFocus)
            grid.removeEventListener('dragstart', dragStart, true); window.removeEventListener('dragend', dragEnd)
        }
    }, [gridRef, scrollRef, renderedKey])

    function focus(key: string) {
        const root = scrollRef.current, grid = gridRef.current, box = bounds(key)
        if (!root || !grid || !box) return
        const viewport = root.getBoundingClientRect()
        if (box.top < viewport.top + root.clientTop) root.scrollTop += box.top - viewport.top - root.clientTop
        else if (box.bottom > viewport.top + root.clientTop + root.clientHeight) root.scrollTop += box.bottom - viewport.top - root.clientTop - root.clientHeight
        // Keyboard navigation may target a card which is not mounted yet.
        flushSync(measure)
        const slot = [...grid.querySelectorAll<HTMLElement>(':scope > [data-gallery-key]')].find(node => node.dataset.galleryKey === key)
        slot?.querySelector<HTMLButtonElement>('[data-card-focus]:not(:disabled)')?.focus({ preventScroll: true })
    }
    const style: CSSProperties = { position: 'relative', display: 'block', height: Math.max(0, Math.ceil(keys.length / layout.columns) * layout.pitch - GAP), overflowAnchor: 'none' }
    function slotStyle(index: number): CSSProperties {
        const slotWidth = (layout.width + GAP) / layout.columns
        return { position: 'absolute', left: index % layout.columns * slotWidth, top: Math.floor(index / layout.columns) * layout.pitch, width: Math.min(320, slotWidth - GAP) }
    }
    return { indices, columns: layout.columns, style, slotStyle, focus }
}
