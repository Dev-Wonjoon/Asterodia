// @vitest-environment happy-dom
import { act, createElement as h, useContext, useLayoutEffect, useRef, useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from '@renderer/app/App'
import { LeaveGuardContext } from '@renderer/shared/hooks/leaveGuardContext'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { useGalleryExpansionMotion } from '@renderer/features/gallery/useGalleryExpansionMotion'
import type { MediaSummaryDto } from '@contracts/media-v2'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

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

afterEach(async () => { await cleanupReact(); vi.restoreAllMocks() })
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

const collapsedRows = [{ id: 1 }, { id: 2 }, { id: 3 }] as MediaSummaryDto[]
const expandedRows = [{ id: 1, galleryFile: { id: 11 } }, { id: 1, galleryFile: { id: 12 } }, { id: 1, galleryFile: { id: 13 } }, { id: 2 }, { id: 3 }] as MediaSummaryDto[]

async function galleryMotionHarness() {
    const effects: { element: HTMLElement; frames: Keyframe[]; options: KeyframeAnimationOptions; cancel: ReturnType<typeof vi.fn>; finish(): void }[] = []
    const animate = vi.spyOn(HTMLElement.prototype, 'animate').mockImplementation(function (this: HTMLElement, frames, options) {
        const completion = deferred<void>()
        const cancel = vi.fn(() => completion.reject(new Error('cancelled')))
        effects.push({ element: this, frames: frames as Keyframe[], options: options as KeyframeAnimationOptions, cancel, finish: () => completion.resolve() })
        return { cancel, finished: completion.promise } as unknown as Animation
    })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
        if (this.dataset.motionRoot) return new DOMRect(0, 0, 400, 500)
        const grid = this.dataset.motionGrid ? this : this.parentElement
        const top = Number(grid?.dataset.top ?? 0)
        if (this.dataset.motionGrid) return new DOMRect(0, top, Number(this.dataset.width ?? 400), 600)
        return new DOMRect(Number(this.dataset.x ?? 0), top + Number(this.dataset.y ?? 0), 180, 140)
    })
    let begin!: () => () => void
    let update!: (value: Partial<{ rows: MediaSummaryDto[]; refreshing: boolean; scope: string; top: number; width: number }>) => void
    function Host() {
        const [state, set] = useState({ rows: collapsedRows, refreshing: false, scope: 'gallery', top: 0, width: 400 })
        const root = useRef<HTMLDivElement>(null), grid = useRef<HTMLDivElement>(null)
        const capture = useGalleryExpansionMotion(grid, root, state.rows, state.refreshing, state.scope)
        begin = () => capture(1)
        update = value => set(current => ({ ...current, ...value }))
        return h('div', { ref: root, 'data-motion-root': 'yes' },
            h('div', { ref: grid, 'data-motion-grid': 'yes', 'data-top': state.top, 'data-width': state.width }, ...state.rows.map((row, index) =>
                h('article', { key: `${row.id}:${row.galleryFile?.id ?? 'group'}`, 'data-album-id': row.id,
                    'data-selection-key': row.galleryFile ? `file:${row.galleryFile.id}` : `media:${row.id}`,
                    'data-x': index % 2 * 200, 'data-y': Math.floor(index / 2) * 160 }, row.galleryFile ? `파일 ${row.galleryFile.id}` : `묶음 ${row.id}`))))
    }
    const unmount = await mount(h(Host))
    const change = async (value: Parameters<typeof update>[0]) => { await act(async () => update(value)) }
    return { effects, animate, begin: () => begin(), change, unmount }
}

it('animates only explicit album toggles after the asynchronous rows arrive, in both directions', async () => {
    const motion = await galleryMotionHarness()
    await motion.change({ rows: [...collapsedRows] })
    expect(motion.effects).toHaveLength(0)
    motion.begin()
    await motion.change({ refreshing: true })
    expect(motion.effects).toHaveLength(0)
    await motion.change({ rows: expandedRows, refreshing: false })
    const entering = motion.effects.filter(effect => effect.frames[0].opacity === 0)
    expect(entering).toHaveLength(3)
    expect(motion.effects.find(effect => effect.element.dataset.albumId === '2')?.frames[0].transform).toBe('translate(0px, -160px)')
    expect(motion.effects.find(effect => effect.element.dataset.albumId === '2')?.options.duration).toBe(220)
    await act(async () => { motion.effects.forEach(effect => effect.finish()) })
    expect(document.querySelector('.motion-gallery-entering')).toBeNull()
    const count = motion.effects.length
    motion.begin()
    await motion.change({ refreshing: true })
    await motion.change({ rows: collapsedRows, refreshing: false })
    const closing = motion.effects.slice(count)
    expect(closing.filter(effect => effect.frames[0].opacity === 0)).toHaveLength(1)
    expect(closing.find(effect => effect.element.dataset.albumId === '2')?.frames[0].transform).toBe('translate(0px, 160px)')
})

it('does not replay an abandoned album capture on a failed refresh or a changed search scope', async () => {
    const motion = await galleryMotionHarness()
    motion.begin()
    await motion.change({ refreshing: true })
    await motion.change({ refreshing: false })
    await motion.change({ rows: expandedRows })
    expect(motion.effects).toHaveLength(0)
    motion.begin()
    await motion.change({ refreshing: true })
    await motion.change({ rows: collapsedRows, refreshing: false, scope: 'search' })
    expect(motion.effects).toHaveLength(0)
    const cancel = motion.begin()
    cancel()
    await motion.change({ rows: expandedRows })
    expect(motion.effects).toHaveLength(0)
})

it('bounds animation work to visible files and cards moving out of the viewport in large albums', async () => {
    const motion = await galleryMotionHarness()
    motion.begin()
    const files = Array.from({ length: 200 }, (_, index) => ({ id: 1, galleryFile: { id: 100 + index } })) as MediaSummaryDto[]
    await motion.change({ rows: [...files, ...collapsedRows.slice(1)] })
    expect(document.querySelectorAll('article')).toHaveLength(202)
    expect(motion.effects.filter(effect => effect.frames[0].opacity === 0)).toHaveLength(8)
    expect(motion.effects.filter(effect => effect.element.dataset.albumId !== '1')).toHaveLength(2)
})

it('keeps expansion animated when a scrollbar narrows the grid inside the same scroll container', async () => {
    const motion = await galleryMotionHarness()
    const root = document.querySelector<HTMLElement>('[data-motion-root]')!
    expect(root.classList.contains('motion-gallery-scroll')).toBe(true)
    motion.begin()
    await motion.change({ refreshing: true })
    await motion.change({ rows: expandedRows, refreshing: false, width: 384 })
    expect(motion.effects.filter(effect => effect.frames[0].opacity === 0)).toHaveLength(3)
    expect(motion.effects.some(effect => effect.element.dataset.albumId === '2')).toBe(true)
    await motion.unmount()
    expect(root.classList.contains('motion-gallery-scroll')).toBe(false)
})

it('preserves the motion duration when production CSS converts milliseconds to seconds', async () => {
    const motion = await galleryMotionHarness()
    document.querySelector<HTMLElement>('[data-motion-grid]')!.style.setProperty('--motion-gallery', '.22s')
    motion.begin()
    await motion.change({ rows: expandedRows })
    expect(motion.effects.filter(effect => effect.frames[0].opacity === 0).every(effect => effect.options.duration === 180)).toBe(true)
    expect(motion.effects.find(effect => effect.element.dataset.albumId === '2')?.options.duration).toBe(220)
})

it('ignores scrolling during file loading and cleans up its own effects on repeated toggles and unmount', async () => {
    const motion = await galleryMotionHarness()
    motion.begin()
    await motion.change({ refreshing: true, top: -80 })
    await motion.change({ rows: expandedRows, refreshing: false })
    expect(motion.effects.find(effect => effect.element.dataset.albumId === '2')?.frames[0].transform).toBe('translate(0px, -160px)')
    const first = [...motion.effects]
    motion.begin()
    expect(first.every(effect => effect.cancel.mock.calls.length === 1)).toBe(true)
    await motion.change({ rows: collapsedRows })
    const last = motion.effects.slice(first.length)
    expect(last.length).toBeGreaterThan(0)
    await motion.unmount()
    expect(last.every(effect => effect.cancel.mock.calls.length === 1)).toBe(true)
})

it('still updates the gallery when a WebView cannot create an animation', async () => {
    const motion = await galleryMotionHarness()
    motion.animate.mockImplementation(() => { throw new Error('unsupported animation') })
    motion.begin()
    await motion.change({ rows: expandedRows })
    expect(document.querySelectorAll('article')).toHaveLength(5)
    expect(document.querySelector('.motion-gallery-entering,.motion-gallery-moving')).toBeNull()
})
