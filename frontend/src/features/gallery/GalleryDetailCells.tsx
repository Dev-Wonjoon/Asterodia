import type { ReactNode, MouseEvent } from 'react'
import type { GallerySort, MediaSummaryDto } from '@contracts/media-v2'
import { getLocale, translate } from '@renderer/shared/i18n'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'

export const detailColumns = 'minmax(260px,2fr) 100px 90px 145px 145px 140px 160px 44px'
export const galleryDate = (value?: number | null) => value == null ? '—' : new Date(value).toLocaleString(getLocale(), { year:'numeric', month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit' })
function bytes(value?: number | null) {
    if (value == null) return '—'
    const unit = value < 1024 ? 'B' : value < 1024 ** 2 ? 'KiB' : value < 1024 ** 3 ? 'MiB' : 'GiB'
    const factor = unit === 'B' ? 1 : unit === 'KiB' ? 1024 : unit === 'MiB' ? 1024 ** 2 : 1024 ** 3
    return `${(value / factor).toFixed(unit === 'B' ? 0 : 1)} ${unit}`
}
export function GalleryDetailCells({ media, previewPath, resolveUrl, onClick, children, disabled }: {
    disabled?: boolean
    media: MediaSummaryDto; previewPath: string | null; resolveUrl?: (path: string | null | undefined) => string | null
    onClick(event: MouseEvent<HTMLButtonElement>): void; children: ReactNode
}) {
    const file = media.galleryFile, title = file?.title || file?.relativePath || file?.fileKey || media.title
    const type = file?.kind || (media.isUserAlbum || media.fileCount > 1 ? translate('gallery.album') : media.cover?.kind || '—')
    const cell = 'min-w-0 truncate px-3 text-xs text-[var(--text-muted)]'
    return <>
        <button type="button" data-card-select data-card-focus disabled={disabled} aria-label={title} onClick={onClick} className="flex h-full min-w-0 items-center gap-3 pl-9 pr-3 text-left text-sm focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]">
            <MediaPreview draggable={false} path={previewPath} resolveUrl={resolveUrl} title={title} className="size-10 shrink-0 rounded object-cover" />
            <span className="truncate" title={title}>{title}</span>
        </button>
        <span className={cell} title={type}>{type}</span>
        <span className={cell}>{bytes(media.byteSize)}</span>
        <time className={cell}>{galleryDate(file?.createdAt ?? media.createdAt)}</time>
        <time className={cell}>{galleryDate(media.fileCreatedAt)}</time>
        <span className={cell} title={media.platformNames?.join(', ')}>{media.platformNames?.join(', ') || '—'}</span>
        <span className={cell} title={media.tagNames?.join(', ')}>{media.tagNames?.join(', ') || '—'}</span>
        {children}
    </>
}
export function GalleryDetailsHeading({ sort, descending, onSort }: { sort: GallerySort; descending: boolean; onSort(value: GallerySort): void }) {
    return <div data-action-boundary className="sticky -top-4 z-[1] grid h-9 min-w-[1184px] items-center border-b border-[var(--border-line)] bg-[var(--bg-app)] text-xs font-medium md:-top-6" style={{ gridTemplateColumns: detailColumns }}>
        {(['name','kind','size','registered','fileCreated','platform','tag'] as const).map(key => key === 'kind' || key === 'size'
            ? <span key={key} className="px-3">{translate('gallery.column.' + key)}</span>
            : <button key={key} type="button" aria-label={translate('gallery.order.' + key)} className="h-full px-3 text-left hover:bg-[var(--bg-hover)]" onClick={() => onSort(key)}>{translate('gallery.order.' + key)}{sort === key ? descending ? ' ↓' : ' ↑' : ''}</button>)}
        <span />
    </div>
}
