import { translateError, translate } from '@renderer/shared/i18n'
import type { MediaApi } from '@contracts/media-api'
import type { MoveSelection } from '@contracts/file-move-api'
import { readExpandedAlbums, saveExpandedAlbums } from '../../../../internal/webserver/web/album-state.js'
import type {
    MediaDetailDto,
    MediaEditRequest,
    MediaErrorCode,
    MediaPage,
    MediaResult,
    MediaSummaryDto,
    MediaUnregisterFilesRequest,
    MediaUnregisterRequest,
    MediaUnregisterResult,
    MediaFilter,
    GalleryView
} from '@contracts/media-v2'

export interface MediaViewError {
    code: MediaErrorCode | 'TRANSPORT_ERROR'
    message: string
}

type Status = 'idle' | 'loading' | 'ready' | 'error' | 'not_ready'

export interface GallerySnapshot {
    expandedMediaIds?: number[]
    list: {
        status: Status
        page: MediaPage<MediaSummaryDto>
        nextOffset: number
        refreshing: boolean
        loadingMore: boolean
        error: MediaViewError | null
        moreError: MediaViewError | null
    }
    detail: {
        mediaId: number | null
        status: Status
        data: MediaDetailDto | null
        offset: number
        error: MediaViewError | null
    }
    busy: boolean
    needsRefresh: boolean
    writeError: MediaViewError | null
    notice: string | null
}

export const MEDIA_PAGE_SIZE = 50
export const GALLERY_PAGE_SIZE = 24

export type GalleryBulkAction =
    | { kind: 'favorite'; value: boolean }
    | { kind: 'tags'; names: string[]; remove: boolean }
    | { kind: 'unregister' }
    | { kind: 'openFolders'; open: (path: string) => Promise<void> }

export interface GalleryBulkResult {
    succeeded: number[]
    failed: { mediaId: number; message: string }[]
}

const transportError: MediaViewError = {
    code: 'TRANSPORT_ERROR',
    get message() { return translate("gallery.couldNotConfirmTheResponseRefreshToCheckThe") }
}

function emptyList(): GallerySnapshot['list'] {
    return {
        status: 'idle',
        page: {
            data: [],
            total: 0,
            hasNextPage: false
        },
        nextOffset: 0,
        refreshing: false,
        loadingMore: false,
        error: null,
        moreError: null
    }
}

export function createGalleryStore(
    api: MediaApi,
    options: {
        favoritesOnly?: boolean
        expandAlbums?: boolean
        albumCards?: boolean
        galleryView?: GalleryView
        showAlbumFiles?: boolean
    } = {}
) {
    const favoritesOnly = options.favoritesOnly ?? false
    const expandAlbums = options.expandAlbums ?? false
    let filter: MediaFilter = {}
    let snapshot: GallerySnapshot = {
        // Explorer entry cards start grouped; old inline expansion preferences
        // remain available to legacy views without changing album membership.
        expandedMediaIds: expandAlbums && !options.albumCards ? readExpandedAlbums() : [],
        list: emptyList(),
        detail: {
            mediaId: null,
            status: 'idle',
            data: null,
            offset: 0,
            error: null
        },
        busy: false,
        needsRefresh: false,
        writeError: null,
        notice: null
    }

    const listeners = new Set<() => void>()
    let expandedAlbums = snapshot.expandedMediaIds ?? []
    let listToken = 0
    let detailToken = 0
    let refreshQueued = false
    let session = 0

    function publish(next: GallerySnapshot) {
        snapshot = next
        listeners.forEach((listener) => listener())
    }

    async function request<T>(
        action: () => Promise<MediaResult<T>>
    ): Promise<{ ok: true; data: T } | { ok: false; error: MediaViewError }> {
        try {
            return await action()
        } catch {
            return {
                ok: false,
                error: transportError
            }
        }
    }

    function cancelReads() {
        session++
        listToken++
        detailToken++

        publish({
            ...snapshot,
            list: {
                ...snapshot.list,
                status: snapshot.list.status === 'loading' ? 'idle' : snapshot.list.status,
                refreshing: false,
                loadingMore: false
            },
            detail:
                snapshot.detail.status === 'loading'
                    ? { ...snapshot.detail, status: 'idle' }
                    : snapshot.detail
        })
    }

    async function loadList(
        mode: 'refresh' | 'append' = 'refresh',
        internal = false,
        refreshCountOverride?: number
    ): Promise<boolean> {
        const append = mode === 'append'
        const previous = snapshot.list

        if (snapshot.busy && !internal) {
            if (!append) refreshQueued = true
            return false
        }

        if (
            append &&
            (snapshot.needsRefresh ||
                previous.status !== 'ready' ||
                previous.refreshing ||
                previous.loadingMore ||
                previous.error !== null ||
                !previous.page.hasNextPage)
        ) {
            return false
        }

        const token = ++listToken
        const requestedFilter = structuredClone(filter)
        const requestedExpanded = [...expandedAlbums]
        const pageSize = expandAlbums ? GALLERY_PAGE_SIZE : MEDIA_PAGE_SIZE

        // 변경 후 재조회할 때 이미 불러온 범위를 유지한다.
        const refreshCount = Math.max(pageSize, refreshCountOverride ?? previous.nextOffset)

        let offset = append ? previous.nextOffset : 0
        const rows = append ? [...previous.page.data] : []

        let total = previous.page.total
        let hasNextPage = false

        publish({
            ...snapshot,
            list: {
                ...previous,
                status: previous.page.data.length > 0 ? 'ready' : 'loading',
                refreshing: !append,
                loadingMore: append,
                error: null,
                moreError: null
            }
        })

        function failList(error: MediaViewError): false {
            if (expandAlbums && !append) expandedAlbums = snapshot.expandedMediaIds ?? []
            publish({
                ...snapshot,
                list: {
                    ...snapshot.list,
                    status:
                        snapshot.list.page.data.length > 0
                            ? 'ready'
                            : error.code === 'NOT_READY'
                              ? 'not_ready'
                              : 'error',
                    refreshing: false,
                    loadingMore: false,
                    error: append ? null : error,
                    moreError: append ? error : null
                }
            })

            return false
        }

        do {
            const result = await request(() =>
                api.list({
                    limit: pageSize,
                    offset,
                    favoritesOnly,
                    filter: requestedFilter,
                    ...(expandAlbums ? { gallery: true, expandedMediaIds: requestedExpanded,
                        ...(options.albumCards ? { albumCards: true } : {}),
                        ...(options.galleryView !== undefined ? { galleryView: options.galleryView } : {}),
                        ...(options.showAlbumFiles !== undefined ? { showAlbumFiles: options.showAlbumFiles } : {}) } : {})
                })
            )

            // 검색 변경·새로고침·편집 이후 도착한 이전 응답은 무시한다.
            if (token !== listToken) return false

            if (!result.ok) {
                return failList(result.error)
            }

            const page = result.data
            if (expandAlbums && append && page.offset !== undefined && page.offset !== offset) {
                return loadList('refresh', internal)
            }

            if (page.hasNextPage && page.data.length === 0) {
                return failList({
                    code: 'INVARIANT_VIOLATION',
                    message: translate("gallery.couldNotLoadTheNextPagePleaseRefresh")
                })
            }

            rows.push(...page.data)
            offset += page.data.length

            total = page.total
            hasNextPage = page.hasNextPage
        } while (!append && hasNextPage && offset < refreshCount)

        const byId = new Map<string, MediaSummaryDto>()

        for (const media of rows) {
            byId.set(media.galleryFile ? `album:${media.id}:file:${media.galleryFile.id}` : `album:${media.id}`, media)
        }

        if (expandAlbums) {
            expandedAlbums = requestedExpanded
            if (!options.albumCards) saveExpandedAlbums(requestedExpanded)
        }

        publish({
            ...snapshot,
            expandedMediaIds: expandAlbums ? requestedExpanded : snapshot.expandedMediaIds,
            list: {
                status: 'ready',
                page: {
                    data: [...byId.values()],
                    total,
                    hasNextPage
                },
                nextOffset: offset,
                refreshing: false,
                loadingMore: false,
                error: null,
                moreError: null
            }
        })

        return true
    }

    async function loadDetail(offset = snapshot.detail.offset, internal = false): Promise<boolean> {
        const mediaId = snapshot.detail.mediaId

        if (mediaId === null || (snapshot.busy && !internal)) {
            return false
        }

        const token = ++detailToken

        publish({
            ...snapshot,
            detail: {
                ...snapshot.detail,
                status: 'loading',
                offset,
                error: null
            }
        })

        const result = await request(() =>
            api.get({
                mediaId,
                fileLimit: MEDIA_PAGE_SIZE,
                fileOffset: offset
            })
        )

        if (token !== detailToken || snapshot.detail.mediaId !== mediaId) {
            return false
        }

        if (!result.ok) {
            publish({
                ...snapshot,
                detail: {
                    ...snapshot.detail,
                    status: result.error.code === 'NOT_READY' ? 'not_ready' : 'error',
                    error: result.error
                }
            })
            return false
        }

        if (offset > 0 && offset >= result.data.files.total) {
            const last = Math.max(
                0,
                Math.floor((result.data.files.total - 1) / MEDIA_PAGE_SIZE) * MEDIA_PAGE_SIZE
            )
            return loadDetail(last, internal)
        }

        publish({
            ...snapshot,
            detail: {
                mediaId,
                status: 'ready',
                data: result.data,
                offset,
                error: null
            }
        })

        return true
    }

    async function refresh(internal = false): Promise<boolean> {
        if (snapshot.busy && !internal) {
            refreshQueued = true
            return false
        }

        const results = await Promise.all([
            loadList('refresh', internal),
            snapshot.detail.mediaId === null
                ? Promise.resolve(true)
                : loadDetail(snapshot.detail.offset, internal)
        ])

        const complete = results.every(Boolean)

        if (complete) {
            publish({
                ...snapshot,
                needsRefresh: false,
                writeError: null
            })
        }

        return complete
    }

    async function mutate<T>(
        action: () => Promise<MediaResult<T>>,
        apply: (data: T) => void,
        successNotice: string | null = translate("gallery.changesApplied")
    ): Promise<boolean> {
        if (snapshot.busy || snapshot.needsRefresh) {
            return false
        }

        cancelReads()
        const operationSession = session

        publish({
            ...snapshot,
            busy: true,
            writeError: null,
            notice: null
        })

        try {
            const result = await request(action)

            if (operationSession !== session) {
                return result.ok
            }

            if (!result.ok) {
                publish({
                    ...snapshot,
                    writeError: result.error,
                    needsRefresh:
                        result.error.code === 'REVISION_CONFLICT' ||
                        result.error.code === 'TRANSPORT_ERROR'
                })
                return false
            }

            apply(result.data)

            // 성공한 변경과 후속 조회 실패를 구분한다.
            publish({
                ...snapshot,
                notice: successNotice,
                detail: {
                    ...snapshot.detail,
                    data: null
                }
            })

            const loaded = await refresh(true)

            if (operationSession === session && !loaded) {
                publish({
                    ...snapshot,
                    needsRefresh: true,
                    notice: translate("gallery.changesWereAppliedButTheViewCouldNotReload")
                })
            }

            return true
        } finally {
            publish({
                ...snapshot,
                busy: false
            })

            if (refreshQueued) {
                refreshQueued = false
                void refresh()
            }
        }
    }

    function edited(media: MediaSummaryDto) {
        const page = snapshot.list.page

        const listed = page.data.some((previous) => previous.id === media.id)

        const removeFromList = favoritesOnly && !media.isFavorite

        const data = removeFromList
            ? page.data.filter((previous) => previous.id !== media.id)
            : page.data.map((previous) => (previous.id === media.id ? { ...previous, ...media, ...(previous.galleryFile ? { isPinned: previous.isPinned } : {}) } : previous))

        const total = removeFromList && listed ? Math.max(0, page.total - 1) : page.total

        publish({
            ...snapshot,
            list: {
                ...snapshot.list,
                page: {
                    data,
                    total,
                    hasNextPage: data.length < total
                }
            }
        })
    }

    function removed(result: MediaUnregisterResult) {
        if (result.parentRemoved) {
            const page = snapshot.list.page

            const wasListed =
                page.data.some((media) => media.id === result.mediaId) ||
                (Object.keys(filter).length === 0 &&
                    (!favoritesOnly ||
                        (snapshot.detail.data?.id === result.mediaId &&
                            snapshot.detail.data.isFavorite)))

            const data = page.data.filter((media) => media.id !== result.mediaId)

            const total = wasListed ? Math.max(0, page.total - 1) : page.total

            publish({
                ...snapshot,
                list: {
                    ...snapshot.list,
                    page: {
                        data,
                        total,
                        hasNextPage: data.length < total
                    }
                }
            })
        }

        if (result.parentRemoved && snapshot.detail.mediaId === result.mediaId) {
            detailToken++

            publish({
                ...snapshot,
                detail: {
                    mediaId: null,
                    status: 'idle',
                    data: null,
                    offset: 0,
                    error: null
                }
            })
        }
    }

    async function bulk(selection: MoveSelection[], action: GalleryBulkAction, progress: (done: number, total: number) => void): Promise<GalleryBulkResult | null> {
        if (snapshot.busy || snapshot.needsRefresh || !selection.length) return null
        const grouped = new Map<number, MoveSelection>(), conflicts = new Set<number>()
        for (const item of selection) {
            const previous = grouped.get(item.mediaId)
            if (previous && action.kind === 'unregister') {
                if (previous.revision !== item.revision) conflicts.add(item.mediaId)
                // A collection selection includes its files. File-only selections
                // stay file-only and share one revision-checked request per parent.
                if (previous.fileIds && item.fileIds) previous.fileIds = [...new Set([...previous.fileIds, ...item.fileIds])]
                else delete previous.fileIds
            } else grouped.set(item.mediaId, { ...item, ...(item.fileIds ? { fileIds: [...new Set(item.fileIds)] } : {}) })
        }
        const targets = [...grouped.values()]
        cancelReads()
        const own = session
        const operationFilter = filter
        const result: GalleryBulkResult = { succeeded: [], failed: [] }
        const folders = new Set<string>()
        let uncertain = false
        publish({ ...snapshot, busy: true, writeError: null, notice: null })
        try {
            for (const target of targets) {
                if (own !== session || operationFilter !== filter || uncertain) {
                    result.failed.push({ mediaId: target.mediaId, message: uncertain ? translate("gallery.confirmThePreviousRequestResultBeforeRunningAgain") : translate("gallery.theViewChangedSoTheActionWasNotRun") })
                    continue
                }
                const outcome = await request(async (): Promise<MediaResult<true>> => {
                    if (conflicts.has(target.mediaId)) return { ok: false, error: { code: 'REVISION_CONFLICT', message: translate("gallery.thisPostChangedAfterSelectionRefreshAndSelectIt"), context: {} } }
                    // Read the current revision without silently replacing the user's selection revision.
                    const current = await api.get({ mediaId: target.mediaId, fileLimit: 1, fileOffset: 0 })
                    if (!current.ok) return current
                    if (current.data.revision !== target.revision) return { ok: false, error: { code: 'REVISION_CONFLICT', message: translate("gallery.thisPostChangedAfterSelectionRefreshAndSelectIt"), context: {} } }
                    if (own !== session || operationFilter !== filter) throw new Error(translate("gallery.theViewHasChanged"))
                    if (action.kind === 'openFolders') {
                        const path = current.data.files.data[0]?.filepath
                        if (!path) return { ok: false, error: { code: 'NOT_FOUND', message: translate("gallery.noRegisteredFilePath"), context: {} } }
                        const normalized = path.replace(/\\/g, '/')
                        const folder = normalized.slice(0, normalized.lastIndexOf('/'))
                        const key = /^[a-z]:\//i.test(normalized) || normalized.startsWith('//') ? folder.toLowerCase() : folder
                        if (!folders.has(key)) { await action.open(path); folders.add(key) }
                    } else if (action.kind === 'unregister') {
                        const response = await (target.fileIds
                            ? api.unregisterFiles({ mediaId: target.mediaId, expectedRevision: target.revision, fileIds: target.fileIds })
                            : api.unregisterMedia({ mediaId: target.mediaId, expectedRevision: target.revision }))
                        if (!response.ok) return response
                        if (own === session) removed(response.data)
                    } else {
                        const response = await api.edit({ mediaId: target.mediaId, expectedRevision: target.revision,
                            ...(action.kind === 'favorite' ? { isFavorite: action.value } : {
                                tagChange: action.remove
                                    ? { removeIds: current.data.tags.filter(tag => action.names.includes(tag.name)).map(tag => tag.id) }
                                    : { addNames: action.names }
                            }) })
                        if (!response.ok) return response
                        if (own === session) edited(response.data)
                    }
                    return { ok: true, data: true }
                })
                if (outcome.ok) result.succeeded.push(target.mediaId)
                else {
                    result.failed.push({ mediaId: target.mediaId, message: translateError(outcome.error) })
                    uncertain = outcome.error.code === 'TRANSPORT_ERROR'
                }
                if (own === session) progress(result.succeeded.length + result.failed.length, targets.length)
            }
            if (own === session && action.kind !== 'openFolders') {
                const loaded = await refresh(true)
                if (own === session) publish({ ...snapshot, needsRefresh: !loaded || uncertain })
            }
            return result
        } finally {
            if (own === session) publish({ ...snapshot, busy: false })
            else publish({ ...snapshot, busy: false, needsRefresh: true })
            if (refreshQueued) { refreshQueued = false; void refresh() }
        }
    }

    return {
        getSnapshot: () => snapshot,

        subscribe(listener: () => void) {
            listeners.add(listener)
            return () => {
                listeners.delete(listener)
            }
        },

        cancelReads,
        bulk,

        setFilter(next: MediaFilter = {}) {
            const copied = structuredClone(next)

            if (JSON.stringify(filter) === JSON.stringify(copied)) {
                return
            }

            filter = copied
            listToken++

            publish({
                ...snapshot,
                list: emptyList()
            })
        },

        refresh: () => refresh(),

        loadMore: () => loadList('append'),

        toggleAlbum(mediaId: number) {
            if (snapshot.busy || snapshot.needsRefresh || snapshot.list.refreshing) return Promise.resolve(false)
            const ids = expandedAlbums
            const next = ids.includes(mediaId) ? ids.filter(id => id !== mediaId) : [...ids, mediaId]
            const shown = snapshot.list.page.data.filter(item => item.id === mediaId && item.galleryFile).length
            const count = ids.includes(mediaId) ? snapshot.list.nextOffset - Math.max(0, shown - 1) : snapshot.list.nextOffset
            expandedAlbums = next
            return loadList('refresh', false, count)
        },

        loadDetail: (offset: number) => loadDetail(offset),

        async openDetail(mediaId: number, fileOffset = 0) {
            if (snapshot.busy) return false

            detailToken++

            publish({
                ...snapshot,
                writeError: null,
                detail: {
                    mediaId,
                    status: 'idle',
                    data: null,
                    offset: fileOffset,
                    error: null
                }
            })

            return loadDetail(fileOffset)
        },

        closeDetail() {
            if (snapshot.busy) return

            detailToken++

            publish({
                ...snapshot,
                detail: {
                    mediaId: null,
                    status: 'idle',
                    data: null,
                    offset: 0,
                    error: null
                }
            })
        },

        edit: (input: MediaEditRequest): Promise<boolean> => {
            const favoriteOnly =
                input.isFavorite !== undefined &&
                Object.keys(input).every((key) =>
                    ['mediaId', 'expectedRevision', 'isFavorite'].includes(key)
                )
            return mutate(() => api.edit(input), media => {
                edited(media)
                if (input.isPinned !== undefined && input.pinFileId) {
                    publish({ ...snapshot, list: { ...snapshot.list, page: { ...snapshot.list.page,
                        data: snapshot.list.page.data.map(item => item.galleryFile?.id === input.pinFileId ? { ...item, isPinned: input.isPinned } : item)
                    } } })
                }
            }, favoriteOnly || input.isPinned !== undefined ? null : undefined)
        },

        unregisterFiles: (input: MediaUnregisterFilesRequest) =>
            mutate(() => api.unregisterFiles(input), removed),

        unregisterMedia: (input: MediaUnregisterRequest) =>
            mutate(() => api.unregisterMedia(input), removed)
    }
}

export type GalleryStore = ReturnType<typeof createGalleryStore>
