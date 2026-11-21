// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { mountSearchSuggestions } from '../../internal/webserver/web/search.js'

afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); document.body.replaceChildren() })
it('uses 50ms mobile queries, safe text rendering, pagination and touch selection', async () => {
    vi.useFakeTimers()
    document.body.innerHTML = '<form><input><button type="submit">검색</button></form>'
    const input = document.querySelector('input')!, form = document.querySelector('form')!
    const apply = vi.fn(), typed = vi.fn()
    const request = vi.fn().mockResolvedValueOnce({ data: [{ kind: 'tag', value: '여행<script>alert(1)</script>' }], hasNextPage: true, revision: 1 }).mockResolvedValueOnce({ data: [{ kind: 'author', value: '은행 작가' }], hasNextPage: false, revision: 1 })
    mountSearchSuggestions({ input, form, request, parameters: () => new URLSearchParams('tag=선택'), onApply: apply, onInput: typed })
    input.value = '행'; input.dispatchEvent(new Event('input', { bubbles: true }))
    await vi.advanceTimersByTimeAsync(49); expect(request).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1); expect(request.mock.calls[0][0]).toContain('q=%ED%96%89')
    expect(document.querySelector('script')).toBeNull(); expect(document.body.textContent).toContain('<script>')
    ;([...document.querySelectorAll('button')].find(node => node.textContent === '추천 더 보기') as HTMLButtonElement).click()
    await vi.advanceTimersByTimeAsync(0)
    expect(request.mock.calls[1][0]).toContain('offset=1')
    ;(document.querySelectorAll('[role=option]')[1] as HTMLButtonElement).click()
    expect(apply).toHaveBeenCalledExactlyOnceWith({ kind: 'author', value: '은행 작가' })
    expect(input.getAttribute('aria-expanded')).toBe('false')
})
it('suppresses IME work and queries immediately at composition end, supports Escape and submit', async () => {
    vi.useFakeTimers()
    document.body.innerHTML = '<form><input></form>'
    const input = document.querySelector('input')!, form = document.querySelector('form')!
    const request = vi.fn().mockResolvedValue({ data: [], hasNextPage: false, revision: 1 }), apply = vi.fn()
    mountSearchSuggestions({ input, form, request, parameters: () => new URLSearchParams(), onApply: apply, onInput: vi.fn() })
    input.dispatchEvent(new CompositionEvent('compositionstart')); input.value = '여행'; input.dispatchEvent(new Event('input'))
    form.dispatchEvent(new Event('submit', { cancelable: true })); await vi.advanceTimersByTimeAsync(100)
    expect(request).not.toHaveBeenCalled(); expect(apply).not.toHaveBeenCalled()
    input.dispatchEvent(new CompositionEvent('compositionend')); await vi.advanceTimersByTimeAsync(0)
    expect(request).toHaveBeenCalledOnce()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })); expect(input.getAttribute('aria-expanded')).toBe('false')
    form.dispatchEvent(new Event('submit', { cancelable: true })); expect(apply).toHaveBeenCalledExactlyOnceWith({ kind: 'title', value: '여행' })
})
