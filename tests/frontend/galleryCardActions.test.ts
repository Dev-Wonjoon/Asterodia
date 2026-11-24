// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { GalleryCardActions } from '@renderer/features/gallery/GalleryCardActions'
import { GalleryCard } from '@renderer/features/gallery/GalleryCard'
import type { MediaSummaryDto } from '@contracts/media-v2'
import { cleanupReact, clickButton, mount } from './reactHarness'

afterEach(cleanupReact)

function props() {
    return { title: '영상', isFavorite: false, isPinned: true, disabled: false, mutationDisabled: false,
        onToggleFavorite: vi.fn(), onTogglePin: vi.fn(), onOpenFolder: vi.fn(), onEdit: vi.fn(),
        onUnregister: vi.fn(), onAdd: vi.fn(), onCreateAnimation: vi.fn() }
}

function cardActions(id = 1, overrides = {}) {
    const p = { ...props(), title: `영상 ${id}`, onView: vi.fn(), onDetails: vi.fn(), ...overrides }
    const onSelect = vi.fn()
    const media: MediaSummaryDto = { id, title: p.title, sourceKind: 'local', requestUrl: null, sourceTitle: null, customTitle: null, revision: 1, coverMode: 'auto', cover: null, fileCount: 1, isFavorite: false, createdAt: 0, updatedAt: null }
    const element = h(GalleryCard, { media, previewPath: null, selected: true, selectionKey: `media:${id}`, onSelect, onView: p.onView, onDetails: p.onDetails, children: h(GalleryCardActions, p) })
    return { p, onSelect, element }
}

async function contextMenu(target: Element, pointer = true) {
    const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, button: 2, clientX: 30, clientY: 40 })
    await act(async () => {
        if (pointer) target.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 2, pointerType: 'mouse' }))
        target.dispatchEvent(event)
    })
    return event
}

it('opens the existing card menu from a right click without selecting or viewing the card', async () => {
    const { element, p, onSelect } = cardActions()
    await mount(element)
    const card = document.querySelector('article')!, trigger = card.querySelector<HTMLButtonElement>('[data-card-menu]')!
    const event = await contextMenu(card.querySelector('[data-card-focus]')!)
    expect(event.defaultPrevented).toBe(true)
    expect(trigger.getAttribute('aria-expanded')).toBe('true')
    const menu = card.querySelector('[role=menu]')!
    expect(document.activeElement).toBe(menu.querySelector('button:not(:disabled)'))
    expect(card.getAttribute('data-selected')).toBe('true')
    expect(onSelect).not.toHaveBeenCalled(); expect(p.onView).not.toHaveBeenCalled(); expect(p.onDetails).not.toHaveBeenCalled()
    await act(async () => menu.querySelector<HTMLButtonElement>('button')!.click())
    expect(p.onView).toHaveBeenCalledOnce(); expect(card.querySelector('[role=menu]')).toBeNull()
})

it('reopens the same menu on repeated right clicks and closes the previous card menu', async () => {
    const first = cardActions(1), second = cardActions(2)
    await mount(h('div', null, first.element, second.element))
    const cards = [...document.querySelectorAll('article')]
    await contextMenu(cards[0]); await contextMenu(cards[0])
    expect(document.querySelectorAll('[role=menu]')).toHaveLength(1)
    expect(cards[0].querySelector('[role=menu]')).not.toBeNull()
    await contextMenu(cards[1])
    expect(document.querySelectorAll('[role=menu]')).toHaveLength(1)
    expect(cards[0].querySelector('[role=menu]')).toBeNull(); expect(cards[1].querySelector('[role=menu]')).not.toBeNull()
    expect(first.onSelect).not.toHaveBeenCalled(); expect(second.onSelect).not.toHaveBeenCalled()
})

it('shares the ellipsis toggle and closes other menus even without a preceding pointer event', async () => {
    await mount(h('div', null, cardActions(1).element, cardActions(2).element))
    const cards = [...document.querySelectorAll('article')]
    await contextMenu(cards[0], false); await contextMenu(cards[1], false)
    expect(document.querySelectorAll('[role=menu]')).toHaveLength(1)
    expect(cards[1].querySelector('[role=menu]')).not.toBeNull()
    const trigger = cards[1].querySelector<HTMLButtonElement>('[data-card-menu]')!
    await contextMenu(trigger)
    expect(trigger.getAttribute('aria-expanded')).toBe('true')
    await act(async () => trigger.click())
    expect(document.querySelector('[role=menu]')).toBeNull()
    await act(async () => trigger.click())
    expect(document.querySelector('[role=menu]')).not.toBeNull()
    await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })))
    expect(document.querySelector('[role=menu]')).toBeNull()
})

it.each([{ key: 'ContextMenu' }, { key: 'F10', shiftKey: true }])('opens the menu with the context-menu key gesture %o and restores focus on Escape', async gesture => {
    await mount(cardActions().element)
    const card = document.querySelector('article')!, preview = card.querySelector<HTMLButtonElement>('[data-card-focus]')!, trigger = card.querySelector('[data-card-menu]')!
    preview.focus()
    const key = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...gesture })
    await act(async () => preview.dispatchEvent(key))
    expect(key.defaultPrevented).toBe(true); expect(card.querySelector('[role=menu]')).not.toBeNull()
    await act(async () => document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    expect(card.querySelector('[role=menu]')).toBeNull(); expect(document.activeElement).toBe(trigger)
})

it('keeps mutation locks and the web client read-only menu when opened by right click', async () => {
    const locked = cardActions(1, { mutationDisabled: true })
    const stop = await mount(locked.element)
    await contextMenu(document.querySelector('article')!)
    const items = [...document.querySelectorAll<HTMLButtonElement>('[role=menuitem]')]
    expect(items.find(item => item.textContent === '즐겨찾기 추가')!.disabled).toBe(true)
    await stop()
    await mount(cardActions(2, { readOnly: true }).element)
    await contextMenu(document.querySelector('article')!)
    expect(document.querySelector('[role=menu]')).not.toBeNull()
    expect(document.querySelector('[role=menu]')!.textContent).not.toContain('등록 해제')
    expect(document.querySelector('[role=menu]')!.textContent).not.toContain('태그 편집')
})

it('leaves the native context menu available when the card menu is disabled or absent', async () => {
    const stop = await mount(cardActions(1, { disabled: true }).element)
    expect((await contextMenu(document.querySelector('article')!)).defaultPrevented).toBe(false)
    expect(document.querySelector('[role=menu]')).toBeNull()
    await stop()
    const { element } = cardActions()
    await mount(h(GalleryCard, { ...element.props, children: null }))
    expect((await contextMenu(document.querySelector('article')!)).defaultPrevented).toBe(false)
})

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
