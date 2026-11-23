// @vitest-environment happy-dom
import { createElement as h } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { GalleryCardActions } from '@renderer/features/gallery/GalleryCardActions'
import { cleanupReact, clickButton, mount } from './reactHarness'

afterEach(cleanupReact)

function props() {
    return { title: '영상', isFavorite: false, isPinned: true, disabled: false, mutationDisabled: false,
        onToggleFavorite: vi.fn(), onTogglePin: vi.fn(), onOpenFolder: vi.fn(), onEdit: vi.fn(),
        onUnregister: vi.fn(), onAdd: vi.fn(), onCreateAnimation: vi.fn() }
}

it('exposes pin, tag, album and animation actions without opening the menu', async () => {
    const p = props()
    await mount(h(GalleryCardActions, p))
    expect(document.querySelector('[aria-label="영상 고정 해제"]')?.getAttribute('aria-pressed')).toBe('true')
    await clickButton('영상 고정 해제')
    await clickButton('영상 태그 편집')
    await clickButton('영상 앨범에 추가')
    await clickButton('영상 GIF, WebP로 만들기')
    expect(p.onTogglePin).toHaveBeenCalledOnce()
    expect(p.onEdit).toHaveBeenCalledExactlyOnceWith('tags')
    expect(p.onAdd).toHaveBeenCalledOnce()
    expect(p.onCreateAnimation).toHaveBeenCalledOnce()
    expect(document.querySelector('[role=menu]')).toBeNull()
    expect(document.querySelector('[aria-label="영상 앨범에 추가"] svg')).not.toBeNull()
})

it('keeps individual file actions available and explains that tags belong to the group', async () => {
    const p = props()
    await mount(h(GalleryCardActions, { ...p, fileOnly: true }))
    expect(document.querySelector('[title="소속 묶음 태그 편집"]')).not.toBeNull()
    await clickButton('영상 앨범에 추가')
    await clickButton('영상 고정 해제')
    expect(p.onAdd).toHaveBeenCalledOnce()
    expect(p.onTogglePin).toHaveBeenCalledOnce()
    expect(document.querySelector('[aria-label="영상 즐겨찾기 추가"]')).toBeNull()
})

it('honors mutation locks and read-only clients for the direct actions', async () => {
    const p = props()
    const stop = await mount(h(GalleryCardActions, { ...p, mutationDisabled: true }))
    for (const label of ['영상 고정 해제', '영상 태그 편집', '영상 앨범에 추가', '영상 GIF, WebP로 만들기']) {
        expect(document.querySelector<HTMLButtonElement>(`[aria-label="${label}"]`)?.disabled).toBe(true)
    }
    await stop()
    await mount(h(GalleryCardActions, { ...p, readOnly: true }))
    expect(document.querySelector('[aria-label="영상 고정 해제"]')).toBeNull()
    expect(document.querySelector('[aria-label="영상 태그 편집"]')).toBeNull()
    expect(document.querySelector('[aria-label="영상 앨범에 추가"]')).toBeNull()
    await clickButton('영상 GIF, WebP로 만들기')
    expect(p.onCreateAnimation).toHaveBeenCalledOnce()
})
