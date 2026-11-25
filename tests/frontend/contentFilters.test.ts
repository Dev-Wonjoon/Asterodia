// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ContentFiltersSettings } from '@renderer/features/settings/ContentFiltersSettings'
import { ContentFilterNotice } from '@renderer/shared/components/ui/ContentFilterNotice'
import { contentFiltersChanged, setShowHidden, useContentVisibility } from '@renderer/shared/hooks/useContentVisibility'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'
import { filterParameters, getApi as getWebApi } from '../../frontend/src/web/api'

const api = vi.hoisted(() => ({ getContentFilters: vi.fn(), setContentFilters: vi.fn(), media: { suggest: vi.fn() } }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => api }))
beforeEach(() => {
    vi.resetAllMocks(); setShowHidden(false)
    api.getContentFilters.mockResolvedValue({ hiddenPlatforms: [], hiddenTags: [] })
    api.setContentFilters.mockImplementation(async value => value)
    api.media.suggest.mockImplementation(async value => ({ ok: true, data: value.kind === 'platform' ? ['YouTube'] : ['spoiler'] }))
})
afterEach(async () => { await cleanupReact(); setShowHidden(false) })
async function input(label: string, value: string) {
    const node = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
    await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(node, value); node.dispatchEvent(new Event('input', { bubbles: true })) })
    return node
}
function Notice() { return h(ContentFilterNotice, { visibility: useContentVisibility() }) }

it('adds exact site and tag suggestions and removes a saved hidden name', async () => {
    await mount(h(ContentFiltersSettings))
    await input('숨길 사이트', 'You'); await clickButton('YouTube')
    expect(api.setContentFilters).toHaveBeenLastCalledWith({ hiddenPlatforms: ['YouTube'], hiddenTags: [] })
    await input('숨길 태그', 'sp'); await clickButton('spoiler')
    expect(api.setContentFilters).toHaveBeenLastCalledWith({ hiddenPlatforms: ['YouTube'], hiddenTags: ['spoiler'] })
    await clickButton('YouTube ×')
    expect(api.setContentFilters).toHaveBeenLastCalledWith({ hiddenPlatforms: [], hiddenTags: ['spoiler'] })
    expect(document.querySelector('[aria-label="spoiler 숨김 해제"]')).not.toBeNull()
})
it('keeps saved names on failure and prevents duplicate saves', async () => {
    api.getContentFilters.mockResolvedValue({ hiddenPlatforms: ['site'], hiddenTags: [] })
    const pending = deferred<any>(); api.setContentFilters.mockReturnValueOnce(pending.promise)
    await mount(h(ContentFiltersSettings)); await clickButton('site ×')
    expect(api.setContentFilters).toHaveBeenCalledOnce()
    expect(document.querySelector<HTMLButtonElement>('[aria-label="site 숨김 해제"]')!.disabled).toBe(true)
    await act(async () => pending.reject(Error('disk full')))
    expect(document.querySelector('[aria-label="site 숨김 해제"]')).not.toBeNull()
    expect(document.querySelector('[role=alert]')?.textContent).toContain('다시 시도')
})
it('disables changes until the initial settings load and retries a failed load', async () => {
    api.getContentFilters.mockRejectedValueOnce(Error()).mockResolvedValueOnce({ hiddenPlatforms: ['site'], hiddenTags: [] })
    await mount(h(ContentFiltersSettings))
    expect(document.querySelector<HTMLInputElement>('[aria-label="숨길 사이트"]')!.disabled).toBe(true)
    await clickButton('다시 시도')
    expect(document.querySelector('[aria-label="site 숨김 해제"]')).not.toBeNull()
    expect(api.setContentFilters).not.toHaveBeenCalled()
})
it('shares the temporary override across notices and reacts to updated lists', async () => {
    api.getContentFilters.mockResolvedValue({ hiddenPlatforms: ['site'], hiddenTags: ['tag'] })
    await mount(h('div', null, h(Notice), h(Notice)))
    expect(document.body.textContent).toContain('사이트 1개 · 태그 1개')
    await act(async () => document.querySelector<HTMLInputElement>('input[type=checkbox]')!.click())
    expect([...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]')].every(input => input.checked)).toBe(true)
    expect(document.body.textContent).toContain('숨긴 항목도 표시 중')
    await act(async () => contentFiltersChanged({ hiddenPlatforms: ['one', 'two'], hiddenTags: [] }))
    expect(document.body.textContent).toContain('사이트 2개 · 태그 0개')
    expect(api.setContentFilters).not.toHaveBeenCalled()
})
it('ignores an outdated read after receiving newly saved settings', async () => {
    const pending = deferred<any>(); api.getContentFilters.mockReturnValueOnce(pending.promise)
    await mount(h(Notice))
    await act(async () => contentFiltersChanged({ hiddenPlatforms: ['new'], hiddenTags: [] }))
    await act(async () => pending.resolve({ hiddenPlatforms: [], hiddenTags: [] }))
    expect(document.body.textContent).toContain('사이트 1개')
})
it('transports the explicit web override without modifying include filters', () => {
    const query = filterParameters({ showHidden: true, tags: ['tag'], platforms: ['site'] })
    expect(query.get('showHidden')).toBe('1'); expect(query.getAll('tag')).toEqual(['tag']); expect(query.getAll('platform')).toEqual(['site'])
    expect(filterParameters({}).has('showHidden')).toBe(false)
})
it('refreshes an open management panel when another client changes the shared list', async () => {
    await mount(h(ContentFiltersSettings))
    api.getContentFilters.mockResolvedValue({ hiddenPlatforms: ['remote-site'], hiddenTags: [] })
    await act(async () => { window.dispatchEvent(new Event('focus')) })
    expect(document.querySelector('[aria-label="remote-site 숨김 해제"]')).not.toBeNull()
    expect(api.setContentFilters).not.toHaveBeenCalled()
})
it('keeps web access tokens out of preference objects and reports save failures', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce({ ok: true, json: async () => ({ hiddenPlatforms: ['site'], hiddenTags: [], csrf: 'read-token' }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ csrf: 'new-token' }) })
        .mockResolvedValueOnce({ ok: false, status: 403 })
    vi.stubGlobal('fetch', fetcher)
    try {
        const value = await getWebApi().getContentFilters()
        expect(value).toEqual({ hiddenPlatforms: ['site'], hiddenTags: [] })
        await expect(getWebApi().setContentFilters(value)).rejects.toThrow('숨김 설정')
        expect(fetcher.mock.calls[2][1].headers['X-StorageBucket-CSRF']).toBe('new-token')
        expect(JSON.parse(fetcher.mock.calls[2][1].body)).toEqual(value)
    } finally { vi.unstubAllGlobals() }
})
