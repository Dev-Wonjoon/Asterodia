import { translateError, translate } from '@renderer/shared/i18n'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { WebServerResult, WebServerStatus } from '@contracts/webserver-api'
import { getApi } from '@renderer/shared/api'

const STATUS_TIMEOUT = Symbol('web-server-status-timeout')
const STATUS_TIMEOUT_MS = 10_000

type StatusRead = { cancel: () => void }

export function useWebServerSettings() {
    const [status, setStatus] = useState<WebServerStatus | null>(null)
    const [loading, setLoading] = useState(true)
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const mounted = useRef(false)
    const locked = useRef(false)
    const reading = useRef<StatusRead | null>(null)
    const revision = useRef(0)

    const refresh = useCallback(async (showLoading = true) => {
        if (!mounted.current || locked.current || reading.current) return
        const own = ++revision.current
        let timer: ReturnType<typeof setTimeout> | undefined
        let cancel!: () => void
        const deadline = new Promise<WebServerResult<WebServerStatus>>((_, reject) => {
            timer = setTimeout(() => reject(STATUS_TIMEOUT), STATUS_TIMEOUT_MS)
            cancel = () => { clearTimeout(timer); reject(new Error(translate("상태 조회 해제"))) }
        })
        const request: StatusRead = { cancel }
        reading.current = request
        setLoading(showLoading)
        setError(null)
        try {
            const result = await Promise.race([getApi().webServer.status(), deadline])
            if (!mounted.current || own !== revision.current) return
            if (result.ok) { setStatus(result.data); setError(result.data.error) }
            else { setStatus(null); setError(translateError(result.error)) }
        } catch (problem) {
            if (mounted.current && own === revision.current) {
                setStatus(null)
                setError(problem === STATUS_TIMEOUT
                    ? translate("웹 서버 상태 조회가 10초 안에 응답하지 않았습니다. 다시 확인해 주세요.")
                    : translate("웹 서버 상태를 확인하지 못했습니다."))
            }
        } finally {
            clearTimeout(timer)
            // An abandoned read must not release the lock of a newer request.
            if (reading.current === request) reading.current = null
            if (mounted.current && own === revision.current) setLoading(false)
        }
    }, [])

    useEffect(() => {
        mounted.current = true
        setBusy(false)
        void refresh()
        return () => {
            mounted.current = false
            revision.current++
            reading.current?.cancel()
            reading.current = null
            locked.current = false
        }
    }, [refresh])
    useEffect(() => {
        if (!status?.running) return
        const timer = setInterval(() => void refresh(false), 2000)
        return () => clearInterval(timer)
    }, [status?.running, refresh])

    const toggle = async () => {
        if (!mounted.current || locked.current || !status) return
        const own = ++revision.current
        locked.current = true
        reading.current?.cancel()
        reading.current = null
        setLoading(false); setBusy(true); setError(null)
        try {
            const result = await (status.running ? getApi().webServer.stop() : getApi().webServer.start())
            if (!mounted.current || own !== revision.current) return
            if (result.ok) { setStatus(result.data); setError(result.data.error) }
            else setError(translateError(result.error))
        } catch {
            if (mounted.current && own === revision.current) setError(translate("웹 서버 요청 결과를 받지 못했습니다. 상태를 다시 확인해 주세요."))
        } finally {
            if (own === revision.current) {
                locked.current = false
                if (mounted.current) setBusy(false)
            }
        }
    }
    const toggleEditing = async () => {
        if (!mounted.current || locked.current || !status) return
        const own = ++revision.current
        locked.current = true; reading.current?.cancel(); reading.current = null
        setLoading(false); setBusy(true); setError(null)
        try {
            const result = await getApi().webServer.setEditing(!status.editing)
            if (!mounted.current || own !== revision.current) return
            if (result.ok) setStatus(result.data)
            else setError(translateError(result.error))
        } catch {
            if (mounted.current && own === revision.current) setError(translate("모바일 편집 허용 상태를 바꾸지 못했습니다."))
        } finally {
            if (own === revision.current) { locked.current = false; if (mounted.current) setBusy(false) }
        }
    }
    return { status, loading, busy, error, refresh, toggle, toggleEditing }
}
