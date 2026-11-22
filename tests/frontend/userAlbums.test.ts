// @vitest-environment happy-dom
import { act, createElement as h, createRef } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AlbumManager, type AlbumControls } from '@renderer/features/gallery/AlbumManager'
import { cleanupReact, clickButton, mount } from './reactHarness'

const api = vi.hoisted(() => ({ albums: vi.fn(), saveAlbum: vi.fn(), get: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ media: api }) }))
const album = { id: 9, title: '여행', revision: 3, fileCount: 3, isUserAlbum: true } as any
const files = [1, 2, 3].map(id => ({ id, title: `사진 ${id}`, filepath: `C:/folder-${id}/image.jpg` }))
const ok = (data: unknown) => ({ ok: true, data })
beforeEach(() => {
    vi.resetAllMocks()
    api.albums.mockResolvedValue(ok({ data: [album], hasNextPage: false }))
    api.get.mockResolvedValue(ok({ ...album, files: { data: files, hasNextPage: false } }))
    api.saveAlbum.mockResolvedValue(ok({ ...album, revision: 4 }))
})
afterEach(cleanupReact)
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
