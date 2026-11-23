// @vitest-environment happy-dom
import { act, createElement as h, StrictMode, useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AlbumDropPanel } from '@renderer/features/gallery/AlbumDropPanel'
import { albumDragType } from '@renderer/features/gallery/useGallerySelection'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ albums: vi.fn(), saveAlbum: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ media: api }) }))
const album = (id: number) => ({ id, title: `앨범 ${id}`, revision: 3, fileCount: 2, isUserAlbum: true })
const page = (ids: number[], hasNextPage = false) => ({ ok: true, data: { data: ids.map(album), hasNextPage } })
const props = () => ({ drag: { fileIds: [11], mediaIds: [] }, disabled: false, refreshKey: 0, onDragEnd: vi.fn(), onBusyChange: vi.fn(), onChanged: vi.fn() })
beforeEach(() => { vi.resetAllMocks(); api.albums.mockResolvedValue(page([9])); api.saveAlbum.mockResolvedValue({ ok: true, data: album(9) }) })
afterEach(cleanupReact)

it('searches all destination albums and appends the next page using the same query', async () => {
 await mount(h(AlbumDropPanel, props()))
 const input = document.querySelector<HTMLInputElement>('input')!
 await act(async () => {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '여행')
  input.dispatchEvent(new Event('input', { bubbles: true }))
 })
 api.albums.mockResolvedValueOnce(page([10], true))
 await clickButton('드롭할 앨범 찾기')
 expect(api.albums).toHaveBeenLastCalledWith({ keyword: '여행', limit: 50, offset: 0 })
 api.albums.mockResolvedValueOnce(page([11]))
 await clickButton('앨범 더 불러오기')
 expect(api.albums).toHaveBeenLastCalledWith({ keyword: '여행', limit: 50, offset: 1 })
 expect([...document.querySelectorAll('[data-album-drop-id]')].map(node => node.textContent)).toEqual(['앨범 10파일 2개', '앨범 11파일 2개'])
})

it('disables drops after a failed album refresh until the target list is reloaded', async () => {
 await mount(h(AlbumDropPanel, props()))
 api.albums.mockRejectedValueOnce(Error('offline'))
 await clickButton('드롭할 앨범 새로고침')
 const drop = async () => {
  const event = new Event('drop', { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'dataTransfer', { value: { types: [albumDragType] } })
  await act(async () => document.querySelector('[data-album-drop-id]')!.dispatchEvent(event))
 }
 await drop()
 expect(api.saveAlbum).not.toHaveBeenCalled()
 expect(document.querySelector('[role="alert"]')?.textContent).toBe('offline')
 await clickButton('드롭할 앨범 새로고침')
 await drop()
 expect(api.saveAlbum).toHaveBeenCalledOnce()
})

it('ignores older album reads after a refresh and survives StrictMode effect replay', async () => {
 const first = deferred<any>(), second = deferred<any>()
 api.albums.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
 let refresh!: () => void
 function Updating() {
  const [refreshKey, setRefreshKey] = useState(0); refresh = () => setRefreshKey(value => value + 1)
  return h(AlbumDropPanel, { ...props(), refreshKey })
 }
 await mount(h(StrictMode, null, h(Updating)))
 await act(async () => second.resolve(page([10])))
 await act(async () => first.resolve(page([9])))
 expect(document.querySelector('[data-album-drop-id="10"]')).not.toBeNull()
 expect(document.querySelector('[data-album-drop-id="9"]')).toBeNull()
 await act(async () => refresh())
 expect(document.querySelector('[data-album-drop-id="9"]')).not.toBeNull()
})

it('does not report a late save after the gallery has unmounted', async () => {
 const pending = deferred<any>(), p = props(); api.saveAlbum.mockReturnValueOnce(pending.promise)
 const stop = await mount(h(AlbumDropPanel, p))
 const drop = new Event('drop', { bubbles: true, cancelable: true })
 Object.defineProperty(drop, 'dataTransfer', { value: { types: [albumDragType] } })
 await act(async () => document.querySelector('[data-album-drop-id]')!.dispatchEvent(drop))
 expect(p.onBusyChange).toHaveBeenCalledExactlyOnceWith(true)
 await stop(); await act(async () => pending.resolve({ ok: true, data: album(9) }))
 expect(p.onChanged).not.toHaveBeenCalled()
 expect(p.onBusyChange).toHaveBeenCalledTimes(1)
})
