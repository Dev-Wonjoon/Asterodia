import { useState, useEffect, useCallback, useRef } from 'react'
import { getApi } from '@renderer/shared/api'
import { useToast } from '@renderer/shared/hooks/useToast'
import { useLeaveGuard } from '@renderer/shared/hooks/useLeaveGuard'
import type { DatabaseImportResult } from '@contracts/database-api'

interface GeneralSettingsViewModel {
    downloadPath: string
    changePath(): Promise<void>
    importDatabase(): Promise<void>
    importing: boolean
    importResult: DatabaseImportResult | null
    importError: string | null
}
export function useGeneralSettingsViewModel(): GeneralSettingsViewModel {
    const [downloadPath, setDownloadPath] = useState('')
    const { showToast } = useToast()
    const [importing, setImporting] = useState(false)
    const [importResult, setImportResult] = useState<DatabaseImportResult | null>(null)
    const [importError, setImportError] = useState<string | null>(null)
    const importLock = useRef(false)
    useLeaveGuard(false, importing)
    useEffect(() => {
        let cancelled = false
        getApi()
            .getDownloadPath()
            .then((value) => {
                if (!cancelled) setDownloadPath(value || '')
            })
            .catch((error: unknown) => {
                if (cancelled) return
                console.error('[Settings] Failed to load download path:', error)
                showToast('다운로드 경로를 읽지 못했습니다.', 'error')
            })
        return () => {
            cancelled = true
        }
    }, [showToast])
    const changePath = useCallback(async (): Promise<void> => {
        try {
            const value = await getApi().setDownloadPath()
            if (value) setDownloadPath(value)
        } catch (error) {
            console.error('[Settings] Failed to change download path:', error)
            showToast('다운로드 경로를 변경하지 못했습니다.', 'error')
        }
    }, [showToast])
    const importDatabase = useCallback(async (): Promise<void> => {
        if (importLock.current) return
        importLock.current = true
        setImporting(true)
        setImportError(null)
        setImportResult(null)
        try {
            const result = await getApi().database.import()
            if (!result.ok) {
                setImportError(result.error.message)
                return
            }
            if (result.data.cancelled) return
            setImportResult(result.data)
            showToast('데이터베이스 병합을 완료했습니다.', 'success')
        } catch {
            setImportError('병합 응답을 확인하지 못했습니다. 갤러리에서 결과를 확인한 뒤 다시 시도하세요.')
        } finally {
            importLock.current = false
            setImporting(false)
        }
    }, [showToast])
    return { downloadPath, changePath, importDatabase, importing, importResult, importError }
}
