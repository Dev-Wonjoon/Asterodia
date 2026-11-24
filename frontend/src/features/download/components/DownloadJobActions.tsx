import { translate, useLanguage } from '@renderer/shared/i18n'
import type { DownloadJobDto } from '@contracts/download-api'
import { useDownloadViewModel } from '../useDownloadViewModel'
import { canCancelDownload, downloadTrigger } from '../downloadPresentation'
import { useState } from 'react'

export function DownloadJobActions({ job }: { job: DownloadJobDto }): React.JSX.Element | null {
    useLanguage()
    const vm = useDownloadViewModel()
    const [cleaning, setCleaning] = useState(false)
    const action = downloadTrigger(job)
    const executing = vm.requestedIds.includes(job.id)
    const cancelling = vm.cancellingIds.includes(job.id)
    const button =
        'rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] px-3 py-2 text-sm hover:bg-[var(--bg-hover)] disabled:opacity-40'
    return (
        <>
            {job.cleanupRequired && <button type="button" className={button} disabled={cleaning || executing || vm.deletingIds?.includes(job.id) || vm.batchRetry?.running} onClick={() => {
                setCleaning(true)
                void vm.cleanupDownload(job.id).finally(() => setCleaning(false))
            }}>{cleaning ? translate("download.cleaningUp") : translate("download.cleanUpTemporaryFiles")}</button>}
            {action && !(job.state === 'pending' && executing) && (
                <button
                    type="button"
                    className={button}
                    disabled={
                        !vm.ready ||
                        Boolean(vm.halted) ||
                        executing ||
                        cancelling ||
                        Boolean(vm.deletingIds?.includes(job.id)) ||
                        Boolean(vm.batchRetry?.running) ||
                        job.cleanupRequired ||
                        job.options === null
                    }
                    onClick={() => vm.runJob(job.id, action)}
                >
                    {executing
                        ? translate("download.requestingExecution")
                        : action === 'manual_initial'
                          ? translate("download.run")
                          : action === 'manual_retry'
                            ? translate("download.retry")
                            : translate("download.restart")}
                </button>
            )}
            {canCancelDownload(job) && (
                <button
                    type="button"
                    className={button}
                    disabled={!vm.ready || cancelling}
                    onClick={() => vm.cancelDownload(job.id)}
                >
                    {cancelling ? translate("download.requestingCancellation") : translate("download.cancelJob")}
                </button>
            )}
        </>
    )
}
