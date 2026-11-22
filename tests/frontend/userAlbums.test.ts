// @vitest-environment happy-dom
import { act, createElement as h, createRef, StrictMode } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AlbumManager, type AlbumControls } from '@renderer/features/gallery/AlbumManager'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ albums: vi.fn(), saveAlbum: vi.fn(), get: vi.fn(), list: vi.fn(), sequence: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ media: api }) }))
const album = { id: 9, title: '여행', revision: 3, fileCount: 3, isUserAlbum: true } as any
const files = [1, 2, 3].map(id => ({ id, title: `사진 ${id}`, filepath: `C:/folder-${id}/image.jpg` }))
const ok = (data: unknown) => ({ ok: true, data })
const observers = new Set<{ callback: IntersectionObserverCallback }>()
const folder = (id: number, title: string, fileCount = 2) => ({ ...album, id, title, isUserAlbum: false, fileCount })
const file = (id: number) => ({ ...files[0], id, title: `사진 ${id}`, kind: 'image', filepath: `C:/folder/image-${id}.jpg` })
async function intersect() {
    await act(async () => { for (const observer of [...observers]) observer.callback([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver) })
}
beforeEach(() => {
    vi.resetAllMocks()
    observers.clear()
    vi.stubGlobal('IntersectionObserver', class {
        constructor(public callback: IntersectionObserverCallback) {}
        observe() { observers.add(this) }
        disconnect() { observers.delete(this) }
    })
    api.albums.mockResolvedValue(ok({ data: [album], hasNextPage: false }))
    api.get.mockResolvedValue(ok({ ...album, files: { data: files, hasNextPage: false } }))
    api.saveAlbum.mockResolvedValue(ok({ ...album, revision: 4 }))
    api.sequence.mockResolvedValue(ok({ ids: [1, 2, 3, 99], title: album.title, revision: 3 }))
    api.list.mockResolvedValue(ok({ data: [album, folder(10, '봄'), folder(20, '여름')], hasNextPage: false }))
})
afterEach(async () => { await cleanupReact(); vi.unstubAllGlobals() })
async function input(label: string, value: string) {
    const node = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
    await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(node, value); node.dispatchEvent(new Event('input', { bubbles: true })) })
}
function props() { return { selection: [{ mediaId: 1, revision: 2 }], fileIds: [7, 8], disabled: false, controlRef: createRef<AlbumControls>(), onChanged: vi.fn(), onClear: vi.fn() } }
it('creates a virtual album from selected files and groups without requesting any physical move', async () => {
    const p = props(); await mount(h(AlbumManager, p)); await clickButton('새 앨범'); await input('앨범 이름', '여행 모음'); await clickButton('만들기')
    expect(api.saveAlbum).toHaveBeenCalledExactlyOnceWith({ title: '여행 모음', addFileIds: [7, 8], addMediaIds: [1] })
    expect(p.onChanged).toHaveBeenCalledOnce(); expect(p.onClear).toHaveBeenCalledOnce(); expect(document.querySelector('dialog')).toBeNull()
})
it('adds an individual detail file using the chosen album revision', async () => {
    const p = props(); await mount(h(AlbumManager, p)); await act(async () => p.controlRef.current!.addFiles([12]))
    const select = document.querySelector<HTMLSelectElement>('select')!
    await act(async () => { select.value = '9'; select.dispatchEvent(new Event('change', { bubbles: true })) }); await clickButton('선택한 앨범에 추가')
    expect(api.saveAlbum).toHaveBeenCalledExactlyOnceWith({ albumId: 9, expectedRevision: 3, addFileIds: [12], addMediaIds: [] })
})
it('reorders album members and confirms album-only deletion inside its modal', async () => {
    const p = props(); await mount(h(AlbumManager, p)); await act(async () => p.controlRef.current!.edit(album))
    await clickButton('2번 파일 위로')
    expect(api.saveAlbum).toHaveBeenLastCalledWith({ albumId: 9, expectedRevision: 3, moveFileId: 2, beforeFileId: 1 })
    await clickButton('앨범 삭제'); expect(api.saveAlbum).toHaveBeenCalledTimes(1)
    expect(document.querySelector('[role=alert]')?.textContent).toContain('원본 파일은 유지')
    await clickButton('취소'); expect(api.saveAlbum).toHaveBeenCalledTimes(1)
    await clickButton('앨범 삭제'); await clickButton('앨범 삭제 확인')
    expect(api.saveAlbum).toHaveBeenLastCalledWith({ albumId: 9, expectedRevision: 3, delete: true })
    expect(p.onChanged).toHaveBeenLastCalledWith(9, true)
})
it('keeps a failed write visible and does not report success or discard selection', async () => {
    api.saveAlbum.mockResolvedValue({ ok: false, error: { code: 'REVISION_CONFLICT', message: '앨범이 변경되었습니다.' } })
    const p = props(); await mount(h(AlbumManager, p)); await clickButton('새 앨범'); await input('앨범 이름', '초안'); await clickButton('만들기')
    expect(document.querySelector('[role=alert]')?.textContent).toContain('앨범이 변경되었습니다.')
    expect(p.onChanged).not.toHaveBeenCalled(); expect(p.onClear).not.toHaveBeenCalled(); expect(document.querySelector('dialog')).not.toBeNull()
})

async function openPicker(strict = false) {
    const p = props(), element = h(AlbumManager, p)
    await mount(strict ? h(StrictMode, null, element) : element)
    await act(async () => p.controlRef.current!.edit(album))
    await clickButton('미디어 추가')
    return p
}
async function check(title: string) {
    const checkbox = document.querySelector<HTMLInputElement>(`input[aria-label="${title} 선택"]`)!
    expect(checkbox.disabled).toBe(false)
    await act(async () => checkbox.click())
}
it('adds unique files across folders to the open album and disables all existing members', async () => {
    api.get.mockImplementation(async (request: any) => ok({ ...album, id: request.mediaId, files: { data: request.mediaId === 9 ? files : request.mediaId === 10 ? [file(99), file(12), file(13)] : [file(12), file(22)], hasNextPage: false } }))
    const p = await openPicker()
    expect(api.list).toHaveBeenLastCalledWith({ limit: 50, offset: 0, filter: { keyword: '' } })
    expect(document.querySelector(`[aria-label="${album.title} 파일 보기"]`)).toBeNull()
    await clickButton('봄 파일 보기')
    const included = document.querySelector<HTMLInputElement>('input[aria-label="사진 99 선택"]')!
    expect(included.disabled).toBe(true); expect(included.checked).toBe(true)
    expect(document.body.textContent).toContain('이미 포함됨')
    await check('사진 12'); await check('사진 13'); await clickButton('목록으로'); await clickButton('여름 파일 보기')
    expect(document.querySelector<HTMLInputElement>('input[aria-label="사진 12 선택"]')!.checked).toBe(true)
    await clickButton('불러온 파일 선택')
    expect(document.body.textContent).toContain('선택 3개')
    await clickButton('선택한 미디어 추가')
    expect(api.saveAlbum).toHaveBeenCalledExactlyOnceWith({ albumId: 9, expectedRevision: 3, addFileIds: [12, 13, 22] })
    expect(p.onChanged).toHaveBeenCalledExactlyOnceWith(9, false)
    expect(p.onClear).not.toHaveBeenCalled()
    expect(document.querySelector('#album-manager-title')!.textContent).toBe('앨범 편집')
    expect(api.get).toHaveBeenLastCalledWith({ mediaId: 9, fileLimit: 50, fileOffset: 0 })
})
it('uses infinite scrolling for both library groups and source files without overlapping requests', async () => {
    api.list.mockResolvedValue(ok({ data: [album, folder(10, '봄')], hasNextPage: true }))
    const p = await openPicker(true)
    const initialCalls = api.list.mock.calls.length
    const next = deferred<any>()
    api.list.mockReturnValueOnce(next.promise)
    await intersect(); await intersect()
    expect(api.list).toHaveBeenCalledTimes(initialCalls + 1)
    expect(api.list).toHaveBeenLastCalledWith({ limit: 50, offset: 2, filter: { keyword: '' } })
    await act(async () => next.resolve(ok({ data: [folder(20, '여름')], hasNextPage: false })))
    expect(document.querySelector('[aria-label="봄 파일 보기"]')).not.toBeNull()
    expect(document.querySelector('[aria-label="여름 파일 보기"]')).not.toBeNull()
    api.get.mockResolvedValueOnce(ok({ ...album, id: 10, files: { data: [file(12), file(13)], hasNextPage: true } }))
    await clickButton('봄 파일 보기'); await check('사진 12')
    api.get.mockResolvedValueOnce(ok({ ...album, id: 10, files: { data: [file(14)], hasNextPage: false } }))
    await intersect()
    expect(api.get).toHaveBeenLastCalledWith({ mediaId: 10, fileLimit: 50, fileOffset: 2 })
    expect(document.querySelectorAll('input[type="checkbox"]')).toHaveLength(3)
    expect(document.querySelector<HTMLInputElement>('input[aria-label="사진 12 선택"]')!.checked).toBe(true)
    await clickButton('돌아가기')
    expect(p.onChanged).not.toHaveBeenCalled(); expect(api.saveAlbum).not.toHaveBeenCalled()
    expect(document.querySelector('#album-manager-title')!.textContent).toBe('앨범 편집')
})
it('keeps loaded results on an append failure and retries at the same offset', async () => {
    api.list.mockResolvedValueOnce(ok({ data: [folder(10, '봄')], hasNextPage: true }))
    await openPicker()
    api.list.mockRejectedValueOnce(new Error('연결 오류'))
    await intersect()
    expect(document.body.textContent).toContain('연결 오류')
    expect(document.querySelector('[aria-label="봄 파일 보기"]')).not.toBeNull()
    const calls = api.list.mock.calls.length
    await intersect(); expect(api.list).toHaveBeenCalledTimes(calls)
    api.list.mockResolvedValueOnce(ok({ data: [folder(20, '여름')], hasNextPage: false }))
    await clickButton('다시 시도')
    expect(api.list).toHaveBeenLastCalledWith({ limit: 50, offset: 1, filter: { keyword: '' } })
    expect(document.querySelector('[aria-label="여름 파일 보기"]')).not.toBeNull()
})
it('preserves file selection through searches and ignores stale search results', async () => {
    api.get.mockResolvedValueOnce(ok({ ...album, files: { data: files, hasNextPage: false } }))
        .mockResolvedValueOnce(ok({ ...album, id: 10, files: { data: [file(12)], hasNextPage: false } }))
    await openPicker(); await clickButton('봄 파일 보기'); await check('사진 12')
    const oldSearch = deferred<any>()
    api.list.mockReturnValueOnce(oldSearch.promise)
    await input('미디어 검색', '이전'); await clickButton('검색')
    api.list.mockResolvedValueOnce(ok({ data: [folder(30, '새 결과')], hasNextPage: false }))
    await input('미디어 검색', '새 검색'); await clickButton('검색')
    await act(async () => oldSearch.resolve(ok({ data: [folder(40, '오래된 결과')], hasNextPage: false })))
    expect(document.querySelector('[aria-label="새 결과 파일 보기"]')).not.toBeNull()
    expect(document.querySelector('[aria-label="오래된 결과 파일 보기"]')).toBeNull()
    expect(document.body.textContent).toContain('선택 1개')
    await clickButton('선택한 미디어 추가')
    expect(api.saveAlbum).toHaveBeenCalledExactlyOnceWith({ albumId: 9, expectedRevision: 3, addFileIds: [12] })
})
it('retains selected files on a failed album write and allows retrying', async () => {
    api.get.mockImplementation(async (request: any) => ok({ ...album, id: request.mediaId, files: { data: request.mediaId === 9 ? files : [file(12)], hasNextPage: false } }))
    const p = await openPicker(); await clickButton('봄 파일 보기'); await check('사진 12')
    api.saveAlbum.mockResolvedValueOnce({ ok: false, error: { code: 'INTERNAL_ERROR', message: '저장 실패' } })
    await clickButton('선택한 미디어 추가')
    expect(document.querySelector('#album-manager-title')!.textContent).toBe('미디어 추가')
    expect(document.body.textContent).toContain('저장 실패')
    expect(document.querySelector<HTMLInputElement>('input[aria-label="사진 12 선택"]')!.checked).toBe(true)
    expect(p.onChanged).not.toHaveBeenCalled()
    await clickButton('선택한 미디어 추가')
    expect(api.saveAlbum).toHaveBeenCalledTimes(2)
    expect(document.querySelector('#album-manager-title')!.textContent).toBe('앨범 편집')
})
it('blocks selection until existing membership is known and rejects a stale album revision', async () => {
    api.sequence.mockResolvedValueOnce({ ok: false, error: { code: 'INTERNAL_ERROR', message: '확인 실패' } })
    api.get.mockImplementation(async (request: any) => ok({ ...album, id: request.mediaId, files: { data: request.mediaId === 9 ? files : [file(12)], hasNextPage: false } }))
    await openPicker(); await clickButton('봄 파일 보기')
    expect(document.body.textContent).toContain('확인 실패')
    expect(document.querySelector<HTMLInputElement>('input[aria-label="사진 12 선택"]')!.disabled).toBe(true)
    api.sequence.mockResolvedValueOnce(ok({ ids: [1, 2, 3], revision: 4 }))
    await clickButton('포함 여부 다시 확인')
    expect(document.body.textContent).toContain('앨범이 변경되었습니다.')
    expect(document.querySelector<HTMLInputElement>('input[aria-label="사진 12 선택"]')!.disabled).toBe(true)
    api.sequence.mockResolvedValueOnce(ok({ ids: [1, 2, 3], revision: 3 }))
    await clickButton('포함 여부 다시 확인'); await check('사진 12')
    expect(api.saveAlbum).not.toHaveBeenCalled()
})
it('rejects a changed source revision instead of appending a stale file page', async () => {
    await openPicker()
    api.get.mockResolvedValueOnce(ok({ ...album, id: 10, files: { data: [file(12)], hasNextPage: true } }))
    await clickButton('봄 파일 보기')
    api.get.mockResolvedValueOnce(ok({ ...album, id: 10, revision: 4, files: { data: [file(13)], hasNextPage: false } }))
    await intersect()
    expect(document.body.textContent).toContain('목록이 변경되었습니다.')
    expect(document.querySelector('input[aria-label="사진 12 선택"]')).not.toBeNull()
    expect(document.querySelector('input[aria-label="사진 13 선택"]')).toBeNull()
})
