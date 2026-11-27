// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { setLanguage } from '@renderer/shared/i18n'
const fetch = vi.fn()
beforeEach(() => { vi.resetModules(); vi.resetAllMocks(); vi.stubGlobal('fetch', fetch); setLanguage('ko') })
afterEach(() => { vi.unstubAllGlobals() })
const response = (data: unknown, status = 200, type = 'application/json') => ({ status, headers: new Headers({ 'Content-Type': type }), json: async () => data })

it('uses only same-origin JSON requests and the current download permission token', async () => {
    const { webDownloads } = await import('../../frontend/src/web/download-api')
    fetch.mockResolvedValueOnce(response({ enabled: true, csrf: 'download-token' })).mockResolvedValueOnce(response({ ok: true, data: { id: 'job' } }))
    await webDownloads.status(); await webDownloads.start('https://example.com/video', { type: 'video', quality: 'best' })
    expect(fetch).toHaveBeenLastCalledWith('/api/download/enqueue', expect.objectContaining({ method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-Asterodia-CSRF': 'download-token' }, body: JSON.stringify({ url: 'https://example.com/video', options: { type: 'video', quality: 'best' } }) }))
    fetch.mockResolvedValueOnce(response({ enabled: false, csrf: '' })).mockResolvedValueOnce(response({ ok: false }, 403))
    await webDownloads.status(); await webDownloads.cancel('job')
    expect(fetch.mock.calls.at(-1)?.[1].headers['X-Asterodia-CSRF']).toBe('')
})
it('translates permission and throttling errors while preserving structured download failures', async () => {
    const { webDownloads } = await import('../../frontend/src/web/download-api')
    fetch.mockResolvedValueOnce(response(null, 403, 'text/plain'))
    await expect(webDownloads.list()).rejects.toThrow('모바일 다운로드 허용')
    fetch.mockResolvedValueOnce(response(null, 429, 'text/plain'))
    await expect(webDownloads.retry('job')).rejects.toThrow('잠시 후')
    const failure = { ok: false, error: { code: 'REDOWNLOAD_CONFIRMATION_REQUIRED', message: 'PC에서 확인', context: { mediaId: 1 } } }
    fetch.mockResolvedValueOnce(response(failure, 400))
    expect(await webDownloads.start('https://example.com/video', {})).toEqual(failure)
})
