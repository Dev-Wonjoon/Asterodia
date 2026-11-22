import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { ChevronDown, ChevronUp, Play, RefreshCw } from 'lucide-react'
import type { MediaFileDto, MediaFilter, MediaSummaryDto } from '@contracts/media-v2'
import { createGalleryStore } from '../features/gallery/galleryStore'
import { GalleryCard, galleryGridClass } from '../features/gallery/GalleryCard'
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
    const store = useMemo(() => createGalleryStore(getApi().media, { expandAlbums: true }), [])
    const state = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot)
    const { list, detail } = state
    const root = useRef<HTMLDivElement>(null), sentinel = useRef<HTMLDivElement>(null)
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
    async function openEditor(media?: MediaSummaryDto, target?: MediaFileDto) {
        if (!editEnabled || editBusy) return
        const own = ++serial.current
        setError(''); setEditBusy(true)
        try {
            let file = target ?? media?.galleryFile
            if (media && !file) {
                const result = await getApi().media.get({ mediaId: media.id, fileOffset: 0, fileLimit: 1 })
                if (!result.ok) throw new Error(result.error.message)
                if (own !== serial.current) return
                if (result.data.revision !== media.revision || result.data.fileCount !== 1 || result.data.files.data[0]?.id !== media.cover?.fileId) throw new Error('미디어가 변경되었습니다. 새로고침한 뒤 다시 열어 주세요.')
                file = result.data.files.data[0]
            }
            if (own === serial.current && (!file || file.kind === 'video' || file.detectedFormat === 'gif')) editor.current?.open(file)
        } catch (failure) { if (own === serial.current) setError(failure instanceof Error ? failure.message : '동영상을 불러오지 못했습니다.') }
        finally { if (own === serial.current) setEditBusy(false) }
    }
    const busy = list.refreshing || list.status === 'loading'
    const openViewer = (media: MediaSummaryDto, fileId = media.galleryFile?.id) => setViewer({ mediaId: media.id, fileId })
    function toggle(media: MediaSummaryDto, trigger: HTMLElement) {
        const card = trigger.closest('article')
        if (card) setAnchor({ id: media.id, top: card.getBoundingClientRect().top })
        void store.toggleAlbum(media.id)
    }
    return <>
        <MediaEditLauncher compact controlRef={editor} allowReveal={false} disabled={!editEnabled} />
        {viewer && <MediaViewer {...viewer} onClose={closeViewer} />}
        <ListDetailLayout listRef={root} detailKey={detail.mediaId} detailLabel="미디어 상세" onClose={store.closeDetail}
            detail={detail.mediaId ? <div className="space-y-4">
                {detail.error && <p role="alert">{detail.error.message}</p>}
                {detail.status === 'loading' && <p role="status">미디어를 불러오는 중…</p>}
                {detail.data && <>
                    <h3 className="break-words font-semibold">{detail.data.title}</h3>
                    {detail.data.files.data.map(file => <section key={file.id} className="space-y-2 rounded-xl border border-[var(--border-line)] p-3">
                        <p className="break-words text-sm">{file.title || `파일 ${file.id}`}</p>
                        <button type="button" className={button} onClick={() => openViewer(detail.data!, file.id)}>감상하기</button>
                        {editEnabled && (file.kind === 'video' || file.kind === 'image' && file.detectedFormat === 'gif') && <button type="button" className={button} disabled={editBusy} onClick={() => void openEditor(undefined, file)}>GIF, WebP로 만들기</button>}
                    </section>)}
                    <nav aria-label="파일 페이지" className="flex gap-2"><button type="button" className={button} disabled={detail.offset === 0 || detail.status === 'loading'} onClick={() => void store.loadDetail(Math.max(0, detail.offset - 24))}>이전 파일</button><button type="button" className={button} disabled={!detail.data.files.hasNextPage || detail.status === 'loading'} onClick={() => void store.loadDetail(detail.offset + 24)}>다음 파일</button></nav>
                </>}
            </div> : undefined}>
            <div className="space-y-5 pb-6">
                <header className="flex flex-wrap items-center justify-between gap-3">
                    <h1 className="text-lg font-semibold">{title}</h1>
                    <div className="flex items-center gap-2">
                        <ActionButton icon={<RefreshCw />} label="새로고침" disabled={busy} onClick={() => void store.refresh()} />
                        {editEnabled && <GalleryActionMenu label="메뉴"><button type="button" role="menuitem" disabled={editBusy} onClick={() => void openEditor()} className="w-full rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)]">GIF, WebP로 만들기</button></GalleryActionMenu>}
                    </div>
                </header>
                {(list.status === 'loading' || editBusy) && <p role="status" className="text-sm text-[var(--text-muted)]">{editBusy ? '동영상 불러오는 중…' : '묶음을 불러오는 중…'}</p>}
                {(error || list.error) && <p role="alert">{error || list.error?.message}</p>}
                {list.status === 'ready' && !list.page.total && <p className="py-16 text-center text-[var(--text-muted)]">{emptyMessage}</p>}
                <div className={galleryGridClass} aria-label="미디어 목록" aria-busy={busy}>
                    {list.page.data.map(media => <GalleryCard key={media.galleryFile ? `${media.id}:${media.galleryFile.id}` : media.id}
                        media={media} active={detail.mediaId === media.id} disabled={busy} viewDisabled={busy}
                        previewPath={media.galleryFile?.thumbnailPath ?? media.cover?.path ?? null} resolveUrl={toMediaUrl}
                        onView={() => openViewer(media)} onDetails={() => void store.openDetail(media.id, media.galleryFileOffset)}>
                        <div className="relative flex flex-nowrap items-center gap-1 px-2 pb-1" data-detail-trigger data-action-boundary>
                            <ActionButton className="gallery-card-action" icon={<Play />} label="감상하기" disabled={busy || !media.fileCount} onClick={() => openViewer(media)} />
                            {(media.fileCount > 1 || media.galleryFile) && <ActionButton className="gallery-card-action" icon={media.galleryFile ? <ChevronUp /> : <ChevronDown />} label={`${media.isUserAlbum ? '앨범' : '파일'} ${media.galleryFile ? '접기' : '펼치기'}`} aria-label={`${media.title} ${media.isUserAlbum ? '앨범' : '파일'} ${media.galleryFile ? '접기' : '펼치기'}`} aria-expanded={!!media.galleryFile} disabled={busy} onClick={event => toggle(media, event.currentTarget)} />}
                        </div>
                        <GalleryCardActions title={media.galleryFile?.title || media.title} isFavorite={media.isFavorite} disabled={busy} mutationDisabled={busy} readOnly
                            onToggleFavorite={noop} onOpenFolder={noop} onEdit={noop} onUnregister={noop}
                            onView={media.fileCount ? () => openViewer(media) : undefined} onDetails={() => void store.openDetail(media.id, media.galleryFileOffset)}
                            onToggleFiles={media.fileCount > 1 || media.galleryFile ? event => toggle(media, event.currentTarget) : undefined}
                            filesLabel={`${media.isUserAlbum ? '앨범' : '파일'} ${media.galleryFile ? '접기' : '펼치기'}`} filesExpanded={!!media.galleryFile}
                            editorDisabled={editBusy} onCreateAnimation={editEnabled && (media.galleryFile ? media.galleryFile.kind === 'video' : media.fileCount === 1 && media.cover?.kind === 'video') ? () => void openEditor(media) : undefined} />
                    </GalleryCard>)}
                </div>
                <div ref={sentinel} className="h-px" aria-hidden="true" />
                {list.loadingMore && <p role="status" className="text-center text-sm">불러오는 중…</p>}
                {list.moreError && <p role="alert">{list.moreError.message}</p>}
                {list.page.hasNextPage && <div className="text-center"><button type="button" className={button} disabled={busy || list.loadingMore} onClick={() => void store.loadMore()}>{list.moreError ? '다시 시도' : '더 불러오기'}</button></div>}
            </div>
        </ListDetailLayout>
    </>
}
