import { useConfirm } from './useConfirm'
import { useContext, useLayoutEffect, useRef } from 'react'
import { LeaveGuardContext } from './leaveGuardContext'
import { confirmDiscard } from '../utils/leaveGuard'

export function useLeaveGuard(dirty: boolean, busy: boolean): void {
    const { confirm } = useConfirm()
    const registeredRef = useContext(LeaveGuardContext)
    const latest = useRef({ dirty, busy })
    useLayoutEffect(() => {
        latest.current = { dirty, busy }
    }, [dirty, busy])
    useLayoutEffect(() => {
        if (!registeredRef) return
        const guard = (): boolean | Promise<boolean> => {
            const before = latest.current
            if (before.busy) return false
            const answer = confirmDiscard(before.dirty, confirm)
            if (typeof answer === 'boolean') return answer
            return answer.then(accepted => accepted && registeredRef.current === guard && latest.current === before)
        }
        registeredRef.current = guard
        return () => {
            if (registeredRef.current === guard) registeredRef.current = null
        }
    }, [registeredRef, confirm])
}
