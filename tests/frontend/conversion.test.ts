// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { BatchConvertPanel } from '@renderer/features/mediaedit/BatchConvertPanel'
import { latestBatchJobs } from '@contracts/conversion'
import type { EditJob } from '@contracts/media-edit-api'
import { setLanguage } from '@renderer/shared/i18n'
import { cleanupReact, clickButton, mount } from './reactHarness'

const api = vi.hoisted(() => ({ batch: vi.fn(), formats: vi.fn(), list: vi.fn(), retryBatch: vi.fn(), cancelBatch: vi.fn(), cancel: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ mediaEdit: api }) }))
const selection = [{ mediaId: 3, revision: 7, fileIds: [11, 12] }]
beforeEach(() => {
    vi.useFakeTimers(); vi.resetAllMocks(); setLanguage('ko')
    api.formats.mockResolvedValue({ ok: true, data: [{ type: 'image', extension: 'webp', videoCodecs: ['webp'], audioCodecs: [] }] })
    api.list.mockResolvedValue({ ok: true, data: { sessions: [], jobs: [] } })
    api.batch.mockResolvedValue({ ok: true, data: { jobs: [], failures: [] } })
    api.retryBatch.mockResolvedValue({ ok: true, data: {} })
    api.cancelBatch.mockResolvedValue({ ok: true, data: null })
})
afterEach(async () => { await cleanupReact(); vi.useRealTimers() })

async function input(label: string, value: string) {
    const element = [...document.querySelectorAll('label')].find(item => item.textContent?.startsWith(label))!.querySelector('input')!
    await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value)
        element.dispatchEvent(new Event('input', { bubbles: true }))
    })
}

it('queues all selected files with a maximum 30 second WebP duration and a stable retry key', async () => {
    await mount(h(BatchConvertPanel, { selection, initialMode: 'webp', onClose: vi.fn() }))
    await clickButton('일괄 작업 등록')
    expect(api.batch).toHaveBeenCalledWith(expect.objectContaining({ selection, mode: 'webp', duration: 30, start: 0, width: 640, fps: 15, loop: 0 }))
    const id = api.batch.mock.calls[0][0].requestId
    await clickButton('일괄 작업 등록')
    expect(api.batch.mock.calls[1][0].requestId).toBe(id)
    await input('출력 길이', '30.1')
    expect([...document.querySelectorAll('button')].find(item => item.textContent === '일괄 작업 등록')!.disabled).toBe(true)
    await input('출력 길이', '12')
    await clickButton('일괄 작업 등록')
    expect(api.batch.mock.calls[2][0]).toEqual(expect.objectContaining({ duration: 12 }))
    expect(api.batch.mock.calls[2][0].requestId).not.toBe(id)
})

function job(id: string, fileId: number, state: string, createdAt: number): EditJob {
    return { id, state, createdAt, sessionId: 'session', progress: 0, error: '', cancelling: false, preview: false, resultFileId: state === 'completed' ? 99 : null,
        resources: { known: false, currentBytes: 0, observedPeakBytes: 0, sampledAt: 0, outputBytes: 0, outputFrames: 0 },
        options: { requestId: id, sessionId: 'session', kind: 'webp', crop: { x: 0, y: 0, width: 0, height: 0 }, deleted: [], mute: false,
            batch: { id: 'batch', fileId, mediaId: 3, title: `file-${fileId}`, mode: 'webp', requestHash: '', format: { type: 'video' }, start: 0, duration: 30, width: 640, fps: 15, loop: 0 } } }
}

it('shows only the newest attempt and retries failures without resubmitting completed siblings', async () => {
    const completed = job('done', 11, 'completed', 1), failed = job('failed', 12, 'failed', 2), old = job('old', 12, 'cancelled', 0)
    expect(latestBatchJobs([old, completed, failed]).map(item => item.id)).toEqual(['failed', 'done'])
    api.list.mockResolvedValue({ ok: true, data: { sessions: [], jobs: [failed, completed, old] } })
    await mount(h(BatchConvertPanel, { selection: [], initialMode: 'webp', onClose: vi.fn() }))
    await clickButton('실패·중단 항목 재시도')
    expect(api.retryBatch).toHaveBeenCalledExactlyOnceWith({ id: 'failed' })
    expect(api.batch).not.toHaveBeenCalled()
})

it('cancels a batch in one request and closing the panel leaves queued work running', async () => {
    api.list.mockResolvedValue({ ok: true, data: { sessions: [], jobs: [job('queued', 11, 'pending', 1), job('running', 12, 'running', 2)] } })
    const close = vi.fn()
    await mount(h(BatchConvertPanel, { selection: [], initialMode: 'webp', onClose: close }))
    await clickButton('배치 취소')
    expect(api.cancelBatch).toHaveBeenCalledExactlyOnceWith({ id: 'batch' })
    await clickButton('닫기')
    expect(close).toHaveBeenCalledOnce()
    expect(api.cancel).not.toHaveBeenCalled()
})
