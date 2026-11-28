import { useEffect, type RefObject } from 'react'

const LOAD_MORE_DISTANCE = 20

interface InfiniteScrollOptions {
    rootRef: RefObject<HTMLElement | null>
    targetRef: RefObject<HTMLElement | null>
    enabled: boolean
    onLoadMore: () => Promise<unknown>
}

export function useInfiniteScroll({
    rootRef,
    targetRef,
    enabled,
    onLoadMore
}: InfiniteScrollOptions): void {
    useEffect(() => {
        const root = rootRef.current
        const target = targetRef.current

        if (!enabled || !root || !target) return

        let active = true, pending = false
        const loadAtBottom = (): void => {
            if (!active || pending || root.clientHeight <= 0 ||
                root.scrollHeight - root.scrollTop - root.clientHeight > LOAD_MORE_DISTANCE) return

            pending = true
            // The caller owns loading/error state; prevent scroll and observer
            // notifications from submitting the same page while it is pending.
            void onLoadMore().then(
                () => { pending = false },
                () => { pending = false }
            )
        }

        const observer = new IntersectionObserver(
            (entries) => {
                if (entries.some((entry) => entry.isIntersecting)) {
                    loadAtBottom()
                }
            },
            {
                root,
                rootMargin: '0px',
                threshold: 0
            }
        )

        observer.observe(target)
        // The sentinel may stay visible while scrolling past the footer, so
        // entering the final 20px must also be checked on actual scroll events.
        root.addEventListener('scroll', loadAtBottom, { passive: true })

        return () => {
            active = false
            observer.disconnect()
            root.removeEventListener('scroll', loadAtBottom)
        }
    }, [rootRef, targetRef, enabled, onLoadMore])
}
