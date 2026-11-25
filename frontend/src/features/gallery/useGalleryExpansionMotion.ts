import { useCallback, useLayoutEffect, useRef, type RefObject } from 'react'
import type { MediaSummaryDto } from '@contracts/media-v2'

type Position = { left: number; top: number }
type Pending = {
    mediaId: string
    rows: MediaSummaryDto[]
    positions: Map<string, Position>
    width: number
    loading: boolean
}

const cards = (grid: HTMLElement) => [...grid.querySelectorAll<HTMLElement>('[data-album-id]')]
const key = (card: HTMLElement) => `${card.dataset.albumId}:${card.dataset.selectionKey ?? 'group'}`

// Only explicit expand/collapse actions capture a layout. Polling, selection,
// pagination and initial rendering never start an animation on their own.
export function useGalleryExpansionMotion(
    gridRef: RefObject<HTMLDivElement | null>,
    scrollRef: RefObject<HTMLDivElement | null>,
    rows: MediaSummaryDto[],
    refreshing: boolean,
    scope: string
) {
    const pending = useRef<Pending | null>(null)
    const running = useRef(new Map<Animation, () => void>())

    useLayoutEffect(() => {
        const root = scrollRef.current
        if (!root) return
        const existed = root.classList.contains('motion-gallery-scroll')
        root.classList.add('motion-gallery-scroll')
        return () => { if (!existed) root.classList.remove('motion-gallery-scroll') }
    }, [scrollRef])
    const stop = useCallback(() => {
        for (const [animation, clear] of running.current) {
            animation.cancel()
            clear()
        }
        running.current.clear()
    }, [])

    useLayoutEffect(() => {
        pending.current = null
        stop()
        return () => { pending.current = null; stop() }
    }, [scope, stop])

    useLayoutEffect(() => {
        const before = pending.current, grid = gridRef.current
        if (!before || !grid) return
        if (refreshing) { before.loading = true; return }
        if (rows === before.rows) {
            if (before.loading) pending.current = null
            return
        }
        pending.current = null
        const bounds = grid.getBoundingClientRect()
        const viewport = scrollRef.current?.getBoundingClientRect()
        // Resizing/navigation while the request is pending is not an album move.
        // Compare the scroll container: a newly appearing scrollbar can narrow
        // the grid without the user resizing the gallery.
        if (Math.abs((viewport?.width ?? bounds.width) - before.width) > 1) return
        const style = grid.ownerDocument.defaultView?.getComputedStyle(grid)
        // Production CSS can minify 220ms to .22s; WAAPI always takes milliseconds.
        const time = style?.getPropertyValue('--motion-gallery').trim() ?? ''
        const value = Number.parseFloat(time)
        const duration = Number.isFinite(value) && value > 0 ? value * (time.endsWith('s') && !time.endsWith('ms') ? 1000 : 1) : 220
        const easing = style?.getPropertyValue('--motion-ease').trim() || 'cubic-bezier(0.2, 0.8, 0.2, 1)'
        const visible = (left: number, top: number, width: number, height: number) => !viewport || viewport.height <= 0 ||
            left + width > viewport.left && top + height > viewport.top && left < viewport.right && top < viewport.bottom
        // Read all positions before starting effects, avoiding layout read/write loops.
        const after = cards(grid).map(card => ({ card, rect: card.getBoundingClientRect() }))
        for (const { card, rect } of after) {
            if (typeof card.animate !== 'function' || rect.width <= 0 || rect.height <= 0) continue
            const previous = before.positions.get(key(card))
            if (!visible(rect.left, rect.top, rect.width, rect.height) &&
                (!previous || !visible(bounds.left + previous.left, bounds.top + previous.top, rect.width, rect.height))) continue
            let frames: Keyframe[], className: string
            if (previous) {
                // Grid-relative coordinates ignore scrolling during the API request.
                const x = previous.left - (rect.left - bounds.left)
                const y = previous.top - (rect.top - bounds.top)
                if (Math.abs(x) < 0.5 && Math.abs(y) < 0.5) continue
                frames = [{ transform: `translate(${x}px, ${y}px)` }, { transform: 'none' }]
                className = 'motion-gallery-moving'
            } else {
                if (card.dataset.albumId !== before.mediaId) continue
                frames = [{ opacity: 0, transform: 'translateY(8px)' }, { opacity: 1, transform: 'none' }]
                className = 'motion-gallery-entering'
            }
            card.classList.add(className)
            try {
                const animation = card.animate(frames, { duration: previous ? duration : Math.min(duration, 180), easing })
                const clear = () => { if (running.current.delete(animation)) card.classList.remove(className) }
                running.current.set(animation, clear)
                void animation.finished.then(clear, clear)
            } catch {
                // Older WebViews can still display the updated gallery normally.
                card.classList.remove(className)
            }
        }
    }, [rows, refreshing, gridRef, scrollRef])

    return useCallback((mediaId: number) => {
        stop()
        pending.current = null
        const grid = gridRef.current
        if (!grid) return () => {}
        const bounds = grid.getBoundingClientRect()
        const positions = new Map(cards(grid).map(card => {
            const rect = card.getBoundingClientRect()
            return [key(card), { left: rect.left - bounds.left, top: rect.top - bounds.top }] as const
        }))
        const own: Pending = { mediaId: String(mediaId), rows, positions,
            width: scrollRef.current?.getBoundingClientRect().width ?? bounds.width, loading: false }
        pending.current = own
        return () => { if (pending.current === own) pending.current = null }
    }, [gridRef, scrollRef, rows, stop])
}
