// @vitest-environment happy-dom
import { act, createElement as h, useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useShortcutCommands } from '@renderer/shared/hooks/useShortcutCommands'
import { ShortcutSettings } from '@renderer/features/settings/ShortcutSettings'
import { KeyboardDialog } from '@renderer/app/KeyboardDialog'
import { setLanguage } from '@renderer/shared/i18n'
import { availableCommands, eventChord, matchShortcut, registerCommands, resetShortcuts, setShortcut, shortcutKeys, shortcutStorageKey } from '../../internal/webserver/web/shortcuts.js'
import { cleanupReact, mount } from './reactHarness'

beforeEach(() => { localStorage.clear(); resetShortcuts(); setLanguage('ko') })
afterEach(async () => { await cleanupReact(); document.body.replaceChildren(); vi.restoreAllMocks(); vi.useRealTimers(); resetShortcuts() })
function key(target: EventTarget, value: string, options: KeyboardEventInit = {}) {
    const event = new KeyboardEvent('keydown', { key: value, bubbles: true, cancelable: true, ...options })
    target.dispatchEvent(event); return event
}
it('uses the agreed app modifiers and physical Latin keys while Korean input is enabled', () => {
    expect(matchShortcut(new KeyboardEvent('keydown', { key: '1', ctrlKey: true }), 'app')?.id).toBe('app.gallery')
    expect(matchShortcut(new KeyboardEvent('keydown', { key: '1', altKey: true }), 'app')).toBeUndefined()
    expect(matchShortcut(new KeyboardEvent('keydown', { key: 'ㄹ', code: 'KeyF', altKey: true }), 'app')?.id).toBe('app.find')
    expect(eventChord(new KeyboardEvent('keydown', { key: ',', code: 'Comma', altKey: true }))).toBe('Alt+,')
    expect(matchShortcut(new KeyboardEvent('keydown', { key: 'f', isComposing: true }), 'gallery')).toBeUndefined()
    expect(matchShortcut(new KeyboardEvent('keydown', { key: 'f', repeat: true }), 'gallery')).toBeUndefined()
    expect(matchShortcut(new KeyboardEvent('keydown', { key: 'ArrowLeft', repeat: true }), 'gallery')?.id).toBe('gallery.left')
})
it('saves remapping and disabling, detects collisions in overlapping scopes, and protects navigation keys', () => {
    expect(setShortcut('app.find', 'Alt+Q')).toBeNull()
    expect(JSON.parse(localStorage.getItem(shortcutStorageKey)!)['app.find']).toBe('Alt+Q')
    expect(setShortcut('gallery.tags', 'Alt+Q')).toBe('conflict')
    expect(setShortcut('gallery.tags', 'F')).toBe('conflict')
    expect(setShortcut('gallery.tags', 'Shift+ArrowRight')).toBe('conflict')
    expect(setShortcut('viewer.next', 'L')).toBe('conflict')
    expect(setShortcut('gallery.tags', 'Tab')).toBe('reserved')
    expect(setShortcut('gallery.tags', 'Escape')).toBe('reserved')
    expect(setShortcut('gallery.favorite', '')).toBeNull()
    expect(matchShortcut(new KeyboardEvent('keydown', { key: 'f' }), 'gallery')).toBeUndefined()
    expect(matchShortcut(new KeyboardEvent('keydown', { key: 'Control', ctrlKey: true }), 'gallery')).toBeUndefined()
    resetShortcuts(); expect(shortcutKeys('app.find')).toBe('Alt+F')
})
it('keeps settings usable if preference storage is unavailable', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw Error('blocked') })
    expect(setShortcut('app.find', 'Alt+Q')).toBeNull()
    expect(shortcutKeys('app.find')).toBe('Alt+Q')
})
it('does not intercept editable fields, composition, handled events, or background actions under a modal', async () => {
    const favorite = vi.fn(), gallery = vi.fn()
    function Harness() {
        useShortcutCommands('gallery', { 'gallery.favorite': { run: favorite } })
        useShortcutCommands('app', { 'app.gallery': { run: gallery } })
        return h('input')
    }
    await mount(h(Harness))
    const input = document.querySelector('input')!
    key(input, 'f'); expect(favorite).not.toHaveBeenCalled()
    key(input, '1', { ctrlKey: true }); expect(gallery).toHaveBeenCalledOnce()
    key(document.body, 'f', { isComposing: true }); expect(favorite).not.toHaveBeenCalled()
    const modal = document.createElement('dialog'); modal.showModal(); document.body.append(modal)
    key(modal, '1', { ctrlKey: true }); key(modal, 'f'); expect(gallery).toHaveBeenCalledOnce(); expect(favorite).not.toHaveBeenCalled()
    modal.remove(); key(document.body, 'f'); expect(favorite).toHaveBeenCalledOnce()
})
it('captures shortcut settings, shows conflict messages in English, and disables with Backspace', async () => {
    await mount(h(ShortcutSettings))
    const input = document.querySelector<HTMLInputElement>('input[aria-label="미디어 검색 · 앱 공통"]')!
    await act(async () => { key(input, 'q', { code: 'KeyQ', altKey: true }) })
    expect(input.value).toBe('Alt+Q')
    await act(async () => { setLanguage('en'); key(input, 'k', { altKey: true }) })
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('already assigned')
    await act(async () => { key(input, 'Backspace') })
    expect(input.value).toBe('Disabled')
    expect(document.querySelector('[role="alert"]')).toBeNull()
})
it('runs a searched command after closing and restores focus to the invoking control', async () => {
    vi.useFakeTimers()
    const run = vi.fn()
    function Harness() {
        const [open, setOpen] = useState(false)
        useShortcutCommands('app', { 'app.gallery': { run } })
        return h('div', null, h('button', { onClick: () => setOpen(true) }, 'open'), open && h(KeyboardDialog, { mode: 'commands', onClose: () => setOpen(false) }))
    }
    await mount(h(Harness))
    const trigger = document.querySelector('button')!; trigger.focus()
    await act(async () => trigger.click())
    const input = document.querySelector('dialog input')!
    expect(document.activeElement).toBe(input)
    await act(async () => { key(input, 'Enter'); await vi.runAllTimersAsync() })
    expect(run).toHaveBeenCalledOnce(); expect(document.querySelector('dialog')).toBeNull(); expect(document.activeElement).toBe(trigger)
})
it('removes unmounted commands and excludes disabled commands from search', () => {
    const remove = registerCommands([{ id: 'gallery.tags', enabled: () => false, run: vi.fn() }])
    expect(availableCommands().some(item => item.id === 'gallery.tags')).toBe(false)
    remove(); expect(availableCommands()).toHaveLength(0)
})
