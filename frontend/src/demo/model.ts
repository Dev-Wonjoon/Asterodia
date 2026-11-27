import type { MediaApi } from '@contracts/media-api'
import type { DownloadApi, DownloadJobDto, DownloadResponse, DownloadRunDto } from '@contracts/download-api'
import type { AlbumEntry, MediaErrorCode, MediaFilter, MediaResult, MediaSummaryDto } from '@contracts/media-v2'
import { createSeed, type DemoItem } from './seed'

const ok = <T>(data: T) => ({ ok: true as const, data: structuredClone(data) })
const fail = (code: MediaErrorCode, message: string = code): MediaResult<never> => ({ ok: false, error: { code, message, context: {} } })
const page = <T>(rows: T[], offset = 0, limit = 50) => ({ data: rows.slice(offset, offset + limit), offset, total: rows.length, hasNextPage: offset + limit < rows.length })

/** A deliberately isolated in-memory backend. No fetch, native runtime, or disk access. */
export function createDemoModel() {
    let { items, files } = createSeed()
    let nextId = 100, nextTag = 10
    const jobs = new Map<string, DownloadJobDto>()
    const timers = new Map<string, ReturnType<typeof setInterval>>()
    const pending = new Map<string, (result: DownloadResponse<DownloadRunDto>) => void>()
    const listeners = new Set<(id: number) => void>()
    const find = (id: number) => items.find(item => item.summary.id === id)
    const file = (id: number) => files.find(value => value.id === id)
    const summary = (item: DemoItem): MediaSummaryDto => ({ ...item.summary, fileCount: item.fileIds.length,
        managedFileCount: item.fileIds.length, childAlbumCount: items.filter(other => other.summary.parentId === item.summary.id).length })
    const touch = (item: DemoItem) => {
        item.summary.revision++
        item.summary.updatedAt = Date.now()
        const cover = file(item.fileIds[0])
        if (item.summary.coverMode === 'auto') item.summary.cover = cover ? { fileId: cover.id, path: cover.thumbnailPath || cover.filepath, kind: cover.kind } : null
        listeners.forEach(listener => listener(item.summary.id))
    }
    const matches = (item: DemoItem, filter?: MediaFilter) => {
        if (!filter) return true
        const tags = item.tags.map(tag => tag.name.toLowerCase())
        const text = [item.summary.title, item.author, item.platform, ...tags].join(' ').toLowerCase()
        return (!filter.keyword || text.includes(filter.keyword.toLowerCase())) &&
            (!filter.tags?.length || (filter.tagMode === 'or' ? filter.tags.some(tag => tags.includes(tag.toLowerCase())) : filter.tags.every(tag => tags.includes(tag.toLowerCase())))) &&
            (!filter.platforms?.length || filter.platforms.includes(item.platform)) && (!filter.authors?.length || filter.authors.includes(item.author)) &&
            (!filter.createdFrom || item.summary.createdAt >= filter.createdFrom) && (!filter.createdTo || item.summary.createdAt <= filter.createdTo)
    }
    const albumFiles = (item: DemoItem, visited = new Set<number>()): number[] => {
        if (visited.has(item.summary.id)) return []
        visited.add(item.summary.id)
        return [...new Set([...item.fileIds, ...items.filter(child => child.summary.parentId === item.summary.id).flatMap(child => albumFiles(child, visited))])]
    }

    const media: MediaApi = {
        async bulkEdit(request) {
            if (!request.selection.length || request.selection.length > 200 || new Set(request.selection.map(row => row.mediaId)).size !== request.selection.length
                || [...(request.addTags ?? []), ...(request.removeTags ?? [])].some(name => !name.trim() || [...name.trim()].length > 100)
                || (request.addTags?.length ?? 0) + (request.removeTags?.length ?? 0) > 100
                || (!request.addTags?.length && !request.removeTags?.length && request.isFavorite === undefined)) return fail('INVALID_ARGUMENT')
            const updated: { mediaId: number; revision: number }[] = []
            const failed: import('@contracts/media-api').MediaBulkEditResult['failed'] = []
            for (const target of request.selection) {
                const item = find(target.mediaId)
                if (!item || item.summary.revision !== target.revision) {
                    const error = fail(item ? 'REVISION_CONFLICT' : 'NOT_FOUND')
                    if (!error.ok) failed.push({ mediaId: target.mediaId, error: error.error })
                    continue
                }
                const result = await media.edit({ mediaId: target.mediaId, expectedRevision: target.revision, isFavorite: request.isFavorite,
                    tagChange: { addNames: request.addTags, removeIds: item.tags.filter(tag => request.removeTags?.map(name => name.trim()).includes(tag.name)).map(tag => tag.id) } })
                if (result.ok) updated.push({ mediaId: target.mediaId, revision: result.data.revision })
                else failed.push({ mediaId: target.mediaId, error: result.error })
            }
            return ok({ updated, failed })
        },
        async metadata(request) {
            return ok({ media: request.mediaIds.flatMap(id => { const item = find(id); return item ? [summary(item)] : [] }),
                files: request.fileIds.flatMap(id => { const value = file(id); return value ? [value] : [] }) })
        },
        async list(request = {}) {
            let rows = items.filter(item => matches(item, request.filter) && (!request.favoritesOnly || item.summary.isFavorite))
            rows = rows.filter(item => !item.summary.parentId)
            const isAlbum = (item: DemoItem) => item.summary.isUserAlbum || item.fileIds.length > 1
            if (request.galleryView === 'albums') rows = rows.filter(isAlbum)
            const expanded = new Set(request.expandedMediaIds ?? [])
            const result = rows.sort((a, b) => Number(!!b.summary.isPinned) - Number(!!a.summary.isPinned) || b.summary.createdAt - a.summary.createdAt).flatMap(item => {
                const card = summary(item)
                const showFiles = request.galleryView === 'files' || (isAlbum(item) && (request.showAlbumFiles || expanded.has(card.id)))
                const fileRows = showFiles ? item.fileIds.flatMap((id, index) => {
                    const value = file(id)
                    return value ? [{ ...card, galleryFile: value, galleryFileOffset: index, galleryCanCollapse: true }] : []
                }) : []
                return request.galleryView === 'files' ? fileRows : [card, ...fileRows]
            })
            return ok(page(result, request.offset, request.limit))
        },
        async get(request) {
            const item = find(request.mediaId)
            if (!item) return fail('NOT_FOUND')
            return ok({ ...summary(item), files: page(item.fileIds.flatMap(id => file(id) ? [file(id)!] : []), request.fileOffset, request.fileLimit),
                tags: item.tags, favorite: item.summary.isFavorite ? { id: item.summary.id, createdAt: item.summary.createdAt } : null,
                localFolder: null, locations: [] })
        },
        async sequence({ mediaId }) {
            const item = find(mediaId)
            return item ? ok({ ids: albumFiles(item), title: item.summary.title, revision: item.summary.revision }) : fail('NOT_FOUND')
        },
        async readFile({ fileId }) { const value = file(fileId); return value ? ok(value) : fail('NOT_FOUND') },
        async edit(request) {
            const item = find(request.mediaId)
            if (!item) return fail('NOT_FOUND')
            if (item.summary.revision !== request.expectedRevision) return fail('REVISION_CONFLICT')
            if (request.cover?.mode === 'manual' && !item.fileIds.includes(request.cover.fileId)) return fail('INVALID_COVER')
            if (request.customTitle !== undefined) {
                item.summary.customTitle = request.customTitle
                item.summary.title = request.customTitle || item.summary.sourceTitle || 'Untitled album'
            }
            if (request.isFavorite !== undefined) item.summary.isFavorite = request.isFavorite
            if (request.isPinned !== undefined) item.summary.isPinned = request.isPinned
            if (request.cover) {
                item.summary.coverMode = request.cover.mode
                if (request.cover.mode === 'manual') {
                    const value = file(request.cover.fileId)!
                    item.summary.cover = { fileId: value.id, path: value.thumbnailPath || value.filepath, kind: value.kind }
                }
            }
            if (request.tagIds) item.tags = item.tags.filter(tag => request.tagIds!.includes(tag.id))
            item.tags = item.tags.filter(tag => !request.tagChange?.removeIds?.includes(tag.id))
            for (const name of request.tagChange?.addNames ?? []) {
                const clean = name.trim()
                if (clean && !item.tags.some(tag => tag.name === clean)) {
                    const existing = items.flatMap(row => row.tags).find(tag => tag.name === clean)
                    item.tags.push(existing ? { ...existing } : { id: nextTag++, name: clean })
                }
            }
            touch(item)
            return ok(summary(item))
        },
        async albums(request = {}) { return ok(page(items.filter(item => item.summary.isUserAlbum && (!request.keyword || item.summary.title.toLowerCase().includes(request.keyword.toLowerCase()))).map(summary), request.offset, request.limit)) },
        async directories() { return ok(page([])) },
        async saveAlbum(request) {
            let item = request.albumId ? find(request.albumId) : undefined
            if (request.albumId && !item) return fail('NOT_FOUND')
            if (item && request.expectedRevision !== item.summary.revision) return fail('REVISION_CONFLICT')
            if (item && !item.summary.isUserAlbum) return fail('INVALID_ARGUMENT')
            if (request.parentId && (!find(request.parentId)?.summary.isUserAlbum || request.parentId === request.albumId)) return fail('INVALID_ARGUMENT')
            if (request.delete && item) {
                items = items.filter(row => row !== item)
                items.forEach(row => { if (row.summary.parentId === item!.summary.id) row.summary.parentId = null })
                return ok(summary(item))
            }
            const added = [...(request.addFileIds ?? []), ...(request.addMediaIds ?? []).flatMap(id => find(id) ? albumFiles(find(id)!) : [])]
            if (added.some(id => !file(id))) return fail('NOT_FOUND')
            if (!item) {
                const title = request.title?.trim() || 'My album'
                item = { summary: { id: nextId++, title, sourceTitle: title, customTitle: title, sourceKind: 'local', requestUrl: null, revision: 0,
                    coverMode: 'auto', cover: null, fileCount: 0, isUserAlbum: true, isFavorite: false, createdAt: Date.now(), updatedAt: null, parentId: request.parentId ?? null },
                    fileIds: [], tags: [], author: '', platform: 'Sample' }
                items.push(item)
            }
            if (request.title?.trim()) item.summary.title = item.summary.customTitle = request.title.trim()
            item.fileIds = [...new Set([...item.fileIds, ...added])].filter(id => !request.removeFileIds?.includes(id))
            if (request.moveFileId && item.fileIds.includes(request.moveFileId)) {
                item.fileIds = item.fileIds.filter(id => id !== request.moveFileId)
                const index = request.beforeFileId ? item.fileIds.indexOf(request.beforeFileId) : -1
                item.fileIds.splice(index < 0 ? item.fileIds.length : index, 0, request.moveFileId)
            }
            touch(item)
            return ok(summary(item))
        },
        async exploreAlbum(request) {
            const item = find(request.mediaId)
            if (!item) return fail('NOT_FOUND')
            const ids = request.recursive ? albumFiles(item) : item.fileIds
            const entries: AlbumEntry[] = ids.flatMap(id => {
                const value = file(id), owner = value && find(value.mediaId)
                return value && owner && matches(owner, request.filter) ? [{ kind: 'file' as const, file: value, albumId: item.summary.id, title: value.title || value.fileKey }] : []
            })
            entries.sort((a, b) => {
                if (a.kind !== 'file' || b.kind !== 'file') return 0
                const field = request.sort
                const result = field === 'date' ? a.file.createdAt - b.file.createdAt : field === 'size' ? (a.file.filesize || 0) - (b.file.filesize || 0) : field === 'duration' ? (a.file.duration || 0) - (b.file.duration || 0) : field === 'kind' ? a.file.kind.localeCompare(b.file.kind) : a.title.localeCompare(b.title)
                return request.descending ? -result : result
            })
            if (!request.recursive) entries.unshift(...items.filter(child => child.summary.parentId === item.summary.id).map(child => ({ kind: 'album' as const, album: summary(child) })))
            const breadcrumbs: { id: number; title: string }[] = []
            let parent = item.summary.parentId ? find(item.summary.parentId) : undefined
            while (parent && breadcrumbs.length < items.length) { breadcrumbs.unshift({ id: parent.summary.id, title: parent.summary.title }); parent = parent.summary.parentId ? find(parent.summary.parentId) : undefined }
            return ok({ ...page(entries, request.offset, request.limit), album: summary(item), breadcrumbs, revisionToken: String(item.summary.revision) })
        },
        async mergeAlbum() { return fail('NOT_READY', 'Sample demo: folder merging is available in the desktop app.') },
        async unregisterFiles(request) {
            const item = find(request.mediaId)
            if (!item) return fail('NOT_FOUND')
            if (item.summary.revision !== request.expectedRevision) return fail('REVISION_CONFLICT')
            item.fileIds = item.fileIds.filter(id => !request.fileIds.includes(id)); touch(item)
            return ok({ mediaId: item.summary.id, parentRemoved: false, revision: item.summary.revision, cleanupTaskId: null })
        },
        async unregisterMedia(request) {
            const item = find(request.mediaId)
            if (!item) return fail('NOT_FOUND')
            if (item.summary.revision !== request.expectedRevision) return fail('REVISION_CONFLICT')
            items = items.filter(row => row !== item)
            items.forEach(row => { if (row.summary.parentId === item.summary.id) row.summary.parentId = null })
            return ok({ mediaId: item.summary.id, parentRemoved: true, revision: null, cleanupTaskId: null })
        },
        async suggest(request) {
            const values = request.kind === 'tag' ? items.flatMap(item => item.tags.map(tag => tag.name)) : items.map(item => item[request.kind])
            return ok([...new Set(values)].filter(value => value.toLowerCase().includes(request.text.toLowerCase())).slice(0, request.limit ?? 10))
        },
        async searchSuggest(request) {
            const values = items.filter(item => matches(item, request.filter) && (!request.favoritesOnly || item.summary.isFavorite)).flatMap(item => [
                { kind: 'title' as const, value: item.summary.title }, { kind: 'author' as const, value: item.author }, { kind: 'platform' as const, value: item.platform }, ...item.tags.map(tag => ({ kind: 'tag' as const, value: tag.name }))])
            const unique = values.filter((value, index, rows) => value.value && (!request.kind || value.kind === request.kind) && value.value.toLowerCase().includes(request.text.toLowerCase()) && rows.findIndex(other => other.kind === value.kind && other.value === value.value) === index)
            return ok({ ...page(unique, request.offset, request.limit ?? 10), revision: 1, indexed: true })
        },
    }

    const stop = (id: string) => { clearInterval(timers.get(id)); timers.delete(id) }
    const downloads: DownloadApi = {
        async enqueue(request) {
            try { if (!['https:', 'http:'].includes(new URL(request.url).protocol)) return fail('INVALID_ARGUMENT') } catch { return fail('INVALID_ARGUMENT') }
            if (request.mode !== 'create') return fail('NOT_READY', 'Use the sample download in this demo.')
            if ([...jobs.values()].some(job => job.state === 'running')) return fail('JOB_ACTIVE')
            const id = `demo-${nextId++}`
            const job: DownloadJobDto = { id, url: request.url, title: 'New moment · 새로운 순간', thumbnailPath: 'sample:coast', state: 'pending', phase: null, progress: 0,
                options: request.options ?? { type: 'video', quality: 'best', playlist: false }, cleanupRequired: false, currentAttemptId: null, errorCode: null }
            jobs.set(id, job); return ok(job)
        },
        async list(request = {}) {
            const rows = [...jobs.values()].reverse()
            return ok({ ...page(rows, request.offset, request.limit), activeJobs: rows.filter(job => job.state === 'running' || job.state === 'pending'), limit: request.limit ?? 50, offset: request.offset ?? 0 })
        },
        async get({ jobId }) { const job = jobs.get(jobId); return job ? ok(job) : fail('NOT_FOUND') },
        async run({ jobId }) {
            const job = jobs.get(jobId)
            if (!job) return fail('NOT_FOUND')
            if (timers.has(jobId) || job.state === 'completed') return fail('INVALID_ARGUMENT')
            job.state = 'running'; job.phase = 'downloading'; job.progress = 0; job.currentAttemptId = `${jobId}-attempt`
            return new Promise<DownloadResponse<DownloadRunDto>>(resolve => {
            pending.set(jobId, resolve)
            timers.set(jobId, setInterval(() => {
                job.progress = Math.min(100, job.progress + 10)
                if (job.progress < 100) return
                stop(jobId); job.state = 'completed'; job.phase = 'finished'
                const source = files[0], id = nextId++
                const newFile = { ...source, id, mediaId: id, title: job.title, createdAt: Date.now() }
                files.push(newFile)
                const seed = createSeed().items[0]
                const item: DemoItem = { ...seed, summary: { ...seed.summary, id, title: job.title!, sourceTitle: job.title, requestUrl: job.url,
                    isFavorite: false, createdAt: Date.now(), cover: { fileId: id, path: source.thumbnailPath!, kind: source.kind } }, fileIds: [id], tags: [{ id: 5, name: 'demo' }] }
                items.push(item)
                const profile = items.find(row => row.summary.id === 7)
                if (profile) { profile.fileIds.push(id); touch(profile) }
                listeners.forEach(listener => listener(id))
                pending.delete(jobId)
                resolve(ok({ status: 'committed', jobId, attemptId: job.currentAttemptId!, mediaIdSnapshot: id }))
            }, 650))
            })
        },
        async cancel({ jobId }) { const job = jobs.get(jobId); if (!job) return fail('NOT_FOUND'); stop(jobId); job.state = 'cancelled'; pending.get(jobId)?.(fail('INVALID_ARGUMENT', 'Cancelled')); pending.delete(jobId); return ok({ accepted: true as const }) },
        async remove({ jobId }) { stop(jobId); jobs.delete(jobId); return ok({ accepted: true as const }) },
        async previewEngine() { return 'gallery-dl' },
        async logs({ jobId }) { const job = jobs.get(jobId); return job ? ok({ entries: [{ sequence: 1, attemptId: job.currentAttemptId || '', recordedAt: Date.now(), engine: 'demo', stream: 'system' as const,
            text: 'Sample simulation only. No URL is fetched and no files are downloaded.' }], attempts: [], currentAttemptId: job.currentAttemptId, state: job.state, hasOlder: false, truncated: false, warning: null }) : fail('NOT_FOUND') },
    }
    return { media, downloads,
        subscribe(listener: (id: number) => void) { listeners.add(listener); return () => { listeners.delete(listener) } },
        reset() { timers.forEach((_, id) => stop(id)); pending.forEach(resolve => resolve(fail('INVALID_ARGUMENT', 'Reset'))); pending.clear(); jobs.clear(); ({ items, files } = createSeed()); nextId = 100; nextTag = 10 },
        dispose() { timers.forEach((_, id) => stop(id)); pending.forEach(resolve => resolve(fail('INVALID_ARGUMENT', 'Closed'))); pending.clear(); listeners.clear() },
    }
}
