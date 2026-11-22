import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import type { UpdateStatus, UpdateApi } from '@contracts/update-api'
import { getApi } from '@renderer/shared/api'

type Action = Exclude<keyof UpdateApi, 'status'>
interface UpdateView {
    status: UpdateStatus | null
    pending: Action | null
    error: string
    act(action: Action): Promise<void>
}
const UpdateContext = createContext<UpdateView | null>(null)
export const useAppUpdate = () => useContext(UpdateContext)

export function AppUpdateProvider({ children }: { children: ReactNode }) {
    const [status, setStatus] = useState<UpdateStatus | null>(null)
    const [pending, setPending] = useState<Action | null>(null)
    const [error, setError] = useState('')
    const alive = useRef(false)
    const generation = useRef(0)
    const locked = useRef(false)
    useEffect(() => {
        alive.current = true
        let stopped = false
        let timer: ReturnType<typeof setTimeout> | undefined
        async function poll() {
            const own = generation.current
            let delay = 5000
            try {
                const result = await getApi().updates.status()
                if (stopped || own !== generation.current) return
                if (result.ok) {
                    setStatus(result.data)
                    if (['checking', 'downloading', 'verifying', 'installing', 'restarting'].includes(result.data.state)) delay = 750
                } else setError(result.error.message)
            } catch {
                if (!stopped && own === generation.current) setError('업데이트 상태를 확인하지 못했습니다.')
            } finally {
                if (!stopped) timer = setTimeout(() => void poll(), delay)
            }
        }
        void poll()
        return () => { stopped = true; alive.current = false; generation.current++; if (timer) clearTimeout(timer) }
    }, [])

    async function act(action: Action) {
        if (locked.current) return
        locked.current = true
        generation.current++
        setPending(action); setError('')
        try {
            const result = await getApi().updates[action]()
            if (!alive.current) return
            generation.current++
            if (result.ok) { setStatus(result.data); setError(result.data.error) }
            else setError(result.error.message)
        } catch {
            if (alive.current) setError('업데이트 요청 결과를 받지 못했습니다. 상태를 확인한 뒤 다시 시도하세요.')
        } finally {
            locked.current = false
            if (alive.current) setPending(null)
        }
    }
    return <UpdateContext.Provider value={{ status, pending, error, act }}>{children}</UpdateContext.Provider>
}
