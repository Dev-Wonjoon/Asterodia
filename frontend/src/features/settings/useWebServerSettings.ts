import { useCallback, useEffect, useRef, useState } from 'react'
import type { WebServerStatus } from '@contracts/webserver-api'
import { getApi } from '@renderer/shared/api'

export function useWebServerSettings() {
    const [status, setStatus] = useState<WebServerStatus | null>(null)
    const [loading, setLoading] = useState(true)
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const mounted = useRef(false)
    const locked = useRef(false)
    const reading = useRef(false)
    const revision = useRef(0)

    const refresh = useCallback(async () => {
        if (locked.current || reading.current) return
        reading.current = true
        const own = ++revision.current
        try {
            const result = await getApi().webServer.status()
            if (!mounted.current || own !== revision.current) return
            if (result.ok) { setStatus(result.data); setError(result.data.error) }
            else setError(result.error.message)
        } catch {
            if (mounted.current && own === revision.current) setError('웹 서버 상태를 확인하지 못했습니다.')
        } finally {
            reading.current = false
            if (mounted.current && own === revision.current) setLoading(false)
        }
    }, [])

    useEffect(() => { mounted.current = true; void refresh(); return () => { mounted.current = false; revision.current++ } }, [refresh])
    useEffect(() => {
        if (!status?.running) return
        const timer = setInterval(() => void refresh(), 2000)
        return () => clearInterval(timer)
    }, [status?.running, refresh])

    const toggle = async () => {
        if (locked.current || !status) return
        locked.current = true; revision.current++; setBusy(true); setError(null)
        try {
            const result = await (status.running ? getApi().webServer.stop() : getApi().webServer.start())
            if (!mounted.current) return
            if (result.ok) { setStatus(result.data); setError(result.data.error) }
            else setError(result.error.message)
        } catch {
            if (mounted.current) setError('웹 서버 요청 결과를 받지 못했습니다. 상태를 다시 확인해 주세요.')
        } finally {
            locked.current = false
            if (mounted.current) setBusy(false)
        }
    }
    return { status, loading, busy, error, refresh, toggle }
}
