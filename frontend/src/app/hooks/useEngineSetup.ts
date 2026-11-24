import { translate } from '@renderer/shared/i18n'
import { useEffect, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { useToast } from '@renderer/shared/hooks/useToast'

export function useEngineSetup(): { showSetup: boolean; checkingSetup: boolean; closeSetup(): void } {
    const [showSetup, setShowSetup] = useState(false)
    const [checkingSetup, setCheckingSetup] = useState(true)
    const { showToast } = useToast()
    useEffect(() => {
        let cancelled = false
        setCheckingSetup(true)
        getApi()
            .getEngineStatus()
            .then((status) => {
                if (!cancelled)
                    setShowSetup(Object.values(status).some((engine) => !engine.installed))
            })
            .catch((error: unknown) => {
                if (cancelled) return
                console.error('[EngineSetup] Failed to load engine status:', error)
                showToast(translate("common.couldNotCheckEngineStatus"), 'error')
            })
            .finally(() => { if (!cancelled) setCheckingSetup(false) })
        return () => {
            cancelled = true
        }
    }, [showToast])
    return { showSetup, checkingSetup, closeSetup: () => setShowSetup(false) }
}
