import { canEditMedia } from '../mediaedit/canEditMedia'
import { translateError, translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useGallerySelection, gallerySelectionKey, galleryDisplayKey, albumDragType } from './useGallerySelection'
import { useVirtualGallery, type GalleryGeometry } from './useVirtualGallery'
import { useGalleryExpansionMotion } from './useGalleryExpansionMotion'
import { FolderMergeDialog } from './FolderMergeDialog'
import { AlbumManager, type AlbumControls } from './AlbumManager'
import { AlbumExplorer } from './AlbumExplorer'
import { MediaViewer } from './MediaViewer'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { MediaEditLauncher, type MediaEditControls } from '../mediaedit/MediaEditLauncher'
import { FileMovePanel, type FileMoveControls } from "./FileMovePanel"
import type { MoveSelection } from "@contracts/file-move-api"
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type DragEvent } from 'react'
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
import type { AlbumRequest, GalleryView, MediaFilter, MediaSummaryDto } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'
import { LocalImportPanel, type LocalImportControls } from '@renderer/features/localimport/LocalImportPanel'
import { GalleryToolbar } from './GalleryToolbar'
import { GallerySelectionBar } from './GallerySelectionBar'
import { GalleryBulkDialog } from './GalleryBulkDialog'
import { useGalleryBulkActions, type BulkCommand } from './useGalleryBulkActions'
import { ActionButton } from '@renderer/shared/components/ui/ActionButton'
import { Play, ChevronDown, ChevronUp, Square, SquareCheck } from 'lucide-react'
import { useShortcutCommands } from '@renderer/shared/hooks/useShortcutCommands'
import { matchShortcut } from '../../../../internal/webserver/web/shortcuts.js'
import { useGalleryKeyboardFocus } from './useGalleryKeyboardFocus'
import { ContentFilterNotice } from '@renderer/shared/components/ui/ContentFilterNotice'
import { prepareRemoval, removalConfirmation, removalCounts, removalLabel, selectedRemovalCounts } from './galleryRemoval'

function CardSelectionIcon({ selected }: { selected: boolean }) {
    const Icon = selected ? SquareCheck : Square
    return <span aria-hidden="true" className="pointer-events-none flex size-[18px] items-center justify-center rounded-[3px] text-[var(--text-muted)] peer-checked:text-[var(--color-primary)] peer-disabled:opacity-40 peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[var(--color-primary)]">
        <Icon className="size-[18px]" />
    </span>
}

export const GalleryPage = ({
    favoritesOnly = false,
    title = translate("navigation.gallery"),
    filter,
    importRequest = 0,
    onImportHandled,
    onExplorerChange,
    emptyMessage = translate("gallery.noCollectionsRegistered")
}: {
    favoritesOnly?: boolean
    title?: string
    filter?: MediaFilter
    importRequest?: number
    onImportHandled?: () => void
    onExplorerChange?: (open: boolean) => void
    emptyMessage?: string
} = {}): React.JSX.Element => {
    useLanguage()
    const { confirm } = useConfirm()
    const generation = useRef(0)
    const [galleryView, setGalleryView] = useState<GalleryView>('all')
    const [showAlbumFiles, setShowAlbumFiles] = useState(false)
    const { store, snapshot: state, openInFolder, downloads, contentVisibility } = useGalleryViewModel(favoritesOnly, filter, galleryView, showAlbumFiles)
    const activeJobs = useMemo(() => !favoritesOnly && !filter && galleryView === 'all' ? downloads.activeJobs : [], [favoritesOnly, filter, galleryView, downloads.activeJobs])
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
    const [explorerId, setExplorerId] = useState<number | null>(null)
    const galleryPosition = useRef<{ scroll: number; focus: HTMLElement | null }>({ scroll: 0, focus: null })
    useEffect(() => { onExplorerChange?.(explorerId !== null) }, [explorerId, onExplorerChange])
    useEffect(() => () => onExplorerChange?.(false), [onExplorerChange])
    useLayoutEffect(() => {
        if (explorerId !== null) return
        if (listScrollRef.current) listScrollRef.current.scrollTop = galleryPosition.current.scroll
        if (galleryPosition.current.focus?.isConnected) galleryPosition.current.focus.focus({ preventScroll: true })
    }, [explorerId])
    const selectionLocked = useRef(false)
    const galleryGeometry = useRef<GalleryGeometry | null>(null)
    const pointerSelection = useGallerySelection(state.list.page.data, selectionLocked, listScrollRef, JSON.stringify([filter ?? {}, favoritesOnly, galleryView, showAlbumFiles, contentVisibility?.key, contentVisibility?.showHidden]), galleryGeometry)
    const galleryItems = useMemo(() => {
        const rows = state.list.page.data
        const firstUnpinnedIndex = rows.findIndex(media => !media.galleryPinned && !media.isPinned)
        const items: ({ kind: 'media'; media: MediaSummaryDto } | { kind: 'download'; job: typeof activeJobs[number] })[] = rows.map(media => ({ kind: 'media', media }))
        items.splice(firstUnpinnedIndex < 0 ? items.length : firstUnpinnedIndex, 0, ...activeJobs.map(job => ({ kind: 'download' as const, job })))
        return items
    }, [state.list.page.data, activeJobs])
    const galleryKeys = useMemo(() => galleryItems.map(item => item.kind === 'media' ? galleryDisplayKey(item.media) : 'download:' + item.job.id), [galleryItems])
    const virtual = useVirtualGallery(pointerSelection.gridRef, listScrollRef, galleryKeys, galleryGeometry)
    const keyboardFocus = useGalleryKeyboardFocus(pointerSelection.gridRef, state.list.page.data, virtual.indices.join(','))
    const selection = pointerSelection.groups
    const albumFileIds = pointerSelection.fileIds
    const albumControls = useRef<AlbumControls>(null)
    const mediaEditControls = useRef<MediaEditControls>(null)
    const mediaEditRequest = useRef(0), mediaEditPending = useRef(false)
    const [mediaEditLoading, setMediaEditLoading] = useState(false), [mediaEditError, setMediaEditError] = useState('')
    const [viewer, setViewer] = useState<{ mediaId: number; fileId?: number } | null>(null)
    const closeViewer = useCallback(() => setViewer(null), [])
    useLayoutEffect(() => setViewer(null), [contentVisibility?.key, contentVisibility?.showHidden])

    const [tagCommand, setTagCommand] = useState<'addTags' | 'removeTags' | null>(null)
    const importControls = useRef<LocalImportControls>(null)
    const moveControls = useRef<FileMoveControls>(null)
    const [importBusy, setImportBusy] = useState(false)
    const [hasImportResult, setHasImportResult] = useState(false)
    const [moveBusy, setMoveBusy] = useState(false)
    const [albumDropBusy, setAlbumDropBusy] = useState(false)
    const albumDropPending = useRef(false), albumDropRequest = useRef(0)
    const [hoveredAlbumId, setHoveredAlbumId] = useState<number | null>(null)
    const [albumDropError, setAlbumDropError] = useState(''), [albumDropNotice, setAlbumDropNotice] = useState('')
    const [cardError, setCardError] = useState('')
    const [mergeSelection, setMergeSelection] = useState<MoveSelection[] | null>(null)
    const bulk = useGalleryBulkActions(store, [...pointerSelection.entries.values()], (ids, command) => pointerSelection.removeMedia(ids, command === 'unregister'))
    const [draftDirty, setDraftDirty] = useState(false)
    const dirtyRef = useRef(false)
    const onDirtyChange = useCallback((dirty: boolean) => {
        dirtyRef.current = dirty
        setDraftDirty(dirty)
    }, [])
    useLeaveGuard(draftDirty, state.busy || bulk.busy || moveBusy || albumDropBusy || mergeSelection !== null)

    const thumbnailRefreshPending = useRef(false)

    const refreshThumbnails = useCallback(() => {
        if (!thumbnailRefreshPending.current) return

        const current = store.getSnapshot()

        if (dirtyRef.current || albumDropBusy || pointerSelection.albumDrag || pointerSelection.dragging || current.busy || current.needsRefresh || current.list.refreshing || current.list.loadingMore) {
            return
        }

        thumbnailRefreshPending.current = false
        void store.refresh()
    }, [store, albumDropBusy, pointerSelection.albumDrag, pointerSelection.dragging])

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
        if (!draftDirty && !albumDropBusy && !state.busy && !state.needsRefresh) {
            refreshThumbnails()
        }
    }, [draftDirty, albumDropBusy, state.busy, state.needsRefresh, state.list.refreshing, state.list.loadingMore, refreshThumbnails])

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
        albumDropRequest.current++; albumDropPending.current = false
        setAlbumDropBusy(false); setHoveredAlbumId(null); setAlbumDropError(''); setAlbumDropNotice(''); setCardError('')
        return () => { albumDropRequest.current++ }
    }, [filterKey, favoritesOnly, store, galleryView, showAlbumFiles])

    const list = state.list

    useInfiniteScroll({
        rootRef: listScrollRef,
        targetRef: sentinelRef,
        enabled: !pointerSelection.dragging && !pointerSelection.albumDrag && !albumDropBusy && list.status === 'ready' && list.page.hasNextPage && !list.refreshing && !list.loadingMore && !list.error && !list.moreError && !state.busy && !bulk.busy && !moveBusy && !state.needsRefresh,
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
    function togglePin(media: MediaSummaryDto): void {
        const current = store.getSnapshot()
        if (current.busy || current.needsRefresh || dirtyRef.current) return
        const listed = current.list.page.data.find(item => gallerySelectionKey(item) === gallerySelectionKey(media))
        if (!listed) return
        void store.edit({ mediaId: listed.id, expectedRevision: listed.revision,
            isPinned: !listed.isPinned, ...(listed.galleryFile ? { pinFileId: listed.galleryFile.id } : {}) })
    }
    async function unregisterMedia(mediaId: number): Promise<void> {
        const current = store.getSnapshot(), own = generation.current
        if (current.busy || current.needsRefresh) return
        const media = current.list.page.data.find(item => item.id === mediaId)
        if (!media) return
        let counts
        try { counts = await prepareRemoval([{ mediaId, revision: media.revision }], current.list.page.data, getApi().media) }
        catch { setCardError(translate('gallery.cannotDetermineRemoval')); return }
        if (generation.current !== own || store.getSnapshot().list.page !== current.list.page) return
        const draftWarning = current.detail.mediaId === mediaId && dirtyRef.current ? translate("gallery.unsavedTitleAndTagEditsWillAlsoClose") : ''
        const options = removalConfirmation(counts, 1, true)
        if (!await confirm({ ...options, message: options.message + draftWarning })) return
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

    const disabled = state.busy || bulk.busy || albumDropBusy || list.status !== 'ready'
    const toolbarDisabled = disabled || moveBusy || draftDirty || state.needsRefresh || mergeSelection !== null || explorerId !== null
    const pageDisabled = toolbarDisabled || list.refreshing
    const importedRequest = useRef(0)
    useEffect(() => {
        if (!importRequest || importedRequest.current === importRequest || favoritesOnly || toolbarDisabled || importBusy) return
        importedRequest.current = importRequest
        importControls.current?.choose(); onImportHandled?.()
    }, [importRequest, favoritesOnly, toolbarDisabled, importBusy, onImportHandled])
    function focusedMedia() {
        const key = (document.activeElement as Element | null)?.closest<HTMLElement>('[data-selection-key]')?.dataset.selectionKey ?? keyboardFocus.current
        return list.page.data.find(item => gallerySelectionKey(item) === key)
    }
    function focusCard(direction: string, event?: KeyboardEvent) {
        const rows = list.page.data
        if (!rows.length) return false
        const original = focusedMedia()
        const index = original ? rows.indexOf(original) : -1
        let next = index < 0 ? 0 : index
        if (direction === 'first') next = 0
        else if (direction === 'last') next = rows.length - 1
        else if (direction === 'left' || direction === 'right') next += direction === 'left' ? -1 : 1
        else if (index >= 0) {
            const sign = direction === 'up' ? -1 : 1
            const position = galleryItems.findIndex(item => item.kind === 'media' && item.media === original)
            const column = position % virtual.columns
            for (let row = Math.floor(position / virtual.columns) + sign; row >= 0 && row * virtual.columns < galleryItems.length; row += sign) {
                const candidates = galleryItems.slice(row * virtual.columns, (row + 1) * virtual.columns)
                    .map((item, column) => ({ item, column })).filter(value => value.item.kind === 'media')
                    .sort((a, b) => Math.abs(a.column - column) - Math.abs(b.column - column))
                const target = candidates[0]?.item
                if (target?.kind === 'media') { next = rows.indexOf(target.media); break }
            }
        }
        const target = rows[Math.max(0, Math.min(rows.length - 1, next))]
        if (event?.shiftKey) {
            if (original && !pointerSelection.entries.size) pointerSelection.select(original, { ctrlKey: false, metaKey: false, shiftKey: false })
        }
        virtual.focus(galleryDisplayKey(target))
        if (event?.shiftKey) pointerSelection.select(target, { ctrlKey: false, metaKey: false, shiftKey: true })
    }
    const cardEnabled = () => !pageDisabled && !viewer && Boolean(focusedMedia())
    const metadataActionEnabled = () => !pageDisabled && !viewer && !pointerSelection.dragging && !pointerSelection.albumDrag && !tagCommand
        && (selection.length > 0 || Boolean(focusedMedia()))
    useShortcutCommands('gallery', {
        'gallery.import': { enabled: () => !favoritesOnly && !toolbarDisabled && !importBusy && !viewer, run: () => importControls.current?.choose() },
        'gallery.refresh': { enabled: () => !toolbarDisabled && !viewer, run: () => { clearSelection(); void store.refresh() } },
        'gallery.gather': { enabled: () => !toolbarDisabled && !viewer && selection.length + albumFileIds.length > 0, run: () => gatherSelection() },
        'gallery.merge': { enabled: () => !toolbarDisabled && !viewer && selection.length + albumFileIds.length > 0, run: () => setMergeSelection(selectedTargets()) },
        ...Object.fromEntries(['left', 'right', 'up', 'down', 'first', 'last'].map(direction => ['gallery.' + direction, { enabled: () => !pageDisabled && !viewer, run: (event?: KeyboardEvent) => focusCard(direction, event) }])),
        'gallery.select': { enabled: cardEnabled, run: () => { const item = focusedMedia(); if (item) pointerSelection.select(item, { ctrlKey: false, metaKey: false, shiftKey: false }, true) } },
        'gallery.all': { enabled: () => !pageDisabled && !viewer, run: pointerSelection.all },
        'gallery.view': { enabled: cardEnabled, run: () => { const item = focusedMedia(); if (item) { if (item.galleryFile) setViewer({ mediaId: item.id, fileId: item.galleryFile.id }); else void openAlbum(item.id) } } },
        'gallery.details': { enabled: cardEnabled, run: () => { const item = focusedMedia(); if (item) void selectDetail(item.id, item.galleryFileOffset) } },
        'gallery.menu': { enabled: cardEnabled, run: () => { const item = focusedMedia(); if (item) [...pointerSelection.gridRef.current?.querySelectorAll<HTMLElement>('[data-selection-key]') ?? []].find(card => card.dataset.selectionKey === gallerySelectionKey(item))?.querySelector<HTMLButtonElement>('[data-card-menu]')?.click() } },
        'gallery.favorite': { enabled: () => metadataActionEnabled() && (selection.length > 0 || !focusedMedia()?.galleryFile), run: () => {
            if (selection.length) void bulk.run(selection.some(target => !list.page.data.find(item => item.id === target.mediaId)?.isFavorite) ? 'favorite' : 'unfavorite')
            else { const item = focusedMedia(); if (item && !item.galleryFile) toggleFavorite(item.id) }
        } },
        'gallery.tags': { enabled: metadataActionEnabled, run: () => { if (selection.length) setTagCommand('addTags'); else { const item = focusedMedia(); if (item) void editDetail(item.id, 'tags') } } },
        'gallery.createAlbum': { enabled: () => !toolbarDisabled && !viewer, run: () => albumControls.current?.create() },
        'gallery.addToAlbum': { enabled: () => !toolbarDisabled && !viewer && (selection.length + albumFileIds.length > 0 || Boolean(focusedMedia()?.fileCount)), run: () => {
            if (selection.length + albumFileIds.length) albumControls.current?.addSelection()
            else { const item = focusedMedia(); if (item) { if (item.galleryFile) albumControls.current?.addFiles([item.galleryFile.id]); else albumControls.current?.addMedia(item.id) } }
        } },
        'gallery.folder': { enabled: cardEnabled, run: () => { if (selection.length) void bulk.run('openFolders'); else { const item = focusedMedia(); if (item) void openCardFolder(item) } } },
        'gallery.unregister': { enabled: () => !pageDisabled && !viewer && !pointerSelection.dragging && !pointerSelection.albumDrag && !tagCommand && (pointerSelection.entries.size > 0 || Boolean(focusedMedia())), run: () => {
            const item = focusedMedia()
            void bulk.run('unregister', [], item ? [{ mediaId: item.id, revision: item.revision, ...(item.galleryFile ? { fileIds: [item.galleryFile.id] } : {}) }] : [])
        } },
    }, event => {
        const target = event.target as Element | null
        if (!target || !listScrollRef.current?.contains(target) || target.closest('[role="menu"]')) return false
        const card = target.closest('[data-selection-key]')
        if (card && pointerSelection.gridRef.current?.contains(card)) {
            // Enter/Space activate a focused title, action button or checkbox.
            // Other card commands work from every control inside that card.
            const control = target.closest('button,input,a[href]')
            const nativeActivation = (event.key === 'Enter' || event.key === ' ') && !event.ctrlKey && !event.altKey && !event.metaKey
            return !(nativeActivation && control && !control.matches('[data-card-focus]'))
        }
        if (target.closest('[data-action-boundary]')) return false
        if (target === listScrollRef.current) {
            const command = matchShortcut(event, 'gallery')?.id
            if (command === 'gallery.unregister') return pointerSelection.entries.size > 0
            if (command === 'gallery.favorite' || command === 'gallery.tags') return selection.length > 0
        }
        return event.ctrlKey || event.altKey || event.metaKey
    })
    selectionLocked.current = pageDisabled || tagCommand !== null || viewer !== null
    useEffect(() => { if (!pointerSelection.albumDrag || pageDisabled) setHoveredAlbumId(null) }, [pointerSelection.albumDrag, pageDisabled])
    async function openAnimationEditor(media: MediaSummaryDto) {
        if (pageDisabled || mediaEditPending.current) return
        setMediaEditError('')
        if (media.galleryFile) {
            if (canEditMedia(media.galleryFile)) mediaEditControls.current?.open(media.galleryFile)
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
            if (result.data.revision !== media.revision || result.data.fileCount !== 1 || file?.id !== media.cover.fileId || file.kind !== 'video') throw new Error(translate("gallery.theMediaHasChangedRefreshTheGalleryAndReopen"))
            mediaEditControls.current?.open(file)
        } catch (error) {
            if (request === mediaEditRequest.current) setMediaEditError(error instanceof Error ? error.message : translate("gallery.couldNotLoadTheVideo"))
        } finally {
            if (request === mediaEditRequest.current) { mediaEditPending.current = false; setMediaEditLoading(false) }
        }
    }
    const clearSelection = pointerSelection.clear
    function acceptsAlbumDrop(album: MediaSummaryDto, event: DragEvent<HTMLElement>) {
        const drag = pointerSelection.getAlbumDrag()
        return Boolean(drag && !selectionLocked.current && !albumDropPending.current && album.isUserAlbum && !album.galleryFile &&
            !drag.mediaIds.includes(album.id) && event.dataTransfer.types.includes(albumDragType))
    }
    async function dropOnAlbum(album: MediaSummaryDto, event: DragEvent<HTMLElement>) {
        event.preventDefault(); event.stopPropagation(); setHoveredAlbumId(null)
        const drag = pointerSelection.getAlbumDrag()
        if (!acceptsAlbumDrop(album, event) || !drag) return
        await saveAlbumChange(album, { addFileIds: drag.fileIds, addMediaIds: drag.mediaIds }, translate("gallery.addedToTheAlbum", { title: album.title }))
    }
    async function saveAlbumChange(album: MediaSummaryDto, changes: Pick<AlbumRequest, 'addFileIds' | 'addMediaIds' | 'removeFileIds'>, notice: string) {
        if (selectionLocked.current || albumDropPending.current) return
        const own = ++albumDropRequest.current
        albumDropPending.current = true; setAlbumDropBusy(true); setAlbumDropError(''); setAlbumDropNotice('')
        pointerSelection.endAlbumDrag()
        try {
            let result = await getApi().media.saveAlbum({
                albumId: album.id, expectedRevision: album.revision,
                ...changes,
            })
            if (own !== albumDropRequest.current) return
            // Adding memberships is idempotent. If a background update changed
            // only the target revision, retry once with its current revision.
            if (!result.ok && result.error.code === 'REVISION_CONFLICT' && !changes.removeFileIds) {
                const latest = await getApi().media.get({ mediaId: album.id, fileLimit: 1, fileOffset: 0 })
                if (own !== albumDropRequest.current) return
                if (latest.ok && latest.data.isUserAlbum && latest.data.id === album.id) {
                    result = await getApi().media.saveAlbum({ albumId: album.id, expectedRevision: latest.data.revision, ...changes })
                    if (own !== albumDropRequest.current) return
                }
            }
            if (!result.ok) throw new Error(translateError(result.error))
            setAlbumDropNotice(notice)
            clearSelection()
            // Keep the next write blocked until the new album revision is visible.
            const refreshed = await store.refresh()
            if (own === albumDropRequest.current && refreshed === false) setAlbumDropError(translate("gallery.albumChangesWereSavedButTheListCouldNot"))
        } catch (error) {
            if (own === albumDropRequest.current) setAlbumDropError(error instanceof Error ? error.message : translate("gallery.couldNotSaveAlbumChangesRefreshAndCheckAgain"))
        } finally {
            if (own === albumDropRequest.current) { albumDropPending.current = false; setAlbumDropBusy(false) }
        }
    }
    function removeFromAlbum(media: MediaSummaryDto) {
        if (!media.isUserAlbum || !media.galleryFile) return
        void saveAlbumChange(media, { removeFileIds: [media.galleryFile.id] }, translate("gallery.removedFromTheAlbum", { title: media.title }))
    }
    async function openCardFolder(media: MediaSummaryDto) {
        setCardError('')
        try {
            if (media.galleryFile) await getApi().showItemInFolder(media.galleryFile.filepath)
            else await openInFolder(media.id)
        } catch { setCardError(translate("gallery.couldNotOpenTheFileLocation")) }
    }
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

    async function openAlbum(id: number) {
        if (pageDisabled || !await closeDetail()) return
        galleryPosition.current = { scroll: listScrollRef.current?.scrollTop ?? 0, focus: document.activeElement instanceof HTMLElement ? document.activeElement : null }
        setExplorerId(id)
    }

    return (
        <>
        {explorerId !== null && <AlbumExplorer mediaId={explorerId} onClose={() => setExplorerId(null)} onChanged={() => { void store.refresh() }} />}
        <div hidden={explorerId !== null} inert={explorerId !== null} className="h-full">
        {viewer && <MediaViewer mediaId={viewer.mediaId} fileId={viewer.fileId} onClose={closeViewer} />}
        {mergeSelection && <FolderMergeDialog selection={mergeSelection} onClose={() => setMergeSelection(null)} onChanged={() => { clearSelection(); void store.refresh() }} onStarted={(jobId, error) => moveControls.current?.track(jobId, error)} />}
        <ListDetailLayout
            detailLabel={downloadDetailId ? translate("download.downloadDetails") : translate("gallery.mediaDetails")}
            detailKey={downloadDetailId ? 'download:' + downloadDetailId : state.detail.mediaId}
            onClose={closeDetail}
            listRef={listScrollRef}
            closeDisabled={state.busy || bulk.busy || moveBusy || albumDropBusy || tagCommand !== null}
            detail={
                downloadDetailId !== null ? <DownloadDetailPane /> : state.detail.mediaId !== null ? (
                    <MediaDetailPane
                        key={state.detail.mediaId}
                        state={albumDropBusy ? { ...state, busy: true } : state}
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
                <header data-action-boundary className="flex flex-wrap items-center gap-3 bg-[var(--bg-app)]">
                    <h1 className="shrink-0 text-lg font-semibold">{title}</h1>
                    <GallerySelectionBar count={selection.length + albumFileIds.length} groupCount={selection.length} fileCount={albumFileIds.length} total={list.status === 'ready' ? list.page.total : null} disabled={pageDisabled || pointerSelection.dragging}
                        removalLabel={removalLabel(selectedRemovalCounts([...pointerSelection.entries.values()], list.page.data))}
                        onAdd={() => albumControls.current?.addSelection()} onMove={gatherSelection} onMerge={() => { if (!pageDisabled) setMergeSelection(selectedTargets()) }} onClear={clearSelection} onBulk={onBulk} />
                    <MediaEditLauncher compact controlRef={mediaEditControls} disabled={state.busy || bulk.busy || moveBusy || albumDropBusy || draftDirty} />

                    <div data-gallery-toolbar className="ml-auto shrink-0"><GalleryToolbar count={selection.length + albumFileIds.length} disabled={state.busy || bulk.busy || moveBusy || albumDropBusy || draftDirty} actionDisabled={state.needsRefresh || list.status !== 'ready'} importDisabled={importBusy || state.needsRefresh} canImport={!favoritesOnly} hasImportResult={hasImportResult}
                        galleryView={galleryView} showAlbumFiles={showAlbumFiles} viewDisabled={pageDisabled || tagCommand !== null}
                        onViewChange={setGalleryView} onShowAlbumFilesChange={setShowAlbumFiles}
                        editorDisabled={mediaEditLoading} onOpenEditor={() => { setMediaEditError(''); mediaEditControls.current?.open() }}
                        onImport={() => importControls.current?.choose()} onRecentImport={() => importControls.current?.reopen()}
                        onSelectAll={pointerSelection.all}
                        onClear={clearSelection} onRefresh={() => { clearSelection(); void store.refresh() }} /></div>
                </header>
                {contentVisibility && <ContentFilterNotice visibility={contentVisibility} disabled={pageDisabled || draftDirty || viewer !== null} />}
                {mediaEditLoading && <p role="status" className="text-sm text-[var(--text-muted)]">{translate("gallery.loadingVideo")}</p>}
                {mediaEditError && <p role="alert" className="text-sm text-red-500">{translateMessage(mediaEditError)}</p>}
                {cardError && <p role="alert" className="text-sm text-red-500">{translateMessage(cardError)}</p>}

                <AlbumManager selection={selection} fileIds={albumFileIds} disabled={toolbarDisabled} controlRef={albumControls} showSelectionActions={false}
                    onChanged={(id, deleted) => { if (deleted && store.getSnapshot().detail.mediaId === id) store.closeDetail(); void store.refresh() }} onClear={clearSelection} />

                {!favoritesOnly && <LocalImportPanel compact controlRef={importControls} onBusyChange={setImportBusy} onHasResultChange={setHasImportResult} disabled={draftDirty || state.busy || bulk.busy || moveBusy || albumDropBusy} onComplete={() => { thumbnailRefreshPending.current = true; refreshThumbnails() }} />}
                <FileMovePanel compact controlRef={moveControls} onBusyChange={setMoveBusy} selection={selection} disabled={draftDirty || state.busy || bulk.busy || albumDropBusy || state.needsRefresh} onCreated={clearSelection} onChanged={() => { thumbnailRefreshPending.current = true; refreshThumbnails() }} />
                {bulk.progress && <p role="status" className="text-sm">{translate("gallery.processingSelection", { processed: bulk.progress.done, total: bulk.progress.total })}</p>}
                {bulk.result && <section aria-label={translate("gallery.bulkOperationResults")} className="rounded-lg border border-[var(--border-line)] p-3 text-sm">
                    <p role="status">{translate("gallery.succeededFailed", { succeeded: bulk.result.succeeded.length, failed: bulk.result.failed.length })}</p>
                    {bulk.result.failed.length > 0 && <><p className="mt-1 text-[var(--text-muted)]">{translate("gallery.failedItemsRemainSelectedIfThereAreConflictingChanges")}</p><ul className="mt-2 max-h-40 overflow-auto">{bulk.result.failed.map(item => <li key={item.mediaId}>{translate("gallery.post", { mediaId: item.mediaId, message: item.message })}</li>)}</ul></>}
                </section>}
                {tagCommand && <GalleryBulkDialog count={selection.length} remove={tagCommand === 'removeTags'} onClose={() => setTagCommand(null)} onApply={names => { const command = tagCommand; setTagCommand(null); if (!toolbarDisabled) void bulk.run(command, names) }} />}

                {state.notice && (
                    <p role="status" className="text-sm">
                        {translateMessage(state.notice)}
                    </p>
                )}
                {albumDropNotice && <p role="status" className="text-sm">{translateMessage(albumDropNotice)}</p>}
                {albumDropError && <p role="alert" className="text-sm text-red-500">{translateMessage(albumDropError)} {translate("gallery.refreshTheGalleryToCheckTheChanges")}</p>}

                {state.writeError && (
                    <p role="alert" className="text-sm">
                        {translateError(state.writeError)}
                        {state.needsRefresh ? translate("gallery.refreshToCheckTheCurrentInformation") : ''}
                    </p>
                )}

                {list.status === 'loading' && <p role="status">{translate("gallery.loadingCollections")}</p>}

                {list.error && (
                    <div role="alert" className="rounded-lg border border-[var(--border-line)] p-4">
                        <p>
                            {list.status === 'not_ready'
                                ? translate("gallery.preparingTheLibraryConnection")
                                : translateError(list.error)}
                        </p>

                        <p className="mt-1 text-sm text-[var(--text-muted)]">{translate("gallery.refreshToCheckAgain")}</p>
                    </div>
                )}

                {activeJobs.length > 0 && <p role="status" className="text-sm text-[var(--text-muted)]">{translate("gallery.downloadsPendingOrInProgress", { count: activeJobs.length })}</p>}
                {list.status === 'ready' && list.page.total === 0 && activeJobs.length === 0 && (
                    <p className="py-16 text-center text-[var(--text-muted)]">{galleryView === 'albums' ? translate("gallery.noAlbumsToDisplay") : galleryView === 'files' ? translate("gallery.noFilesToDisplay") : emptyMessage}{galleryView !== 'albums' && !showAlbumFiles && translate("gallery.filesIncludedInAlbumsAreHiddenUseShowHidden")}</p>
                )}

                {pointerSelection.rectangle && <div aria-hidden="true" className="pointer-events-none fixed z-50 border border-[var(--color-primary)] bg-[var(--color-primary)]/15" style={pointerSelection.rectangle} />}
                <div ref={pointerSelection.gridRef} className={galleryGridClass + ' motion-gallery-grid min-h-32'} style={virtual.style}
                    onDragOver={event => { if (!event.defaultPrevented) event.dataTransfer.dropEffect = 'none' }}
                    onDrop={event => event.preventDefault()}>
                    {virtual.indices.map(index => {
                        const item = galleryItems[index]
                        if (item.kind === 'download') {
                            const { job } = item
                            return <div key={'download:' + job.id} data-gallery-key={'download:' + job.id} style={virtual.slotStyle(index)}>
                                <GalleryDownloadCard job={job} selected={downloadDetailId === job.id} disabled={disabled || moveBusy || tagCommand !== null} cancelling={downloads.cancellingIds.includes(job.id)} onOpen={() => { void openDownload(job.id) }} onCancel={() => downloads.cancelDownload(job.id)} />
                            </div>
                        }
                        const { media } = item
                        return <div key={galleryDisplayKey(media)} data-gallery-key={galleryDisplayKey(media)} style={virtual.slotStyle(index)}>
                        <GalleryCard
                            key={media.galleryFile ? `album:${media.id}:file:${media.galleryFile.id}` : `album:${media.id}`}
                            media={media}
                            selected={pointerSelection.entries.has(gallerySelectionKey(media))}
                            selectionKey={gallerySelectionKey(media)}
                            onSelect={event => pointerSelection.select(media, event)}
                            onAlbumDragStart={event => pointerSelection.startAlbumDrag(media, event)}
                            onAlbumDragEnd={pointerSelection.endAlbumDrag}
                            albumDropActive={Boolean(pointerSelection.albumDrag) && hoveredAlbumId === media.id && !media.galleryFile}
                            onAlbumDragOver={media.isUserAlbum && !media.galleryFile ? event => {
                                if (!acceptsAlbumDrop(media, event)) { event.dataTransfer.dropEffect = 'none'; return }
                                event.preventDefault(); event.dataTransfer.dropEffect = 'copy'; setHoveredAlbumId(media.id)
                            } : undefined}
                            onAlbumDragLeave={media.isUserAlbum && !media.galleryFile ? event => {
                                if (!(event.relatedTarget instanceof Node) || !event.currentTarget.contains(event.relatedTarget)) setHoveredAlbumId(previous => previous === media.id ? null : previous)
                            } : undefined}
                            onAlbumDrop={media.isUserAlbum && !media.galleryFile ? event => { void dropOnAlbum(media, event) } : undefined}
                            active={state.detail.mediaId === media.id}
                            disabled={disabled} viewDisabled={pageDisabled}
                            previewPath={media.galleryFile ? media.galleryFile.thumbnailPath ?? media.galleryFile.previewPath ?? (media.galleryFile.kind === 'image' ? media.galleryFile.filepath : null) : media.cover?.path ?? null}
                            onView={() => setViewer({ mediaId: media.id, fileId: media.galleryFile?.id })}
                            onOpen={!media.galleryFile ? () => { void openAlbum(media.id) } : undefined}
                            onDetails={() => { void selectDetail(media.id, media.galleryFileOffset) }}
                            selection={!media.galleryFile && <label className="absolute right-2 top-2 z-10 flex size-7 items-center justify-center rounded-lg bg-[var(--bg-sidebar)] shadow-sm hover:bg-[var(--bg-hover)]" onClick={event => event.stopPropagation()}>
                                <input type="checkbox" data-gallery-select className="peer absolute inset-0 m-0 size-full cursor-pointer opacity-0 disabled:cursor-default" aria-label={media.title + translate("gallery.suffix.select")} checked={pointerSelection.entries.has(gallerySelectionKey(media))} disabled={toolbarDisabled} onChange={() => {}} onClick={event => pointerSelection.select(media, event, true)} />
                                <CardSelectionIcon selected={pointerSelection.entries.has(gallerySelectionKey(media))} />
                            </label>}
                        >
                            <GalleryCardActions
                                keyboardShortcuts
                                primaryActions={<>
                                    <ActionButton className="gallery-card-action" icon={<Play />} label={translate("viewer.open")} disabled={pageDisabled || media.fileCount === 0} onClick={() => setViewer({ mediaId: media.id, fileId: media.galleryFile?.id })} />
                                    {media.galleryFile && <label title={translate("gallery.selectFilesToAddToAnAlbum")} className="relative flex size-8 shrink-0 items-center justify-center rounded-lg hover:bg-[var(--bg-hover)]">
                                        <input type="checkbox" data-gallery-select className="peer absolute inset-0 m-0 size-full cursor-pointer opacity-0 disabled:cursor-default" aria-label={translate("gallery.selectToAddToAnAlbum", { title: media.galleryFile.title || translate("media.file") })} disabled={pageDisabled} checked={pointerSelection.entries.has(gallerySelectionKey(media))} onChange={() => {}} onClick={event => pointerSelection.select(media, event, true)} />
                                        <CardSelectionIcon selected={pointerSelection.entries.has(gallerySelectionKey(media))} />
                                    </label>}
                                    {media.galleryCanCollapse !== false && (media.fileCount > 1 || media.galleryFile) && <ActionButton icon={media.galleryFile ? <ChevronUp /> : <ChevronDown />} label={`${media.isUserAlbum ? translate("gallery.album") : translate("media.file")} ${media.galleryFile ? translate("gallery.collapse") : translate("gallery.expand")}`} className="gallery-card-action" disabled={pageDisabled} aria-expanded={Boolean(media.galleryFile)} aria-label={`${media.title} ${media.isUserAlbum ? translate("gallery.album") : translate("media.file")} ${media.galleryFile ? translate("gallery.collapse") : translate("gallery.expand")}`} onClick={event => {
                                        toggleFiles(media.id, event.currentTarget)
                                    }} />}
                                </>}
                                title={media.galleryFile?.title || media.title}
                                isFavorite={media.isFavorite}
                                isPinned={media.isPinned}
                                onTogglePin={() => togglePin(media)}
                                disabled={disabled || moveBusy}
                                mutationDisabled={pageDisabled}
                                favoriteDisabledReason={
                                    draftDirty
                                        ? translate("gallery.saveTitleAndTagEditsBeforeChangingFavorites")
                                        : undefined
                                }
                                onToggleFavorite={() => toggleFavorite(media.id)}
                                onOpenFolder={() => void openCardFolder(media)}
                                onView={media.fileCount > 0 ? () => setViewer({ mediaId: media.id, fileId: media.galleryFile?.id }) : undefined}
                                onDetails={() => { void selectDetail(media.id, media.galleryFileOffset) }}
                                onCreateAnimation={media.galleryFile ? canEditMedia(media.galleryFile) ? () => { void openAnimationEditor(media) } : undefined : media.fileCount === 1 && media.cover?.kind === 'video' ? () => { void openAnimationEditor(media) } : undefined}
                                editorDisabled={mediaEditLoading}
                                onAdd={media.fileCount > 0 ? () => media.galleryFile ? albumControls.current?.addFiles([media.galleryFile.id]) : albumControls.current?.addMedia(media.id) : undefined}
                                addLabel={!media.galleryFile && media.fileCount > 1 ? translate("gallery.addAll") : translate("gallery.album.add")}
                                onEditAlbum={media.isUserAlbum && !media.galleryFile ? () => albumControls.current?.edit(media) : undefined}
                                onRemoveFromAlbum={media.isUserAlbum && media.galleryFile ? () => removeFromAlbum(media) : undefined}
                                onToggleFiles={!media.galleryFile && media.galleryCanCollapse !== false && media.fileCount > 1 ? event => toggleFiles(media.id, event.currentTarget) : undefined}
                                filesLabel={`${media.isUserAlbum ? translate("gallery.album") : translate("media.file")} ${media.galleryFile ? translate("gallery.collapse") : translate("gallery.expand")}`}
                                filesExpanded={Boolean(media.galleryFile)}
                                fileOnly={Boolean(media.galleryFile)}
                                onEdit={(field) => editDetail(media.id, field)}
                                onUnregister={() => unregisterMedia(media.id)}
                                removalLabel={removalLabel(removalCounts(media))}
                            />
                        </GalleryCard>
                        </div>
                    })}
                </div>

                {list.page.total > 0 && (
                    <div className={list.loadingMore || list.refreshing || list.moreError || list.page.hasNextPage ? 'space-y-3 py-4 text-center' : undefined}>
                        <div ref={sentinelRef} className="h-px" aria-hidden="true" />
                        {(list.loadingMore || list.refreshing) && <p role="status" className="text-sm">{translate("gallery.loading")}</p>}
                        {list.moreError && <p role="alert" className="text-sm">{translateError(list.moreError)}</p>}
                        {list.page.hasNextPage && !list.error && <button type="button" data-detail-trigger className={button} disabled={pageDisabled || list.loadingMore} onClick={() => { void store.loadMore() }}>{list.moreError ? translate("common.retry") : translate("gallery.loadMore")}</button>}
                    </div>
                )}
            </div>
        </ListDetailLayout>
        </div>
        </>
    )
}
