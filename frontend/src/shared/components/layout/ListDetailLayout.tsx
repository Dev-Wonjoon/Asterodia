import { translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useId, useRef, useState, Ref, type ReactNode } from 'react'
import { flushSync } from 'react-dom'

export function ListDetailLayout({
    children,
    detail,
    detailKey,
    detailLabel,
    onClose,
    closeDisabled = false,
    listRef
}: {
    children: ReactNode
    detail?: ReactNode
    detailKey?: string | number | null
    detailLabel: string
    onClose: () => boolean | void | Promise<boolean | void>
    closeDisabled?: boolean
    listRef?: Ref<HTMLDivElement>
}): React.JSX.Element {
    useLanguage()
    const closing = useRef(false)
    const [replayTarget, setReplayTarget] = useState<HTMLElement | null>(null)
    const root = useRef<HTMLDivElement>(null)
    const panel = useRef<HTMLElement>(null)
    const restoreFocus = useRef(true)
    const closeButton = useRef<HTMLButtonElement>(null)
    const returnFocus = useRef<HTMLElement | null>(null)
    const headingId = useId()
    const [overlay, setOverlay] = useState(false)
    const open = detail != null

    useEffect(() => {
        const element = root.current
        if (!element) return
        const observer = new ResizeObserver(([entry]) => setOverlay(entry.contentRect.width < 960))
        observer.observe(element)
        return () => observer.disconnect()
    }, [])

    useEffect(() => {
        if (!open) return
        restoreFocus.current = true
        returnFocus.current =
            document.activeElement instanceof HTMLElement ? document.activeElement : null
        closeButton.current?.focus({ preventScroll: true })
        return () => {
            if (restoreFocus.current && returnFocus.current?.isConnected)
                returnFocus.current.focus({ preventScroll: true })
        }
    }, [open])

    useEffect(() => {
        if (!replayTarget || open) return
        setReplayTarget(null)
        closing.current = false
        if (root.current && replayTarget.isConnected) { replayTarget.focus({ preventScroll: true }); replayTarget.click() }
    }, [open, replayTarget])

    useEffect(() => {
        if (!open) return
        const closeOutside = (event: MouseEvent): void => {
            if (event.button !== 0 || event.defaultPrevented || closeDisabled || document.querySelector('[data-confirm-dialog]')) return
            if (closing.current) { event.preventDefault(); event.stopPropagation(); return }
            const target = event.target
            if (!(target instanceof Node) || panel.current?.contains(target)) return
            if (target instanceof Element && target.closest('[data-detail-trigger]')) return

            // Commit the close before the clicked control runs its own handler.
            // This clears the navigation guard once, without a second prompt.
            restoreFocus.current = false
            let result: boolean | void | Promise<boolean | void>
            flushSync(() => { result = onClose() })
            if (result! instanceof Promise) {
                closing.current = true
                event.preventDefault(); event.stopPropagation()
                const control = target instanceof Element ? target.closest<HTMLElement>('button,a,input,select,textarea,[role="button"]') : null
                void result.then(accepted => {
                    if (accepted === false) { restoreFocus.current = true; return }
                    // Replay only after React commits the detail close and removes its guard.
                    if (root.current && control?.isConnected) setReplayTarget(control)
                }).finally(() => { closing.current = false })
            } else if (result! === false) {
                restoreFocus.current = true
                event.preventDefault(); event.stopPropagation()
            }
        }
        document.addEventListener('click', closeOutside, true)
        return () => document.removeEventListener('click', closeOutside, true)
    }, [open, closeDisabled, onClose])

    return (
        <div
            ref={root}
            onKeyDown={(event) => {
                if (open && event.key === 'Escape' && !event.defaultPrevented && !event.nativeEvent.isComposing && event.keyCode !== 229) {
                    event.preventDefault()
                    if (!closeDisabled) onClose()
                }
            }}
            className="relative flex h-full min-h-0 min-w-0 overflow-hidden"
        >
            <div
                ref={listRef}
                data-keyboard-region tabIndex={-1}
                inert={open && overlay}
                className="min-h-0 min-w-0 flex-1 overflow-y-auto p-4 md:p-6"
            >
                {children}
            </div>
            {open && (
                <>
                    {overlay && (
                        <div className="motion-backdrop absolute inset-0 z-20 bg-black/30" aria-hidden="true" />
                    )}
                    <aside
                        data-keyboard-detail
                        data-keyboard-region tabIndex={-1}
                        ref={panel}
                        role={overlay ? 'dialog' : undefined}
                        aria-labelledby={headingId}
                        className={
                            'motion-panel flex min-h-0 flex-col border-l border-[var(--border-line)] bg-[var(--bg-app)] ' +
                            (overlay
                                ? 'absolute inset-y-0 right-0 z-30 w-full max-w-[400px] shadow-2xl'
                                : 'w-[400px] shrink-0')
                        }
                    >
                        <header className="flex shrink-0 items-center justify-between gap-3 border-b border-[var(--border-line)] px-5 py-4">
                            <h2 id={headingId} className="font-semibold">
                                {detailLabel}
                            </h2>
                            <button
                                ref={closeButton}
                                type="button"
                                disabled={closeDisabled}
                                onClick={onClose}
                                aria-label={detailLabel + translate("common.suffix.close")}
                                className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40"
                            >{translate("common.close")}</button>
                        </header>
                        <div key={detailKey} className="motion-page min-h-0 flex-1 overflow-y-auto p-5">
                            {detail}
                        </div>
                    </aside>
                </>
            )}
        </div>
    )
}
