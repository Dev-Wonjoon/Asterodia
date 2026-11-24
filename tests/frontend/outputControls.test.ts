// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mountOutputControls } from '../../internal/webserver/web/output-controls.js'
import { setLanguage } from '@renderer/shared/i18n'
import { deferred } from './reactHarness'

let controls: ReturnType<typeof mountOutputControls> | undefined
beforeEach(() => setLanguage('ko'))
afterEach(() => { controls?.destroy(); controls = undefined; document.body.replaceChildren(); vi.useRealTimers() })
const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve() }
const ok = (extra = {}) => ({ ok: true, data: { directory: 'D:/Working', albums: [{ id: 1, title: '여행' }, { id: 2, title: '움짤' }], tags: ['원본'], hasNextPage: false, offset: 0, ...extra } })
const button = (text: string) => [...document.querySelectorAll('button')].find(node => node.textContent === text)!
function setup(chooseDirectory?: () => Promise<string | null>, outputOptions = vi.fn(async () => ok())) {
    controls = mountOutputControls(document.body, { api: { outputOptions }, chooseDirectory })
    return outputOptions
}
it('saves one physical directory independently of multiple albums and pending tags', async () => {
    setup(async () => 'E:/One-off'); await flush()
    document.querySelector<HTMLInputElement>('input[data-album-id="1"]')!.click()
    document.querySelector<HTMLInputElement>('input[data-album-id="2"]')!.click()
    expect(document.activeElement).toBe(document.querySelector('input[data-album-id="2"]'))
    const tag = document.querySelector<HTMLInputElement>('[aria-label="결과물 태그 입력"]')!; tag.value = '움짤, 여행, 움짤'
    expect(controls!.request()).toEqual({ outputDirectory: 'D:/Working', albumIds: [1, 2], tagNames: ['여행', '움짤'].sort() })
    expect(controls!.request(true)).toEqual({})
    button('이번 출력 폴더 선택').click(); await flush(); expect(controls!.request().outputDirectory).toBe('E:/One-off')
    button('기본 작업 폴더 사용').click(); expect(controls!.request().outputDirectory).toBe('D:/Working')
    expect(controls!.request().albumIds).toEqual([1, 2])
})
it('supports mobile classification without sending or exposing PC paths', async () => {
    setup(undefined, vi.fn(async () => ok({ directory: '' }))); await flush()
    document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click()
    expect(controls!.request()).toEqual({ albumIds: [1] })
    expect(document.querySelector('.sbe-output-path')?.textContent).toBe('PC에서 설정한 작업 폴더')
    expect(button('이번 출력 폴더 선택').parentElement!.hidden).toBe(true)
})
it('keeps entered tags and selected albums when language changes', async () => {
    setup(); await flush()
    const tag = document.querySelector<HTMLInputElement>('[aria-label="결과물 태그 입력"]')!; tag.value = '갤러리'
    document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click()
    setLanguage('en')
    expect(document.querySelector('[aria-label="Enter result tags"]')).toBe(tag)
    expect(tag.value).toBe('갤러리'); expect(controls!.request()).toEqual({ albumIds: [1], tagNames: ['갤러리'] })
    expect(document.body.textContent).toContain('여행')
})
it('does not commit an IME Enter and validates tag limits before submission', async () => {
    setup(); await flush()
    const tag = document.querySelector<HTMLInputElement>('[aria-label="결과물 태그 입력"]')!; tag.value = '한글'
    tag.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true })); expect(tag.value).toBe('한글')
    tag.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })); expect(tag.value).toBe('')
    tag.value = '가'.repeat(101); expect(() => controls!.request()).toThrow('100자')
})
it('offers retry after load failure and ignores late responses after disposal', async () => {
    const outputOptions = vi.fn().mockRejectedValueOnce(Error('offline')).mockResolvedValueOnce(ok())
    setup(undefined, outputOptions); await flush(); expect(controls!.ready()).toBe(false)
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('불러오지 못했습니다')
    button('다시 시도').click(); await flush(); expect(controls!.ready()).toBe(true)
    controls!.destroy(); const pending = deferred<any>(); setup(undefined, vi.fn(() => pending.promise)); controls!.destroy()
    pending.resolve(ok()); await flush(); expect(document.querySelector('.sbe-output')).toBeNull()
})
it('keeps selections across paginated album results and discards stale searches', async () => {
    vi.useFakeTimers(); const old = deferred<any>(), fresh = deferred<any>()
    const read = vi.fn().mockResolvedValueOnce(ok({ hasNextPage: true })).mockResolvedValueOnce(ok({ offset: 2, albums: [{ id: 3, title: '새 앨범' }] })).mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    setup(undefined, read); await flush(); document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click()
    button('앨범 더 불러오기').click(); await flush(); expect(read).toHaveBeenLastCalledWith({ keyword: '', offset: 2 })
    expect(document.querySelectorAll('input[type="checkbox"]')).toHaveLength(3)
    const search = document.querySelector<HTMLInputElement>('input[type="search"]')!
    search.value = 'old'; search.dispatchEvent(new Event('input')); await vi.advanceTimersByTimeAsync(250)
    search.value = 'new'; search.dispatchEvent(new Event('input')); await vi.advanceTimersByTimeAsync(250)
    fresh.resolve(ok({ albums: [{ id: 4, title: '최신' }] })); await flush(); old.resolve(ok({ albums: [{ id: 5, title: '오래됨' }] })); await flush()
    expect(document.querySelector('.sbe-output-albums')?.textContent).toBe('최신'); expect(controls!.request().albumIds).toEqual([1])
})
