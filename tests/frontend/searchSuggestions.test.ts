// @vitest-environment happy-dom
import { act, createElement as h, useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'
import { SearchSuggestions } from '@renderer/features/search/SearchSuggestions'
import type { MediaFilter, MediaResult, SearchSuggestion, SearchSuggestionPage } from '@contracts/media-v2'

const mocks = vi.hoisted(() => ({ suggest: vi.fn(), choose: vi.fn(), submit: vi.fn(), composition: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ media: { searchSuggest: mocks.suggest } }) }))
const page = (values: string[], next = false, revision = 1, offset = 0): MediaResult<SearchSuggestionPage> => ({ ok: true, data: { data: values.map(value => ({ kind: 'title', value })), offset, hasNextPage: next, revision, indexed: true } })
function Harness() {
    const [value, setValue] = useState('')
    const [filter, setFilter] = useState<MediaFilter>({})
    return h('div', null, h(SearchSuggestions, { value, filter, onChange: setValue, onSubmit: mocks.submit, onCompositionChange: mocks.composition, onSelect: (item: SearchSuggestion) => { mocks.choose(item); setValue(item.value) } }), h('button', { onClick: () => setFilter({ tags: ['선택 태그'] }) }, '필터 적용'))
}
const input = () => document.querySelector<HTMLInputElement>('input')!
async function type(value: string) { await act(async () => { input().focus(); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input(), value); input().dispatchEvent(new Event('input', { bubbles: true })) }) }
async function tick(ms = 50) { await act(async () => { await vi.advanceTimersByTimeAsync(ms) }) }
async function key(key: string, composing = false) { await act(async () => input().dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, isComposing: composing }))) }
beforeEach(() => { vi.useFakeTimers(); mocks.suggest.mockReset().mockResolvedValue(page(['여행', '은행'])); mocks.choose.mockReset(); mocks.submit.mockReset(); mocks.composition.mockReset() })
afterEach(async () => { await cleanupReact(); vi.clearAllTimers(); vi.useRealTimers() })

it('starts one-character contains suggestions after 50ms and supports keyboard selection', async () => {
    await mount(h(Harness)); await type('행'); await tick(49); expect(mocks.suggest).not.toHaveBeenCalled()
    await tick(1); expect(mocks.suggest).toHaveBeenCalledExactlyOnceWith({ text: '행', filter: {}, limit: 10, offset: 0 })
    expect([...document.querySelectorAll('[role=option]')].map(item => item.textContent)).toEqual(['제목여행', '제목은행'])
    await key('ArrowDown'); await key('ArrowDown'); await key('Enter')
    expect(mocks.choose).toHaveBeenCalledExactlyOnceWith({ kind: 'title', value: '은행' })
    expect(input().getAttribute('aria-expanded')).toBe('false')
})
it('ignores stale responses and closes pending reads on escape', async () => {
    const old = deferred<MediaResult<SearchSuggestionPage>>()
    mocks.suggest.mockReturnValueOnce(old.promise)
    await mount(h(Harness)); await type('여'); await tick(); await type('은행'); await tick()
    await act(async () => old.resolve(page(['오래된 후보'])))
    expect(document.body.textContent).not.toContain('오래된 후보')
    await key('Escape'); expect(input().getAttribute('aria-expanded')).toBe('false')
    await tick(1000); expect(mocks.suggest).toHaveBeenCalledTimes(2)
})
it('suppresses composition queries and Enter, then queries immediately on composition end', async () => {
    await mount(h(Harness)); await act(async () => { input().focus(); input().dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true })) })
    await type('여행'); await tick(500); await key('Enter', true)
    expect(mocks.suggest).not.toHaveBeenCalled(); expect(mocks.submit).not.toHaveBeenCalled()
    await act(async () => input().dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '여행' })))
    await tick(0); expect(mocks.suggest).toHaveBeenCalledOnce(); expect(mocks.composition.mock.calls.map(call => call[0])).toEqual([true, false])
})
it('loads every page and restarts when the catalog changes between pages', async () => {
    mocks.suggest.mockResolvedValueOnce(page(['여행'], true)).mockResolvedValueOnce(page(['혼합하면 안 되는 페이지'], false, 2, 1)).mockResolvedValueOnce(page(['새로운 여행'], true, 2))
    await mount(h(Harness)); await type('행'); await tick(); await clickButton('추천 더 보기')
    expect(mocks.suggest.mock.calls.map(call => call[0].offset)).toEqual([0, 1, 0])
    expect(document.body.textContent).toContain('새로운 여행'); expect(document.body.textContent).not.toContain('혼합하면 안 되는 페이지')
})
it('shows cached candidates immediately while revalidating and refreshes on filter changes', async () => {
    await mount(h(Harness)); await type('행'); await tick(); await type('다른'); await tick(); await type('행')
    expect(document.body.textContent).toContain('은행'); expect(mocks.suggest).toHaveBeenCalledTimes(2)
    await tick(); expect(mocks.suggest).toHaveBeenCalledTimes(3)
    await clickButton('필터 적용'); await type('행'); await tick()
    expect(mocks.suggest).toHaveBeenLastCalledWith(expect.objectContaining({ filter: { tags: ['선택 태그'] } }))
})
it('retries a failed page from the beginning and submits typed text without an active candidate', async () => {
    mocks.suggest.mockRejectedValueOnce(new Error('조회 실패'))
    await mount(h(Harness)); await type('행'); await tick(); expect(document.body.textContent).toContain('조회 실패')
    await clickButton('다시 시도'); expect(document.body.textContent).toContain('은행')
    await key('Enter'); expect(mocks.submit).toHaveBeenCalledExactlyOnceWith('행')
})
