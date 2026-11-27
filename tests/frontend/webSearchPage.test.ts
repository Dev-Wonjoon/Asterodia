// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanupReact, clickButton, mount, deferred } from './reactHarness'
import { SearchPage } from '@renderer/features/search/SearchPage'
import { WebGalleryPage } from '@renderer/web/WebGalleryPage'
import { toMediaUrl } from '@renderer/web/media'

vi.mock('@renderer/shared/api', async () => import('@renderer/web/api'))
vi.mock('@renderer/shared/utils/media', async () => import('@renderer/web/media'))
vi.mock('@renderer/features/gallery/MediaViewer', () => ({ MediaViewer: ({ mediaId, fileId, onClose }: any) => h('div', { role: 'dialog', 'aria-label': `감상 ${mediaId} ${fileId ?? ''}` }, h('button', { onClick: onClose }, '감상 닫기')) }))
const editor = vi.hoisted(() => ({ mount: vi.fn() }))
vi.mock('../../internal/webserver/web/editor.js', () => ({ mountMediaEditor: editor.mount }))
let observers: { callback: (entries: { isIntersecting: boolean }[]) => void; active: boolean }[]
beforeEach(() => {
    vi.useFakeTimers(); localStorage.clear(); observers = []
    vi.stubGlobal('IntersectionObserver', class {
        record: typeof observers[number]
        constructor(callback: typeof observers[number]['callback']) { this.record = { callback, active: false }; observers.push(this.record) }
        observe() { this.record.active = true }
        disconnect() { this.record.active = false }
    })
    editor.mount.mockReset().mockImplementation((host: HTMLElement, options: any) => {
        const node = document.createElement('button'); node.textContent = '편집기 닫기'; node.onclick = options.onClose; host.append(node)
        return { destroy: () => node.remove() }
    })
})
afterEach(async () => { await cleanupReact(); vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })
const flush = async () => { for (let i = 0; i < 25; i++) await Promise.resolve() }
async function setup(enabled = false) {
    let hidden = { hiddenPlatforms: [] as string[], hiddenTags: [] as string[] }
    const fetcher = vi.fn(async (path: string, options?: RequestInit): Promise<any> => {
        const url = new URL(path, 'http://localhost'), q = url.searchParams
        const json = (data: any) => ({ ok: true, json: async () => data })
        if (url.pathname === '/api/edit/status') return json({ enabled, csrf: 'test-csrf' })
        if (url.pathname === '/api/content-filters') {
            if (options?.method === 'POST') hidden = JSON.parse(String(options.body))
            return json({ ...hidden, csrf: 'content-csrf' })
        }
        if (url.pathname === '/api/suggestions') return json([q.get('kind') === 'platform' ? 'YouTube' : q.get('kind') === 'author' ? '작가' : '여행'])
        if (url.pathname === '/api/search-suggestions') return json({ data: [{ kind: 'platform', value: 'YouTube' }], hasNextPage: false, offset: 0, revision: 1 })
        if (url.pathname.startsWith('/api/albums/') && url.pathname.endsWith('/contents')) {
            const id = Number(url.pathname.split('/')[3])
            return json({ album: { id, title: id === 1 ? '영상' : '하위 앨범', revision: 4, fileCount: 1, isUserAlbum: id !== 1, sourceKind: 'local', createdAt: 1 }, breadcrumbs: [{ id: 1, title: '영상' }, ...(id === 1 ? [] : [{ id, title: '하위 앨범' }])], data: id === 1 ? [{ kind: 'album', album: { id: 40, title: '하위 앨범', fileCount: 1, isUserAlbum: true, revision: 4, createdAt: 1 } }] : [{ kind: 'file', albumId: id, title: '내부 사진', file: { id: 41, kind: 'image', title: '내부 사진', filesize: 100, createdAt: 1 } }], total: 1, hasNextPage: false, offset: 0 })
        }
        if (url.pathname === '/api/library/1') return json({ id: 1, revision: 4, title: '영상', fileCount: 1, files: { data: [{ id: 11, title: '영상', kind: 'video', duration: 2.5 }], hasNextPage: false, total: 1 } })
        const expanded = q.get('expanded') === '2'
        const rows = (hidden.hiddenPlatforms.includes('YouTube') || hidden.hiddenTags.includes('여행')) && q.get('showHidden') !== '1' ? [] : [
            { id: 1, title: '영상', revision: 4, fileCount: 1, sourceKind: 'download', createdAt: 1, cover: { fileId: 11, kind: 'video' } },
            ...(expanded ? Array.from({ length: 53 }, (_, i) => ({ id: 2, title: '묶음', revision: 4, sourceKind: 'local', createdAt: 1, fileCount: 53, galleryFileOffset: i, galleryFile: { id: 20 + i, title: '사진 ' + i, kind: 'image' } })) : [{ id: 2, title: '묶음', fileCount: 53, sourceKind: 'local', createdAt: 1 }]),
            ...Array.from({ length: 28 }, (_, i) => ({ id: i + 3, title: '사진 ' + i, fileCount: 1, createdAt: 1 }))
        ]
        const offset = Number(q.get('offset')), limit = Number(q.get('limit'))
        return json({ data: rows.slice(offset, offset + limit), total: rows.length, hasNextPage: offset + limit < rows.length, offset })
    })
    vi.stubGlobal('fetch', fetcher)
    await mount(h(SearchPage, { Gallery: WebGalleryPage })); await act(flush)
    return fetcher
}
async function input(label: string, value: string, enter = false) {
    const node = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
    await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(node, value)
        node.dispatchEvent(new Event('input', { bubbles: true }))
    })
    if (enter) await act(async () => node.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })))
}
function listQuery(fetcher: Awaited<ReturnType<typeof setup>>) {
    return new URL(fetcher.mock.calls.filter(([path]) => path.startsWith('/api/library?')).at(-1)![0], 'http://localhost').searchParams
}
async function intersect() { await act(async () => { observers.findLast(item => item.active)!.callback([{ isIntersecting: true }]); await flush() }) }
it('opens the shared album explorer over HTTP and keeps album searches separate from the original gallery search', async () => {
    localStorage.setItem('asterodia:expanded-albums:v1', JSON.stringify([1, 2]))
    const fetcher = await setup()
    expect(listQuery(fetcher).get('albumCards')).toBe('1')
    expect(listQuery(fetcher).get('expanded')).toBe('')
    await input('미디어 검색', '기존 검색'); await act(async () => vi.advanceTimersByTimeAsync(310))
    const card = document.querySelector<HTMLElement>('article[data-album-id="1"]')!
    await act(async () => card.querySelector('[data-card-focus]')!.dispatchEvent(new MouseEvent('dblclick', { detail: 2, bubbles: true })))
    expect(document.querySelector('[aria-label="앨범 탐색기"]')).not.toBeNull()
    await input('미디어 검색', '앨범 검색'); await act(async () => vi.advanceTimersByTimeAsync(310))
    const query = new URL(fetcher.mock.calls.filter(([path]) => path.startsWith('/api/albums/1/contents?')).at(-1)![0], 'http://localhost').searchParams
    expect(query.get('q')).toBe('앨범 검색'); expect(listQuery(fetcher).get('q')).toBe('기존 검색')
    await act(async () => document.querySelector('[data-explorer-key="album:40"]')!.dispatchEvent(new MouseEvent('dblclick', { detail: 2, bubbles: true })))
    expect(document.querySelector('[data-explorer-key="file:41"] img')?.getAttribute('src')).toBe('/api/files/41/thumbnail')
    await clickButton('갤러리')
    expect(document.querySelector('[aria-label="앨범 탐색기"]')).toBeNull()
    expect(document.querySelector<HTMLInputElement>('[aria-label="미디어 검색"]')!.value).toBe('기존 검색')
})
it('manages shared hidden sites and tags from an empty list and refreshes the web gallery after saving', async () => {
    const fetcher = await setup()
    await clickButton('숨김 관리')
    expect(document.querySelector('[aria-label="숨길 사이트"]')).not.toBeNull()
    async function add(label: string, value: string) {
        await input(label, value)
        await act(async () => { document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!.form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); await flush() })
    }
    await add('숨길 사이트', 'YouTube')
    const post = fetcher.mock.calls.find(([, options]) => options?.method === 'POST')!
    expect(post[1]?.headers).toEqual({ 'Content-Type': 'application/json', 'X-Asterodia-CSRF': 'content-csrf' })
    expect(JSON.parse(String(post[1]?.body))).toEqual({ hiddenPlatforms: ['YouTube'], hiddenTags: [] })
    expect(document.querySelector('[aria-label="YouTube 숨김 해제"]')).not.toBeNull()
    expect(document.querySelectorAll('article')).toHaveLength(0)
    await act(async () => { document.querySelector<HTMLInputElement>('input[type=checkbox]')!.click(); await flush() })
    expect(listQuery(fetcher).get('showHidden')).toBe('1')
    expect(document.querySelectorAll('article')).toHaveLength(8)
    await act(async () => { document.querySelector<HTMLInputElement>('input[type=checkbox]')!.click(); await flush() })
    await add('숨길 태그', '여행')
    await clickButton('YouTube ×'); await act(flush)
    expect(document.querySelectorAll('article')).toHaveLength(0)
    await clickButton('여행 ×'); await act(flush)
    expect(document.querySelectorAll('article')).toHaveLength(8)
    await clickButton('숨김 관리')
    expect(document.querySelector('[aria-label="숨길 사이트"]')).toBeNull()
})
it('renders the shared desktop search fields and compact card structure with HTTP media URLs', async () => {
    await setup()
    expect(document.querySelectorAll('input')).toHaveLength(4)
    const card = document.querySelector('article')!
    expect(card.className).toContain('bg-[var(--bg-sidebar)]')
    expect(card.querySelector('img')!.getAttribute('src')).toBe('/api/files/11/thumbnail')
    expect(card.textContent).toContain('다운로드 묶음'); expect(card.textContent).toContain('파일 1개')
    expect(card.querySelector('[aria-label="영상 더보기"]')!.textContent).toBe('')
    expect(document.body.textContent).not.toContain('모든 항목을 불러왔습니다.')
    await clickButton('감상하기'); expect(document.querySelector('[aria-label="감상 1 "]')).not.toBeNull()
    await clickButton('감상 닫기'); await clickButton('영상 상세보기')
    expect(document.querySelector('[aria-label="파일 페이지"]')).not.toBeNull()
})
it('applies separate tag, platform and author fields, AND/OR mode and reset through HTTP', async () => {
    const fetcher = await setup()
    await input('태그 추가', '여행', true); await input('태그 추가', '풍경', true)
    await clickButton('AND'); await input('플랫폼 추가', 'YouTube', true); await input('작성자 추가', '작가', true)
    const query = listQuery(fetcher)
    expect(query.getAll('tag')).toEqual(['여행', '풍경']); expect(query.get('tagMode')).toBe('or')
    expect(query.getAll('platform')).toEqual(['YouTube']); expect(query.getAll('author')).toEqual(['작가'])
    expect(fetcher.mock.calls.some(([path]) => path.startsWith('/api/suggestions?') && path.includes('kind=platform'))).toBe(true)
    await clickButton('초기화')
    expect(listQuery(fetcher).getAll('tag')).toEqual([]); expect(listQuery(fetcher).getAll('platform')).toEqual([]); expect(listQuery(fetcher).getAll('author')).toEqual([])
})
it('turns a platform recommendation into a removable filter and carries it into later searches', async () => {
    const fetcher = await setup()
    await input('미디어 검색', 'You')
    await act(async () => { await vi.advanceTimersByTimeAsync(60) })
    await act(async () => document.querySelector<HTMLButtonElement>('[role=option]')!.click())
    expect(listQuery(fetcher).get('platform')).toBe('YouTube')
    expect(document.querySelector<HTMLInputElement>('[aria-label="미디어 검색"]')!.value).toBe('')
    await input('미디어 검색', '영상')
    await act(async () => { await vi.advanceTimersByTimeAsync(310) })
    expect(listQuery(fetcher).get('q')).toBe('영상'); expect(listQuery(fetcher).get('platform')).toBe('YouTube')
})
it('uses the common gallery store and observer to append expanded files without a completion footer', async () => {
    const fetcher = await setup()
    expect(document.querySelectorAll('article')).toHaveLength(8)
    await clickButton('묶음 파일 펼치기')
    await intersect(); expect(listQuery(fetcher).get('offset')).toBe('24')
    await intersect(); expect(listQuery(fetcher).get('offset')).toBe('48')
    await intersect(); expect(listQuery(fetcher).get('offset')).toBe('72')
    expect(document.querySelectorAll('article')).toHaveLength(8)
    expect(document.body.textContent).not.toContain('개 항목')
    expect(document.body.textContent).not.toContain('모든 항목을 불러왔습니다.')
    expect([...document.querySelectorAll('button')].some(node => node.textContent === '더 불러오기')).toBe(false)
})
it('retains cards on an append failure, stops automatic retries and allows a manual retry', async () => {
    const fetcher = await setup(); fetcher.mockRejectedValueOnce(new Error('offline'))
    await intersect(); expect(document.querySelectorAll('article')).toHaveLength(8)
    expect(observers.some(item => item.active)).toBe(false)
    await clickButton('다시 시도'); expect(document.querySelectorAll('article')).toHaveLength(8)
    expect(listQuery(fetcher).get('offset')).toBe('24')
})
it('opens the correct card video in the editor while keeping its menu unmounted', async () => {
    const fetcher = await setup(true)
    await clickButton('영상 더보기'); await clickButton('GIF, WebP로 만들기')
    expect(fetcher.mock.calls.some(([path]) => path === '/api/library/1?fileOffset=0')).toBe(true)
    expect(editor.mount.mock.lastCall![1].file).toEqual(expect.objectContaining({ id: 11, src: '/api/files/11/content' }))
    expect(editor.mount.mock.lastCall![1].reveal).toBeUndefined()
    expect(document.querySelector('[role=menu]')).toBeNull()
    await clickButton('편집기 닫기'); await clickButton('메뉴'); await clickButton('GIF, WebP로 만들기')
    expect(editor.mount.mock.lastCall![1].file).toBeUndefined()
})
it('ignores a pending source request after the search filters change', async () => {
    const fetcher = await setup(true), pending = deferred<any>()
    fetcher.mockReturnValueOnce(pending.promise)
    await clickButton('영상 더보기'); await clickButton('GIF, WebP로 만들기')
    await input('태그 추가', '여행', true)
    await act(async () => pending.resolve({ ok: true, json: async () => ({ id: 1, revision: 4, fileCount: 1, files: { data: [{ id: 11, kind: 'video' }] } }) }))
    expect(editor.mount).not.toHaveBeenCalled()
})
it('never turns a PC path or arbitrary URL into a browser media source', () => {
    expect(toMediaUrl('C:/private/video.mp4')).toBeNull(); expect(toMediaUrl('https://example.com/file')).toBeNull()
    expect(toMediaUrl('/api/files/11/content')).toBe('/api/files/11/content')
})
