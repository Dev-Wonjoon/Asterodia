import { useEffect, useRef, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { useActionDisplay } from '@renderer/shared/hooks/useActionDisplay'
import '@renderer/styles/action-display.css'

export function ActionContent({ icon, label }: { icon: ReactNode; label: string }) {
    const { mode } = useActionDisplay()
    const root = useRef<HTMLSpanElement>(null)
    useEffect(() => {
        const node = root.current, button = node?.closest('button')
        if (!node || !button) return
        const position = () => {
            const text = node.querySelector<HTMLElement>('.action-label-text')
            if (!text) return
            const bounds = node.closest('[data-action-boundary]')?.getBoundingClientRect()
            const left = node.getBoundingClientRect().left - 13
            const width = 44 + text.scrollWidth
            const min = (bounds?.left ?? 0) + 4 - left
            const max = (bounds?.right ?? document.documentElement.clientWidth) - 4 - left - width
            node.style.setProperty('--action-shift', `${Math.max(min, Math.min(0, max))}px`)
        }
        button.addEventListener('pointerenter', position)
        button.addEventListener('focus', position)
        if (button.matches(':hover, :focus-visible')) position()
        return () => { button.removeEventListener('pointerenter', position); button.removeEventListener('focus', position) }
    }, [label, mode])
    return <span ref={root} className="action-content" data-display={mode} aria-hidden="true">
        <span className="action-surface">
            <span className="action-icon">{icon}</span>
            <span className="action-label"><span><span className="action-label-text">{label}</span></span></span>
        </span>
    </span>
}

export function ActionButton({ icon, label, className = '', type = 'button', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { icon: ReactNode; label: string }) {
    return <button type={type} aria-label={label} title={label} className={'action-button rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40 ' + className} {...props}>
        <ActionContent icon={icon} label={label} />
    </button>
}
