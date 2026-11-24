import { translateError, translate } from '@renderer/shared/i18n'
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
        (url: string, options?: DownloadJobOptions) => getApi().downloads.previewEngine(url, options),
        []
    )

    const startDownload = useCallback(
        async (url: string, options?: DownloadJobOptions): Promise<boolean> => {
            const result = await store.startDownload(url, options)

            if (!result.ok) {
                showToast(translateError(result.error), 'error')
                return false
            }

            showToast(translate("download.addedToTheDownloadQueue"), 'info')

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
            if (result) showToast(result.ok ? translate("download.temporaryFilesCleanedUp") : translateError(result.error), result.ok ? 'success' : 'error')
            await store.refresh()
        } catch { showToast(translate("download.couldNotFinishCleaningUpTemporaryFiles"), 'error') }
    }, [store, showToast])

    return {
        ...snapshot,
        previewEngine,
        startDownload,
        runJob,
        cancelDownload,
        cleanupDownload,
        deleteJob: store.deleteJob,
        retryAll: store.retryAll,
        refresh: store.refresh,
        openDetail: store.openDetail,
        closeDetail: store.closeDetail
    }
}
