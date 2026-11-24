import { translate } from '@renderer/shared/i18n'
import type { MediaApi } from '@contracts/media-api'
import type { MediaEditApi } from '@contracts/media-edit-api'
import type { MediaFileDto, MediaFilter, MediaSummaryDto, MediaDetailDto, MediaResult, MediaPage } from '@contracts/media-v2'

export async function request<T>(path: string, options?: RequestInit): Promise<T> {
    const response = await fetch(path, { credentials: 'same-origin', ...options })
    if (!response.ok) throw new Error(response.status === 403 ? translate("common.checkMobileEditingPermissionInThePcApp") : translate("common.couldNotConnectToTheServerTryAgainShortly"))
    return response.json()
}
export function filterParameters(filter: MediaFilter = {}) {
    const query = new URLSearchParams({ q: filter.keyword ?? '', tagMode: filter.tagMode ?? 'and' })
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
    list: (value = {}) => result(async () => {
        const query = filterParameters(value.filter)
        query.set('limit', String(value.limit ?? 24)); query.set('offset', String(value.offset ?? 0))
        query.set('gallery', value.gallery ? '1' : '0'); query.set('expanded', (value.expandedMediaIds ?? []).join(','))
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
    albums: unavailable, saveAlbum: unavailable, mergeAlbum: unavailable, directories: unavailable, edit: unavailable, unregisterFiles: unavailable, unregisterMedia: unavailable,
}
export interface EditAccess { enabled: boolean; csrf: string }
let access: EditAccess = { enabled: false, csrf: '' }
export async function refreshAccess() { access = await request<EditAccess>('/api/edit/status'); return access }
async function editRequest(name: string, value?: unknown, read = false, attempt = 0): Promise<any> {
    const query = read && value ? '?' + new URLSearchParams(Object.entries(value).map(([key, item]) => [key, String(item)])) : ''
    const response = await fetch('/api/edit/' + name + query, read ? { credentials: 'same-origin' } : { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-StorageBucket-CSRF': access.csrf }, body: JSON.stringify(value) })
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
const api = { media, mediaEdit, showItemInFolder: async () => {} }
export function getApi() { return api }
