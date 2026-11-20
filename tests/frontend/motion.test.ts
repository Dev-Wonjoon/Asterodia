// @vitest-environment happy-dom
import { act, createElement as h, useContext, useLayoutEffect, useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from '@renderer/app/App'
import { LeaveGuardContext } from '@renderer/shared/hooks/leaveGuardContext'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { cleanupReact, clickButton, mount } from './reactHarness'

const mocks = vi.hoisted(() => ({ allow: true, startDownload: vi.fn() }))
vi.mock('@renderer/shared/hooks/useConfirm', () => ({ useConfirm: () => ({ isOpen: false }) }))
vi.mock('@renderer/features/download/useDownloadViewModel', () => ({ useDownloadViewModel: () => ({ startDownload: mocks.startDownload }) }))
vi.mock('@renderer/app/hooks/useEngineSetup', () => ({ useEngineSetup: () => ({ showSetup: false }) }))
vi.mock('@renderer/app/hooks/useGlobalPaste', () => ({ useGlobalPaste: () => {} }))
vi.mock('@renderer/features/download/components/DownloadBar', () => ({ DownloadBar: () => null }))
vi.mock('@renderer/features/settings/components/EngineSetupModal', () => ({ EngineSetupModal: () => null }))
vi.mock('@renderer/app/layouts/Sidebar', () => ({ Sidebar: ({ onMenuClick, onToggle }: any) => h('nav', null,
    ...['gallery', 'downloads'].map(menu => h('button', { key: menu, onClick: () => onMenuClick(menu) }, 'nav-' + menu)),
    h('button', { onClick: onToggle }, 'collapse')) }))
vi.mock('@renderer/features/gallery/GalleryPage', () => ({ GalleryPage: () => {
    const guard = useContext(LeaveGuardContext)
    useLayoutEffect(() => {
        if (guard) guard.current = () => mocks.allow
        return () => { if (guard) guard.current = null }
    }, [guard])
    return h('div', null, 'gallery fixture')
} }))
vi.mock('@renderer/features/download/DownloadPage', () => ({ DownloadPage: () => h('div', null, 'download fixture') }))
vi.mock('@renderer/features/favorites/FavoritesPage', () => ({ FavoritesPage: () => null }))
vi.mock('@renderer/features/search/SearchPage', () => ({ SearchPage: () => null }))
vi.mock('@renderer/features/settings/SettingsPage', () => ({ SettingsPage: () => null }))

afterEach(cleanupReact)
beforeEach(() => { mocks.allow = true; mocks.startDownload.mockReset() })

it('keeps the page animation element mounted on ordinary updates and sidebar changes', async () => {
    let update!: () => void
    function Host() {
        const [, tick] = useState(0)
        update = () => tick(value => value + 1)
        return h(App)
    }
    await mount(h(Host))
    const page = document.querySelector('main > .motion-page')!
    expect(page.textContent).toContain('gallery fixture')
    await act(async () => update())
    expect(document.querySelector('main > .motion-page')).toBe(page)
    await clickButton('collapse')
    expect(document.querySelector('main > .motion-page')).toBe(page)
})

it('starts a new page surface only after allowed navigation and keeps same-page clicks stable', async () => {
    await mount(h(App))
    const initial = document.querySelector('main > .motion-page')!
    mocks.allow = false
    await clickButton('nav-downloads')
    expect(document.querySelector('main > .motion-page')).toBe(initial)
    mocks.allow = true
    await clickButton('nav-downloads')
    const next = document.querySelector('main > .motion-page')!
    expect(next).not.toBe(initial); expect(next.textContent).toContain('download fixture')
    await clickButton('nav-downloads')
    expect(document.querySelector('main > .motion-page')).toBe(next)
})

it('updates actual progress immediately on the same fill element without replaying a page entry', async () => {
    let update!: (value: number) => void
    function Host() {
        const [value, set] = useState(20); update = set
        return h('div', { className: 'motion-page' }, h(ProgressBar, { label: '진행률', value }))
    }
    await mount(h(Host))
    const surface = document.querySelector('.motion-page'), bar = document.querySelector('[role=progressbar]')!, fill = bar.firstElementChild as HTMLElement
    expect(fill.style.width).toBe('20%')
    await act(async () => update(80))
    expect(document.querySelector('.motion-page')).toBe(surface)
    expect(bar.firstElementChild).toBe(fill); expect(fill.style.width).toBe('80%')
    expect(bar.getAttribute('aria-valuenow')).toBe('80')
    await act(async () => update(100))
    expect(bar.getAttribute('aria-valuenow')).toBe('100')
})
