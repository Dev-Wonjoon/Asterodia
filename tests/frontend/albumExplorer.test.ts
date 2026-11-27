// @vitest-environment happy-dom
import { act, StrictMode, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AlbumExplorer } from '@renderer/features/gallery/AlbumExplorer'
import { setShowHidden } from '@renderer/shared/hooks/useContentVisibility'
import { readExplorerPreferences } from '@renderer/features/gallery/useAlbumExplorer'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ getContentFilters: vi.fn(), media: { exploreAlbum: vi.fn(), saveAlbum: vi.fn(), list: vi.fn(), suggest: vi.fn(), searchSuggest: vi.fn() } }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => api }))
vi.mock('@renderer/features/gallery/MediaViewer', () => ({ MediaViewer: ({ mediaId, fileId, onClose }: any) => h('div', { role: 'dialog', 'aria-label': `감상 ${mediaId}/${fileId}` }, h('button', { onClick: onClose }, '감상 닫기')) }))
const album = (id: number) => ({ id, title: id === 1 ? '여행' : id === 2 ? '제주' : '제주 안쪽', revision: 5, createdAt: 1000, fileCount: 1, isUserAlbum: true, parentId: id === 1 ? null : id - 1 })
const file = (id: number, albumId = 1, title = '파일 ' + id) => ({ kind: 'file', albumId, title, file: { id, mediaId: 50, title, filepath: '/test/' + id, kind: 'image', filesize: id * 100, duration: null, createdAt: 1000, thumbnailPath: null } })
const child = (id: number) => ({ kind: 'album', album: album(id) })
const page = (id: number, data = id === 1 ? [child(2), file(10)] : id === 2 ? [child(3), file(20, 2)] : [], extra: any = {}) => ({ album: album(id), breadcrumbs: Array.from({ length: id }, (_, i) => ({ id: i + 1, title: album(i + 1).title })), data, total: data.length, hasNextPage: false, offset: 0, ...extra })
const onClose = vi.fn(), onChanged = vi.fn()
beforeEach(() => {
    vi.useFakeTimers(); vi.resetAllMocks(); localStorage.clear(); setShowHidden(false)
    vi.spyOn(HTMLDialogElement.prototype, 'showModal').mockImplementation(function () { this.open = true })
    api.getContentFilters.mockResolvedValue({ hiddenPlatforms: [], hiddenTags: [] })
    api.media.exploreAlbum.mockImplementation(async value => ({ ok: true, data: page(value.mediaId) }))
    api.media.suggest.mockResolvedValue({ ok: true, data: [] })
    api.media.searchSuggest.mockResolvedValue({ ok: true, data: { data: [], hasNextPage: false, revision: 1, indexed: true } })
    api.media.list.mockResolvedValue({ ok: true, data: { data: [album(2)], hasNextPage: false, total: 1 } })
    api.media.saveAlbum.mockResolvedValue({ ok: true, data: album(1) })
})
afterEach(async () => { await cleanupReact(); setShowHidden(false); vi.clearAllTimers(); vi.useRealTimers(); vi.restoreAllMocks() })
const render = () => mount(h(AlbumExplorer, { mediaId: 1, onClose, onChanged }))
async function input(label: string, value: string, enter = false) {
    const node = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
    await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(node, value); node.dispatchEvent(new Event('input', { bubbles: true })) })
    if (enter) await act(async () => node.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })))
}
async function change(label: string, value: string) {
    await menu()
    const node = document.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
    await act(async () => { node.value = value; node.dispatchEvent(new Event('change', { bubbles: true })) })
}
async function toggle(label: string) {
    await menu()
    const node = [...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]')].find(node => node.getAttribute('aria-label') === label || node.parentElement?.textContent === label)!
    await act(async () => node.click())
}
async function open(key: string) {
    await act(async () => document.querySelector<HTMLElement>(`[data-explorer-key="${key}"]`)!.dispatchEvent(new MouseEvent('dblclick', { detail: 2, bubbles: true })))
}
async function menu() {
    if (!document.querySelector('[role="dialog"][aria-label="탐색기 메뉴"]')) await clickButton('탐색기 메뉴')
}
async function menuAction(label: string) { await menu(); await clickButton(label) }

it('navigates nested albums and restores the previous selection, focus and scroll position', async () => {
    await render()
    const row = document.querySelector<HTMLElement>('[data-explorer-key="album:2"]')!
    await act(async () => { row.focus(); row.dispatchEvent(new MouseEvent('click', { detail: 1, bubbles: true })) })
    const scroll = document.querySelector<HTMLElement>('[data-explorer-scroll]')!
    scroll.scrollTop = 230; scroll.dispatchEvent(new Event('scroll'))
    await open('album:2')
    expect(api.media.exploreAlbum.mock.lastCall![0].mediaId).toBe(2)
    expect(document.querySelector('[aria-label="앨범 경로"]')?.textContent).toContain('갤러리/여행/제주')
    await open('file:20'); expect(document.querySelector('[aria-label="감상 2/20"]')).not.toBeNull()
    await clickButton('감상 닫기'); await clickButton('뒤로')
    expect(document.querySelector('[data-explorer-key="album:2"]')?.getAttribute('aria-selected')).toBe('true')
    expect(document.querySelector<HTMLElement>('[data-explorer-scroll]')!.scrollTop).toBe(230)
    expect((document.activeElement as HTMLElement).dataset.explorerKey).toBe('album:2')
    await clickButton('앞으로'); expect(api.media.exploreAlbum.mock.lastCall![0].mediaId).toBe(2)
    await clickButton('상위로'); expect(api.media.exploreAlbum.mock.lastCall![0].mediaId).toBe(1)
    await clickButton('갤러리'); expect(onClose).toHaveBeenCalledOnce()
})
it('persists view, sorting, visible columns and the preview pane independently for each album', async () => {
    const unmount = await render()
    await change('보기', 'details'); await change('정렬', 'size'); await clickButton('오름차순')
    await toggle('미리보기 창'); await toggle('크기')
    expect(document.querySelector('thead')?.textContent).not.toContain('크기')
    await act(async () => document.querySelector('[data-explorer-key="file:10"]')!.dispatchEvent(new MouseEvent('click', { detail: 1, bubbles: true })))
    expect(document.querySelector('[aria-label="미리보기 창"]')?.textContent).toContain('파일 10')
    await unmount(); await render()
    await menu()
    expect(document.querySelector<HTMLSelectElement>('[aria-label="보기"]')!.value).toBe('details')
    expect(document.querySelector<HTMLSelectElement>('[aria-label="정렬"]')!.value).toBe('size')
    expect(document.querySelector('thead')?.textContent).not.toContain('크기')
    expect(readExplorerPreferences(2).mode).toBe('icons')
    expect(api.media.exploreAlbum.mock.lastCall![0]).toMatchObject({ sort: 'size', descending: true })
})
it('sends title, tag AND/OR, platform and author filters together with the recursive and hidden-content options', async () => {
    api.getContentFilters.mockResolvedValue({ hiddenPlatforms: ['blocked'], hiddenTags: [] })
    await render()
    await input('미디어 검색', '제주', true)
    await input('태그 추가', '여행', true); await input('태그 추가', '풍경', true); await clickButton('AND')
    await input('플랫폼 추가', 'YouTube', true); await input('작성자 추가', '작가', true)
    await toggle('하위 앨범 포함'); await toggle('숨긴 항목 표시')
    expect(api.media.exploreAlbum.mock.lastCall![0]).toMatchObject({ mediaId: 1, recursive: true, filter: { keyword: '제주', tags: ['여행', '풍경'], tagMode: 'or', platforms: ['YouTube'], authors: ['작가'], showHidden: true } })
})
it('ignores an outdated response after changing the album search', async () => {
    const old = deferred<any>()
    api.media.exploreAlbum.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ ok: true, data: page(1, [file(30, 1, '새 결과')]) })
    await render(); await input('미디어 검색', 'new', true)
    expect(document.querySelector('[data-explorer-key="file:30"]')).not.toBeNull()
    await act(async () => old.resolve({ ok: true, data: page(1) }))
    expect(document.querySelector('[data-explorer-key="file:30"]')).not.toBeNull()
    expect(document.querySelector('[data-explorer-key="file:10"]')).toBeNull()
})
it('keeps the loaded page when a later page has a changed album revision', async () => {
    api.media.exploreAlbum.mockResolvedValueOnce({ ok: true, data: page(1, Array.from({ length: 100 }, (_, i) => file(i + 1)), { total: 101, hasNextPage: true }) })
        .mockResolvedValueOnce({ ok: true, data: page(1, [file(101)], { album: { ...album(1), revision: 6 }, total: 101 }) })
    await render(); await clickButton('더 불러오기')
    expect(document.querySelectorAll('[data-explorer-key]')).toHaveLength(100)
    expect(document.querySelector('[role=alert]')).not.toBeNull()
    expect(document.querySelector('[data-explorer-key="file:101"]')).toBeNull()
})
it('rejects a later page when descendants changed without changing the current album revision', async () => {
    api.media.exploreAlbum.mockResolvedValueOnce({ ok: true, data: page(1, Array.from({ length: 100 }, (_, i) => file(i + 1)), { total: 101, hasNextPage: true, revisionToken: 'before' }) })
        .mockResolvedValueOnce({ ok: true, data: page(1, [file(101)], { total: 101, revisionToken: 'after' }) })
    await render(); await clickButton('더 불러오기')
    expect(document.querySelectorAll('[data-explorer-key]')).toHaveLength(100)
    expect(document.querySelector('[role=alert]')).not.toBeNull()
})
it('creates and renames subalbums and sends a chosen destination for moving the current album', async () => {
    await render(); await menuAction('새 하위 앨범'); await input('이름', '새 앨범'); await clickButton('확인')
    expect(api.media.saveAlbum).toHaveBeenLastCalledWith({ title: '새 앨범', parentId: 1 })
    expect(onChanged).toHaveBeenCalledOnce()
    await menuAction('앨범 이름 변경'); await input('이름', '새 이름'); await clickButton('확인')
    expect(api.media.saveAlbum).toHaveBeenLastCalledWith({ albumId: 1, expectedRevision: 5, title: '새 이름' })
    await menuAction('앨범 이동')
    const destination = document.querySelector('[aria-label="이동할 위치"]')!
    await act(async () => [...destination.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent === '제주')!.click())
    await clickButton('확인')
    expect(api.media.saveAlbum).toHaveBeenLastCalledWith({ albumId: 1, expectedRevision: 5, parentId: 2 })
})
it('confirms album-reference removal before Delete and preserves the selection on a save failure', async () => {
    api.media.saveAlbum.mockResolvedValueOnce({ ok: false, error: { code: 'REVISION_CONFLICT', message: 'changed', context: {} } })
    await render()
    const row = document.querySelector<HTMLElement>('[data-explorer-key="file:10"]')!
    await act(async () => { row.focus(); row.dispatchEvent(new MouseEvent('click', { detail: 1, bubbles: true })) })
    await act(async () => row.dispatchEvent(new KeyboardEvent('keydown', { key: 'Delete', bubbles: true, cancelable: true })))
    expect(api.media.saveAlbum).not.toHaveBeenCalled()
    expect(document.querySelector('dialog')?.textContent).toContain('실제 파일과 다른 앨범의 구성은 유지')
    await clickButton('확인')
    expect(api.media.saveAlbum).toHaveBeenLastCalledWith({ albumId: 1, expectedRevision: 5, removeFileIds: [10] })
    expect(document.querySelector('dialog [role=alert]')).not.toBeNull()
    await clickButton('취소')
    expect(row.getAttribute('aria-selected')).toBe('true')
})
it('falls back to safe preferences when stored values are malformed', () => {
    localStorage.setItem('asterodia:album-view:1', JSON.stringify({ mode: 'bad', iconSize: 1000, sort: 'bad', columns: ['kind', 'kind', 'other'] }))
    expect(readExplorerPreferences(1)).toMatchObject({ mode: 'icons', iconSize: 240, sort: 'name', columns: ['kind'] })
})
it('restores album selection under StrictMode while shared hiding rules are active', async () => {
    api.getContentFilters.mockResolvedValue({ hiddenPlatforms: ['blocked'], hiddenTags: [] })
    await mount(h(StrictMode, null, h(AlbumExplorer, { mediaId: 1, onClose, onChanged })))
    const row = document.querySelector<HTMLElement>('[data-explorer-key="album:2"]')!
    await act(async () => row.dispatchEvent(new MouseEvent('click', { detail: 1, bubbles: true })))
    await open('album:2'); await clickButton('뒤로')
    expect(document.querySelector('[data-explorer-key="album:2"]')?.getAttribute('aria-selected')).toBe('true')
})
it('keeps options in one dropdown, stays open while adjusting settings, and restores focus on Escape', async () => {
    await render()
    const header = document.querySelector('header')!
    expect(header.querySelectorAll('select,input')).toHaveLength(0)
    expect(document.querySelector('[aria-label="보기"]')).toBeNull()
    await menu()
    expect(document.querySelector('[role="dialog"][aria-label="탐색기 메뉴"]')).not.toBeNull()
    await change('보기', 'details'); await change('정렬', 'size')
    expect(document.querySelector<HTMLButtonElement>('[aria-label="탐색기 메뉴"]')!.getAttribute('aria-expanded')).toBe('true')
    const control = document.querySelector('[aria-label="정렬"]')!
    await act(async () => control.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    expect(document.querySelector('[role="dialog"][aria-label="탐색기 메뉴"]')).toBeNull()
    expect(document.activeElement?.getAttribute('aria-label')).toBe('탐색기 메뉴')
})
it('closes the dropdown on outside clicks and before starting an album action', async () => {
    await render(); await menu()
    await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })))
    expect(document.querySelector('[role="dialog"][aria-label="탐색기 메뉴"]')).toBeNull()
    await menuAction('새 하위 앨범')
    expect(document.querySelector('[role="dialog"][aria-label="탐색기 메뉴"]')).toBeNull()
    expect(document.querySelector('dialog[aria-label="새 하위 앨범"]')).not.toBeNull()
    expect(api.media.saveAlbum).not.toHaveBeenCalled()
    await clickButton('취소')
})
