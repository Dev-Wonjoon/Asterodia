import { translateError, translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { DOWNLOAD_PAGE_SIZE } from '../downloadStore'
import { useDownloadViewModel } from '../useDownloadViewModel'
import { canDeleteDownload, downloadPlatform, downloadStatus } from '../downloadPresentation'
import { X } from 'lucide-react'
import { DownloadJobActions } from './DownloadJobActions'
import { DownloadThumbnail } from './DownloadThumbnail'
import { DownloadProgress } from './DownloadProgress'

export function DownloadQueue(): React.JSX.Element {
    useLanguage()
    const vm = useDownloadViewModel()
    const button =
        'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    return (
        <section aria-label={translate("download.downloadJobList")} className="space-y-4">
            {vm.error && <p role="alert">{translateError(vm.error)}</p>}
            {vm.halted && <p role="alert">{translateMessage(vm.halted)}</p>}
            {!vm.ready && !vm.error && <p role="status">{translate("download.loadingJobs")}</p>}
            {vm.ready && vm.queue.length === 0 && (
                <p className="py-16 text-center text-[var(--text-muted)]">{translate("download.noJobsYetPasteALinkAboveToDownload")}</p>
            )}
            <ul className="space-y-3">
                {vm.queue.map((job) => {
                    const selected = vm.detail.jobId === job.id
                    return (
                        <li
                            key={job.id}
                            className={
                                'relative flex items-start gap-4 rounded-xl border bg-[var(--bg-sidebar)] p-4 ' +
                                (selected
                                    ? 'border-[var(--color-primary)]'
                                    : 'border-[var(--border-line)]')
                            }
                        >
                            <DownloadThumbnail
                                url={job.url}
                                localPath={job.thumbnailPath}
                                completed={job.state === 'completed'}
                                title={job.title || translate("download.downloadThumbnail")}
                            />
                            <div className={'min-w-0 flex-1 space-y-2' + (canDeleteDownload(job) ? ' pr-7' : '')}>
                                <p className="truncate text-sm font-medium" title={job.url}>
                                    {job.url}
                                </p>
                                <p className="text-xs text-[var(--text-muted)]">
                                    {downloadPlatform(job.url)}
                                </p>
                                <p className="text-sm">
                                    {downloadStatus(job)}
                                    {job.cleanupRequired ? translate("download.cleanupRequired") : ''}
                                    {job.options === null ? translate("download.reviewPreviousOptions") : ''}
                                </p>
                                <DownloadProgress job={job} />
                                <div className="flex flex-wrap gap-2">
                                    <button
                                        type="button"
                                        className={button}
                                        data-detail-trigger
                                        aria-expanded={selected}
                                        onClick={() =>
                                            selected ? vm.closeDetail() : void vm.openDetail(job.id)
                                        }
                                    >
                                        {selected ? translate("download.closeDetails") : translate("gallery.details")}
                                    </button>
                                    <DownloadJobActions job={job} />
                                </div>
                                {job.state === 'running' && job.phase === 'committing' && (
                                    <p className="text-xs text-[var(--text-muted)]">{translate("download.cannotCancelWhileSaving")}</p>
                                )}
                            </div>
                            {canDeleteDownload(job) && <button type="button" title={translate("download.deleteRequestHistory")} aria-label={translate("download.deleteRequestHistory")}
                                disabled={Boolean(vm.deletingIds?.includes(job.id)) || vm.requestedIds.includes(job.id) || vm.cancellingIds.includes(job.id) || vm.batchRetry?.running}
                                onClick={() => void vm.deleteJob(job.id)}
                                className="absolute right-2 top-2 rounded-md p-2 text-[var(--text-muted)] hover:bg-[var(--bg-hover)] disabled:opacity-40">
                                <X className="size-4" aria-hidden="true" />
                            </button>}
                        </li>
                    )
                })}
            </ul>
            {(vm.queue.length > 0 || vm.offset > 0) && (
                <nav aria-label={translate("download.downloadJobsPage")} className="flex items-center gap-3">
                    <button
                        type="button"
                        className={button}
                        disabled={vm.loading || vm.offset === 0}
                        onClick={() => void vm.refresh(Math.max(0, vm.offset - DOWNLOAD_PAGE_SIZE))}
                    >{translate("download.previous")}</button>
                    <span className="text-sm">{translate("download.page", { page: Math.floor(vm.offset / DOWNLOAD_PAGE_SIZE) + 1 })}</span>
                    <button
                        type="button"
                        className={button}
                        disabled={vm.loading || !vm.hasNextPage}
                        onClick={() => void vm.refresh(vm.offset + DOWNLOAD_PAGE_SIZE)}
                    >{translate("download.next")}</button>
                </nav>
            )}
        </section>
    )
}
