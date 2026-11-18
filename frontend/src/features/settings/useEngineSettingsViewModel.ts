import { useState, useEffect, useCallback, useRef } from 'react'
import type { EngineInstallProgress, EngineStatus } from '@contracts/types'
import { getApi } from '@renderer/shared/api'
import { useToast } from '@renderer/shared/hooks/useToast'
import { mergeInstallProgress } from './engineInstall'

export function useEngineSettingsViewModel() {
    const [engines, setEngines] = useState<Record<string, EngineStatus>>({})
    const [progress, setProgress] = useState<Record<string, EngineInstallProgress>>({})
    const [installing, setInstalling] = useState<string | null>(null)
    const [busy, setBusy] = useState(false)
    const [loading, setLoading] = useState(true)
    const [statusError, setStatusError] = useState<string | null>(null)
    const mounted = useRef(false)
    const locked = useRef(false)
    const active = useRef<{ engine: string; id: string } | null>(null)
    const statusRevision = useRef(0)
    const { showToast } = useToast()

    const refreshStatus = useCallback(async () => {
        const revision = ++statusRevision.current
        try {
            const status = await getApi().getEngineStatus()
            if (mounted.current && revision === statusRevision.current) { setEngines(status); setStatusError(null) }
        } catch {
            if (mounted.current && revision === statusRevision.current) setStatusError('엔진 상태를 확인하지 못했습니다. 다시 확인해 주세요.')
        } finally {
            if (mounted.current && revision === statusRevision.current) setLoading(false)
        }
    }, [])

    useEffect(() => {
        mounted.current = true
        const unsubscribe = getApi().onEngineInstallProgress(event => {
            const request = active.current
            if (!request || event.installId !== request.id || event.engine !== request.engine) return
            setProgress(previous => ({ ...previous, [event.engine]: mergeInstallProgress(previous[event.engine], event) }))
        })
        void refreshStatus()
        return () => { mounted.current = false; statusRevision.current++; unsubscribe() }
    }, [refreshStatus])

    const installMany = useCallback(async (names: string[]) => {
        if (locked.current || names.length === 0) return
        locked.current = true; setBusy(true)
        try {
            for (const engine of names) {
                if (!mounted.current) break
                const id = crypto.randomUUID()
                const initial: EngineInstallProgress = { installId: id, engine, phase: 'preparing', downloaded: 0, total: null }
                active.current = { engine, id }
                setInstalling(engine)
                setProgress(previous => ({ ...previous, [engine]: initial }))
                try {
                    if (!await getApi().installEngine(engine, id)) throw new Error('다시 시도해 주세요.')
                    if (!mounted.current) break
                    setProgress(previous => ({ ...previous, [engine]: { ...previous[engine], phase: 'completed', error: undefined } }))
                    await refreshStatus()
                } catch (error) {
                    if (!mounted.current) break
                    const message = `${engine} 설치에 실패했습니다. ${error instanceof Error ? error.message : '요청을 처리하지 못했습니다.'}`
                    setProgress(previous => ({ ...previous, [engine]: { ...previous[engine], phase: 'failed', error: message } }))
                    showToast(message, 'error')
                } finally { active.current = null }
            }
        } finally {
            locked.current = false
            if (mounted.current) { setInstalling(null); setBusy(false) }
        }
    }, [refreshStatus, showToast])

    const missing = Object.entries(engines).filter(([, status]) => !status.installed).map(([name]) => name)
    return {
        engines, progress, installing, busy, loading, statusError, refreshStatus,
        allInstalled: !loading && statusError === null && missing.length === 0,
        install: (name: string) => installMany([name]),
        installAll: () => installMany(missing),
    }
}
