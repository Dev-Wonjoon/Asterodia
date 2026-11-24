// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { WebDownloadPage } from '../../frontend/src/web/WebDownloadPage'
import { canEditMedia } from '../../frontend/src/features/mediaedit/canEditMedia'
import { setLanguage } from '@renderer/shared/i18n'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ status: vi.fn(), list: vi.fn(), start: vi.fn(), retry: vi.fn(), cancel: vi.fn(), cleanup: vi.fn() }))
vi.mock('../../frontend/src/web/download-api', () => ({ webDownloads: api }))
const ok = (data: unknown) => ({ ok: true, data })
const empty = { data: [], activeJobs: [], limit: 50, offset: 0, hasNextPage: false }
const job = (value: Record<string, unknown> = {}) => ({ id: 'job', url: 'https://example.com/media', title: '내 미디어', state: 'running', phase: 'downloading', progress: 31, options: { type: 'video', quality: 'best', playlist: false }, cleanupRequired: false, currentAttemptId: 'attempt', errorCode: null, errorMessage: null, ...value })
const button = (label: string) => [...document.querySelectorAll('button')].find(button => button.textContent === label)!
async function typeURL(value: string) {
    const input = document.querySelector<HTMLInputElement>('input[type=url]')!
    await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value); input.dispatchEvent(new Event('input', { bubbles: true })) })
    return input
}
beforeEach(() => {
    vi.useFakeTimers(); vi.resetAllMocks(); setLanguage('ko')
    api.status.mockResolvedValue({ enabled: true, csrf: 'token' }); api.list.mockResolvedValue(ok(empty))
    for (const method of [api.start, api.retry, api.cancel, api.cleanup]) method.mockResolvedValue(ok({ accepted: true }))
})
afterEach(async () => { await cleanupReact(); vi.useRealTimers() })

it('supports GIF and WebP inputs while excluding other image formats', () => {
    expect(canEditMedia({ kind: 'image', detectedFormat: 'webp' })).toBe(true)
    expect(canEditMedia({ kind: 'image', filepath: 'C:/animated.WEBP' })).toBe(true)
    expect(canEditMedia({ kind: 'image', detectedFormat: 'gif' })).toBe(true)
    expect(canEditMedia({ kind: 'video' })).toBe(true)
    expect(canEditMedia({ kind: 'image', detectedFormat: 'png' })).toBe(false)
})

it('requires the separate PC download permission and notices changes while open', async () => {
    api.status.mockResolvedValue({ enabled: false, csrf: '' })
    await mount(h(WebDownloadPage, { onGallery: vi.fn() }))
    expect(button('다운로드 시작').disabled).toBe(true); expect(api.list).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('모바일 다운로드 허용')
    api.status.mockResolvedValue({ enabled: true, csrf: 'token' })
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    await typeURL('https://example.com/video'); expect(button('다운로드 시작').disabled).toBe(false)
})

it('starts a URL download with selected options, prevents duplicates, and clears only accepted input', async () => {
    const pending = deferred<any>(); api.start.mockReturnValueOnce(pending.promise)
    await mount(h(WebDownloadPage, { onGallery: vi.fn() }))
    const input = await typeURL('https://example.com/video')
    const quality = document.querySelector<HTMLSelectElement>('[aria-label="다운로드 화질"]')!
    await act(async () => { quality.value = '1080'; quality.dispatchEvent(new Event('change', { bubbles: true })) })
    await clickButton('다운로드 시작')
    expect(api.start).toHaveBeenCalledExactlyOnceWith('https://example.com/video', { type: 'video', quality: '1080', playlist: false })
    expect(input.disabled).toBe(true); expect(document.querySelector<HTMLButtonElement>('button[type=submit]')!.disabled).toBe(true)
    await act(async () => { document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
    expect(api.start).toHaveBeenCalledTimes(1)
    await act(async () => { pending.resolve(ok(job())) })
    expect(input.value).toBe(''); expect(document.body.textContent).toContain('이 화면을 닫아도 PC에서 계속 진행')
    api.start.mockResolvedValueOnce({ ok: false, error: { code: 'INVALID_ARGUMENT', message: 'URL 오류', context: {} } })
    await typeURL('https://example.com/other'); await clickButton('다운로드 시작')
    const message = document.querySelector('[role=alert]')!.textContent
    expect(input.value).toBe('https://example.com/other')
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(document.querySelector('[role=alert]')!.textContent).toBe(message)
})

it('shows real transfer progress, cancellation, retries, cleanup, and gallery navigation', async () => {
    const gallery = vi.fn()
    api.list.mockResolvedValue(ok({ ...empty, data: [job({ id: 'failed', title: '실패한 작업', state: 'failed' }), job({ id: 'cleanup', cleanupRequired: true })], activeJobs: [job()] }))
    await mount(h(WebDownloadPage, { onGallery: gallery }))
    expect(document.querySelectorAll('article')).toHaveLength(3)
    expect(document.body.textContent).toContain('31.0%')
    await clickButton('취소'); expect(api.cancel).toHaveBeenCalledExactlyOnceWith('job')
    await clickButton('다시 시도'); expect(api.retry).toHaveBeenCalledExactlyOnceWith('failed')
    await clickButton('임시 파일 정리'); expect(api.cleanup).toHaveBeenCalledExactlyOnceWith('cleanup')
    await clickButton('갤러리에서 보기'); expect(gallery).toHaveBeenCalledOnce()
})

it('keeps active jobs visible across paginated history and ignores outdated reads', async () => {
    api.list.mockResolvedValueOnce(ok({ ...empty, data: [job()], activeJobs: [job()], hasNextPage: true }))
    await mount(h(WebDownloadPage, { onGallery: vi.fn() }))
    expect(document.querySelectorAll('article')).toHaveLength(1)
    api.list.mockResolvedValueOnce(ok({ ...empty, offset: 50, data: [job({ id: 'old', state: 'completed' })], activeJobs: [job()] }))
    await clickButton('다음 파일'); expect(api.list).toHaveBeenLastCalledWith(50)
    expect(document.querySelectorAll('article')).toHaveLength(2)
    const pending = deferred<any>(); api.list.mockReturnValueOnce(pending.promise)
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    await clickButton('이전 파일')
    await act(async () => { pending.resolve(ok({ ...empty, data: [job({ title: '오래된 응답' })] })) })
    expect(document.body.textContent).not.toContain('오래된 응답')
})

it('releases polling on unmount and leaves an accepted download running on the PC', async () => {
    const pending = deferred<any>(); api.start.mockReturnValueOnce(pending.promise)
    const stop = await mount(h(WebDownloadPage, { onGallery: vi.fn() }))
    await typeURL('https://example.com/video'); await clickButton('다운로드 시작'); await stop()
    expect(vi.getTimerCount()).toBe(0)
    await act(async () => { pending.resolve(ok(job())); await vi.advanceTimersByTimeAsync(5000) })
    expect(api.cancel).not.toHaveBeenCalled(); expect(api.status).toHaveBeenCalledTimes(1)
})
