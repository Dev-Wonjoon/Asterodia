import type { DownloadJobDto } from '@contracts/download-api'
import { DownloadThumbnail } from '../download/components/DownloadThumbnail'
import { canCancelDownload, downloadPlatform, downloadStatus, visibleProgress } from '../download/downloadPresentation'

export function GalleryDownloadCard({ job, selected, disabled, cancelling, onOpen, onCancel }: {
    job: DownloadJobDto
    selected: boolean
    disabled: boolean
    cancelling: boolean
    onOpen(): void
    onCancel(): void
}) {
    const title = job.title || downloadPlatform(job.url)
    const progress = visibleProgress(job)
    return <article data-detail-trigger className={'overflow-hidden rounded-xl border bg-[var(--bg-sidebar)] ' + (selected ? 'border-[var(--color-primary)]' : 'border-[var(--border-line)]')} aria-label={title + ' 다운로드 작업'}>
        <DownloadThumbnail url={job.url} localPath={job.thumbnailPath} title={title} fullWidth />
        <button type="button" aria-label={title + ' 다운로드 상세보기'} aria-pressed={selected} disabled={disabled} onClick={onOpen} className="block w-full space-y-2 p-3 text-left disabled:opacity-50">
            <h2 className="truncate font-medium">{title}</h2>
            <p role="status" className="text-sm text-[var(--text-muted)]">{cancelling ? '취소 중' : downloadStatus(job)}{progress !== null ? ` · ${progress}%` : ''}</p>
            {progress !== null && <progress aria-label="다운로드 진행률" value={progress} max={100} className="h-1 w-full accent-[var(--color-primary)]" />}
            <p className="text-xs text-[var(--text-muted)]">완료 후 갤러리에 등록됩니다.</p>
        </button>
        {canCancelDownload(job) && <div className="px-3 pb-3"><button type="button" onClick={onCancel} disabled={disabled || cancelling} className="rounded-lg border border-[var(--border-line)] px-3 py-1 text-sm disabled:opacity-40">{cancelling ? '취소 중' : '다운로드 취소'}</button></div>}
    </article>
}
