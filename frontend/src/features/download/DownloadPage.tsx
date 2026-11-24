import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect } from 'react'
import { ListDetailLayout } from '@renderer/shared/components/layout/ListDetailLayout'
import { useEngineSettingsViewModel } from '@renderer/features/settings/useEngineSettingsViewModel'
import { EngineInstallActions, EngineInstallProgress } from '@renderer/features/settings/components/EngineInstallProgress'
import { useLeaveGuard } from '@renderer/shared/hooks/useLeaveGuard'
import { useDownloadViewModel } from './useDownloadViewModel'
import { DownloadQueue } from './components/DownloadQueue'
import { DownloadDetailPane } from './components/DownloadDetailPane'
import { useShortcutCommands } from '@renderer/shared/hooks/useShortcutCommands'

export function DownloadPage(): React.JSX.Element {
    useLanguage()
    const vm = useDownloadViewModel()
    const { closeDetail } = vm
    useEffect(() => () => closeDetail(), [closeDetail])
    const engine = useEngineSettingsViewModel()
    useLeaveGuard(false, engine.busy)
    const missing = Object.entries(engine.engines).filter(([, status]) => !status.installed)
    useShortcutCommands('downloads', {
        'downloads.retryAll': { enabled: () => vm.ready && !vm.halted && !vm.loading && !vm.batchRetry?.running, run: () => { void vm.retryAll() } },
        'downloads.refresh': { enabled: () => !vm.loading, run: () => { void vm.refresh() } },
    })
    return <ListDetailLayout detailLabel={translate("download.downloadDetails")} detailKey={vm.detail.jobId} onClose={vm.closeDetail}
        detail={vm.detail.jobId !== null ? <DownloadDetailPane /> : undefined}>
        <div className="space-y-5">
            <header className="flex flex-wrap items-center justify-between gap-3">
                <div><h1 className="text-lg font-semibold">{translate("navigation.downloads")}</h1>
                    <p className="mt-1 text-sm text-[var(--text-muted)]">{translate("download.checkJobStatusCancelOrRestartAJob")}</p></div>
                <div className="flex flex-wrap gap-2">
                <button type="button" title={translate("download.retryFailedAndCancelledRequestsAcrossAllPagesOnce")} disabled={!vm.ready || Boolean(vm.halted) || vm.loading || vm.batchRetry?.running} onClick={() => void vm.retryAll()}
                    className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{vm.batchRetry?.running ? translate("download.retryingAll") : translate("download.retryFailedAndCancelled")}</button>
                <button type="button" disabled={vm.loading} onClick={() => void vm.refresh()}
                    className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{vm.loading ? translate("download.checking") : translate("common.refresh")}</button>
                </div>
            </header>
            {vm.batchRetry && <p role="status" className="text-xs text-[var(--text-muted)]">
                {vm.batchRetry.running && vm.batchRetry.total === 0 ? translate("download.checkingRetryCandidatesAcrossAllPages") : translate("download.retry.summaryWithState", { state: vm.batchRetry.running ? translate("download.inProgress") : translate("download.finished"), processed: vm.batchRetry.processed, total: vm.batchRetry.total, failed: vm.batchRetry.failed, skipped: vm.batchRetry.skipped })}
            </p>}
            {(missing.length > 0 || engine.statusError || engine.batch) && <section aria-label={translate("download.installRequiredEngines")} aria-busy={engine.busy}
                className="space-y-3 rounded-xl border border-[var(--border-line)] p-4 text-sm">
                <h2 className="font-medium">{translate("download.installRequiredEngines")}</h2>
                {engine.statusError && <p role="alert" className="text-red-500">{translateMessage(engine.statusError)} <button type="button" className="underline" onClick={() => void engine.refreshStatus()}>{translate("download.checkAgain")}</button></p>}
                <EngineInstallActions installer={engine} />
                {Object.entries(engine.engines).filter(([name, status]) => !status.installed || engine.batch?.names.includes(name)).map(([name, status]) => <div key={name}>
                    <div className="flex items-center justify-between gap-4 px-4 py-2">
                        <div className="min-w-0"><p className="font-medium">{name}</p><p className="mt-1 break-words text-xs text-[var(--text-muted)]">{status.installed ? status.version || translate("download.installed") : status.error || translate("download.installationRequired")}</p></div>
                    </div>
                    <EngineInstallProgress name={name} progress={engine.progress[name]} />
                </div>)}
            </section>}
            <DownloadQueue />
        </div>
    </ListDetailLayout>
}
