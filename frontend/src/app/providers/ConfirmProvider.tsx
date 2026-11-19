import { useCallback, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { flushSync } from 'react-dom'
import { ConfirmContext, type Confirm, type ConfirmOptions } from '@renderer/shared/hooks/confirmContext'
import { ConfirmDialog } from '@renderer/shared/components/ui/ConfirmDialog'

type Request = { id: number; options: ConfirmOptions; resolve: (accepted: boolean) => void; origin: HTMLElement | null }
export function ConfirmProvider({ children }: { children: ReactNode }) {
 const [request, setRequest] = useState<Request | null>(null)
 const active = useRef<Request | null>(null)
 const sequence = useRef(0)
 const alive = useRef(false)
 useLayoutEffect(() => {
  alive.current = true
  return () => { alive.current = false; const pending = active.current; active.current = null; pending?.resolve(false) }
 }, [])
 const confirm = useCallback<Confirm>(input => {
  if (!alive.current || active.current) return Promise.resolve(false)
  const focused = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const origin = focused
  return new Promise(resolve => {
   const value: Request = { id: ++sequence.current, options: typeof input === 'string' ? { message: input } : { ...input }, resolve, origin }
   active.current = value; setRequest(value)
  })
 }, [])
 const finish = useCallback((id: number, accepted: boolean) => {
  const current = active.current
  if (!current || current.id !== id) return
  active.current = null
  // Release inert and remove the dialog before any confirmed continuation runs.
  flushSync(() => setRequest(null))
  if (current.origin?.isConnected) current.origin.focus({ preventScroll: true })
  current.resolve(accepted)
 }, [])
 const value = useMemo(() => ({ confirm, isOpen: request !== null }), [confirm, request])
 return <ConfirmContext.Provider value={value}>
  <div className="contents" data-confirm-background inert={request !== null}>{children}</div>
  {request && <ConfirmDialog key={request.id} options={request.options} onAnswer={answer => finish(request.id, answer)} />}
 </ConfirmContext.Provider>
}
