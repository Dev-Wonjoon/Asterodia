// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ActionButton } from '@renderer/shared/components/ui/ActionButton'
import { Sidebar } from '@renderer/app/layouts/Sidebar'
import { GalleryCardActions } from '@renderer/features/gallery/GalleryCardActions'
import { cleanupReact, clickButton, mount } from './reactHarness'

vi.mock('@renderer/features/download/useDownloadViewModel', () => ({ useDownloadViewModel: () => ({ requestedIds: [] }) }))
const key = 'asterodia:action-display:v1'
beforeEach(() => { localStorage.clear() })
afterEach(cleanupReact)
const sidebar = () => h(Sidebar, { activeMenu: 'gallery', collapsed: false, onToggle: vi.fn(), onMenuClick: vi.fn() })

it('places the toggle beside the theme button and updates all actions without replacing their DOM', async () => {
    const click = vi.fn()
    await mount(h('div', null, sidebar(), h(ActionButton, { icon: h('svg'), label: '앨범에 추가', onClick: click }), h(ActionButton, { icon: h('svg'), label: '전체 추가', disabled: true })))
    const toggle = document.querySelector<HTMLButtonElement>('[aria-label="기능 버튼 설명 모드"]')!
    expect(toggle.previousElementSibling?.getAttribute('aria-label')).toMatch(/모드 전환/)
    const content = document.querySelector('[aria-label="앨범에 추가"] .action-content')!
    const icon = content.querySelector('.action-icon'), label = content.querySelector('.action-label')
    expect(content.getAttribute('data-display')).toBe('icons')
    await clickButton('기능 버튼 설명 모드')
    expect(toggle.getAttribute('aria-pressed')).toBe('true')
    expect([...document.querySelectorAll('.action-content')].every(node => node.getAttribute('data-display') === 'details')).toBe(true)
    expect(localStorage.getItem(key)).toBe('details')
    expect(document.querySelector('[role="status"]')?.textContent).toContain('설명 모드')
    expect(content.querySelector('.action-icon')).toBe(icon); expect(content.querySelector('.action-label')).toBe(label)
    expect(document.querySelector<HTMLButtonElement>('[aria-label="전체 추가"]')!.disabled).toBe(true)
    await clickButton('앨범에 추가'); expect(click).toHaveBeenCalledOnce()
    await clickButton('기능 버튼 설명 모드')
    expect(content.getAttribute('data-display')).toBe('icons')
    expect(content.getAttribute('aria-hidden')).toBe('true')
    expect(document.querySelector('[aria-label="앨범에 추가"]')!.getAttribute('title')).toBe('앨범에 추가')
})

it('restores the saved mode across mounts and responds to changes from another window', async () => {
    localStorage.setItem(key, 'details')
    const stop = await mount(h(ActionButton, { icon: h('svg'), label: '전체 추가' }))
    expect(document.querySelector('.action-content')!.getAttribute('data-display')).toBe('details')
    await stop()
    await mount(h(ActionButton, { icon: h('svg'), label: '전체 추가' }))
    await act(async () => {
        localStorage.setItem(key, 'icons')
        window.dispatchEvent(new StorageEvent('storage', { key, newValue: 'icons' }))
    })
    expect(document.querySelector('.action-content')!.getAttribute('data-display')).toBe('icons')
})

it('keeps the more-menu trigger icon-only in both modes and preserves keyboard navigation', async () => {
    await mount(h('div', null, sidebar(), h(GalleryCardActions, {
        title: '영상', isFavorite: false, disabled: false, mutationDisabled: false,
        onToggleFavorite: vi.fn(), onOpenFolder: vi.fn(), onEdit: vi.fn(), onUnregister: vi.fn()
    })))
    const more = document.querySelector<HTMLButtonElement>('[aria-label="영상 더보기"]')!
    expect(more.textContent).toBe('')
    expect(more.querySelector('svg')).not.toBeNull()
    await clickButton('기능 버튼 설명 모드')
    expect(more.textContent).toBe('')
    expect(more.querySelector('.action-label')).toBeNull()
    await clickButton('영상 더보기')
    expect(document.activeElement?.textContent).toContain('즐겨찾기 추가')
    await act(async () => document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })))
    expect(document.activeElement?.textContent).toContain('파일 위치 열기')
    await act(async () => document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(document.activeElement).toBe(more)
})
