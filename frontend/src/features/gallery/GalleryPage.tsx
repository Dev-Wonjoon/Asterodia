import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { FileMovePanel, type FileMoveControls } from "./FileMovePanel"
import type { MoveSelection } from "@contracts/file-move-api"
import { useCallback, useEffect, useRef, useState } from 'react'
import { useInfiniteScroll } from '@renderer/shared/hooks/useInfiniteScroll'
import { ListDetailLayout } from '@renderer/shared/components/layout/ListDetailLayout'
import { useLeaveGuard } from '@renderer/shared/hooks/useLeaveGuard'
import { confirmDiscard } from '@renderer/shared/utils/leaveGuard'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'
import { MediaDetailPane, type MediaDetailFocusRequest } from './MediaDetailPane'
import { GalleryCardActions } from './GalleryCardActions'
import { useGalleryViewModel } from './useGalleryViewModel'
import type { MediaFilter } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'
import { LocalImportPanel, type LocalImportControls } from '@renderer/features/localimport/LocalImportPanel'
import { GalleryToolbar } from './GalleryToolbar'
import { GalleryBulkDialog } from './GalleryBulkDialog'
import { useGalleryBulkActions, type BulkCommand } from './useGalleryBulkActions'

export const GalleryPage = ({
    favoritesOnly = false,
    title = '갤러리',
    filter,
    emptyMessage = '등록된 묶음이 없습니다.'
}: {
    favoritesOnly?: boolean
    title?: string
    filter?: MediaFilter
    emptyMessage?: string
} = {}): React.JSX.Element => {
    const { confirm } = useConfirm()
    const generation = useRef(0)
    const { store, snapshot: state, openInFolder } = useGalleryViewModel(favoritesOnly, filter)

    useEffect(() => { ++generation.current; return () => { ++generation.current } }, [store])
    const [selection, setSelection] = useState<MoveSelection[]>([])
    const [tagCommand, setTagCommand] = useState<'addTags' | 'removeTags' | null>(null)
    const importControls = useRef<LocalImportControls>(null)
    const moveControls = useRef<FileMoveControls>(null)
    const [importBusy, setImportBusy] = useState(false)
    const [hasImportResult, setHasImportResult] = useState(false)
    const [moveBusy, setMoveBusy] = useState(false)
    const bulk = useGalleryBulkActions(store, selection, ids => setSelection(previous => previous.filter(item => !ids.includes(item.mediaId))))
    const [draftDirty, setDraftDirty] = useState(false)
    const dirtyRef = useRef(false)
    const onDirtyChange = useCallback((dirty: boolean) => {
        dirtyRef.current = dirty
        setDraftDirty(dirty)
    }, [])
    useLeaveGuard(draftDirty, state.busy || bulk.busy || moveBusy)

    const thumbnailRefreshPending = useRef(false)

    const refreshThumbnails = useCallback(() => {
        if (!thumbnailRefreshPending.current) return

        const current = store.getSnapshot()

        if (dirtyRef.current || current.busy || current.needsRefresh) {
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
    }, [draftDirty, state.busy, state.needsRefresh, refreshThumbnails])

    const [editFocus, setEditFocus] = useState<MediaDetailFocusRequest | null>(null)
    const editSequence = useRef(0)
    const listScrollRef = useRef<HTMLDivElement>(null)
    const sentinelRef = useRef<HTMLDivElement>(null)

    const filterKey = JSON.stringify(filter ?? {})

    useEffect(() => {
        listScrollRef.current?.scrollTo({ top: 0 })
        setSelection([]); setTagCommand(null)
    }, [filterKey, favoritesOnly])

    const list = state.list

    useInfiniteScroll({
        rootRef: listScrollRef,
        targetRef: sentinelRef,
        enabled:
            list.status === 'ready' &&
            list.page.hasNextPage &&
            !list.refreshing &&
            !list.loadingMore &&
            !list.error &&
            !list.moreError &&
            !state.busy && !bulk.busy &&
            !state.needsRefresh,
        onLoadMore: store.loadMore
    })

    function selectDetail(mediaId: number): boolean | Promise<boolean> {
        const current = store.getSnapshot(), own = generation.current
        if (current.busy) return false
        if (current.detail.mediaId === mediaId) return true
        const apply = (accepted: boolean) => {
            const latest = store.getSnapshot()
            if (!accepted || generation.current !== own || latest.busy || latest.detail.mediaId !== current.detail.mediaId) return false
            setEditFocus(null)
            void store.openDetail(mediaId)
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
        const draftWarning = current.detail.mediaId === mediaId && dirtyRef.current ? '\n\n작성 중인 제목·태그도 닫힙니다.' : ''
        if (!await confirm({ title: '묶음 등록 해제', message: '묶음의 등록을 해제할까요? 외부 원본과 보관 방식 미확인 원본은 삭제하지 않습니다. 앱 관리 파일의 정리는 별도로 처리합니다.' + draftWarning, confirmLabel: '등록 해제', danger: true })) return
        const latest = store.getSnapshot()
        if (generation.current !== own || latest.busy || latest.needsRefresh || latest.detail.mediaId !== current.detail.mediaId || latest.list.page.data.find(item => item.id === mediaId)?.revision !== media.revision) return
        void store.unregisterMedia({ mediaId, expectedRevision: media.revision })
    }
    function closeDetail(): boolean | Promise<boolean> {
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
    const toolbarDisabled = disabled || moveBusy || draftDirty || state.needsRefresh
    const onBulk = (command: BulkCommand) => {
        if (toolbarDisabled) return
        if (command === 'addTags' || command === 'removeTags') setTagCommand(command)
        else void bulk.run(command)
    }

    const button =
        'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'

    return (
        <ListDetailLayout
            detailLabel="미디어 상세"
            detailKey={state.detail.mediaId}
            onClose={closeDetail}
            listRef={listScrollRef}
            closeDisabled={state.busy || bulk.busy || moveBusy || tagCommand !== null}
            detail={
                state.detail.mediaId !== null ? (
                    <MediaDetailPane
                        key={state.detail.mediaId}
                        state={state}
                        store={store}
                        onDirtyChange={onDirtyChange}
                        focusRequest={
                            editFocus?.mediaId === state.detail.mediaId ? editFocus : null
                        }
                    />
                ) : undefined
            }
        >
            <div className="space-y-5 pb-6">
                <header className="flex flex-wrap items-center justify-between gap-3">
                    <h1 className="text-lg font-semibold">{title}</h1>

                    <GalleryToolbar total={list.status === 'ready' ? list.page.total : null} count={selection.length} disabled={state.busy || bulk.busy || moveBusy || draftDirty} actionDisabled={state.needsRefresh || list.status !== 'ready'} importDisabled={importBusy || state.needsRefresh} canImport={!favoritesOnly} hasImportResult={hasImportResult}
                        onImport={() => importControls.current?.choose()} onRecentImport={() => importControls.current?.reopen()}
                        onSelectAll={() => { if (!toolbarDisabled) setSelection(list.page.data.map(media => ({ mediaId: media.id, revision: media.revision }))) }}
                        onClear={() => setSelection([])} onRefresh={() => { setSelection([]); void store.refresh() }}
                        onMove={() => { if (!toolbarDisabled) moveControls.current?.choose() }} onBulk={onBulk} />
                </header>

                {!favoritesOnly && <LocalImportPanel compact controlRef={importControls} onBusyChange={setImportBusy} onHasResultChange={setHasImportResult} disabled={draftDirty || state.busy || bulk.busy || moveBusy} onComplete={() => { thumbnailRefreshPending.current = true; refreshThumbnails() }} />}
                <FileMovePanel compact controlRef={moveControls} onBusyChange={setMoveBusy} selection={selection} disabled={draftDirty || state.busy || bulk.busy || state.needsRefresh} onCreated={() => setSelection([])} onChanged={() => { thumbnailRefreshPending.current = true; refreshThumbnails() }} />
                {bulk.progress && <p role="status" className="text-sm">일괄 처리 중 · {bulk.progress.done} / {bulk.progress.total}개</p>}
                {bulk.result && <section aria-label="일괄 처리 결과" className="rounded-lg border border-[var(--border-line)] p-3 text-sm">
                    <p role="status">성공 {bulk.result.succeeded.length}개 · 실패 {bulk.result.failed.length}개</p>
                    {bulk.result.failed.length > 0 && <><p className="mt-1 text-[var(--text-muted)]">실패 항목은 선택을 유지합니다. 변경 충돌이 있으면 새로고침 후 다시 선택하세요.</p><ul className="mt-2 max-h-40 overflow-auto">{bulk.result.failed.map(item => <li key={item.mediaId}>게시글 #{item.mediaId}: {item.message}</li>)}</ul></>}
                </section>}
                {tagCommand && <GalleryBulkDialog count={selection.length} remove={tagCommand === 'removeTags'} onClose={() => setTagCommand(null)} onApply={names => { const command = tagCommand; setTagCommand(null); if (!toolbarDisabled) void bulk.run(command, names) }} />}

                {state.notice && (
                    <p role="status" className="text-sm">
                        {state.notice}
                    </p>
                )}

                {state.writeError && (
                    <p role="alert" className="text-sm">
                        {state.writeError.message}
                        {state.needsRefresh ? ' 새로고침 후 현재 정보를 확인하세요.' : ''}
                    </p>
                )}

                {list.status === 'loading' && <p role="status">묶음을 불러오는 중...</p>}

                {list.error && (
                    <div role="alert" className="rounded-lg border border-[var(--border-line)] p-4">
                        <p>
                            {list.status === 'not_ready'
                                ? '라이브러리 연결을 준비 중입니다.'
                                : list.error.message}
                        </p>

                        <p className="mt-1 text-sm text-[var(--text-muted)]">
                            새로고침으로 다시 확인할 수 있습니다.
                        </p>
                    </div>
                )}

                {list.status === 'ready' && list.page.total === 0 && (
                    <p className="py-16 text-center text-[var(--text-muted)]">{emptyMessage}</p>
                )}

                <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,200px),1fr))] gap-4">
                    {list.page.data.map((media) => (
                        <article
                            key={media.id}
                            data-selected={selection.some(item => item.mediaId === media.id) || undefined}
                            data-detail-trigger
                            className={
                                'motion-card relative rounded-xl border bg-[var(--bg-sidebar)] text-left ' +
                                (state.detail.mediaId === media.id
                                    ? 'border-[var(--color-primary)]'
                                    : 'border-[var(--border-line)]')
                            }
                        >
                            <label className="absolute right-2 top-2 z-10 flex size-7 items-center justify-center rounded bg-[var(--bg-sidebar)] shadow-sm" onClick={event => event.stopPropagation()}>
                                <input type="checkbox" className="size-4 accent-[var(--color-primary)]" aria-label={media.title + ' 선택'} checked={selection.some(item => item.mediaId === media.id)} disabled={toolbarDisabled} onChange={event => setSelection(previous => event.target.checked ? [...previous.filter(item => item.mediaId !== media.id), { mediaId: media.id, revision: media.revision }] : previous.filter(item => item.mediaId !== media.id))} />
                            </label>
                            <button
                                type="button"
                                disabled={disabled}
                                aria-pressed={state.detail.mediaId === media.id}
                                aria-label={media.title + ' 상세보기'}
                                onClick={() => {
                                    selectDetail(media.id)
                                }}
                                className="block w-full overflow-hidden rounded-t-xl text-left disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]"
                            >
                                <MediaPreview
                                    path={media.cover?.path ?? null}
                                    title={media.title}
                                    className="aspect-video w-full bg-[var(--bg-active)] object-cover"
                                />

                                <div className="space-y-1 p-3">
                                    <h2 className="truncate font-medium">{media.title}</h2>

                                    <p className="text-xs text-[var(--text-muted)]">
                                        {media.sourceKind === 'local'
                                            ? '로컬 폴더'
                                            : media.sourceKind === 'download'
                                              ? '다운로드'
                                              : '기존 자료'}

                                        {' · 파일 ' + media.fileCount + '개'}
                                    </p>

                                    <p className="text-xs text-[var(--text-muted)]">
                                        {new Date(media.createdAt).toLocaleDateString()}
                                    </p>
                                </div>
                            </button>
                            <GalleryCardActions
                                title={media.title}
                                isFavorite={media.isFavorite}
                                disabled={disabled}
                                mutationDisabled={state.needsRefresh}
                                favoriteDisabledReason={
                                    draftDirty
                                        ? '제목·태그 편집을 저장한 뒤 즐겨찾기를 변경하세요.'
                                        : undefined
                                }
                                onToggleFavorite={() => toggleFavorite(media.id)}
                                onOpenFolder={() => void openInFolder(media.id)}
                                onEdit={(field) => editDetail(media.id, field)}
                                onUnregister={() => unregisterMedia(media.id)}
                            />
                        </article>
                    ))}
                </div>

                {list.page.total > 0 && (
                    <div className="space-y-3 py-4 text-center">
                        <div ref={sentinelRef} className="h-px" aria-hidden="true" />

                        {list.loadingMore && (
                            <p role="status" className="text-sm text-[var(--text-muted)]">
                                더 불러오는 중...
                            </p>
                        )}

                        {list.moreError && (
                            <p role="alert" className="text-sm">
                                {list.moreError.message}
                            </p>
                        )}

                        {list.page.hasNextPage && !list.error && (
                            <button
                                type="button"
                                data-detail-trigger
                                className={button}
                                disabled={
                                    list.loadingMore ||
                                    list.refreshing ||
                                    state.busy ||
                                    state.needsRefresh
                                }
                                onClick={() => void store.loadMore()}
                            >
                                {list.moreError ? '다시 시도' : '더 불러오기'}
                            </button>
                        )}

                        {list.status === 'ready' &&
                            !list.page.hasNextPage &&
                            list.page.data.length > 0 && (
                                <p className="text-sm text-[var(--text-muted)]">
                                    모든 항목을 불러왔습니다.
                                </p>
                            )}
                    </div>
                )}
            </div>
        </ListDetailLayout>
    )
}
