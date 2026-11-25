// @vitest-environment happy-dom
import { StrictMode, act, createElement as h, useCallback, useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MediaViewer } from '@renderer/features/gallery/MediaViewer'
import { cleanupReact, clickButton, mount } from './reactHarness'

const api = vi.hoisted(() => ({ sequence: vi.fn(), readFile: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ media: api }) }))
beforeEach(() => {
    vi.resetAllMocks()
    api.sequence.mockResolvedValue({ ok: true, data: { ids: [7], title: '앨범', revision: 1 } })
    api.readFile.mockResolvedValue({ ok: true, data: { id: 7, kind: 'image', filepath: 'C:/fixture/image.jpg', thumbnailPath: null, previewPath: null } })
})
afterEach(async () => { await cleanupReact(); document.body.replaceChildren(); document.body.style.overflow = ''; vi.restoreAllMocks() })

it('keeps the viewer open after StrictMode effect cleanup and closes only on user action', async () => {
    const onClose = vi.fn()
    function Host() {
        const [open, setOpen] = useState(false)
        const close = useCallback(() => { onClose(); setOpen(false) }, [])
        return h('div', null, h('button', { onClick: () => setOpen(true) }, '감상하기'), open && h(MediaViewer, { mediaId: 9, onClose: close }))
    }
    await mount(h(StrictMode, null, h(Host)))
    await clickButton('감상하기')
    expect(document.querySelectorAll('.sb-viewer')).toHaveLength(1)
    expect(document.querySelector<HTMLDialogElement>('.sb-viewer')?.open).toBe(true)
    expect(document.querySelector('.sbv-image')).not.toBeNull()
    expect(document.body.style.overflow).toBe('hidden')
    expect(onClose).not.toHaveBeenCalled()
    await clickButton('닫기 · Esc')
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)) })
    expect(onClose).toHaveBeenCalledOnce()
    expect(document.querySelector('.sb-viewer')).toBeNull()
    expect(document.body.style.overflow).toBe('')
    await clickButton('감상하기')
    expect(document.querySelector<HTMLDialogElement>('.sb-viewer')?.open).toBe(true)
    expect(onClose).toHaveBeenCalledOnce()
})

it('releases a viewer on unmount without requesting that its parent close again', async () => {
    const onClose = vi.fn()
    const unmount = await mount(h(MediaViewer, { mediaId: 9, onClose }))
    await unmount()
    expect(onClose).not.toHaveBeenCalled()
    expect(document.querySelector('.sb-viewer')).toBeNull()
    expect(document.body.style.overflow).toBe('')
})

it.each(['audio', 'video'])('starts %s after one viewing click, including StrictMode cleanup', async kind => {
    const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {})
    api.readFile.mockResolvedValue({ ok: true, data: { id: 7, kind, filepath: 'C:/fixture/media', thumbnailPath: 'C:/cover.jpg', previewPath: null } })
    function Host() {
        const [open, setOpen] = useState(false)
        const close = useCallback(() => setOpen(false), [])
        return h('div', null, h('button', { onClick: () => setOpen(true) }, '감상하기'), open && h(MediaViewer, { mediaId: 9, onClose: close }))
    }
    await mount(h(StrictMode, null, h(Host)))
    expect(play).not.toHaveBeenCalled()
    await clickButton('감상하기')
    expect(document.querySelectorAll('.sb-viewer')).toHaveLength(1)
    expect(document.querySelector('.sb-player .sbp-controls')).not.toBeNull()
    expect(play).toHaveBeenCalledOnce()
})
