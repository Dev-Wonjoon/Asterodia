import { beforeEach, describe, expect, it, vi } from 'vitest'

const bindings = vi.hoisted(() => ({
    webStatus: vi.fn(), webStart: vi.fn(), webStop: vi.fn(),
    searchSuggest: vi.fn(),
    list: vi.fn(), edit: vi.fn(), enqueue: vi.fn(), run: vi.fn(), preview: vi.fn(), logs: vi.fn(), remove: vi.fn(),
    thumbnail: vi.fn(), path: vi.fn(), on: vi.fn(), importDatabase: vi.fn(), chooseFolder: vi.fn(), localGet: vi.fn(), localRun: vi.fn(), localCancel: vi.fn(), localRescan: vi.fn(), directories: vi.fn(), localPause: vi.fn(), localResume: vi.fn(), localFiles: vi.fn(), localRetry: vi.fn(), localExclude: vi.fn()
}))
vi.mock('@wailsio/runtime', () => ({ Events: { On: bindings.on } }))
vi.mock('@bindings/update', () => ({ Service: { Status: vi.fn(), Check: vi.fn(), Download: vi.fn(), Restart: vi.fn() } }))
vi.mock('@bindings/filemove', () => ({ Service: { Create: vi.fn(), Get: vi.fn(), Run: vi.fn(), Pause: vi.fn(), Retry: vi.fn() } }))
vi.mock('@bindings/mediaedit', () => ({ Service: { Prepare: vi.fn(), Create: vi.fn(), GetSession: vi.fn(), GetJob: vi.fn(), List: vi.fn(), Cancel: vi.fn(), Release: vi.fn(), ResultPath: vi.fn() } }))
vi.mock('@bindings/webserver', () => ({ Service: { Status: bindings.webStatus, Start: bindings.webStart, Stop: bindings.webStop } }))
vi.mock('@bindings/download', () => ({ Service: { Enqueue: bindings.enqueue, Run: bindings.run, PreviewEngine: bindings.preview, Logs: bindings.logs, Delete: bindings.remove } }))
vi.mock('@bindings/media', () => ({ Service: { List: bindings.list, Edit: bindings.edit, Directories: bindings.directories, SearchSuggest: bindings.searchSuggest } }))
vi.mock('@bindings/thumbnail', () => ({ Service: { Get: bindings.thumbnail, GetDownloadPreview: vi.fn() } }))
vi.mock('@bindings/settings', () => ({ Service: { GetDownloadPath: bindings.path } }))
vi.mock('@bindings/desktop', () => ({ SystemService: { GetDoubleClickInterval: vi.fn() }, Service: { ImportDatabase: bindings.importDatabase } }))
vi.mock('@bindings/engine', () => ({ Manager: {} }))
vi.mock('@bindings/localimport', () => ({ Service: { Choose: bindings.chooseFolder, Get: bindings.localGet, Run: bindings.localRun, Cancel: bindings.localCancel, Rescan: bindings.localRescan, Pause: bindings.localPause, Resume: bindings.localResume, Files: bindings.localFiles, Retry: bindings.localRetry, Exclude: bindings.localExclude } }))
import { getApi } from '@renderer/shared/api'

describe('Wails API boundary', () => {
    it('preserves typed suggestion filters, pagination and index revision', async () => {
        const request = { text: '행', offset: 10, limit: 10, filter: { tags: ['여행'], authors: ['작가'] } }
        const result = { ok: true, data: { data: [{ kind: 'title', value: '은행' }], offset: 10, hasNextPage: true, revision: 12, indexed: true } }
        bindings.searchSuggest.mockResolvedValue(result)
        expect(await getApi().media.searchSuggest(request)).toEqual(result)
        expect(bindings.searchSuggest).toHaveBeenCalledExactlyOnceWith(request)
    })
    beforeEach(() => vi.clearAllMocks())
    it('keeps web server lifecycle calls separate from library mutations', async () => {
        const stopped = { ok: true, data: { running: false, url: 'http://127.0.0.1:9246', error: null } }
        const running = { ok: true, data: { ...stopped.data, running: true } }
        bindings.webStatus.mockResolvedValue(stopped); bindings.webStart.mockResolvedValue(running); bindings.webStop.mockResolvedValue(stopped)
        expect(await getApi().webServer.status()).toEqual(stopped)
        expect(await getApi().webServer.start()).toEqual(running)
        expect(await getApi().webServer.stop()).toEqual(stopped)
        expect(bindings.webStart).toHaveBeenCalledWith(); expect(bindings.webStop).toHaveBeenCalledWith()
    })
    it('deletes only the requested history record through the download service', async () => {
        const result = { ok: true, data: { accepted: true } }
        bindings.remove.mockResolvedValue(result)
        expect(await getApi().downloads.remove({ jobId: 'failed-request' })).toEqual(result)
        expect(bindings.remove).toHaveBeenCalledWith({ jobId: 'failed-request' })
    })
    it('preserves actual log output, attempt selection and older-page cursor', async () => {
        const result = { ok: true, data: { entries: [{ sequence: 4, attemptId: 'attempt', recordedAt: 1000, engine: 'yt-dlp', stream: 'stderr', text: 'ERROR: HTTP 403' }], attempts: [], currentAttemptId: 'attempt', state: 'failed', hasOlder: true, truncated: false, warning: null } }
        bindings.logs.mockResolvedValue(result)
        expect(await getApi().downloads.logs({ jobId: 'job', attemptId: 'attempt', before: 8, limit: 100 })).toEqual(result)
        expect(bindings.logs).toHaveBeenCalledWith({ jobId: 'job', attemptId: 'attempt', before: 8, limit: 100 })
    })
    it('passes durable pause/resume and file retry/exclusion requests unchanged', async () => {
        const success = { ok: true, data: true }
        for (const method of [bindings.localPause, bindings.localResume, bindings.localRetry, bindings.localExclude]) method.mockResolvedValue(success)
        const files = { ok: true, data: { files: [{ id: 'target', folderId: 'folder', path: 'a/b.png', state: 'failed', error: '손상' }], total: 1, hasNextPage: false } }
        bindings.localFiles.mockResolvedValue(files)
        await getApi().localImport.pause({ jobId: 'job' })
        await getApi().localImport.resume({ jobId: 'job' })
        expect(await getApi().localImport.files({ jobId: 'job', fileState: 'failed', offset: 25, limit: 25 })).toEqual(files)
        await getApi().localImport.retry({ jobId: 'job', targetId: 'target' })
        await getApi().localImport.exclude({ jobId: 'job', allFailed: true })
        expect(bindings.localPause).toHaveBeenCalledWith({ jobId: 'job' })
        expect(bindings.localResume).toHaveBeenCalledWith({ jobId: 'job' })
        expect(bindings.localFiles).toHaveBeenCalledWith({ jobId: 'job', fileState: 'failed', offset: 25, limit: 25 })
        expect(bindings.localRetry).toHaveBeenCalledWith({ jobId: 'job', targetId: 'target' })
        expect(bindings.localExclude).toHaveBeenCalledWith({ jobId: 'job', allFailed: true })
    })
    it('lists shared directories independently of media groups', async () => {
        const result = { ok: true, data: { data: [{ id: 7, directoryPath: 'C:/B', keyVersion: 0, importMediaId: 10, fileCount: 12, mediaCount: 3 }], total: 1, hasNextPage: false } }
        bindings.directories.mockResolvedValue(result)
        expect(await getApi().media.directories({ offset: 0, limit: 25, keyword: 'B' })).toEqual(result)
        expect(bindings.directories).toHaveBeenCalledWith({ offset: 0, limit: 25, keyword: 'B' })
    })
    it('keeps folder import selection, pagination, cancellation and rescan separate', async () => {
        const result = { ok: true, data: { job: { id: 'local-1', state: 'ready' }, folders: [], total: 40, hasNextPage: true } }
        bindings.chooseFolder.mockResolvedValue(result)
        bindings.localGet.mockResolvedValue(result)
        bindings.localRun.mockResolvedValue({ ok: true, data: true })
        bindings.localCancel.mockResolvedValue({ ok: true, data: true })
        bindings.localRescan.mockResolvedValue(result)
        expect(await getApi().localImport.choose()).toEqual(result)
        expect(await getApi().localImport.get({ jobId: 'local-1', offset: 25, limit: 25 })).toEqual(result)
        expect(bindings.localGet).toHaveBeenCalledWith({ jobId: 'local-1', offset: 25, limit: 25 })
        await getApi().localImport.run({ jobId: 'local-1' })
        await getApi().localImport.cancel({ jobId: 'local-1' })
        await getApi().localImport.rescan({ jobId: 'local-1' })
        expect(bindings.localRun).toHaveBeenCalledWith({ jobId: 'local-1' })
        expect(bindings.localCancel).toHaveBeenCalledWith({ jobId: 'local-1' })
        expect(bindings.localRescan).toHaveBeenCalledWith({ jobId: 'local-1' })
    })
    it('previews the engine with the actual selected options', async () => {
        bindings.preview.mockResolvedValueOnce('gallery-dl')
        expect(await getApi().downloads.previewEngine('https://www.instagram.com/p/fixture/', { type: 'video', quality: 'best', playlist: false })).toBe('gallery-dl')
        expect(bindings.preview).toHaveBeenCalledWith('https://www.instagram.com/p/fixture/', { type: 'video', quality: 'best', extension: '', playlist: false })
    })
    it('preserves database import counts and cancellation', async () => {
        const cancelled = { ok: true, data: { cancelled: true } }
        bindings.importDatabase.mockResolvedValueOnce(cancelled)
        expect(await getApi().database.import()).toEqual(cancelled)
        const imported = { ok: true, data: { cancelled: false, addedMedia: 2, addedFiles: 3, skippedMedia: 1, skippedFiles: 1, backupPath: 'C:/test/backups/before.db' } }
        bindings.importDatabase.mockResolvedValueOnce(imported)
        expect(await getApi().database.import()).toEqual(imported)
        expect(bindings.importDatabase).toHaveBeenCalledTimes(2)
    })
    it('preserves database migration failures for the settings screen', async () => {
        const failure = { ok: false, error: { code: 'DATABASE_IMPORT_FAILED', message: '알 수 없는 스키마', context: {} } }
        bindings.importDatabase.mockResolvedValueOnce(failure)
        expect(await getApi().database.import()).toEqual(failure)
    })
    it('passes pagination and filters without changing the contract', async () => {
        const expected = { ok: true, data: { data: [], total: 0, hasNextPage: false } }
        bindings.list.mockResolvedValue(expected)
        const request = { offset: 50, filter: { tags: ['여행'], tagMode: 'and' as const } }
        expect(await getApi().media.list(request)).toEqual(expected)
        expect(bindings.list).toHaveBeenCalledWith(request)
    })
    it('preserves explicit title reset and transport error envelopes', async () => {
        const expected = { ok: false, error: { code: 'REVISION_CONFLICT', message: 'changed', context: {} } }
        bindings.edit.mockResolvedValue(expected)
        const request = { mediaId: 4, expectedRevision: 2, customTitle: null }
        expect(await getApi().media.edit(request)).toEqual(expected)
        expect(bindings.edit).toHaveBeenCalledWith(request)
    })
    it('rejects malformed service responses', async () => {
        bindings.list.mockResolvedValue({ data: [] })
        await expect(getApi().media.list()).rejects.toThrow('응답 형식')
    })
    it('forwards replacement identity and options', async () => {
        const request = { mode: 'replace' as const, url: 'https://example.test/item', mediaId: 5, expectedRevision: 7, options: { type: 'audio' as const, extension: 'mp3' as const } }
        bindings.enqueue.mockResolvedValue({ ok: true, data: { id: 'job' } })
        await getApi().downloads.enqueue(request)
        expect(bindings.enqueue).toHaveBeenCalledWith(request)
    })
    it('subscribes to actual Wails thumbnail events and exposes unsubscribe', () => {
        const off = vi.fn(), listener = vi.fn()
        bindings.on.mockReturnValue(off)
        expect(getApi().onMediaThumbnailUpdated(listener)).toBe(off)
        expect(bindings.on.mock.calls[0][0]).toBe('media:thumbnail-updated')
        bindings.on.mock.calls[0][1]({ data: 9 })
        expect(listener).toHaveBeenCalledWith(9)
    })
})
