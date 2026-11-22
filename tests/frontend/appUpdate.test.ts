// @vitest-environment happy-dom
import { act, createElement as h, Fragment, StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AppUpdateProvider } from '@renderer/features/settings/useAppUpdate'
import { UpdateSettings, UpdateNotice } from '@renderer/features/settings/UpdateSettings'
import type { UpdateResult, UpdateStatus } from '@contracts/update-api'
import { LeaveGuardContext } from '@renderer/shared/hooks/leaveGuardContext'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ status: vi.fn(), check: vi.fn(), download: vi.fn(), restart: vi.fn(), confirm: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ updates: api }) }))
vi.mock('@renderer/shared/hooks/useConfirm', () => ({ useConfirm: () => ({ confirm: api.confirm }) }))
const state = (extra: Partial<UpdateStatus> = {}): UpdateStatus => ({ currentVersion: '2.0.1', state: 'idle', version: '', notes: '', size: 1000, written: 0, error: '', lastChecked: 0, supported: true, ...extra })
const ok = (data: UpdateStatus): UpdateResult => ({ ok: true, data })
const button = (label: string) => [...document.querySelectorAll('button')].find(b => b.textContent === label)!
const screen = (open = vi.fn()) => h(AppUpdateProvider, null, h(Fragment, null, h(UpdateSettings), h(UpdateNotice, { onOpen: open })))
beforeEach(() => { vi.resetAllMocks(); api.status.mockResolvedValue(ok(state())); api.confirm.mockResolvedValue(true) })
afterEach(async () => { await cleanupReact(); vi.useRealTimers() })

describe('app updates', () => {
    it('shows a polled update notice without downloading or restarting automatically', async () => {
        vi.useFakeTimers()
        const open = vi.fn(); await mount(screen(open))
        expect(document.body.textContent).toContain('현재 버전 2.0.1')
        expect(button('업데이트 보기')).toBeUndefined()
        api.status.mockResolvedValue(ok(state({ state: 'available', version: '2.0.2' })))
        await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
        await clickButton('업데이트 보기'); expect(open).toHaveBeenCalledOnce()
        expect(api.check).not.toHaveBeenCalled(); expect(api.download).not.toHaveBeenCalled(); expect(api.restart).not.toHaveBeenCalled()
    })
    it('checks manually, renders release notes as text, downloads and confirms restart', async () => {
        const available = state({ state: 'available', version: '2.0.2', notes: '<script>untrusted()</script>\nChanges' })
        api.check.mockResolvedValue(ok(available))
        api.download.mockResolvedValue(ok({ ...available, state: 'ready' }))
        api.restart.mockResolvedValue(ok({ ...available, state: 'restarting' }))
        await mount(screen()); await clickButton('업데이트 확인')
        expect(document.querySelector('script')).toBeNull()
        expect(document.querySelector('pre')?.textContent).toContain('<script>untrusted()</script>')
        await clickButton('업데이트 다운로드')
        expect(button('업데이트 확인').disabled).toBe(true)
        await clickButton('재시작하여 적용')
        expect(api.confirm).toHaveBeenCalledWith(expect.objectContaining({ title: '업데이트 적용' }))
        expect(api.restart).toHaveBeenCalledOnce()
        expect(document.body.textContent).toContain('앱을 다시 시작합니다')
    })
    it('keeps the app running if restart is declined or active work blocks it', async () => {
        api.status.mockResolvedValue(ok(state({ state: 'ready', version: '2.0.2' })))
        api.confirm.mockResolvedValueOnce(false)
        await mount(screen()); await clickButton('재시작하여 적용')
        expect(api.restart).not.toHaveBeenCalled()
        api.restart.mockResolvedValue({ ok: false, error: { code: 'UPDATE_WORK_ACTIVE', message: '편집 작업을 마친 뒤 업데이트를 적용하세요' } })
        await clickButton('재시작하여 적용')
        expect(document.querySelector('[role="alert"]')?.textContent).toContain('편집 작업')
        expect(button('재시작하여 적용').disabled).toBe(false)
    })
    it('respects the existing unsaved-edit guard before requesting a restart', async () => {
        api.status.mockResolvedValue(ok(state({ state: 'ready', version: '2.0.2' })))
        const guard = vi.fn().mockResolvedValue(false)
        await mount(h(LeaveGuardContext.Provider, { value: { current: guard } }, screen()))
        await clickButton('재시작하여 적용')
        expect(guard).toHaveBeenCalledOnce(); expect(api.confirm).not.toHaveBeenCalled(); expect(api.restart).not.toHaveBeenCalled()
    })
    it('shows live download progress and coalesces clicks', async () => {
        vi.useFakeTimers()
        const pending = deferred<UpdateResult>()
        api.status.mockResolvedValue(ok(state({ state: 'available', version: '2.0.2' })))
        api.download.mockReturnValue(pending.promise)
        await mount(screen())
        await act(async () => { button('업데이트 다운로드').click(); button('업데이트 다운로드').click() })
        expect(api.download).toHaveBeenCalledOnce()
        api.status.mockResolvedValue(ok(state({ state: 'downloading', version: '2.0.2', written: 500 })))
        await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
        expect(document.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow')).toBe('50')
        await act(async () => { pending.resolve(ok(state({ state: 'ready', version: '2.0.2' }))) })
        expect(button('재시작하여 적용')).toBeTruthy()
    })
    it('ignores abandoned StrictMode reads and stops polling on unmount', async () => {
        vi.useFakeTimers()
        const old = deferred<UpdateResult>()
        api.status.mockReturnValueOnce(old.promise).mockResolvedValue(ok(state()))
        const unmount = await mount(h(StrictMode, null, screen()))
        await act(async () => { old.resolve(ok(state({ state: 'available', version: '9.0.0' }))) })
        expect(document.body.textContent).not.toContain('9.0.0')
        const count = api.status.mock.calls.length
        await unmount(); await act(async () => { await vi.advanceTimersByTimeAsync(20000) })
        expect(api.status).toHaveBeenCalledTimes(count)
    })
    it('shows network/verification failures and supports a retry', async () => {
        api.check.mockRejectedValueOnce(new Error('offline')).mockResolvedValue(ok(state({ state: 'available', version: '2.0.2' })))
        await mount(screen()); await clickButton('업데이트 확인')
        expect(document.querySelector('[role="alert"]')?.textContent).toContain('요청 결과')
        await clickButton('업데이트 확인')
        api.download.mockResolvedValue(ok(state({ state: 'error', version: '2.0.2', error: 'SHA256 검증 실패' })))
        await clickButton('업데이트 다운로드')
        expect(document.querySelector('[role="alert"]')?.textContent).toContain('SHA256 검증 실패')
        expect(button('재시작하여 적용')).toBeUndefined()
    })
})
