// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from '../../frontend/node_modules/typescript/lib/typescript.js'
import { catalogs } from '../../internal/webserver/web/translations.js'
import { shortcuts } from '../../internal/webserver/web/shortcuts.js'
import { languageKey, resolveLanguage, subscribeLanguage } from '../../internal/webserver/web/i18n.js'
import { getLanguage, getLanguagePreference, getLocale, setLanguage, translate, translateMessage, translateError, formatNumber, formatDate } from '@renderer/shared/i18n'
import { LanguageSelect } from '@renderer/shared/i18n/LanguageSelect'
import { SearchPage } from '@renderer/features/search/SearchPage'
import { GalleryCard } from '@renderer/features/gallery/GalleryCard'
import { mountVideoPlayer } from '../../internal/webserver/web/player.js'
import { mountMediaEditor } from '../../internal/webserver/web/editor.js'
import { mountMediaViewer } from '../../internal/webserver/web/viewer.js'
import { cleanupReact, mount } from './reactHarness'

vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ media: {
    suggest: async () => ({ ok: true, data: [] }),
    searchSuggest: async () => ({ ok: true, data: { data: [], hasNextPage: false, revision: 1, indexed: true } }),
} }) }))
let widget: { destroy(): void } | undefined
beforeEach(() => { localStorage.clear(); setLanguage('ko') })
afterEach(async () => {
    widget?.destroy(); widget = undefined
    await cleanupReact(); document.body.replaceChildren()
    vi.restoreAllMocks(); vi.unstubAllGlobals(); setLanguage('ko')
})

it('detects the primary system language and falls back to English for unsupported languages', () => {
    expect(resolveLanguage('system', ['ko-KR', 'en-US'])).toBe('ko')
    expect(resolveLanguage('system', ['KO'])).toBe('ko')
    expect(resolveLanguage('system', ['en-GB', 'ko-KR'])).toBe('en')
    expect(resolveLanguage('system', ['ja-JP', 'ko-KR'])).toBe('en')
    expect(resolveLanguage('ko', ['fr-FR'])).toBe('ko')
    expect(resolveLanguage('en', ['ko-KR'])).toBe('en')
    expect(resolveLanguage('system', [])).toBe('en')
    localStorage.setItem(languageKey, 'invalid')
    expect(getLanguagePreference()).toBe('system')
})

it('saves language choices, updates document language, and returns to system detection', () => {
    setLanguage('en')
    expect(localStorage.getItem(languageKey)).toBe('en')
    expect(getLanguage()).toBe('en'); expect(document.documentElement.lang).toBe('en')
    expect(translate("navigation.gallery")).toBe('Gallery')
    setLanguage('system')
    expect(getLanguagePreference()).toBe('system')
    expect(getLanguage()).toBe('ko'); expect(document.documentElement.lang).toBe('ko')
})

it('keeps the chosen language usable when preference storage is blocked', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw Error('blocked') })
    setLanguage('en')
    expect(getLanguage()).toBe('en'); expect(translate("navigation.settings")).toBe('Settings')
    setLanguage('ko')
    expect(getLanguage()).toBe('ko')
})

it('responds to other tabs and system language changes, and releases its listeners', () => {
    const listener = vi.fn(), stop = subscribeLanguage(listener)
    localStorage.setItem(languageKey, 'en')
    window.dispatchEvent(new StorageEvent('storage', { key: languageKey }))
    expect(listener).toHaveBeenCalledOnce(); expect(document.documentElement.lang).toBe('en')
    setLanguage('system')
    vi.stubGlobal('navigator', { languages: ['fr-FR'], language: 'fr-FR' })
    window.dispatchEvent(new Event('languagechange'))
    expect(document.documentElement.lang).toBe('en')
    stop(); const count = listener.mock.calls.length
    window.dispatchEvent(new StorageEvent('storage', { key: languageKey }))
    expect(listener).toHaveBeenCalledTimes(count)
})

it('preserves interpolation values and formats dates and numbers in the selected locale', () => {
    setLanguage('en')
    expect(translate("download.engine", { engine: '{1} <script>' })).toBe('Engine: {1} <script>')
    expect(translate('unknown key')).toBe('unknown key')
    expect(translate('__proto__')).toBe('__proto__')
    expect(formatNumber(1234567)).toBe('1,234,567')
    expect(getLocale()).toBe('en-US')
    expect(formatDate(0, { timeZone: 'UTC', year: 'numeric', month: 'long', day: 'numeric' })).toBe('January 1, 1970')
    setLanguage('ko'); expect(getLocale()).toBe('ko-KR')
    expect(formatDate(0, { timeZone: 'UTC', year: 'numeric', month: 'long', day: 'numeric' })).toContain('1970년')
})

it('uses English if a translation is missing from the selected catalog', () => {
    const ko = catalogs.ko as Record<string, string>
    const original = ko["navigation.search"]
    delete ko["navigation.search"]
    try { expect(translate("navigation.search")).toBe('Search') }
    finally { ko["navigation.search"] = original }
})

it('retranslates messages already held in state and supplies English guidance for backend error codes', () => {
    const message = translate("settings.addedCollectionsAndFiles", { collections: 2, files: 3 })
    setLanguage('en')
    expect(translateMessage(message)).toBe('Added 2 collections and 3 files.')
    expect(translateError({ code: 'REVISION_CONFLICT', message: '서버의 별도 충돌 안내' })).toContain('Refresh')
    expect(translateError({ code: 'NEW_ERROR', message: '아직 번역하지 않은 오류' })).toContain('NEW_ERROR')
    expect(translateError({ code: 'ENGINE_ERROR', message: 'HTTP 403: forbidden' })).toBe('HTTP 403: forbidden')
    const english = translateMessage(message)
    setLanguage('ko'); expect(translateMessage(english)).toBe(message)
})

it('has complete catalogs, matching interpolation parameters, and no missing UI keys', () => {
    const ko = catalogs.ko as Record<string, string>, en = catalogs.en as Record<string, string>
    const parameters = (text: string) => [...text.matchAll(/\{(\w+)\}/g)].map(match => match[1]).sort()
    expect(Object.keys(ko).sort()).toEqual(Object.keys(en).sort())
    for (const [key, value] of Object.entries(en)) {
        expect(key).toMatch(/^[a-zA-Z][a-zA-Z0-9]*(\.[a-zA-Z0-9]+)+$/)
        expect(ko[key].trim(), key).not.toBe('')
        expect(value.trim(), key).not.toBe('')
        expect(parameters(value), key).toEqual(parameters(ko[key]))
        expect(parameters(value).every(name => !/^\d+$/.test(name)), key).toBe(true)
    }
    const files: string[] = []
    function collect(directory: string) {
        for (const entry of readdirSync(directory, { withFileTypes: true })) {
            const filename = path.join(directory, entry.name)
            if (entry.isDirectory()) collect(filename)
            else if (/\.(tsx?|js)$/.test(filename)) files.push(filename)
        }
    }
    const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
    collect(path.join(root, 'frontend/src'))
    files.push(...['editor', 'player', 'viewer', 'viewer-viewport', 'viewer-picker', 'app', 'search', 'shortcuts', 'i18n', 'output-controls'].map(name => path.join(root, `internal/webserver/web/${name}.js`)))
    for (const language of ['ko', 'en']) {
        const filename = path.join(root, `internal/webserver/web/langs/${language}.json`)
        const source = ts.parseJsonText(filename, readFileSync(filename, 'utf8'))
        function validate(node: ts.Node) {
            if (ts.isObjectLiteralExpression(node)) {
                const keys = node.properties.map(property => property.name?.getText(source))
                expect(new Set(keys).size, `${language}: duplicate JSON key`).toBe(keys.length)
            }
            ts.forEachChild(node, validate)
        }
        validate(source)
    }
    for (const shortcut of shortcuts) expect(en[shortcut.label], shortcut.id).toBeDefined()
    for (const filename of files) {
        const source = ts.createSourceFile(filename, readFileSync(filename, 'utf8'), ts.ScriptTarget.Latest, true)
        function visit(node: ts.Node) {
            if (ts.isCallExpression(node) && node.expression.getText(source) === 'translate' && node.arguments[0] && ts.isStringLiteral(node.arguments[0])) {
                expect(en[node.arguments[0].text], `${filename}: ${node.arguments[0].text}`).toBeDefined()
            }
            ts.forEachChild(node, visit)
        }
        visit(source)
    }
})

it('renders structured application messages in the current language without translating user values', () => {
    const message = { key: 'gallery.album.add', values: {} }
    expect(translateMessage(message)).toBe('앨범에 추가')
    setLanguage('en')
    expect(translateMessage(message)).toBe('Add to album')
    expect(translate('download.engine', { engine: '갤러리 {engine}' })).toBe('Engine: 갤러리 {engine}')
})

it('switches real search controls without replacing input nodes or losing a draft', async () => {
    await mount(h('div', null, h(LanguageSelect), h(SearchPage, { Gallery: () => null })))
    const input = document.querySelector<HTMLInputElement>('input[aria-label="태그 추가"]')!
    await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '내 태그 {0}')
        input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    const selector = document.querySelector<HTMLSelectElement>('select[aria-label="언어"]')!
    await act(async () => { selector.value = 'en'; selector.dispatchEvent(new Event('change', { bubbles: true })) })
    expect(document.querySelector('input[aria-label="Add tags"]')).toBe(input)
    expect(input.value).toBe('내 태그 {0}')
    expect(selector.getAttribute('aria-label')).toBe('Language')
    expect(document.documentElement.lang).toBe('en')
})

it('keeps user media titles intact even when they equal a translation key', async () => {
    const media = { id: 1, title: '갤러리', fileCount: 1, createdAt: 0, sourceKind: 'local', cover: null } as any
    await mount(h(GalleryCard, { media, previewPath: null, onView: vi.fn(), onDetails: vi.fn(), children: null }))
    await act(async () => setLanguage('en'))
    expect(document.querySelector('h2')!.textContent).toBe('갤러리')
    expect(document.body.textContent).toContain('Local folder')
})

it('updates an open player while preserving video, playback position, and volume', () => {
    const player = mountVideoPlayer(document.body, { src: '/fixture.mp4', title: '갤러리' }); widget = player
    player.video.currentTime = 24; player.video.volume = 0.4
    const node = player.video
    setLanguage('en')
    expect(document.querySelector('button[aria-label="Player settings"]')).not.toBeNull()
    expect(document.querySelector('.sbp-jumps button')!.textContent).toBe('-5min')
    expect(player.video).toBe(node); expect(node.currentTime).toBe(24); expect(node.volume).toBe(0.4)
    expect(player.root.getAttribute('aria-label')).toBe('갤러리 player')
    setLanguage('ko'); expect(document.querySelector('button[aria-label="플레이어 설정"]')).not.toBeNull()
})

it('updates an open viewer without changing its current file, title, or playback options', async () => {
    const viewer = mountMediaViewer({ mediaId: 1,
        getSequence: async () => ({ ids: [1, 2], title: '갤러리' }),
        getFile: async (id: number) => ({ id, kind: 'image', title: '설정' }),
        contentURL: () => '/fixture.png', previewURL: () => null, thumbnailURL: () => null,
    }); widget = viewer
    for (let i = 0; i < 8; i++) await Promise.resolve()
    const checkbox = document.querySelector<HTMLInputElement>('.sbv-options input')!
    checkbox.checked = true; checkbox.dispatchEvent(new Event('change'))
    const image = document.querySelector('.sbv-stage img')
    setLanguage('en')
    expect(viewer.dialog.querySelector('h2')!.textContent).toBe('갤러리')
    expect(document.querySelector('.sbv-stage img')).toBe(image)
    expect(document.querySelector('.sbv-options input')).toBe(checkbox)
    expect(checkbox.checked).toBe(true)
    expect(document.querySelector('.sbv-options label')!.textContent).toBe('Auto next')
    expect(viewer.currentFile.id).toBe(1)
})

it('updates an open editor while keeping controls, user titles, and edited values', async () => {
    const host = document.createElement('div'); document.body.append(host)
    const editor = mountMediaEditor(host, {
        file: { id: 1, title: '갤러리', duration: 10, src: '/fixture.mp4' },
        api: { list: async () => ({ ok: true, data: { sessions: [], jobs: [] } }) },
    }); widget = editor
    document.querySelector<HTMLButtonElement>('.sbe-format-choice')!.click()
    for (let i = 0; i < 6; i++) await Promise.resolve()
    const input = document.querySelector<HTMLInputElement>('input[type="number"]')!
    input.value = '2.5'
    setLanguage('en')
    expect(document.querySelector('dialog')!.getAttribute('aria-label')).toBe('Create GIF or trim video')
    expect(document.querySelector('.sbe-source-surface h3')!.textContent).toBe('갤러리')
    expect(document.querySelector('input[type="number"]')).toBe(input)
    expect(input.value).toBe('2.5')
    expect(document.body.textContent).not.toContain('() =>')
    expect(document.querySelector('.sbe-trim-range')!.getAttribute('aria-label')).toBe('Original range selection')
    expect([...document.querySelectorAll('[role="tab"]')].map(tab => tab.textContent)).toEqual(['Edit', 'Drafts', 'Export jobs'])
    expect(document.querySelector('.sbe-sample-controls button')!.textContent).toBe('Create preview')
    setLanguage('ko')
    expect(input.isConnected).toBe(true)
    expect(document.body.textContent).toContain('시작 시간(초)')
})

it('translates the format chooser while preserving its buttons, focus, and user video title', () => {
    const host = document.createElement('div'); document.body.append(host)
    widget = mountMediaEditor(host, { file: { id: 1, title: '갤러리', kind: 'video', src: '/source.mp4' }, api: { list: async () => ({ ok: true, data: { sessions: [], jobs: [] } }) } })
    const choice = document.querySelector<HTMLButtonElement>('.sbe-format-choice')!
    expect(choice.textContent).toBe('GIF로 만들기'); expect(document.activeElement).toBe(choice)
    setLanguage('en')
    expect(document.querySelector('.sbe-format-choice')).toBe(choice); expect(choice.textContent).toBe('Create GIF')
    expect(document.querySelector('.sbe-format-source')?.textContent).toBe('갤러리')
    expect(document.activeElement).toBe(choice)
})
