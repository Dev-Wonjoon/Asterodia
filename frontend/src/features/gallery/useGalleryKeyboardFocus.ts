import { useLayoutEffect, useRef, type RefObject } from 'react'

// A grid is one Tab stop; Tab then visits the focused card's actions.
export function useGalleryKeyboardFocus(grid: RefObject<HTMLDivElement | null>, rows: unknown, visible?: string) {
    const active = useRef<string | undefined>(undefined)
    useLayoutEffect(() => {
        const root = grid.current
        if (!root) return
        function update() {
            const cards = [...root!.querySelectorAll<HTMLElement>('[data-selection-key]')]
            const tabKey = cards.some(card => card.dataset.selectionKey === active.current) ? active.current : cards[0]?.dataset.selectionKey
            if (!active.current) active.current = tabKey
            for (const card of cards) {
                const current = card.dataset.selectionKey === tabKey
                for (const control of card.querySelectorAll<HTMLElement>('button,input,a[href]')) control.tabIndex = current ? 0 : -1
            }
        }
        const focus = (event: FocusEvent) => {
            const card = (event.target as Element)?.closest<HTMLElement>('[data-selection-key]')
            if (card && root.contains(card)) { active.current = card.dataset.selectionKey; update() }
        }
        update(); root.addEventListener('focusin', focus)
        return () => root.removeEventListener('focusin', focus)
    }, [grid, rows, visible])
    return active
}
