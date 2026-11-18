import { useCallback, useContext, useSyncExternalStore } from 'react'
import { useToast } from '@renderer/shared/hooks/useToast'
import type { ManualDownloadTrigger, DownloadJobOptions } from '@contracts/download-api'
import { DownloadContext } from './downloadContext'
import { getApi } from '@renderer/shared/api'


export function useDownloadViewModel() {
    const store = useContext(DownloadContext)

    if (!store) {
        throw new Error('useDownloadViewModel must be used within DownloadProvider')
    }

    const snapshot = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot)

    const { showToast } = useToast()

    const previewEngine = useCallback(
        (url: string) => getApi().downloads.previewEngine(url),
        []
    )

    const startDownload = useCallback(
        async (url: string, options?: DownloadJobOptions): Promise<boolean> => {
            const result = await store.startDownload(url, options)

            if (!result.ok) {
                showToast(result.error.message, 'error')
                return false
            }

            showToast('다운로드 대기열에 등록했습니다.', 'info')

            void Promise.resolve()
                .then(() => getApi().getDownloadPreview(url))
                .catch(() => {})

            return true
        },
        [store, showToast]
    )

    const runJob = useCallback(
        (jobId: string, trigger: ManualDownloadTrigger) => {
            void store.runJob(jobId, trigger)
        },
        [store]
    )

    const cancelDownload = useCallback(
        (jobId: string) => {
            void store.cancelDownload(jobId)
        },
        [store]
    )
    const cleanupDownload = useCallback(async (jobId: string) => {
        try {
            const result = await getApi().downloads.cleanup?.({ jobId })
            if (result) showToast(result.ok ? '임시 파일 정리를 완료했습니다.' : result.error.message, result.ok ? 'success' : 'error')
            await store.refresh()
        } catch { showToast('임시 파일 정리를 완료하지 못했습니다.', 'error') }
    }, [store, showToast])

    return {
        ...snapshot,
        previewEngine,
        startDownload,
        runJob,
        cancelDownload,
        cleanupDownload,
        refresh: store.refresh,
        openDetail: store.openDetail,
        closeDetail: store.closeDetail
    }
}
