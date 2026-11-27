// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CookieSettings } from '@renderer/features/settings/CookieSettings'
import type { CookieState } from '@contracts/cookie-api'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ list: vi.fn(), importFile: vi.fn(), deleteEntry: vi.fn(), deleteSite: vi.fn(), setEnabled: vi.fn() }))
const confirm = vi.hoisted(() => vi.fn())
const toast = vi.hoisted(() => vi.fn())
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ cookies: api }) }))
vi.mock('@renderer/shared/hooks/useConfirm', () => ({ useConfirm: () => ({ confirm }) }))
vi.mock('@renderer/shared/hooks/useToast', () => ({ useToast: () => ({ showToast: toast }) }))

function state(): CookieState {
    return { warning: '', groups: [{ site: 'berriz.in', count: 1, active: 1, enabled: true, entries: [{ id: 'cookie-id', domain: 'berriz.in', subdomains: true, path: '/', name: 'session', secure: true, httpOnly: true, expires: 0, expired: false, source: 'file:cookies.txt', ...{ value: 'must-never-render' } }] }] }
}
beforeEach(() => { vi.resetAllMocks(); api.list.mockResolvedValue(state()); confirm.mockResolvedValue(true) })
afterEach(cleanupReact)

describe('cookie management', () => {
    it('shows grouped metadata and session expiry without exposing values', async () => {
        await mount(h(CookieSettings))
        expect(document.body.textContent).toContain('berriz.in')
        expect(document.body.textContent).toContain('session')
        expect(document.body.textContent).toContain('세션 쿠키')
        expect(document.body.textContent).toContain('파일: cookies.txt')
        expect(document.body.textContent).not.toContain('must-never-render')
    })
    it('imports and handles dialog cancellation while blocking overlapping changes', async () => {
        await mount(h(CookieSettings))
        const pending = deferred<CookieState | null>()
        api.importFile.mockReturnValueOnce(pending.promise)
        await clickButton('쿠키 파일 가져오기')
        expect([...document.querySelectorAll('button')].find(button => button.textContent === '쿠키 파일 가져오기')!.disabled).toBe(true)
        const next = state(); next.groups[0].count = 2
        await act(async () => pending.resolve(next))
        expect(document.body.textContent).toContain('전체 2개')
        api.importFile.mockResolvedValueOnce(null)
        await clickButton('쿠키 파일 가져오기')
        expect(document.body.textContent).toContain('전체 2개')
    })
    it('deletes only the confirmed entry and refreshes the list', async () => {
        await mount(h(CookieSettings))
        const label = 'berriz.in/의 session 쿠키 삭제'
        confirm.mockResolvedValueOnce(false)
        await clickButton(label)
        expect(api.deleteEntry).not.toHaveBeenCalled()
        api.deleteEntry.mockResolvedValue({ groups: [], warning: '' })
        await clickButton(label)
        expect(api.deleteEntry).toHaveBeenCalledExactlyOnceWith('cookie-id')
        expect(document.querySelector('table')).toBeNull()
    })
    it('deletes a site with its exclusion and can explicitly enable it again', async () => {
        await mount(h(CookieSettings))
        const next = state(); next.groups[0] = { ...next.groups[0], count: 0, active: 0, enabled: false, entries: [] }
        api.deleteSite.mockResolvedValue(next)
        await clickButton('사이트 쿠키 전체 삭제')
        expect(api.deleteSite).toHaveBeenCalledExactlyOnceWith('berriz.in')
        expect(document.body.textContent).toContain('사용 안 함')
        api.setEnabled.mockResolvedValue(state())
        const toggle = document.querySelector<HTMLInputElement>('input[type="checkbox"]')!
        await act(async () => toggle.click())
        expect(api.setEnabled).toHaveBeenCalledExactlyOnceWith('berriz.in', true)
        expect(document.body.textContent).toContain('사용 중')
    })
    it('supports excluding a site before its browser cookies have been read', async () => {
        api.list.mockResolvedValue({ groups: [], warning: '' })
        await mount(h(CookieSettings))
        const next = state(); next.groups[0] = { ...next.groups[0], count: 0, active: 0, enabled: false, entries: [] }
        api.setEnabled.mockResolvedValue(next)
        const input = document.querySelector<HTMLInputElement>('input[placeholder="example.com"]')!
        await act(async () => { const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!; setter.call(input, 'berriz.in'); input.dispatchEvent(new Event('input', { bubbles: true })) })
        await act(async () => document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
        expect(api.setEnabled).toHaveBeenCalledExactlyOnceWith('berriz.in', false)
        expect(document.body.textContent).toContain('사용 안 함')
    })
    it('keeps the previous state on failure and reports legacy import failures', async () => {
        const initial = state(); initial.warning = 'legacy-import-failed'; api.list.mockResolvedValue(initial)
        await mount(h(CookieSettings))
        expect(document.querySelector('[role="alert"]')?.textContent).toContain('다시 가져오세요')
        api.deleteEntry.mockRejectedValue(new Error('failed'))
        await clickButton('berriz.in/의 session 쿠키 삭제')
        expect(document.body.textContent).toContain('session')
        expect(toast).toHaveBeenCalledWith(expect.any(String), 'error')
    })
    it('shows an unavailable vault without offering actions that could overwrite it', async () => {
        api.list.mockResolvedValue({ groups: [], warning: 'store-unavailable' })
        await mount(h(CookieSettings))
        expect(document.querySelector('[role="alert"]')?.textContent).toContain('쿠키 저장소를 열지 못했습니다')
        expect([...document.querySelectorAll('button')].every(button => button.disabled)).toBe(true)
        expect(api.importFile).not.toHaveBeenCalled()
    })
})
