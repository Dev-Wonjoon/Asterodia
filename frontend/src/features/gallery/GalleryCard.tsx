import { getLocale, translate, useLanguage } from '@renderer/shared/i18n'
import type { DragEvent, MouseEvent, ReactNode } from 'react'
import type { MediaSummaryDto } from '@contracts/media-v2'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'

export const galleryGridClass = 'grid grid-cols-2 sm:grid-cols-[repeat(auto-fill,minmax(min(100%,200px),1fr))] items-start gap-4'

/** The desktop and HTTP clients render this same card. Actions depend on the client. */
export function GalleryCard({ media, previewPath, resolveUrl, selected, active, selection, disabled, viewDisabled, onView, onDetails, onSelect, selectionKey, onAlbumDragStart, onAlbumDragEnd, onAlbumDragOver, onAlbumDragLeave, onAlbumDrop, albumDropActive, children }: {
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
    onSelect?: (event: MouseEvent) => void
    selectionKey?: string
    onAlbumDragStart?: (event: DragEvent<HTMLElement>) => void
    onAlbumDragEnd?: () => void
    onAlbumDragOver?: (event: DragEvent<HTMLElement>) => void
    onAlbumDragLeave?: (event: DragEvent<HTMLElement>) => void
    onAlbumDrop?: (event: DragEvent<HTMLElement>) => void
    albumDropActive?: boolean
    children: ReactNode
}) {
    useLanguage()
    const title = media.galleryFile?.title || media.title
    const draggable = Boolean(onAlbumDragStart) && !viewDisabled && media.fileCount > 0
    return <article data-album-id={media.id} data-selection-key={selectionKey} data-selected={selected || undefined} data-detail-trigger
        draggable={onAlbumDragStart ? draggable : undefined} data-album-draggable={draggable || undefined}
        onDragStart={event => {
            if (draggable && !(event.target as Element).closest('input,label,a,[data-action-boundary],button:not([data-card-select])')) onAlbumDragStart?.(event)
            else if (onSelect || onAlbumDragStart) event.preventDefault()
        }}
        onDragEnd={onAlbumDragEnd}
        onDragOver={onAlbumDragOver}
        onDragLeave={onAlbumDragLeave}
        onDrop={onAlbumDrop}
        data-album-drop-id={onAlbumDrop ? media.id : undefined} data-drop-active={albumDropActive || undefined}
        onClick={event => { if (onSelect && event.detail < 2 && !viewDisabled && !(event.target as Element).closest('button,input,label,a,[data-action-boundary]')) onSelect(event) }}
        onDoubleClick={event => { if (onSelect && !viewDisabled && media.fileCount > 0 && !event.ctrlKey && !event.metaKey && !event.shiftKey && !(event.target as Element).closest('input,label,[data-action-boundary],button:not([data-card-select])')) onView() }}
        className={'motion-card relative min-w-0 rounded-xl border bg-[var(--bg-sidebar)] text-left ' + (onSelect ? 'select-none ' : '') + (albumDropActive || selected ? 'border-[var(--color-primary)] ring-2 ring-[var(--color-primary)]' : active ? 'border-[var(--color-primary)]' : 'border-[var(--border-line)]')}>
        {albumDropActive && <div aria-hidden="true" className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center rounded-xl bg-[var(--bg-app)]/80 p-3">
            <span className="rounded-lg bg-[var(--color-primary)] px-3 py-2 text-sm font-semibold text-white">{translate("앨범에 추가")}</span>
        </div>}
        {selection}
        <button type="button" data-card-select disabled={viewDisabled || (!onSelect && media.fileCount === 0)} aria-label={title + (onSelect ? translate(" 선택 · 두 번 클릭하여 감상") : translate(" 감상하기"))} onClick={event => { if (!onSelect || event.detail === 0 && !event.ctrlKey && !event.shiftKey && !event.metaKey) onView(); else if (event.detail < 2) onSelect(event) }}
            className="block w-full overflow-hidden rounded-t-xl text-left disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]">
            <MediaPreview path={previewPath} resolveUrl={resolveUrl} title={media.title} className="aspect-video w-full bg-[var(--bg-active)] object-cover" />
        </button>
        <div className="space-y-1 p-3">
            <h2 className="truncate font-medium"><button type="button" data-card-select aria-label={title + translate(" 상세보기")} disabled={onSelect ? viewDisabled : disabled} onClick={event => { if (!onSelect || event.detail === 0 && !event.ctrlKey && !event.shiftKey && !event.metaKey) onDetails(); else if (event.detail < 2) onSelect(event) }}
                className="block w-full truncate text-left hover:underline focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]">{media.galleryFile ? media.galleryFile.title || media.galleryFile.relativePath || translate("파일 {0}", { "0": (media.galleryFileOffset ?? 0) + 1 }) : media.title}</button></h2>
            <p className="flex min-w-0 items-center gap-1 text-xs text-[var(--text-muted)]"><span className={'inline-flex shrink-0 rounded-full border px-2 py-0.5 text-[11px] ' + (media.isUserAlbum ? 'border-violet-400/40 text-violet-400' : 'border-[var(--border-line)]')}>
                {media.isUserAlbum ? translate("사용자 앨범") : media.sourceKind === 'local' ? translate("로컬 폴더") : media.sourceKind === 'download' ? translate("다운로드 묶음") : media.sourceKind === 'edited' ? translate("편집 결과") : translate("기존 자료")}
            </span><span className="min-w-0 truncate" title={media.galleryFile ? `${media.title} · ${(media.galleryFileOffset ?? 0) + 1} / ${media.fileCount}` : undefined}>
                {media.galleryFile ? `${media.title} · ${(media.galleryFileOffset ?? 0) + 1} / ${media.fileCount}` : '· ' + translate('파일 {0}개', { '0': media.fileCount })}
            </span></p>
            <p className="text-xs text-[var(--text-muted)]">{new Date(media.createdAt).toLocaleDateString(getLocale())}</p>
        </div>
        {children}
    </article>
}
