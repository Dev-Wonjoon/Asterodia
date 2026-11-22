import type { ReactNode } from 'react'
import type { MediaSummaryDto } from '@contracts/media-v2'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'

export const galleryGridClass = 'grid grid-cols-2 sm:grid-cols-[repeat(auto-fill,minmax(min(100%,200px),1fr))] items-start gap-4'

/** The desktop and HTTP clients render this same card. Actions depend on the client. */
export function GalleryCard({ media, previewPath, resolveUrl, selected, active, selection, disabled, viewDisabled, onView, onDetails, children }: {
    media: MediaSummaryDto
    previewPath: string | null
    resolveUrl?: (path: string | null | undefined) => string | null
    selected?: boolean
    active?: boolean
    selection?: ReactNode
    disabled?: boolean
    viewDisabled?: boolean
    onView(): void
    onDetails(): void
    children: ReactNode
}) {
    const title = media.galleryFile?.title || media.title
    return <article data-album-id={media.id} data-selected={selected || undefined} data-detail-trigger
        className={'motion-card relative min-w-0 rounded-xl border bg-[var(--bg-sidebar)] text-left ' + (active ? 'border-[var(--color-primary)]' : 'border-[var(--border-line)]')}>
        {selection}
        <button type="button" disabled={viewDisabled || media.fileCount === 0} aria-label={title + ' 감상하기'} onClick={onView}
            className="block w-full overflow-hidden rounded-t-xl text-left disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]">
            <MediaPreview path={previewPath} resolveUrl={resolveUrl} title={media.title} className="aspect-video w-full bg-[var(--bg-active)] object-cover" />
        </button>
        <div className="space-y-1 p-3">
            <h2 className="truncate font-medium"><button type="button" aria-label={title + ' 상세보기'} disabled={disabled} onClick={onDetails}
                className="block w-full truncate text-left hover:underline focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]">{media.galleryFile ? media.galleryFile.title || media.galleryFile.relativePath || `파일 ${(media.galleryFileOffset ?? 0) + 1}` : media.title}</button></h2>
            {media.galleryFile && <p className="truncate text-xs text-[var(--text-muted)]">{media.title} · {(media.galleryFileOffset ?? 0) + 1} / {media.fileCount}</p>}
            <p className="text-xs text-[var(--text-muted)]"><span className={'inline-flex rounded-full border px-2 py-0.5 text-[11px] ' + (media.isUserAlbum ? 'border-violet-400/40 text-violet-400' : 'border-[var(--border-line)]')}>
                {media.isUserAlbum ? '사용자 앨범' : media.sourceKind === 'local' ? '로컬 폴더' : media.sourceKind === 'download' ? '다운로드 묶음' : media.sourceKind === 'edited' ? '편집 결과' : '기존 자료'}
            </span>{' · 파일 ' + media.fileCount + '개'}</p>
            <p className="text-xs text-[var(--text-muted)]">{new Date(media.createdAt).toLocaleDateString()}</p>
        </div>
        {children}
    </article>
}
