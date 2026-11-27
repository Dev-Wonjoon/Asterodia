import { translate } from '@renderer/shared/i18n'
import type { MediaApi } from '@contracts/media-api'
import type { MediaEditApi } from '@contracts/media-edit-api'
import type { MediaFileDto, MediaFilter, MediaSummaryDto, MediaDetailDto, MediaResult, MediaPage, ContentFilters } from '@contracts/media-v2'
import type { AlbumExplorePage } from '@contracts/media-v2'

export async function request<T>(path: string, options?: RequestInit): Promise<T> {
    const response = await fetch(path, { credentials: 'same-origin', ...options })
    if (response.status === 401) window.dispatchEvent(new Event('asterodia:access-expired'))
    if (!response.ok) throw new Error(response.status === 403 ? translate("common.checkMobileEditingPermissionInThePcApp") : translate("common.couldNotConnectToTheServerTryAgainShortly"))
    return response.json()
}
export function filterParameters(filter: MediaFilter = {}) {
    const query = new URLSearchParams({ q: filter.keyword ?? '', tagMode: filter.tagMode ?? 'and' })
    if (filter.showHidden) query.set('showHidden', '1')
    for (const [key, values] of [['tag', filter.tags], ['platform', filter.platforms], ['author', filter.authors]] as const)
        for (const value of values ?? []) query.append(key, value)
    return query
}
async function result<T>(read: () => Promise<T>): Promise<MediaResult<T>> {
    try { return { ok: true, data: await read() } }
    catch (error) { return { ok: false, error: { code: 'INTERNAL_ERROR', message: error instanceof Error ? error.message : translate("common.couldNotConfirmTheServerResponse"), context: {} } } }
}
const unavailable = async (): Promise<MediaResult<never>> => ({ ok: false, error: { code: 'NOT_READY', message: translate("common.thisFeatureIsAvailableInThePcApp"), context: {} } })
export function fileDTO(file: Partial<MediaFileDto> & Pick<MediaFileDto, 'id'>): MediaFileDto {
    return { mediaId: 0, fileKey: String(file.id), fileUuid: '', filenameToken: '', directoryId: null, storageKind: 'external_reference', sourceUrl: null, title: null, relativePath: null, kind: 'unknown', position: 0, filesize: null, duration: null, detectedFormat: null, width: null, height: null, orientation: null, frameCount: null, pageCount: null, primaryImageIndex: null, thumbnailOrigin: 'none', createdAt: 0,
        ...file, filepath: `/api/files/${file.id}/content`, thumbnailPath: `/api/files/${file.id}/thumbnail`, previewPath: `/api/files/${file.id}/preview` }
}
function summary(row: MediaSummaryDto): MediaSummaryDto {
    return { ...row, cover: row.cover ? { ...row.cover, path: `/api/files/${row.cover.fileId}/thumbnail` } : null, galleryFile: row.galleryFile ? fileDTO(row.galleryFile) : undefined }
}
const media: MediaApi = {
    bulkEdit: unavailable,
    exploreAlbum: value => result(async () => {
        const query = filterParameters(value.filter)
        query.set('limit', String(value.limit ?? 100)); query.set('offset', String(value.offset ?? 0))
        query.set('recursive', value.recursive ? '1' : '0'); query.set('sort', value.sort ?? 'name'); query.set('descending', value.descending ? '1' : '0')
        const page = await request<AlbumExplorePage>(`/api/albums/${value.mediaId}/contents?` + query)
        return { ...page, album: summary(page.album), data: page.data.map(entry => entry.kind === 'album' ? { ...entry, album: summary(entry.album) } : { ...entry, file: fileDTO(entry.file) }) }
    }),
    saveAlbum: async value => {
        const access = await request<{ csrf: string }>('/api/albums/access')
        const response = await request<MediaResult<MediaSummaryDto>>('/api/albums', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Asterodia-CSRF': access.csrf }, body: JSON.stringify(value) })
        return response.ok ? { ok: true, data: summary(response.data) } : response
    },
    list: (value = {}) => result(async () => {
        const query = filterParameters(value.filter)
        query.set('limit', String(value.limit ?? 24)); query.set('offset', String(value.offset ?? 0))
        query.set('gallery', value.gallery ? '1' : '0'); query.set('expanded', (value.expandedMediaIds ?? []).join(','))
        if (value.albumCards) query.set('albumCards', '1')
        if (value.anchorMediaId) query.set('anchor', String(value.anchorMediaId))
        const page = await request<MediaPage<MediaSummaryDto>>('/api/library?' + query)
        return { ...page, data: page.data.map(summary) }
    }),
    get: value => result(async () => {
        const row = await request<MediaDetailDto>(`/api/library/${value.mediaId}?fileOffset=${value.fileOffset ?? 0}`)
        return { ...row, ...summary(row), files: { ...row.files, data: row.files.data.map(fileDTO) } }
    }),
    searchSuggest: value => result(() => {
        const query = filterParameters(value.filter)
        query.set('q', value.text); query.set('limit', String(value.limit ?? 10)); query.set('offset', String(value.offset ?? 0))
        if (value.kind) query.set('kind', value.kind)
        return request('/api/search-suggestions?' + query)
    }),
    suggest: value => result(() => request('/api/suggestions?' + new URLSearchParams({ kind: value.kind, q: value.text, limit: String(value.limit ?? 10) }))),
    sequence: value => result(() => request(`/api/library/${value.mediaId}/sequence`)),
    readFile: value => result(async () => fileDTO(await request(`/api/files/${value.fileId}/info`))),
    albums: unavailable, mergeAlbum: unavailable, directories: unavailable, edit: unavailable, unregisterFiles: unavailable, unregisterMedia: unavailable,
}
export interface EditAccess { enabled: boolean; csrf: string }
let access: EditAccess = { enabled: false, csrf: '' }
export async function refreshAccess() { access = await request<EditAccess>('/api/edit/status'); return access }
async function editRequest(name: string, value?: unknown, read = false, attempt = 0): Promise<any> {
    const query = read && value ? '?' + new URLSearchParams(Object.entries(value).map(([key, item]) => [key, String(item)])) : ''
    const response = await fetch('/api/edit/' + name + query, read ? { credentials: 'same-origin' } : { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-Asterodia-CSRF': access.csrf }, body: JSON.stringify(value) })
    if (response.status === 429 && attempt < 5) { await new Promise(resolve => setTimeout(resolve, 1200)); return editRequest(name, value, read, attempt + 1) }
    if (!response.ok && !response.headers.get('Content-Type')?.includes('application/json')) throw new Error(await response.text())
    return response.json()
}
const mediaEdit: MediaEditApi = {
    outputOptions: value => editRequest('output-options', value, true),
    overview: value => editRequest('overview', value, true),
    frames: value => editRequest('frames', value), frameStatus: value => editRequest('frames', value, true), selection: value => editRequest('selection', value), resources: value => editRequest('resources', value),
    prepare: value => editRequest('prepare', value), getSession: value => editRequest('session', value, true), create: value => editRequest('create', value), getJob: value => editRequest('job', value, true),
    list: () => editRequest('list', undefined, true), cancel: value => editRequest('cancel', value), release: value => editRequest('release', value),
    resultPath: async () => ({ ok: false, error: { code: 'NOT_READY', message: translate("common.youCanViewTheResultInTheGallery") } }),
}
async function readContentFilters() {
    return request<ContentFilters & { csrf?: string }>('/api/content-filters')
}
const api = {
    media, mediaEdit, showItemInFolder: async () => {},
    getContentFilters: async (): Promise<ContentFilters> => {
        const value = await readContentFilters()
        return { hiddenPlatforms: value.hiddenPlatforms, hiddenTags: value.hiddenTags }
    },
    setContentFilters: async (value: ContentFilters): Promise<ContentFilters> => {
        // Read a fresh token so saving also works after the PC restarts the web server.
        const access = await readContentFilters()
        if (!access.csrf) throw new Error(translate('contentFilters.saveError'))
        const response = await fetch('/api/content-filters', {
            method: 'POST', credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json', 'X-Asterodia-CSRF': access.csrf },
            body: JSON.stringify({ hiddenPlatforms: value.hiddenPlatforms, hiddenTags: value.hiddenTags }),
        })
        if (!response.ok) throw new Error(translate('contentFilters.saveError'))
        return response.json()
    },
}
export function getApi() { return api }
