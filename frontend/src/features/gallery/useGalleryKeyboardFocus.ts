import { useLayoutEffect, useRef, type RefObject } from 'react'

// A grid is one Tab stop; Tab then visits the focused card's actions.
export function useGalleryKeyboardFocus(grid: RefObject<HTMLDivElement | null>, _rows: unknown, _visible?: string) {
    const active = useRef<string | undefined>(undefined)
    const focused = useRef<HTMLElement | null>(null)
    const restorePending = useRef(false)
    const beforeCommit = grid.current?.contains(document.activeElement) ? document.activeElement : null
    // Refresh can change disabled controls without changing rows or the window.
    useLayoutEffect(() => {
        const root = grid.current
        if (!root) return
        function update() {
            const cards = [...root!.querySelectorAll<HTMLElement>('[data-selection-key]')]
            const lostFocus = focused.current !== null && document.activeElement === document.body
                && (!focused.current.isConnected || beforeCommit === focused.current || restorePending.current)
            if (lostFocus) restorePending.current = true
            const tabKey = cards.some(card => card.dataset.selectionKey === active.current) ? active.current : cards[0]?.dataset.selectionKey
            active.current = tabKey
            for (const card of cards) {
                const current = card.dataset.selectionKey === tabKey
                for (const control of card.querySelectorAll<HTMLElement>('button,input,a[href]')) control.tabIndex = current ? 0 : -1
            }
            if (lostFocus && !root!.closest('[hidden],[inert]')) {
                cards.find(card => card.dataset.selectionKey === tabKey)?.querySelector<HTMLElement>('[data-card-focus]:not(:disabled)')?.focus({ preventScroll: true })
            }
        }
        const focus = (event: FocusEvent) => {
            const target = event.target instanceof HTMLElement ? event.target : null
            const card = target?.closest<HTMLElement>('[data-selection-key]')
            focused.current = card && root.contains(card) ? target : null
            restorePending.current = false
            if (focused.current && card) { active.current = card.dataset.selectionKey; update() }
        }
        document.addEventListener('focusin', focus)
        update()
        return () => document.removeEventListener('focusin', focus)
    })
    return active
}
