// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mountViewerPicker } from '../../internal/webserver/web/viewer-picker.js'
import { setLanguage } from '@renderer/shared/i18n'
import { deferred } from './reactHarness'

let picker: ReturnType<typeof mountViewerPicker> | undefined
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve() }
const button = (text: string) => [...document.querySelectorAll<HTMLButtonElement>('.sbv-picker button')].find(el => el.textContent === text)!
beforeEach(() => { setLanguage('ko') })
afterEach(() => { picker?.destroy(); picker = undefined; document.body.replaceChildren(); vi.restoreAllMocks(); setLanguage('ko') })
function setup(extra = {}) {
    const trigger = document.createElement('button'); document.body.append(trigger); trigger.focus()
    const listMedia = vi.fn(async () => ({ data: [{ id: 20, title: '찾은 앨범', fileCount: 3 }], hasNextPage: false }))
    const getAlbum = vi.fn(async () => ({ title: '현재 앨범', revision: 1, files: { data: [{ id: 1, kind: 'image', title: '<img src=x onerror=alert(1)>' }, { id: 2, kind: 'audio', title: '오디오' }, { id: 3, kind: 'video', title: '영상' }], hasNextPage: false } }))
    const onSelect = vi.fn(), onClose = vi.fn()
    picker = mountViewerPicker(document.body, { mediaId: 9, listMedia, getAlbum, onSelect, onClose, ...extra })
    return { trigger, listMedia, getAlbum, onSelect, onClose }
}

it('filters unsupported files, treats titles as text and selects the file with its album', async () => {
    const { onSelect, onClose } = setup(); await flush()
    expect(document.activeElement).toBe(document.querySelector('input'))
    expect(document.querySelectorAll('[data-file-id]')).toHaveLength(2); expect(document.querySelector('.sbv-picker img')).toBeNull()
    button('영상').click(); expect(onSelect).toHaveBeenCalledExactlyOnceWith({ mediaId: 9, fileId: 3 }); expect(onClose).toHaveBeenCalledOnce()
    expect(document.querySelector('.sbv-picker')).toBeNull()
})

it('searches the library, opens an album and restores focus on cancellation', async () => {
    const { trigger, listMedia, getAlbum, onClose } = setup(); await flush()
    const input = document.querySelector<HTMLInputElement>('input')!; input.value = '찾기'
    document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); await flush()
    expect(listMedia).toHaveBeenCalledExactlyOnceWith({ keyword: '찾기', offset: 0 })
    button('찾은 앨범').click(); await flush(); expect(getAlbum).toHaveBeenLastCalledWith(20, 0)
    picker!.dialog.close(); await flush(); expect(document.activeElement).toBe(trigger); expect(onClose).toHaveBeenCalledOnce()
})

it('discards old album results after switching to the library and after destruction', async () => {
    const pending = deferred<any>(), getAlbum = vi.fn(() => pending.promise)
    setup({ getAlbum }); button('목록으로').click(); await flush()
    pending.resolve({ title: '오래된 앨범', files: { data: [{ id: 999, kind: 'image' }], hasNextPage: false } }); await flush()
    expect(button('찾은 앨범')).toBeTruthy(); expect(document.querySelector('[data-file-id="999"]')).toBeNull()
    picker!.destroy(); await flush(); expect(document.querySelector('.sbv-picker')).toBeNull()
})

it('pages by raw file count, retains loaded rows on retry and detects collection revision changes', async () => {
    const getAlbum = vi.fn()
        .mockResolvedValueOnce({ title: '앨범', revision: 1, files: { data: [{ id: 1, kind: 'image', title: '첫 이미지' }, { id: 2, kind: 'audio' }], hasNextPage: true } })
        .mockRejectedValueOnce(new Error('연결 실패'))
        .mockResolvedValueOnce({ title: '앨범', revision: 1, files: { data: [{ id: 3, kind: 'video', title: '다음 영상' }], hasNextPage: true } })
        .mockResolvedValueOnce({ title: '앨범', revision: 2, files: { data: [{ id: 4, kind: 'image', title: '변경된 파일' }], hasNextPage: false } })
    setup({ getAlbum }); await flush(); button('더 불러오기').click(); await flush()
    expect(getAlbum).toHaveBeenLastCalledWith(9, 2); expect(button('첫 이미지')).toBeTruthy()
    button('다시 시도').click(); await flush(); expect(getAlbum).toHaveBeenLastCalledWith(9, 2); expect(button('다음 영상')).toBeTruthy()
    button('더 불러오기').click(); await flush(); expect(getAlbum).toHaveBeenLastCalledWith(9, 3)
    expect(document.querySelector('[data-file-id="4"]')).toBeNull(); expect(document.querySelector('[role="status"]')!.textContent).toContain('변경')
})

it('does not apply a pending selection after closing and translates an already open picker', async () => {
    const pending = deferred<any>(); setup({ getAlbum: () => pending.promise }); setLanguage('en')
    expect(button('Current album')).toBeTruthy(); picker!.destroy()
    pending.resolve({ title: 'late', files: { data: [{ id: 1, kind: 'video' }], hasNextPage: false } }); await flush()
    expect(document.querySelector('.sbv-picker')).toBeNull()
})
