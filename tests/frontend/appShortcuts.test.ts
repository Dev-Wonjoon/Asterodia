// @vitest-environment happy-dom
import { act, createElement as h, useEffect } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from '@renderer/app/App'
import { resetShortcuts } from '../../internal/webserver/web/shortcuts.js'
import { cleanupReact, mount } from './reactHarness'

const state = vi.hoisted(() => ({ allow: true, confirming: false, imports: vi.fn(), guard: vi.fn(), download: vi.fn() }))
vi.mock('@renderer/shared/hooks/useConfirm', () => ({ useConfirm: () => ({ isOpen: state.confirming }) }))
vi.mock('@renderer/app/hooks/useEngineSetup', () => ({ useEngineSetup: () => ({ checkingSetup: false, showSetup: false }) }))
vi.mock('@renderer/app/hooks/useGlobalPaste', () => ({ useGlobalPaste: () => {} }))
vi.mock('@renderer/features/download/useDownloadViewModel', () => ({ useDownloadViewModel: () => ({ startDownload: state.download }) }))
vi.mock('@renderer/features/settings/useAppUpdate', () => ({ AppUpdateProvider: ({ children }: any) => children }))
vi.mock('@renderer/features/settings/UpdateSettings', () => ({ UpdateNotice: () => null }))
vi.mock('@renderer/features/settings/components/EngineSetupModal', () => ({ EngineSetupModal: () => null }))
vi.mock('@renderer/app/providers/LeaveGuardProvider', () => ({ LeaveGuardProvider: ({ children, guard }: any) => {
    useEffect(() => { guard.current = () => { state.guard(); return state.allow }; return () => { guard.current = null } }, [guard])
    return children
} }))
vi.mock('@renderer/app/layouts/Sidebar', () => ({ Sidebar: ({ activeMenu, onMenuClick }: any) => h('nav', { 'data-keyboard-region': true },
    h('output', { 'data-active-menu': true }, activeMenu), h('button', { onClick: () => onMenuClick('favorites') }, 'favorites')) }))
vi.mock('@renderer/features/download/components/DownloadBar', () => ({ DownloadBar: () => h('input', { 'data-download-url': true }) }))
vi.mock('@renderer/features/gallery/GalleryPage', () => ({ GalleryPage: ({ importRequest, onImportHandled }: any) => {
    useEffect(() => { if (importRequest) { state.imports(); onImportHandled() } }, [importRequest])
    return h('button', null, 'gallery')
} }))
vi.mock('@renderer/features/download/DownloadPage', () => ({ DownloadPage: () => h('p', null, 'downloads') }))
vi.mock('@renderer/features/search/SearchPage', () => ({ SearchPage: () => h('input', { 'data-search-input': true }) }))
vi.mock('@renderer/features/favorites/FavoritesPage', () => ({ FavoritesPage: () => h('p', null, 'favorites') }))
vi.mock('@renderer/features/settings/SettingsPage', () => ({ SettingsPage: () => h('p', null, 'settings') }))
beforeEach(() => { resetShortcuts(); state.allow = true; state.confirming = false; vi.clearAllMocks() })
afterEach(async () => { await cleanupReact(); document.body.replaceChildren() })
async function key(value: string, options: KeyboardEventInit = {}) { await act(async () => { document.body.dispatchEvent(new KeyboardEvent('keydown', { key: value, bubbles: true, cancelable: true, ...options })) }) }
const active = () => document.querySelector('[data-active-menu]')?.textContent
it('switches pages, focuses search and download fields, and imports once across later navigation', async () => {
    await mount(h(App))
    await key('4', { ctrlKey: true }); expect(active()).toBe('favorites')
    await key('l', { altKey: true }); expect(active()).toBe('downloads'); expect(document.activeElement).toBe(document.querySelector('[data-download-url]'))
    await key('f', { altKey: true }); expect(active()).toBe('search'); expect(document.activeElement).toBe(document.querySelector('[data-search-input]'))
    await key('o', { altKey: true }); expect(active()).toBe('gallery'); expect(state.imports).toHaveBeenCalledOnce()
    await key('4', { ctrlKey: true }); await key('1', { ctrlKey: true }); expect(state.imports).toHaveBeenCalledOnce()
    await key(',', { altKey: true }); expect(active()).toBe('settings')
})
it('honors the same leave guard for keyboard navigation and focus commands', async () => {
    await mount(h(App)); state.allow = false
    await key('2', { ctrlKey: true }); expect(active()).toBe('gallery')
    await key('f', { altKey: true }); expect(active()).toBe('gallery'); expect(document.activeElement).not.toBe(document.querySelector('[data-search-input]'))
    await key('o', { altKey: true }); expect(state.imports).not.toHaveBeenCalled()
    expect(state.guard).toHaveBeenCalledTimes(2)
})
it('opens contextual shortcut help and blocks page changes while it is open', async () => {
    await mount(h(App)); await key('F1')
    expect(document.querySelector('dialog[open]')).not.toBeNull()
    await key('2', { ctrlKey: true }); expect(active()).toBe('gallery')
    expect(document.querySelector('dialog')?.textContent).toContain('Ctrl+1')
})
