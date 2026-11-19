// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { LocalImportJob, LocalImportPage } from '@contracts/local-import-api'
import { LocalImportPanel } from '@renderer/features/localimport/LocalImportPanel'
import { useLocalImportViewModel } from '@renderer/features/localimport/useLocalImportViewModel'
import { canDismissImport, readDismissedImport, writeDismissedImport } from '@renderer/features/localimport/panelVisibility'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ get: vi.fn(), choose: vi.fn(), files: vi.fn(), pause: vi.fn(), resume: vi.fn(), run: vi.fn(), retry: vi.fn(), exclude: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ localImport: api }) }))
const page = (state: LocalImportJob['state'] = 'completed', id = 'first'): LocalImportPage => ({
    job: { id, root: 'C:/fixture/media', state, counts: { discovered: 2, processed: 2, failed: 0, added: 2, updated: 0, unchanged: 0, excluded: 0, scanExcluded: 0, userExcluded: 0, scanErrors: 0, totalBytes: 100, processedBytes: 100, completedFolders: 1, failedFolders: 0 },
        cancelRequested: false, pauseRequested: false, scanComplete: true, canResume: true, currentPath: '', error: null, elapsedMillis: 1000, bytesPerSecond: 100, filesPerSecond: 2, largeFolders: 0, revision: 1 },
    folders: [{ id: 'folder', path: '.', state: 'completed', error: null, fileCount: 2 }], total: 1, hasNextPage: false,
})
const success = (data: LocalImportPage) => ({ ok: true as const, data })
beforeEach(() => { vi.resetAllMocks(); localStorage.clear(); api.get.mockResolvedValue(success(page())); api.files.mockResolvedValue({ ok: true, data: { files: [], total: 0, hasNextPage: false } }) })
afterEach(async () => { await cleanupReact(); vi.unstubAllGlobals(); vi.restoreAllMocks() })
const render = () => mount(h(LocalImportPanel, { disabled: false, onComplete: vi.fn() }))

describe('completed import panel visibility', () => {
    it.each(['completed', 'completed_with_errors'] as const)('closes %s without changing a job and reopens the result', async state => {
        api.get.mockResolvedValue(success(page(state))); await render()
        await clickButton('가져오기 결과 닫기')
        expect(document.body.textContent).not.toContain('전체 2개')
        expect(document.body.textContent).toContain('폴더 가져오기'); expect(document.body.textContent).toContain('최근 결과')
        expect(api.pause).not.toHaveBeenCalled(); expect(api.exclude).not.toHaveBeenCalled()
        await clickButton('최근 결과'); expect(document.body.textContent).toContain('전체 2개')
        expect(readDismissedImport()).toBeNull()
    })
    it('stays collapsed after leaving and returning, but opens a different new job', async () => {
        const close = await render(); await clickButton('가져오기 결과 닫기'); await close()
        await render(); expect(document.body.textContent).not.toContain('전체 2개')
        api.choose.mockResolvedValue(success(page('ready', 'new-job')))
        await clickButton('폴더 가져오기'); expect(document.body.textContent).toContain('등록 시작')
        expect(document.body.textContent).toContain('전체 2개'); expect(document.body.textContent).not.toContain('최근 결과')
    })
    it('keeps a cancelled chooser collapsed and displays chooser errors without hiding them', async () => {
        await render(); await clickButton('가져오기 결과 닫기')
        api.choose.mockResolvedValue(success({ job: null, folders: [], total: 0, hasNextPage: false }))
        await clickButton('폴더 가져오기'); expect(document.body.textContent).toContain('최근 결과')
        api.choose.mockResolvedValue({ ok: false, error: { code: 'LOCAL_IMPORT_FAILED', message: '폴더 접근 실패' } })
        await clickButton('폴더 가져오기'); expect(document.querySelector('[role="alert"]')!.textContent).toBe('폴더 접근 실패')
    })
    it('keeps the closed result compact while the folder chooser is open', async () => {
        await render(); await clickButton('가져오기 결과 닫기')
        const pending = deferred<ReturnType<typeof success>>(); api.choose.mockReturnValue(pending.promise)
        await clickButton('폴더 가져오기'); expect(document.body.textContent).not.toContain('전체 2개')
        expect(document.body.textContent).toContain('최근 결과')
        await act(async () => { pending.resolve(success({ job: null, folders: [], total: 0, hasNextPage: false })) })
        expect(document.body.textContent).not.toContain('전체 2개')
    })
    it.each(['scanning', 'ready', 'running', 'paused', 'failed', 'cancelled', 'interrupted'] as const)('does not offer a close button for %s', async state => {
        writeDismissedImport('first'); api.get.mockResolvedValue(success(page(state))); await render()
        expect(document.querySelector('[aria-label="가져오기 결과 닫기"]')).toBeNull()
        expect(document.body.textContent).not.toContain('최근 결과')
    })
    it('does not dismiss while a request is in flight or when no job exists', async () => {
        let model!: ReturnType<typeof useLocalImportViewModel>
        function Probe() { model = useLocalImportViewModel(vi.fn()); return null }
        api.get.mockResolvedValue(success({ job: null, folders: [], total: 0, hasNextPage: false })); await mount(h(Probe))
        await act(async () => { model.dismiss() }); expect(readDismissedImport()).toBeNull()
        api.get.mockResolvedValue(success(page())); await act(async () => { await model.refresh() })
        const pending = deferred<ReturnType<typeof success>>(); api.choose.mockReturnValue(pending.promise)
        let request!: Promise<void>; await act(async () => { request = model.choose() })
        expect(model.canDismiss).toBe(false); await act(async () => { model.dismiss() }); expect(readDismissedImport()).toBeNull()
        await act(async () => { pending.resolve(success(page())); await request })
        await act(async () => { model.dismiss() }); expect(readDismissedImport()).toBe('first')
    })
})

describe('dismissal persistence and decision conditions', () => {
    it('requires a finished job and an idle request for either successful terminal state', () => {
        expect(canDismissImport(undefined, false)).toBe(false)
        expect(canDismissImport(null, false)).toBe(false)
        expect(canDismissImport(page().job, true)).toBe(false)
        expect(canDismissImport(page().job, false)).toBe(true)
        expect(canDismissImport(page('completed_with_errors').job, false)).toBe(true)
        expect(canDismissImport(page('running').job, false)).toBe(false)
    })
    it('persists only the dismissed job ID and can clear it', () => {
        expect(readDismissedImport()).toBeNull(); writeDismissedImport('job'); expect(readDismissedImport()).toBe('job')
        writeDismissedImport(null); expect(readDismissedImport()).toBeNull()
    })
    it('keeps the close/reopen controls usable when storage is blocked', async () => {
        vi.stubGlobal('localStorage', { getItem() { throw new Error('blocked') }, setItem() { throw new Error('blocked') }, removeItem() { throw new Error('blocked') } })
        expect(readDismissedImport()).toBeNull(); expect(() => writeDismissedImport('job')).not.toThrow(); expect(() => writeDismissedImport(null)).not.toThrow()
        await render(); await clickButton('가져오기 결과 닫기'); expect(document.body.textContent).toContain('최근 결과')
        await clickButton('최근 결과'); expect(document.body.textContent).toContain('전체 2개')
    })
})
