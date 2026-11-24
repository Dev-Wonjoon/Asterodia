import { getLocale, translate, useLanguage } from '@renderer/shared/i18n'
import type { DragEvent, MouseEvent, ReactNode } from 'react'
import type { MediaSummaryDto } from '@contracts/media-v2'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'
import { Pin } from 'lucide-react'

export const galleryGridClass = 'gallery-grid grid items-start gap-4'

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
            <span className="rounded-lg bg-[var(--color-primary)] px-3 py-2 text-sm font-semibold text-white">{translate("gallery.album.add")}</span>
        </div>}
        {selection}
        {media.isPinned && <span title={translate("gallery.pinned")} aria-label={translate("gallery.pinned")} className="pointer-events-none absolute left-2 top-2 z-10 flex size-7 items-center justify-center rounded bg-[var(--bg-sidebar)] text-[var(--color-primary)] shadow-sm"><Pin className="size-4" fill="currentColor" aria-hidden="true" /></span>}
        <button type="button" data-card-select data-card-focus disabled={viewDisabled || (!onSelect && media.fileCount === 0)} aria-label={title + (onSelect ? translate("gallery.selectDoubleClickToView") : translate("gallery.view"))} onClick={event => { if (!onSelect || event.detail === 0 && !event.ctrlKey && !event.shiftKey && !event.metaKey) onView(); else if (event.detail < 2) onSelect(event) }}
            className="block w-full overflow-hidden rounded-t-xl text-left disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]">
            <MediaPreview draggable={false} path={previewPath} resolveUrl={resolveUrl} title={media.title} className="aspect-video w-full bg-[var(--bg-active)] object-cover" />
        </button>
        <div className="space-y-1 p-3">
            <h2 className="truncate font-medium"><button type="button" data-card-select aria-label={title + translate("gallery.suffix.details")} disabled={onSelect ? viewDisabled : disabled} onClick={event => { if (!onSelect || event.detail === 0 && !event.ctrlKey && !event.shiftKey && !event.metaKey) onDetails(); else if (event.detail < 2) onSelect(event) }}
                className="block w-full truncate text-left hover:underline focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]">{media.galleryFile ? media.galleryFile.title || media.galleryFile.relativePath || translate("gallery.file", { fileId: (media.galleryFileOffset ?? 0) + 1 }) : media.title}</button></h2>
            <p className="flex min-w-0 items-center gap-1 text-xs text-[var(--text-muted)]"><span className={'inline-flex shrink-0 rounded-full border px-2 py-0.5 text-[11px] ' + (media.isUserAlbum ? 'border-violet-400/40 text-violet-400' : 'border-[var(--border-line)]')}>
                {media.isUserAlbum ? translate("gallery.userAlbums") : media.sourceKind === 'local' ? translate("gallery.localFolder") : media.sourceKind === 'download' ? translate("gallery.downloadCollection") : media.sourceKind === 'edited' ? translate("gallery.editedOutput") : translate("gallery.existingMedia")}
            </span><span className="min-w-0 truncate" title={media.galleryFile ? `${media.title} · ${(media.galleryFileOffset ?? 0) + 1} / ${media.fileCount}` : undefined}>
                {media.galleryFile ? `${media.title} · ${(media.galleryFileOffset ?? 0) + 1} / ${media.fileCount}` : '· ' + translate("gallery.files.count", { count: media.fileCount })}
            </span></p>
            <p className="text-xs text-[var(--text-muted)]">{new Date(media.createdAt).toLocaleDateString(getLocale())}</p>
        </div>
        {children}
    </article>
}
