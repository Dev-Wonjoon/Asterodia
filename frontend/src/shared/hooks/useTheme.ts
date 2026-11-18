import { useLayoutEffect, useRef, useState } from 'react'
import { flushSync } from 'react-dom'

type Theme = 'light' | 'dark'

export function useTheme(): { theme: Theme; toggleTheme(): void } {
    const [theme, setTheme] = useState<Theme>(() => {
        const saved = localStorage.getItem('theme')
        if (saved === 'light' || saved === 'dark') return saved
        return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
    })
    const pendingTheme = useRef(theme)
    const revision = useRef(0)
    const transition = useRef<ViewTransition | null>(null)
    useLayoutEffect(() => {
        document.documentElement.classList.toggle('dark', theme === 'dark')
        localStorage.setItem('theme', theme)
    }, [theme])
    useLayoutEffect(() => () => {
        revision.current++
        transition.current?.skipTransition()
        document.documentElement.classList.remove('theme-switching')
    }, [])

    const toggleTheme = (): void => {
        const root = document.documentElement
        const token = ++revision.current
        const next = pendingTheme.current === 'light' ? 'dark' : 'light'
        pendingTheme.current = next
        transition.current?.skipTransition()
        root.classList.add('theme-switching')
        const apply = (): void => {
            if (token !== revision.current) return
            flushSync(() => setTheme(next))
            // Commit all element styles while their independent transitions are off.
            void root.offsetWidth
        }
        const finish = (): void => {
            if (token !== revision.current) return
            transition.current = null
            root.classList.remove('theme-switching')
        }
        if (document.startViewTransition && !window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) {
            try {
                const active = document.startViewTransition(apply)
                transition.current = active
                void active.ready.catch(() => {})
                void active.finished.then(finish, finish)
                return
            } catch {
                // Older WebViews switch every color in the same frame.
            }
        }
        apply()
        requestAnimationFrame(() => requestAnimationFrame(finish))
    }
    return { theme, toggleTheme }
}
