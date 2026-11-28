import { translate, useLanguage } from '@renderer/shared/i18n'
import type { DownloadJobDto } from '@contracts/download-api'
import { DownloadThumbnail } from '../download/components/DownloadThumbnail'
import { canCancelDownload, downloadPlatform, downloadStatus } from '../download/downloadPresentation'
import { DownloadProgress } from '../download/components/DownloadProgress'

export function GalleryDownloadCard({ job, selected, disabled, cancelling, onOpen, onCancel, details = false }: {
    details?: boolean
    job: DownloadJobDto
    selected: boolean
    disabled: boolean
    cancelling: boolean
    onOpen(): void
    onCancel(): void
}) {
    useLanguage()
    const title = job.title || downloadPlatform(job.url)
    if (details) return <article data-detail-trigger className="flex h-16 min-w-[1184px] items-center gap-4 border-b border-[var(--border-line)] bg-[var(--bg-sidebar)] px-3">
        <button type="button" className="min-w-0 flex-1 truncate text-left text-sm" disabled={disabled} onClick={onOpen}>{title}</button>
        <span role="status" className="text-xs text-[var(--text-muted)]">{cancelling ? translate('gallery.cancelling') : downloadStatus(job)}</span>
        {canCancelDownload(job) && <button type="button" className="text-sm" disabled={disabled || cancelling} onClick={onCancel}>{translate('gallery.cancelDownload')}</button>}
    </article>
    return <article data-detail-trigger className={'overflow-hidden rounded-xl border bg-[var(--bg-sidebar)] ' + (selected ? 'border-[var(--color-primary)]' : 'border-[var(--border-line)]')} aria-label={title + translate("gallery.downloadJob")}>
        <DownloadThumbnail url={job.url} localPath={job.thumbnailPath} title={title} fullWidth />
        <button type="button" aria-label={title + translate("gallery.downloadDetails")} aria-pressed={selected} disabled={disabled} onClick={onOpen} className="block w-full space-y-2 p-3 text-left disabled:opacity-50">
            <h2 className="truncate font-medium">{title}</h2>
            <p role="status" className="text-sm text-[var(--text-muted)]">{cancelling ? translate("gallery.cancelling") : downloadStatus(job)}</p>
            <DownloadProgress job={job} />
            <p className="text-xs text-[var(--text-muted)]">{translate("gallery.willBeAddedToTheGalleryWhenCompleted")}</p>
        </button>
        {canCancelDownload(job) && <div className="px-3 pb-3"><button type="button" onClick={onCancel} disabled={disabled || cancelling} className="rounded-lg border border-[var(--border-line)] px-3 py-1 text-sm disabled:opacity-40">{cancelling ? translate("gallery.cancelling") : translate("gallery.cancelDownload")}</button></div>}
    </article>
}
