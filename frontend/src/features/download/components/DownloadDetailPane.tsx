import { translateError, translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { downloadFailure } from '@contracts/download-api'
import { useDownloadViewModel } from '../useDownloadViewModel'
import { downloadPlatform, downloadStatus } from '../downloadPresentation'
import { DownloadProgress } from './DownloadProgress'
import { DownloadJobActions } from './DownloadJobActions'
import { DownloadThumbnail } from './DownloadThumbnail'
import { DownloadLogPane } from './DownloadLogPane'

export function DownloadDetailPane(): React.JSX.Element | null {
    useLanguage()
    const vm = useDownloadViewModel()
    const { data: job, loading, error } = vm.detail
    if (loading) return <p role="status">{translate("download.loadingJobDetails")}</p>
    if (error)
        return (
            <div role="alert" className="space-y-3">
                <p>{translateError(error)}</p>
                <button
                    type="button"
                    className="underline"
                    onClick={() => vm.detail.jobId && void vm.openDetail(vm.detail.jobId)}
                >{translate("download.reload")}</button>
            </div>
        )
    if (!job) return null
    const failure = job.errorCode ? downloadFailure(job.errorCode) : null
    return (
        <section className="space-y-5" aria-label={translate("download.selectedDownloadJob")}>
            <DownloadThumbnail url={job.url} localPath={job.thumbnailPath} completed={job.state === 'completed'} title={job.title || translate("download.downloadThumbnail")} />
            <div className="space-y-2">
                <h3 className="break-words text-lg font-semibold">
                    {job.title || translate("download.downloadJob")}
                </h3>
                <p className="text-sm text-[var(--text-muted)]">{downloadPlatform(job.url)}</p>
                <p className="select-text break-all text-sm">{job.url}</p>
            </div>
            <div className="rounded-xl border border-[var(--border-line)] p-4 space-y-2">
                <p className="font-medium">
                    {downloadStatus(job)}
                </p>
                <DownloadProgress job={job} />
                {job.phase === 'committing' && job.state === 'running' && (
                    <p className="text-sm text-[var(--text-muted)]">{translate("download.cannotCancelWhileSaving")}</p>
                )}
                {job.cleanupRequired && (
                    <p className="text-sm">{translate("download.temporaryFilesNeedCleanupUseTheCleanupButtonBelow")}</p>
                )}
                {job.historyOnly && <p className="text-sm">{translate("download.thisIsImportedOrMigratedHistoryAddANew")}</p>}
                {job.options === null && !job.historyOnly && (
                    <p className="text-sm">{translate("download.reviewThePreviousDownloadOptionsBeforeRestarting")}</p>
                )}
                {failure && !failure.ok && (
                    <p role="alert" className="text-sm">
                        {translateError(failure.error)}
                        <br />
                        <span className="text-[var(--text-muted)]">{translate("download.errorCode", { code: job.errorCode })}</span>
                    </p>
                )}
                {job.errorMessage && <div className="space-y-1 text-sm">
                    <p className="font-medium">{translate("download.errorDetails")}</p>
                    <pre className="max-h-52 overflow-auto whitespace-pre-wrap break-all rounded bg-[var(--bg-app)] p-3 font-mono text-xs">{job.errorMessage}</pre>
                </div>}
            </div>
            <DownloadLogPane key={job.id} jobId={job.id} />
            <div className="flex flex-wrap gap-2">
                <DownloadJobActions job={job} />
            </div>
            {job.options && (
                <div className="space-y-3">
                    <h3 className="font-medium">{translate("download.downloadOptions")}</h3>
                    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                        <dt className="text-[var(--text-muted)]">{translate("download.type")}</dt>
                        <dd>
                            {job.options.type === 'audio' || job.options.quality === 'audio'
                                ? translate("media.audio")
                                : translate("media.video")}
                        </dd>
                        <dt className="text-[var(--text-muted)]">{translate("download.quality")}</dt>
                        <dd>{job.options.quality || translate("download.defaultQuality")}</dd>
                        <dt className="text-[var(--text-muted)]">{translate("download.format")}</dt>
                        <dd>{job.options.extension || translate("download.defaultFormat")}</dd>
                        <dt className="text-[var(--text-muted)]">{translate("download.playlist")}</dt>
                        <dd>{job.options.playlist ? translate("download.included") : translate("download.singleVideo")}</dd>
                    </dl>
                </div>
            )}
            {vm.halted && (
                <p role="alert" className="text-sm">
                    {translateMessage(vm.halted)}
                </p>
            )}
        </section>
    )
}
