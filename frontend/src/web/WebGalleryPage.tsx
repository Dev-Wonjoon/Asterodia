import { canEditMedia } from '../features/mediaedit/canEditMedia'
import { translateError, translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { ChevronDown, ChevronUp, Play, RefreshCw } from 'lucide-react'
import type { MediaFileDto, MediaFilter, MediaSummaryDto } from '@contracts/media-v2'
import { createGalleryStore } from '../features/gallery/galleryStore'
import { GalleryCard, galleryGridClass } from '../features/gallery/GalleryCard'
import { useGalleryExpansionMotion } from '../features/gallery/useGalleryExpansionMotion'
import { GalleryCardActions } from '../features/gallery/GalleryCardActions'
import { GalleryActionMenu } from '../features/gallery/GalleryToolbar'
import { MediaViewer } from '../features/gallery/MediaViewer'
import { MediaEditLauncher, type MediaEditControls } from '../features/mediaedit/MediaEditLauncher'
import { ListDetailLayout } from '../shared/components/layout/ListDetailLayout'
import { ActionButton } from '../shared/components/ui/ActionButton'
import { useInfiniteScroll } from '../shared/hooks/useInfiniteScroll'
import { getApi, refreshAccess } from './api'
import { toMediaUrl } from './media'

const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
const noop = () => {}
export function WebGalleryPage({ title, filter, emptyMessage }: { title: string; filter: MediaFilter; emptyMessage: string }) {
    useLanguage()
    const store = useMemo(() => createGalleryStore(getApi().media, { expandAlbums: true }), [])
    const state = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot)
    const { list, detail } = state
    const root = useRef<HTMLDivElement>(null), sentinel = useRef<HTMLDivElement>(null)
    const grid = useRef<HTMLDivElement>(null)
    const editor = useRef<MediaEditControls>(null), serial = useRef(0)
    const [editEnabled, setEditEnabled] = useState(false), [editBusy, setEditBusy] = useState(false), [error, setError] = useState('')
    const [viewer, setViewer] = useState<{ mediaId: number; fileId?: number } | null>(null)
    const closeViewer = useCallback(() => setViewer(null), [])
    const [anchor, setAnchor] = useState<{ id: number; top: number } | null>(null)
    const filterKey = JSON.stringify(filter)
    useEffect(() => {
        store.setFilter(JSON.parse(filterKey)); void store.refresh()
        root.current?.scrollTo({ top: 0 }); ++serial.current; setError(''); setEditBusy(false)
        return () => { ++serial.current; store.cancelReads() }
    }, [store, filterKey])
    useEffect(() => {
        let alive = true
        const check = async () => { try { const access = await refreshAccess(); if (alive) setEditEnabled(access.enabled) } catch { if (alive) setEditEnabled(false) } }
        void check(); const timer = setInterval(check, 5000)
        return () => { alive = false; clearInterval(timer) }
    }, [])
    useInfiniteScroll({ rootRef: root, targetRef: sentinel, enabled: list.status === 'ready' && list.page.hasNextPage && !list.refreshing && !list.loadingMore && !list.moreError && !list.error && !detail.mediaId && !viewer, onLoadMore: store.loadMore })
    useLayoutEffect(() => {
        if (!anchor || list.refreshing) return
        const card = root.current?.querySelector(`[data-album-id="${anchor.id}"]`)
        if (card && root.current) root.current.scrollTop += card.getBoundingClientRect().top - anchor.top
        setAnchor(null)
    }, [anchor, list.refreshing, list.page.data])
    const beginExpansionMotion = useGalleryExpansionMotion(grid, root, list.page.data, list.refreshing, filterKey)
    async function openEditor(media?: MediaSummaryDto, target?: MediaFileDto) {
        if (!editEnabled || editBusy) return
        const own = ++serial.current
        setError(''); setEditBusy(true)
        try {
            let file = target ?? media?.galleryFile
            if (media && !file) {
                const result = await getApi().media.get({ mediaId: media.id, fileOffset: 0, fileLimit: 1 })
                if (!result.ok) throw new Error(translateError(result.error))
                if (own !== serial.current) return
                if (result.data.revision !== media.revision || result.data.fileCount !== 1 || result.data.files.data[0]?.id !== media.cover?.fileId) throw new Error(translate("common.theMediaHasChangedRefreshAndReopenIt"))
                file = result.data.files.data[0]
            }
            if (own === serial.current && (!file || canEditMedia(file))) editor.current?.open(file)
        } catch (failure) { if (own === serial.current) setError(failure instanceof Error ? failure.message : translate("gallery.couldNotLoadTheVideo")) }
        finally { if (own === serial.current) setEditBusy(false) }
    }
    const busy = list.refreshing || list.status === 'loading'
    const openViewer = (media: MediaSummaryDto, fileId = media.galleryFile?.id) => setViewer({ mediaId: media.id, fileId })
    function toggle(media: MediaSummaryDto, trigger: HTMLElement) {
        const card = trigger.closest('article')
        if (card) setAnchor({ id: media.id, top: card.getBoundingClientRect().top })
        const cancelMotion = beginExpansionMotion(media.id)
        void Promise.resolve(store.toggleAlbum(media.id)).then(changed => { if (changed === false) cancelMotion() }, cancelMotion)
    }
    return <>
        <MediaEditLauncher compact controlRef={editor} allowReveal={false} disabled={!editEnabled} />
        {viewer && <MediaViewer {...viewer} onClose={closeViewer} />}
        <ListDetailLayout listRef={root} detailKey={detail.mediaId} detailLabel={translate("gallery.mediaDetails")} onClose={store.closeDetail}
            detail={detail.mediaId ? <div className="space-y-4">
                {detail.error && <p role="alert">{translateError(detail.error)}</p>}
                {detail.status === 'loading' && <p role="status">{translate("common.loadingMedia")}</p>}
                {detail.data && <>
                    <h3 className="break-words font-semibold">{detail.data.title}</h3>
                    {detail.data.files.data.map(file => <section key={file.id} className="space-y-2 rounded-xl border border-[var(--border-line)] p-3">
                        <p className="break-words text-sm">{file.title || translate("gallery.file", { fileId: file.id })}</p>
                        <button type="button" className={button} onClick={() => openViewer(detail.data!, file.id)}>{translate("viewer.open")}</button>
                        {editEnabled && (canEditMedia(file)) && <button type="button" className={button} disabled={editBusy} onClick={() => void openEditor(undefined, file)}>{translate("gallery.editor.createAnimation")}</button>}
                    </section>)}
                    <nav aria-label={translate("common.filePage")} className="flex gap-2"><button type="button" className={button} disabled={detail.offset === 0 || detail.status === 'loading'} onClick={() => void store.loadDetail(Math.max(0, detail.offset - 24))}>{translate("gallery.previousFiles")}</button><button type="button" className={button} disabled={!detail.data.files.hasNextPage || detail.status === 'loading'} onClick={() => void store.loadDetail(detail.offset + 24)}>{translate("gallery.nextFiles")}</button></nav>
                </>}
            </div> : undefined}>
            <div className="space-y-5 pb-6">
                <header className="flex flex-wrap items-center justify-between gap-3">
                    <h1 className="text-lg font-semibold">{title}</h1>
                    <div className="flex items-center gap-2">
                        <ActionButton icon={<RefreshCw />} label={translate("common.refresh")} disabled={busy} onClick={() => void store.refresh()} />
                        {editEnabled && <GalleryActionMenu label={translate("gallery.menu")}><button type="button" role="menuitem" disabled={editBusy} onClick={() => void openEditor()} className="w-full rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)]">{translate("gallery.editor.createAnimation")}</button></GalleryActionMenu>}
                    </div>
                </header>
                {(list.status === 'loading' || editBusy) && <p role="status" className="text-sm text-[var(--text-muted)]">{editBusy ? translate("gallery.loadingVideo") : translate("common.loadingCollections")}</p>}
                {(error || list.error) && <p role="alert">{translateMessage(error) || translateError(list.error)}</p>}
                {list.status === 'ready' && !list.page.total && <p className="py-16 text-center text-[var(--text-muted)]">{emptyMessage}</p>}
                <div ref={grid} className={galleryGridClass + ' motion-gallery-grid'} aria-label={translate("common.mediaList")} aria-busy={busy}>
                    {list.page.data.map(media => <GalleryCard key={media.galleryFile ? `${media.id}:${media.galleryFile.id}` : media.id}
                        media={media} active={detail.mediaId === media.id} disabled={busy} viewDisabled={busy}
                        selectionKey={media.galleryFile ? `file:${media.galleryFile.id}` : `media:${media.id}`}
                        previewPath={media.galleryFile?.thumbnailPath ?? media.cover?.path ?? null} resolveUrl={toMediaUrl}
                        onView={() => openViewer(media)} onDetails={() => void store.openDetail(media.id, media.galleryFileOffset)}>
                        <GalleryCardActions
                            primaryActions={<>
                                <ActionButton className="gallery-card-action" icon={<Play />} label={translate("viewer.open")} disabled={busy || !media.fileCount} onClick={() => openViewer(media)} />
                                {(media.fileCount > 1 || media.galleryFile) && <ActionButton className="gallery-card-action" icon={media.galleryFile ? <ChevronUp /> : <ChevronDown />} label={`${media.isUserAlbum ? translate("gallery.album") : translate("media.file")} ${media.galleryFile ? translate("gallery.collapse") : translate("gallery.expand")}`} aria-label={`${media.title} ${media.isUserAlbum ? translate("gallery.album") : translate("media.file")} ${media.galleryFile ? translate("gallery.collapse") : translate("gallery.expand")}`} aria-expanded={!!media.galleryFile} disabled={busy} onClick={event => toggle(media, event.currentTarget)} />}
                            </>}
                            title={media.galleryFile?.title || media.title} isFavorite={media.isFavorite} disabled={busy} mutationDisabled={busy} readOnly
                            onToggleFavorite={noop} onOpenFolder={noop} onEdit={noop} onUnregister={noop}
                            onView={media.fileCount ? () => openViewer(media) : undefined} onDetails={() => void store.openDetail(media.id, media.galleryFileOffset)}
                            onToggleFiles={media.fileCount > 1 || media.galleryFile ? event => toggle(media, event.currentTarget) : undefined}
                            filesLabel={`${media.isUserAlbum ? translate("gallery.album") : translate("media.file")} ${media.galleryFile ? translate("gallery.collapse") : translate("gallery.expand")}`} filesExpanded={!!media.galleryFile}
                            editorDisabled={editBusy} onCreateAnimation={editEnabled && (media.galleryFile ? canEditMedia(media.galleryFile) : media.fileCount === 1 && media.cover?.kind === 'video') ? () => void openEditor(media) : undefined} />
                    </GalleryCard>)}
                </div>
                <div ref={sentinel} className="h-px" aria-hidden="true" />
                {list.loadingMore && <p role="status" className="text-center text-sm">{translate("gallery.loading")}</p>}
                {list.moreError && <p role="alert">{translateError(list.moreError)}</p>}
                {list.page.hasNextPage && <div className="text-center"><button type="button" className={button} disabled={busy || list.loadingMore} onClick={() => void store.loadMore()}>{list.moreError ? translate("common.retry") : translate("gallery.loadMore")}</button></div>}
            </div>
        </ListDetailLayout>
    </>
}
