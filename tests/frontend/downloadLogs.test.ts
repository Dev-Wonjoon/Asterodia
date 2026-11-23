// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { DownloadJobDto, DownloadLogPage } from '@contracts/download-api'
import { DownloadLogPane } from '@renderer/features/download/components/DownloadLogPane'
import { DownloadPage } from '@renderer/features/download/DownloadPage'
import { DownloadDetailPane } from '@renderer/features/download/components/DownloadDetailPane'
import { ToastContext } from '@renderer/shared/context/toastContext'
import { formatDownloadLog, pollDownloadLogs } from '@renderer/features/download/downloadLogs'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ logs: vi.fn(), getEngineStatus: vi.fn(), installEngine: vi.fn(), onEngineInstallProgress: vi.fn(), getDownloadPreview: vi.fn(), onMediaThumbnailUpdated: vi.fn() }))
const view = vi.hoisted(() => ({ value: {} as Record<string, unknown> }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ ...api, downloads: { logs: api.logs } }) }))
vi.mock('@renderer/features/download/useDownloadViewModel', () => ({ useDownloadViewModel: () => view.value }))
const page = (patch: Partial<DownloadLogPage> = {}): DownloadLogPage => ({
    entries: [{ sequence: 10, attemptId: 'a', recordedAt: 1000, engine: 'yt-dlp', stream: 'stdout', text: '[download] 42.0% of 9.19MiB' }, { sequence: 11, attemptId: 'a', recordedAt: 2000, engine: 'yt-dlp', stream: 'stderr', text: 'ERROR: native error' }],
    attempts: [{ id: 'a', number: 2, outcome: 'failed' }], currentAttemptId: 'a', state: 'failed', hasOlder: true, truncated: false, warning: null, ...patch,
})
beforeEach(() => {
    vi.resetAllMocks()
    api.logs.mockResolvedValue({ ok: true, data: page() }); api.getEngineStatus.mockResolvedValue({ 'yt-dlp': { installed: true, version: '1' } })
    api.onEngineInstallProgress.mockReturnValue(vi.fn()); api.getDownloadPreview.mockResolvedValue(null)
    api.onMediaThumbnailUpdated.mockReturnValue(vi.fn())
    view.value = { detail: { jobId: null, data: null, loading: false, error: null }, queue: [], ready: true, loading: false, error: null, halted: null, requestedIds: [], cancellingIds: [], deletingIds: [], batchRetry: null, retryAll: vi.fn(), deleteJob: vi.fn(), offset: 0, hasNextPage: false, closeDetail: vi.fn(), refresh: vi.fn(), openDetail: vi.fn() }
})
afterEach(async () => { await cleanupReact(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })
const renderPage = () => mount(h(ToastContext.Provider, { value: { showToast: vi.fn() } }, h(DownloadPage)))

describe('actual engine log viewer', () => {
    it('loads on demand, shows output literally, pages older logs and copies only visible output', async () => {
        const copy = vi.fn().mockResolvedValue(undefined); vi.spyOn(navigator.clipboard, 'writeText').mockImplementation(copy)
        await mount(h(DownloadLogPane, { jobId: 'job' })); expect(api.logs).not.toHaveBeenCalled()
        await clickButton('로그 상세보기'); expect(document.body.textContent).toContain('[download] 42.0%'); expect(document.body.textContent).toContain('ERROR: native error')
        await clickButton('표시된 로그 복사'); expect(copy).toHaveBeenCalledWith(expect.stringContaining('ERROR: native error'))
        expect(document.body.textContent).toContain('복사했습니다')
        await clickButton('이전 로그'); expect(api.logs).toHaveBeenLastCalledWith({ jobId: 'job', attemptId: '', before: 10, limit: 100 })
        await clickButton('최신 로그'); expect(api.logs).toHaveBeenLastCalledWith({ jobId: 'job', attemptId: '', before: 0, limit: 100 })
        const selector = document.querySelector('select')!; await act(async () => { selector.value = 'a'; selector.dispatchEvent(new Event('change', { bubbles: true })) })
        expect(api.logs).toHaveBeenLastCalledWith({ jobId: 'job', attemptId: 'a', before: 0, limit: 100 })
        const check = document.querySelector('input[type="checkbox"]') as HTMLInputElement; await act(async () => { check.click() }); expect(check.checked).toBe(false)
        await clickButton('로그 닫기'); expect(document.querySelector('[aria-label="엔진 출력 로그"]')).toBeNull()
    })
    it('polls real output while running, stops after completion and cancels polling on close', async () => {
        vi.useFakeTimers(); api.logs.mockResolvedValueOnce({ ok: true, data: page({ state: 'running' }) }).mockResolvedValue({ ok: true, data: page({ state: 'completed' }) })
        await mount(h(DownloadLogPane, { jobId: 'job' })); await clickButton('로그 상세보기')
        await act(async () => { await vi.advanceTimersByTimeAsync(1000) }); expect(api.logs).toHaveBeenCalledTimes(2)
        await act(async () => { await vi.advanceTimersByTimeAsync(5000) }); expect(api.logs).toHaveBeenCalledTimes(2)
        api.logs.mockResolvedValue({ ok: true, data: page({ state: 'running' }) }); await clickButton('최신 로그'); const calls = api.logs.mock.calls.length
        await clickButton('로그 닫기'); await act(async () => { await vi.advanceTimersByTimeAsync(2000) }); expect(api.logs).toHaveBeenCalledTimes(calls)
    })
    it('displays truncation/storage warnings, copy failure and read failures without losing the controls', async () => {
        api.logs.mockResolvedValue({ ok: true, data: page({ truncated: true, warning: '저장 공간 부족' }) })
        vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('blocked'))
        await mount(h(DownloadLogPane, { jobId: 'job' })); await clickButton('로그 상세보기')
        expect(document.body.textContent).toContain('오래된 로그'); expect(document.body.textContent).toContain('저장 공간 부족')
        await clickButton('표시된 로그 복사'); expect(document.body.textContent).toContain('직접 복사')
        api.logs.mockResolvedValue({ ok: false, error: { code: 'NOT_FOUND', message: '기록 없음' } }); await clickButton('최신 로그'); expect(document.body.textContent).toContain('기록 없음')
        api.logs.mockRejectedValue(new Error('offline')); await clickButton('새로고침'); expect(document.body.textContent).toContain('로그를 읽지 못했습니다')
    })
    it('ignores a late response after closing the log view', async () => {
        const pending = deferred<unknown>(); api.logs.mockReturnValue(pending.promise)
        const unmount = await mount(h(DownloadLogPane, { jobId: 'job' })); await clickButton('로그 상세보기'); await unmount()
        await act(async () => { pending.resolve({ ok: true, data: page() }) })
    })
    it('formats legacy records and each live polling condition', () => {
        expect(formatDownloadLog({ ...page().entries[0], recordedAt: null })).toContain('[이전 기록] [yt-dlp/stdout]')
        expect(pollDownloadLogs(page({ state: 'running' }), 0, '')).toBe(true)
        expect(pollDownloadLogs(page({ state: 'pending' }), 0, 'a')).toBe(true)
        expect(pollDownloadLogs(page({ state: 'running' }), 10, '')).toBe(false)
        expect(pollDownloadLogs(page(), 0, '')).toBe(false)
        expect(pollDownloadLogs(page({ state: 'running' }), 0, 'old')).toBe(false)
    })
})

describe('download page engine requirements and original errors', () => {
    it('offers X only for clean failed/cancelled requests and wires the bulk action', async () => {
        const base: DownloadJobDto = { id: 'base', url: 'https://example.invalid/file', title: null, state: 'failed', phase: 'finished', progress: 0, options: {}, cleanupRequired: false, currentAttemptId: 'a', errorCode: null }
        view.value.queue = [base, { ...base, id: 'cancelled', state: 'cancelled' }, { ...base, id: 'completed', state: 'completed' }, { ...base, id: 'cleanup', cleanupRequired: true }, { ...base, id: 'running', state: 'running' }]
        await renderPage()
        const buttons = [...document.querySelectorAll<HTMLButtonElement>('[aria-label="요청 기록 삭제"]')]
        expect(buttons).toHaveLength(2)
        expect(buttons[0].title).toBe('요청 기록 삭제')
        await clickButton('요청 기록 삭제'); expect(view.value.deleteJob).toHaveBeenCalledWith('base')
        await clickButton('실패·취소 일괄 재시도'); expect(view.value.retryAll).toHaveBeenCalledTimes(1)
    })
    it('locks deleting/reserved cards and displays bulk progress and excluded counts', async () => {
        const job: DownloadJobDto = { id: 'reserved', url: 'https://example.invalid/file', title: null, state: 'failed', phase: 'finished', progress: 0, options: {}, cleanupRequired: false, currentAttemptId: 'a', errorCode: null }
        view.value.queue = [job]; view.value.deletingIds = ['reserved']
        await renderPage(); expect(document.querySelector<HTMLButtonElement>('[aria-label="요청 기록 삭제"]')!.disabled).toBe(true)
        expect([...document.querySelectorAll('button')].find(b => b.textContent === '재시도')!.disabled).toBe(true)
        await cleanupReact(); view.value.deletingIds = []; view.value.batchRetry = { running: true, total: 3, processed: 1, failed: 0, skipped: 2 }
        await renderPage(); expect(document.body.textContent).toContain('처리 1/3건'); expect(document.body.textContent).toContain('제외 2건')
        expect(document.querySelector<HTMLButtonElement>('[aria-label="요청 기록 삭제"]')!.disabled).toBe(true)
        expect([...document.querySelectorAll('button')].find(b => b.textContent === '일괄 재시도 중…')!.disabled).toBe(true)
    })
    it('hides the engine section when all engines work', async () => {
        await renderPage(); expect(document.querySelector('[aria-label="필요한 엔진 설치"]')).toBeNull()
    })
    it('shows only missing engines and keeps the batch result visible after installation', async () => {
        api.getEngineStatus.mockResolvedValue({ ready: { installed: true, version: 'already ready' }, ffprobe: { installed: false, version: null, error: '미설치' } })
        const pending = deferred<boolean>(); api.installEngine.mockReturnValue(pending.promise)
        await renderPage(); expect(document.body.textContent).not.toContain('already ready'); expect(document.body.textContent).toContain('ffprobe')
        await clickButton('설치'); expect(document.body.textContent).toContain('설치 중')
        api.getEngineStatus.mockResolvedValue({ ffprobe: { installed: true, version: 'ready' } }); await act(async () => { pending.resolve(true) })
        expect(document.querySelector('[aria-label="필요한 엔진 설치"]')).not.toBeNull()
        expect(document.body.textContent).toContain('처리 1/1개 · 완료 1개 · 실패 0개')
    })
    it('does not hide a status-query failure and supports retry', async () => {
        api.getEngineStatus.mockRejectedValue(new Error('offline')); await renderPage(); expect(document.querySelector('[role="alert"]')).not.toBeNull()
        api.getEngineStatus.mockResolvedValue({ 'yt-dlp': { installed: true, version: '1' } }); await clickButton('다시 확인'); expect(document.querySelector('[aria-label="필요한 엔진 설치"]')).toBeNull()
    })
    it('displays the actual error and an indeterminate download without fabricated percent', async () => {
        const job: DownloadJobDto = { id: 'job', url: 'https://www.youtube.com/shorts/xhzS_pawX9Y', title: 'Shorts', state: 'failed', phase: 'finished', progress: 0, options: {}, cleanupRequired: false, currentAttemptId: 'a', errorCode: 'DOWNLOAD_ENGINE_FAILED', errorMessage: 'ERROR: ffmpeg executable is missing' }
        view.value.detail = { jobId: 'job', data: job, loading: false, error: null }
        await mount(h(DownloadDetailPane)); expect(document.body.textContent).toContain('ERROR: ffmpeg executable is missing'); expect(document.body.textContent).toContain('로그 상세보기')
        await cleanupReact(); view.value.detail = { jobId: null, data: null, loading: false, error: null }; view.value.queue = [{ ...job, state: 'running', phase: 'downloading' }]
        await renderPage(); const bar = document.querySelector('[aria-label="다운로드 진행률"]')!
        expect(bar.hasAttribute('aria-valuenow')).toBe(false)
    })
})
