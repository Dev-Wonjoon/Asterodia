import { useId, useLayoutEffect, useRef } from 'react'
import { BaseModal } from './BaseModal'
import type { ConfirmOptions } from '../../hooks/confirmContext'

export function ConfirmDialog({ options, onAnswer }: { options: ConfirmOptions; onAnswer: (accepted: boolean) => void }) {
 const heading = useId(), description = useId()
 const panel = useRef<HTMLElement>(null)
 const cancel = useRef<HTMLButtonElement>(null)
 const accept = useRef<HTMLButtonElement>(null)
 useLayoutEffect(() => {
  cancel.current?.focus({ preventScroll: true })
  const key = (event: KeyboardEvent) => {
   if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); onAnswer(false) }
   if (event.key === 'Tab') {
    event.preventDefault(); event.stopImmediatePropagation()
    if (document.activeElement === cancel.current) accept.current?.focus()
    else cancel.current?.focus()
   }
  }
  const focus = (event: FocusEvent) => { if (event.target instanceof Node && !panel.current?.contains(event.target)) cancel.current?.focus({ preventScroll: true }) }
  document.addEventListener('keydown', key, true)
  document.addEventListener('focusin', focus, true)
  return () => { document.removeEventListener('keydown', key, true); document.removeEventListener('focusin', focus, true) }
 }, [onAnswer])
 return <BaseModal closeOnOverlay={false} layer={100000}>
  <section ref={panel} role="alertdialog" aria-modal="true" aria-labelledby={heading} aria-describedby={description} data-confirm-dialog data-detail-trigger
   className="max-h-[calc(100dvh-2rem)] overflow-y-auto w-[min(28rem,calc(100vw-2rem))] space-y-5 p-6 text-[var(--text-main)]">
   <h2 id={heading} className="text-lg font-semibold">{options.title ?? '확인'}</h2>
   <p id={description} className="whitespace-pre-wrap break-words text-sm leading-relaxed text-[var(--text-muted)]">{options.message}</p>
   <div className="flex justify-end gap-2">
    <button ref={cancel} type="button" onClick={() => onAnswer(false)} className="rounded-lg border border-[var(--border-line)] px-4 py-2 text-sm focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]">{options.cancelLabel ?? '취소'}</button>
    <button ref={accept} type="button" onClick={() => onAnswer(true)} className={`rounded-lg px-4 py-2 text-sm text-white focus-visible:outline-2 focus-visible:outline-[var(--color-primary)] ${options.danger ? 'bg-red-600 hover:bg-red-700' : 'bg-[var(--color-primary)] hover:bg-[var(--color-primary-hover)]'}`}>{options.confirmLabel ?? '확인'}</button>
   </div>
  </section>
 </BaseModal>
}
