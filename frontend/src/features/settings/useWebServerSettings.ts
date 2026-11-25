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
            cancel = () => { clearTimeout(timer); reject(new Error(translate("settings.releaseStatusRequest"))) }
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
                    ? translate("settings.theWebServerStatusRequestDidNotRespondWithin")
                    : translate("settings.couldNotCheckWebServerStatus"))
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
            if (mounted.current && own === revision.current) setError(translate("settings.noWebServerResponseReceivedCheckStatusAgain"))
        } finally {
            if (own === revision.current) {
                locked.current = false
                if (mounted.current) setBusy(false)
            }
        }
    }
    const togglePermission = async (kind: 'editing' | 'downloading' | 'access') => {
        if (!mounted.current || locked.current || !status) return
        const own = ++revision.current
        locked.current = true; reading.current?.cancel(); reading.current = null
        setLoading(false); setBusy(true); setError(null)
        try {
            const result = await (kind === 'access' ? getApi().webServer.revokeAccess() : kind === 'editing' ? getApi().webServer.setEditing(!status.editing) : getApi().webServer.setDownloading(!status.downloading))
            if (!mounted.current || own !== revision.current) return
            if (result.ok) setStatus(result.data)
            else setError(translateError(result.error))
        } catch {
            if (mounted.current && own === revision.current) setError(translate(kind === 'editing' ? 'settings.couldNotChangeMobileEditingPermission' : 'settings.mobileDownloads.permissionError'))
        } finally {
            if (own === revision.current) { locked.current = false; if (mounted.current) setBusy(false) }
        }
    }
    return { status, loading, busy, error, refresh, toggle, revokeAccess: () => togglePermission('access'), toggleEditing: () => togglePermission('editing'), toggleDownloading: () => togglePermission('downloading') }
}
