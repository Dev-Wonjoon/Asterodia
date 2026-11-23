import { translateError, translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useGallerySelection, gallerySelectionKey } from './useGallerySelection'
import { useGalleryExpansionMotion } from './useGalleryExpansionMotion'
import { FolderMergeDialog } from './FolderMergeDialog'
import { AlbumManager, type AlbumControls } from './AlbumManager'
import { MediaViewer } from './MediaViewer'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { MediaEditLauncher, type MediaEditControls } from '../mediaedit/MediaEditLauncher'
import { FileMovePanel, type FileMoveControls } from "./FileMovePanel"
import type { MoveSelection } from "@contracts/file-move-api"
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useInfiniteScroll } from '@renderer/shared/hooks/useInfiniteScroll'
import { ListDetailLayout } from '@renderer/shared/components/layout/ListDetailLayout'
import { useLeaveGuard } from '@renderer/shared/hooks/useLeaveGuard'
import { confirmDiscard } from '@renderer/shared/utils/leaveGuard'
import { GalleryCard, galleryGridClass } from './GalleryCard'
import { MediaDetailPane, type MediaDetailFocusRequest } from './MediaDetailPane'
import { GalleryCardActions } from './GalleryCardActions'
import { useGalleryViewModel } from './useGalleryViewModel'
import { GalleryDownloadCard } from './GalleryDownloadCard'
import { DownloadDetailPane } from '../download/components/DownloadDetailPane'
import type { GalleryView, MediaFilter, MediaSummaryDto } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'
import { LocalImportPanel, type LocalImportControls } from '@renderer/features/localimport/LocalImportPanel'
import { GalleryToolbar } from './GalleryToolbar'
import { GallerySelectionBar } from './GallerySelectionBar'
import { GalleryBulkDialog } from './GalleryBulkDialog'
import { useGalleryBulkActions, type BulkCommand } from './useGalleryBulkActions'
import { ActionButton } from '@renderer/shared/components/ui/ActionButton'
import { Play, ListPlus, ChevronDown, ChevronUp } from 'lucide-react'

export const GalleryPage = ({
    favoritesOnly = false,
    title = translate("갤러리"),
    filter,
    emptyMessage = translate("등록된 묶음이 없습니다.")
}: {
    favoritesOnly?: boolean
    title?: string
    filter?: MediaFilter
    emptyMessage?: string
} = {}): React.JSX.Element => {
    useLanguage()
    const { confirm } = useConfirm()
    const generation = useRef(0)
    const [galleryView, setGalleryView] = useState<GalleryView>('all')
    const [showAlbumFiles, setShowAlbumFiles] = useState(false)
    const { store, snapshot: state, openInFolder, downloads } = useGalleryViewModel(favoritesOnly, filter, galleryView, showAlbumFiles)
    const activeJobs = !favoritesOnly && !filter && galleryView === 'all' ? downloads.activeJobs : []
    const [downloadDetailId, setDownloadDetailId] = useState<string | null>(null)
    useEffect(() => () => downloads.closeDetail(), [downloads.closeDetail])
    useEffect(() => {
        if (downloadDetailId && !activeJobs.some(job => job.id === downloadDetailId)) {
            setDownloadDetailId(null)
            downloads.closeDetail()
        }
    }, [activeJobs, downloadDetailId, downloads.closeDetail])

    useEffect(() => { ++generation.current; return () => { ++generation.current } }, [store])
    const listScrollRef = useRef<HTMLDivElement>(null)
    const selectionLocked = useRef(false)
    const pointerSelection = useGallerySelection(state.list.page.data, selectionLocked, listScrollRef, JSON.stringify([filter ?? {}, favoritesOnly, galleryView, showAlbumFiles]))
    const selection = pointerSelection.groups
    const albumFileIds = pointerSelection.fileIds
    const albumControls = useRef<AlbumControls>(null)
    const mediaEditControls = useRef<MediaEditControls>(null)
    const mediaEditRequest = useRef(0), mediaEditPending = useRef(false)
    const [mediaEditLoading, setMediaEditLoading] = useState(false), [mediaEditError, setMediaEditError] = useState('')
    const [viewer, setViewer] = useState<{ mediaId: number; fileId?: number } | null>(null)
    const closeViewer = useCallback(() => setViewer(null), [])

    const [tagCommand, setTagCommand] = useState<'addTags' | 'removeTags' | null>(null)
    const importControls = useRef<LocalImportControls>(null)
    const moveControls = useRef<FileMoveControls>(null)
    const [importBusy, setImportBusy] = useState(false)
    const [hasImportResult, setHasImportResult] = useState(false)
    const [moveBusy, setMoveBusy] = useState(false)
    const [mergeSelection, setMergeSelection] = useState<MoveSelection[] | null>(null)
    const bulk = useGalleryBulkActions(store, selection, pointerSelection.removeMedia)
    const [draftDirty, setDraftDirty] = useState(false)
    const dirtyRef = useRef(false)
    const onDirtyChange = useCallback((dirty: boolean) => {
        dirtyRef.current = dirty
        setDraftDirty(dirty)
    }, [])
    useLeaveGuard(draftDirty, state.busy || bulk.busy || moveBusy || mergeSelection !== null)

    const thumbnailRefreshPending = useRef(false)

    const refreshThumbnails = useCallback(() => {
        if (!thumbnailRefreshPending.current) return

        const current = store.getSnapshot()

        if (dirtyRef.current || current.busy || current.needsRefresh || current.list.refreshing || current.list.loadingMore) {
            return
        }

        thumbnailRefreshPending.current = false
        void store.refresh()
    }, [store])

    useEffect(() => {
        let timer: ReturnType<typeof setTimeout> | undefined

        const unsubscribe = getApi().onMediaThumbnailUpdated(() => {
            thumbnailRefreshPending.current = true

            if (timer !== undefined) clearTimeout(timer)

            timer = setTimeout(() => {
                timer = undefined
                refreshThumbnails()
            }, 100)
        })

        return () => {
            unsubscribe()
            if (timer !== undefined) clearTimeout(timer)
        }
    }, [refreshThumbnails])

    useEffect(() => {
        if (!draftDirty && !state.busy && !state.needsRefresh) {
            refreshThumbnails()
        }
    }, [draftDirty, state.busy, state.needsRefresh, state.list.refreshing, state.list.loadingMore, refreshThumbnails])

    const [editFocus, setEditFocus] = useState<MediaDetailFocusRequest | null>(null)
    const editSequence = useRef(0)
    const sentinelRef = useRef<HTMLDivElement>(null)
    const [albumAnchor, setAlbumAnchor] = useState<{ id: number; top: number } | null>(null)

    const filterKey = JSON.stringify(filter ?? {})
    useEffect(() => {
        mediaEditRequest.current++; mediaEditPending.current = false
        setMediaEditLoading(false); setMediaEditError('')
        return () => { mediaEditRequest.current++; mediaEditPending.current = false }
    }, [store, filterKey, favoritesOnly, galleryView, showAlbumFiles])

    useEffect(() => {
        listScrollRef.current?.scrollTo({ top: 0 })
        pointerSelection.clear(); setTagCommand(null)
    }, [filterKey, favoritesOnly, store, galleryView, showAlbumFiles])

    const list = state.list

    useInfiniteScroll({
        rootRef: listScrollRef,
        targetRef: sentinelRef,
        enabled: !pointerSelection.dragging && list.status === 'ready' && list.page.hasNextPage && !list.refreshing && !list.loadingMore && !list.error && !list.moreError && !state.busy && !bulk.busy && !moveBusy && !state.needsRefresh,
        onLoadMore: store.loadMore
    })

    useLayoutEffect(() => {
        if (!albumAnchor || list.refreshing || list.status !== 'ready') return
        if (list.error) { setAlbumAnchor(null); return }
        const root = listScrollRef.current
        const card = root?.querySelector<HTMLElement>(`[data-album-id="${albumAnchor.id}"]`)
        if (root && card) root.scrollTop += card.getBoundingClientRect().top - albumAnchor.top
        setAlbumAnchor(null)
    }, [albumAnchor, list.refreshing, list.page.data, list.status, list.error])

    const beginExpansionMotion = useGalleryExpansionMotion(pointerSelection.gridRef, listScrollRef, list.page.data, list.refreshing,
        JSON.stringify([filterKey, favoritesOnly, galleryView, showAlbumFiles]))

    function selectDetail(mediaId: number, fileOffset?: number): boolean | Promise<boolean> {
        const current = store.getSnapshot(), own = generation.current
        if (current.busy) return false
        if (current.detail.mediaId === mediaId && fileOffset === undefined) return true
        const apply = (accepted: boolean) => {
            const latest = store.getSnapshot()
            if (!accepted || generation.current !== own || latest.busy || latest.detail.mediaId !== current.detail.mediaId) return false
            setEditFocus(null)
            setDownloadDetailId(null); downloads.closeDetail()
            void store.openDetail(mediaId, fileOffset)
            return true
        }
        const answer = confirmDiscard(dirtyRef.current, confirm)
        return typeof answer === 'boolean' ? apply(answer) : answer.then(apply)
    }
    async function editDetail(mediaId: number, field: 'title' | 'tags'): Promise<void> {
        if (await selectDetail(mediaId)) setEditFocus({ mediaId, field, sequence: ++editSequence.current })
    }
    function toggleFavorite(mediaId: number): void {
        const current = store.getSnapshot()
        if (current.busy || current.needsRefresh || dirtyRef.current) return
        const media = current.list.page.data.find((item) => item.id === mediaId)
        if (!media) return
        void store.edit({
            mediaId,
            expectedRevision: media.revision,
            isFavorite: !media.isFavorite
        })
    }
    async function unregisterMedia(mediaId: number): Promise<void> {
        const current = store.getSnapshot(), own = generation.current
        if (current.busy || current.needsRefresh) return
        const media = current.list.page.data.find(item => item.id === mediaId)
        if (!media) return
        const draftWarning = current.detail.mediaId === mediaId && dirtyRef.current ? translate("\n\n작성 중인 제목·태그도 닫힙니다.") : ''
        if (!await confirm({ title: media.isUserAlbum ? translate("앨범 삭제") : translate("묶음 등록 해제"), message: (media.isUserAlbum ? translate("앨범을 삭제할까요? 원본 파일과 다른 앨범은 유지됩니다.") : translate("묶음의 등록을 해제할까요? 외부 원본과 보관 방식 미확인 원본은 삭제하지 않습니다. 앱 관리 파일의 정리는 별도로 처리합니다.")) + draftWarning, confirmLabel: media.isUserAlbum ? translate("앨범 삭제") : translate("등록 해제"), danger: true })) return
        const latest = store.getSnapshot()
        if (generation.current !== own || latest.busy || latest.needsRefresh || latest.detail.mediaId !== current.detail.mediaId || latest.list.page.data.find(item => item.id === mediaId)?.revision !== media.revision) return
        void store.unregisterMedia({ mediaId, expectedRevision: media.revision })
    }
    function closeDetail(): boolean | Promise<boolean> {
        if (downloadDetailId !== null) {
            setDownloadDetailId(null); downloads.closeDetail(); return true
        }
        const current = store.getSnapshot(), own = generation.current
        if (current.busy) return false
        const apply = (accepted: boolean) => {
            const latest = store.getSnapshot()
            if (!accepted || generation.current !== own || latest.busy || latest.detail.mediaId !== current.detail.mediaId) return false
            setEditFocus(null); store.closeDetail(); return true
        }
        const answer = confirmDiscard(dirtyRef.current, confirm)
        return typeof answer === 'boolean' ? apply(answer) : answer.then(apply)
    }

    const disabled = state.busy || bulk.busy || list.status !== 'ready'
    const toolbarDisabled = disabled || moveBusy || draftDirty || state.needsRefresh || mergeSelection !== null
    const pageDisabled = toolbarDisabled || list.refreshing
    selectionLocked.current = pageDisabled
    async function openAnimationEditor(media: MediaSummaryDto) {
        if (pageDisabled || mediaEditPending.current) return
        setMediaEditError('')
        if (media.galleryFile) {
            if (media.galleryFile.kind === 'video') mediaEditControls.current?.open(media.galleryFile)
            return
        }
        if (media.fileCount !== 1 || media.cover?.kind !== 'video') return
        const request = ++mediaEditRequest.current
        mediaEditPending.current = true; setMediaEditLoading(true)
        try {
            const result = await getApi().media.get({ mediaId: media.id, fileLimit: 1, fileOffset: 0 })
            if (request !== mediaEditRequest.current) return
            if (!result.ok) throw new Error(translateError(result.error))
            const latest = store.getSnapshot(), file = result.data.files.data[0]
            if (!latest.list.page.data.some(item => item.id === media.id && item.revision === media.revision) || latest.busy || dirtyRef.current) return
            if (result.data.revision !== media.revision || result.data.fileCount !== 1 || file?.id !== media.cover.fileId || file.kind !== 'video') throw new Error(translate("미디어가 변경되었습니다. 갤러리를 새로고침한 뒤 다시 열어 주세요."))
            mediaEditControls.current?.open(file)
        } catch (error) {
            if (request === mediaEditRequest.current) setMediaEditError(error instanceof Error ? error.message : translate("동영상을 불러오지 못했습니다."))
        } finally {
            if (request === mediaEditRequest.current) { mediaEditPending.current = false; setMediaEditLoading(false) }
        }
    }
    const clearSelection = pointerSelection.clear
    const selectedTargets = () => {
        const grouped = new Map<number, MoveSelection>(selection.map(item => [item.mediaId, { ...item }]))
        for (const value of pointerSelection.fileSelections) {
            const existing = grouped.get(value.mediaId)
            if (existing && !existing.fileIds) continue
            grouped.set(value.mediaId, { ...value, fileIds: [...new Set([...(existing?.fileIds ?? []), ...value.fileIds!])] })
        }
        return [...grouped.values()]
    }
    const gatherSelection = () => { if (!toolbarDisabled) moveControls.current?.choose(selectedTargets()) }
    const toggleFiles = (mediaId: number, trigger: HTMLElement) => {
        if (pageDisabled) return
        const card = trigger.closest('article')
        if (card) setAlbumAnchor({ id: mediaId, top: card.getBoundingClientRect().top })
        const cancelMotion = beginExpansionMotion(mediaId)
        clearSelection()
        void Promise.resolve(store.toggleAlbum(mediaId)).then(changed => { if (changed === false) cancelMotion() }, cancelMotion)
    }
    const onBulk = (command: BulkCommand) => {
        if (toolbarDisabled) return
        if (command === 'addTags' || command === 'removeTags') setTagCommand(command)
        else void bulk.run(command)
    }

    const button =
        'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'

    async function openDownload(jobId: string) {
        const own = generation.current
        if (disabled || moveBusy || tagCommand !== null || !await closeDetail()) return
        // A discard dialog may outlive the selected download or gallery view.
        if (generation.current !== own || !downloads.activeJobs.some(job => job.id === jobId)) return
        setDownloadDetailId(jobId)
        void downloads.openDetail(jobId)
    }

    return (
        <>
        {viewer && <MediaViewer mediaId={viewer.mediaId} fileId={viewer.fileId} onClose={closeViewer} />}
        {mergeSelection && <FolderMergeDialog selection={mergeSelection} onClose={() => setMergeSelection(null)} onChanged={() => { clearSelection(); void store.refresh() }} onStarted={(jobId, error) => moveControls.current?.track(jobId, error)} />}
        <ListDetailLayout
            detailLabel={downloadDetailId ? translate("다운로드 상세") : translate("미디어 상세")}
            detailKey={downloadDetailId ? 'download:' + downloadDetailId : state.detail.mediaId}
            onClose={closeDetail}
            listRef={listScrollRef}
            closeDisabled={state.busy || bulk.busy || moveBusy || tagCommand !== null}
            detail={
                downloadDetailId !== null ? <DownloadDetailPane /> : state.detail.mediaId !== null ? (
                    <MediaDetailPane
                        key={state.detail.mediaId}
                        state={state}
                        store={store}
                        onDirtyChange={onDirtyChange}
                        onViewFile={fileId => setViewer({ mediaId: state.detail.mediaId!, fileId })}
                        onAddFiles={fileIds => albumControls.current?.addFiles(fileIds)}
                        onAddMedia={mediaId => albumControls.current?.addMedia(mediaId)}
                        onGatherFiles={fileIds => { const value = state.detail.data; if (value) moveControls.current?.choose([{ mediaId: value.id, revision: value.revision, fileIds }]) }}
                        focusRequest={
                            editFocus?.mediaId === state.detail.mediaId ? editFocus : null
                        }
                    />
                ) : undefined
            }
        >
            <div className="space-y-5 pb-6">
                <header data-action-boundary className="sticky top-0 z-30 flex flex-wrap items-center gap-3 bg-[var(--bg-app)]">
                    <h1 className="shrink-0 text-lg font-semibold">{title}</h1>
                    <GallerySelectionBar count={selection.length + albumFileIds.length} groupCount={selection.length} fileCount={albumFileIds.length} total={list.status === 'ready' ? list.page.total : null} disabled={pageDisabled || pointerSelection.dragging}
                        onAdd={() => albumControls.current?.addSelection()} onMove={gatherSelection} onMerge={() => { if (!pageDisabled) setMergeSelection(selectedTargets()) }} onClear={clearSelection} onBulk={onBulk} />
                    <MediaEditLauncher compact controlRef={mediaEditControls} disabled={state.busy || bulk.busy || moveBusy || draftDirty} />

                    <div data-gallery-toolbar className="ml-auto shrink-0"><GalleryToolbar count={selection.length + albumFileIds.length} disabled={state.busy || bulk.busy || moveBusy || draftDirty} actionDisabled={state.needsRefresh || list.status !== 'ready'} importDisabled={importBusy || state.needsRefresh} canImport={!favoritesOnly} hasImportResult={hasImportResult}
                        galleryView={galleryView} showAlbumFiles={showAlbumFiles} viewDisabled={pageDisabled || tagCommand !== null}
                        onViewChange={setGalleryView} onShowAlbumFilesChange={setShowAlbumFiles}
                        editorDisabled={mediaEditLoading} onOpenEditor={() => { setMediaEditError(''); mediaEditControls.current?.open() }}
                        onImport={() => importControls.current?.choose()} onRecentImport={() => importControls.current?.reopen()}
                        onSelectAll={pointerSelection.all}
                        onClear={clearSelection} onRefresh={() => { clearSelection(); void store.refresh() }} /></div>
                </header>
                {mediaEditLoading && <p role="status" className="text-sm text-[var(--text-muted)]">{translate("동영상 불러오는 중…")}</p>}
                {mediaEditError && <p role="alert" className="text-sm text-red-500">{translateMessage(mediaEditError)}</p>}

                <AlbumManager selection={selection} fileIds={albumFileIds} disabled={toolbarDisabled} controlRef={albumControls} showSelectionActions={false}
                    onChanged={(id, deleted) => { if (deleted && store.getSnapshot().detail.mediaId === id) store.closeDetail(); void store.refresh() }} onClear={clearSelection} />

                {!favoritesOnly && <LocalImportPanel compact controlRef={importControls} onBusyChange={setImportBusy} onHasResultChange={setHasImportResult} disabled={draftDirty || state.busy || bulk.busy || moveBusy} onComplete={() => { thumbnailRefreshPending.current = true; refreshThumbnails() }} />}
                <FileMovePanel compact controlRef={moveControls} onBusyChange={setMoveBusy} selection={selection} disabled={draftDirty || state.busy || bulk.busy || state.needsRefresh} onCreated={clearSelection} onChanged={() => { thumbnailRefreshPending.current = true; refreshThumbnails() }} />
                {bulk.progress && <p role="status" className="text-sm">{translate("일괄 처리 중 · {0} / {1}개", { "0": bulk.progress.done, "1": bulk.progress.total })}</p>}
                {bulk.result && <section aria-label={translate("일괄 처리 결과")} className="rounded-lg border border-[var(--border-line)] p-3 text-sm">
                    <p role="status">{translate("성공 {0}개 · 실패 {1}개", { "0": bulk.result.succeeded.length, "1": bulk.result.failed.length })}</p>
                    {bulk.result.failed.length > 0 && <><p className="mt-1 text-[var(--text-muted)]">{translate("실패 항목은 선택을 유지합니다. 변경 충돌이 있으면 새로고침 후 다시 선택하세요.")}</p><ul className="mt-2 max-h-40 overflow-auto">{bulk.result.failed.map(item => <li key={item.mediaId}>{translate("게시글 #{0}: {1}", { "0": item.mediaId, "1": item.message })}</li>)}</ul></>}
                </section>}
                {tagCommand && <GalleryBulkDialog count={selection.length} remove={tagCommand === 'removeTags'} onClose={() => setTagCommand(null)} onApply={names => { const command = tagCommand; setTagCommand(null); if (!toolbarDisabled) void bulk.run(command, names) }} />}

                {state.notice && (
                    <p role="status" className="text-sm">
                        {state.notice}
                    </p>
                )}

                {state.writeError && (
                    <p role="alert" className="text-sm">
                        {translateError(state.writeError)}
                        {state.needsRefresh ? translate(" 새로고침 후 현재 정보를 확인하세요.") : ''}
                    </p>
                )}

                {list.status === 'loading' && <p role="status">{translate("묶음을 불러오는 중...")}</p>}

                {list.error && (
                    <div role="alert" className="rounded-lg border border-[var(--border-line)] p-4">
                        <p>
                            {list.status === 'not_ready'
                                ? translate("라이브러리 연결을 준비 중입니다.")
                                : translateError(list.error)}
                        </p>

                        <p className="mt-1 text-sm text-[var(--text-muted)]">{translate("새로고침으로 다시 확인할 수 있습니다.")}</p>
                    </div>
                )}

                {activeJobs.length > 0 && <p role="status" className="text-sm text-[var(--text-muted)]">{translate("다운로드 대기·진행 중 {0}건", { "0": activeJobs.length })}</p>}
                {list.status === 'ready' && list.page.total === 0 && activeJobs.length === 0 && (
                    <p className="py-16 text-center text-[var(--text-muted)]">{galleryView === 'albums' ? translate("표시할 앨범이 없습니다.") : galleryView === 'files' ? translate("표시할 파일이 없습니다.") : emptyMessage}{galleryView !== 'albums' && !showAlbumFiles && translate(" 앨범에 포함된 파일은 숨겨집니다. 보기 메뉴에서 “숨긴 파일 표시”로 확인할 수 있습니다.")}</p>
                )}

                {pointerSelection.rectangle && <div aria-hidden="true" className="pointer-events-none fixed z-50 border border-[var(--color-primary)] bg-[var(--color-primary)]/15" style={pointerSelection.rectangle} />}
                <div ref={pointerSelection.gridRef} className={galleryGridClass + ' motion-gallery-grid min-h-32'}>
                    {activeJobs.map(job => <GalleryDownloadCard key={'download:' + job.id} job={job} selected={downloadDetailId === job.id} disabled={disabled || moveBusy || tagCommand !== null} cancelling={downloads.cancellingIds.includes(job.id)} onOpen={() => { void openDownload(job.id) }} onCancel={() => downloads.cancelDownload(job.id)} />)}
                    {list.page.data.map((media) => (
                        <GalleryCard
                            key={media.galleryFile ? `album:${media.id}:file:${media.galleryFile.id}` : `album:${media.id}`}
                            media={media}
                            selected={pointerSelection.entries.has(gallerySelectionKey(media))}
                            selectionKey={gallerySelectionKey(media)}
                            onSelect={event => pointerSelection.select(media, event)}
                            active={state.detail.mediaId === media.id}
                            disabled={disabled} viewDisabled={pageDisabled}
                            previewPath={media.galleryFile ? media.galleryFile.thumbnailPath ?? media.galleryFile.previewPath ?? (media.galleryFile.kind === 'image' ? media.galleryFile.filepath : null) : media.cover?.path ?? null}
                            onView={() => setViewer({ mediaId: media.id, fileId: media.galleryFile?.id })}
                            onDetails={() => { void selectDetail(media.id, media.galleryFileOffset) }}
                            selection={!media.galleryFile && <label className="absolute right-2 top-2 z-10 flex size-7 items-center justify-center rounded bg-[var(--bg-sidebar)] shadow-sm" onClick={event => event.stopPropagation()}>
                                <input type="checkbox" className="size-4 accent-[var(--color-primary)]" aria-label={media.title + translate(" 선택")} checked={pointerSelection.entries.has(gallerySelectionKey(media))} disabled={toolbarDisabled} onChange={() => {}} onClick={event => pointerSelection.select(media, event, true)} />
                            </label>}
                        >
                            <div className="relative flex flex-nowrap items-center gap-1 px-2 pb-1" data-detail-trigger data-action-boundary>
                                <ActionButton className="gallery-card-action" icon={<Play />} label={translate("감상하기")} disabled={pageDisabled || media.fileCount === 0} onClick={() => setViewer({ mediaId: media.id, fileId: media.galleryFile?.id })} />
                                {!media.galleryFile && <ActionButton className="gallery-card-action" icon={<ListPlus />} label={media.fileCount === 1 ? translate("앨범에 추가") : translate("전체 추가")} aria-label={`${media.title} ${media.fileCount === 1 ? translate("앨범에 추가") : translate("전체 추가")}`} title={media.fileCount === 1 ? translate("앨범에 추가") : translate("파일 {0}개 전체를 앨범에 추가", { "0": media.fileCount })} disabled={toolbarDisabled || media.fileCount === 0} onClick={() => albumControls.current?.addMedia(media.id)} />}
                                {media.galleryFile && <label title={translate("앨범에 추가할 파일 선택")} className="flex size-8 shrink-0 items-center justify-center"><input type="checkbox" aria-label={translate("{0} 앨범 추가 선택", { "0": media.galleryFile.title || translate("파일") })} disabled={pageDisabled} checked={pointerSelection.entries.has(gallerySelectionKey(media))} onChange={() => {}} onClick={event => pointerSelection.select(media, event, true)} /></label>}
                                {media.galleryCanCollapse !== false && (media.fileCount > 1 || media.galleryFile) && <ActionButton icon={media.galleryFile ? <ChevronUp /> : <ChevronDown />} label={`${media.isUserAlbum ? translate("앨범") : translate("파일")} ${media.galleryFile ? translate("접기") : translate("펼치기")}`} className="gallery-card-action" disabled={pageDisabled} aria-expanded={Boolean(media.galleryFile)} aria-label={`${media.title} ${media.isUserAlbum ? translate("앨범") : translate("파일")} ${media.galleryFile ? translate("접기") : translate("펼치기")}`} onClick={event => {
                                    toggleFiles(media.id, event.currentTarget)
                                }} />}
                            </div>
                            <GalleryCardActions
                                title={media.galleryFile?.title || media.title}
                                isFavorite={media.isFavorite}
                                disabled={disabled || moveBusy}
                                mutationDisabled={pageDisabled}
                                favoriteDisabledReason={
                                    draftDirty
                                        ? translate("제목·태그 편집을 저장한 뒤 즐겨찾기를 변경하세요.")
                                        : undefined
                                }
                                onToggleFavorite={() => toggleFavorite(media.id)}
                                onOpenFolder={() => void openInFolder(media.id)}
                                onView={media.fileCount > 0 ? () => setViewer({ mediaId: media.id, fileId: media.galleryFile?.id }) : undefined}
                                onDetails={() => { void selectDetail(media.id, media.galleryFileOffset) }}
                                onCreateAnimation={media.galleryFile ? media.galleryFile.kind === 'video' ? () => { void openAnimationEditor(media) } : undefined : media.fileCount === 1 && media.cover?.kind === 'video' ? () => { void openAnimationEditor(media) } : undefined}
                                editorDisabled={mediaEditLoading}
                                onAdd={media.fileCount > 0 ? () => media.galleryFile ? albumControls.current?.addFiles([media.galleryFile.id]) : albumControls.current?.addMedia(media.id) : undefined}
                                onEditAlbum={media.isUserAlbum ? () => albumControls.current?.edit(media) : undefined}
                                onToggleFiles={media.galleryCanCollapse !== false && (media.fileCount > 1 || media.galleryFile) ? event => toggleFiles(media.id, event.currentTarget) : undefined}
                                filesLabel={`${media.isUserAlbum ? translate("앨범") : translate("파일")} ${media.galleryFile ? translate("접기") : translate("펼치기")}`}
                                filesExpanded={Boolean(media.galleryFile)}
                                fileOnly={Boolean(media.galleryFile)}
                                onEdit={(field) => editDetail(media.id, field)}
                                onUnregister={() => unregisterMedia(media.id)}
                            />
                        </GalleryCard>
                    ))}
                </div>

                {list.page.total > 0 && (
                    <div className={list.loadingMore || list.refreshing || list.moreError || list.page.hasNextPage ? 'space-y-3 py-4 text-center' : undefined}>
                        <div ref={sentinelRef} className="h-px" aria-hidden="true" />
                        {(list.loadingMore || list.refreshing) && <p role="status" className="text-sm">{translate("불러오는 중…")}</p>}
                        {list.moreError && <p role="alert" className="text-sm">{translateError(list.moreError)}</p>}
                        {list.page.hasNextPage && !list.error && <button type="button" data-detail-trigger className={button} disabled={pageDisabled || list.loadingMore} onClick={() => { void store.loadMore() }}>{list.moreError ? translate("다시 시도") : translate("더 불러오기")}</button>}
                    </div>
                )}
            </div>
        </ListDetailLayout>
        </>
    )
}
