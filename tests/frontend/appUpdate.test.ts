// @vitest-environment happy-dom
import { act, createElement as h, Fragment, StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AppUpdateProvider } from '@renderer/features/settings/useAppUpdate'
import { UpdateSettings, UpdateNotice } from '@renderer/features/settings/UpdateSettings'
import { ConfirmProvider } from '@renderer/app/providers/ConfirmProvider'
import { useEngineSetup } from '@renderer/app/hooks/useEngineSetup'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import type { UpdateResult, UpdateStatus } from '@contracts/update-api'
import { LeaveGuardContext, type LeaveGuardRef } from '@renderer/shared/hooks/leaveGuardContext'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ status: vi.fn(), check: vi.fn(), download: vi.fn(), restart: vi.fn(), getEngineStatus: vi.fn(), showToast: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ updates: api, getEngineStatus: api.getEngineStatus }) }))
vi.mock('@renderer/shared/hooks/useToast', () => ({ useToast: () => ({ showToast: api.showToast }) }))
const state = (extra: Partial<UpdateStatus> = {}): UpdateStatus => ({ currentVersion: '2.0.1', state: 'idle', version: '', notes: '', size: 1000, written: 0, error: '', lastChecked: 0, supported: true, ...extra })
const available = () => state({ state: 'available', version: '2.0.2' })
const ready = () => state({ state: 'ready', version: '2.0.2' })
const ok = (data: UpdateStatus): UpdateResult => ({ ok: true, data })
const button = (label: string) => [...document.querySelectorAll('button')].find(b => b.textContent === label)!
const dialog = () => document.querySelector('[role="alertdialog"]')
const content = (open = vi.fn()) => h(Fragment, null, h(UpdateSettings), h(UpdateNotice, { onOpen: open }))
const screen = (open = vi.fn(), guard?: LeaveGuardRef) => h(ConfirmProvider, null, h(AppUpdateProvider, { leaveGuard: guard }, content(open)))
beforeEach(() => { vi.resetAllMocks(); api.status.mockResolvedValue(ok(state())) })
afterEach(async () => { await cleanupReact(); vi.useRealTimers() })

describe('app updates', () => {
    it('offers an update once at startup, keeps the banner after deferral and allows a manual prompt', async () => {
        vi.useFakeTimers()
        const open = vi.fn(); await mount(screen(open))
        expect(document.body.textContent).toContain('현재 버전 2.0.1')
        expect(dialog()).toBeNull()
        api.status.mockResolvedValue(ok(available()))
        await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
        expect(dialog()?.textContent).toContain('현재 버전: 2.0.1')
        expect(dialog()?.textContent).toContain('새 버전: 2.0.2')
        expect(api.download).not.toHaveBeenCalled(); expect(api.restart).not.toHaveBeenCalled()
        await clickButton('나중에')
        await act(async () => { await vi.advanceTimersByTimeAsync(20000) })
        expect(dialog()).toBeNull()
        await clickButton('업데이트 보기'); expect(open).toHaveBeenCalledOnce()
        api.check.mockResolvedValue(ok(available()))
        await clickButton('업데이트 확인')
        expect(dialog()?.textContent).toContain('새 버전 업데이트')
        await clickButton('나중에')
        expect(api.download).not.toHaveBeenCalled(); expect(api.restart).not.toHaveBeenCalled()
    })

    it('uses the same manual prompt, renders notes as text and separately confirms restart after downloading', async () => {
        const release = { ...available(), notes: '<script>untrusted()</script>\nChanges' }
        api.check.mockResolvedValue(ok(release))
        api.download.mockResolvedValue(ok(ready()))
        api.restart.mockResolvedValue(ok(state({ ...ready(), state: 'restarting' })))
        await mount(screen()); await clickButton('업데이트 확인')
        expect(document.querySelector('script')).toBeNull()
        expect(dialog()?.textContent).toContain('<script>untrusted()</script>')
        expect(document.querySelector('[data-confirm-background]')?.hasAttribute('inert')).toBe(true)
        expect(api.download).not.toHaveBeenCalled()
        await clickButton('업데이트')
        expect(api.download).toHaveBeenCalledOnce()
        expect(button('업데이트 확인').disabled).toBe(true)
        expect(dialog()?.textContent).toContain('업데이트 준비 완료')
        expect(api.restart).not.toHaveBeenCalled()
        await clickButton('지금 재시작')
        expect(api.restart).toHaveBeenCalledOnce()
        expect(document.body.textContent).toContain('앱을 다시 시작합니다')
    })

    it('keeps latest-version startup checks quiet and shows an explicit manual result', async () => {
        vi.useFakeTimers()
        api.status.mockResolvedValue(ok(state({ state: 'up-to-date' })))
        api.check.mockResolvedValue(ok(state({ state: 'up-to-date' })))
        await mount(screen())
        expect(dialog()).toBeNull()
        await clickButton('업데이트 확인')
        expect(dialog()?.textContent).toContain('최신 버전입니다.')
        await clickButton('확인')
        api.status.mockResolvedValue(ok(available()))
        await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
        expect(dialog()).toBeNull()
        expect(button('업데이트 보기')).toBeTruthy()
    })

    it('offers the startup prompt again after the app is mounted for a new session', async () => {
        api.status.mockResolvedValue(ok(available()))
        const unmount = await mount(screen())
        await clickButton('나중에'); await unmount()
        await mount(screen())
        expect(dialog()?.textContent).toContain('새 버전 업데이트')
    })

    it('keeps running when restart is deferred and reports active-work rejection on retry', async () => {
        api.status.mockResolvedValue(ok(ready()))
        await mount(screen())
        await clickButton('나중에')
        expect(api.restart).not.toHaveBeenCalled()
        api.restart.mockResolvedValue({ ok: false, error: { code: 'UPDATE_WORK_ACTIVE', message: '편집 작업을 마친 뒤 업데이트를 적용하세요' } })
        await clickButton('재시작하여 적용'); await clickButton('지금 재시작')
        expect(api.restart).toHaveBeenCalledOnce()
        expect(document.querySelector('[role="alert"]')?.textContent).toContain('편집 작업')
        expect(dialog()?.textContent).toContain('편집 작업')
        await clickButton('확인')
        expect(button('재시작하여 적용').disabled).toBe(false)
    })

    it('checks unsaved edits only after the user chooses to restart and respects refusal', async () => {
        api.status.mockResolvedValue(ok(ready()))
        const guard = vi.fn().mockResolvedValue(false)
        await mount(h(LeaveGuardContext.Provider, { value: { current: guard } }, screen()))
        expect(guard).not.toHaveBeenCalled()
        await clickButton('지금 재시작')
        expect(guard).toHaveBeenCalledOnce()
        expect(dialog()).toBeNull(); expect(api.restart).not.toHaveBeenCalled()
        guard.mockResolvedValue(true)
        await clickButton('재시작하여 적용')
        expect(dialog()?.textContent).toContain('업데이트 준비 완료')
        await clickButton('나중에')
        expect(guard).toHaveBeenCalledOnce()
        expect(api.restart).not.toHaveBeenCalled()
        api.restart.mockResolvedValue(ok(state({ ...ready(), state: 'restarting' })))
        await clickButton('재시작하여 적용'); await clickButton('지금 재시작')
        expect(guard).toHaveBeenCalledTimes(2)
        expect(api.restart).toHaveBeenCalledOnce()
    })

    it('uses the app guard passed to the provider even outside the page guard context', async () => {
        api.status.mockResolvedValue(ok(ready()))
        const guard = vi.fn().mockResolvedValue(false)
        await mount(screen(vi.fn(), { current: guard }))
        await clickButton('지금 재시작')
        expect(guard).toHaveBeenCalledOnce()
        expect(dialog()).toBeNull(); expect(api.restart).not.toHaveBeenCalled()
    })

    it('coalesces rapid restart clicks into one confirmation', async () => {
        api.status.mockResolvedValue(ok(ready()))
        await mount(screen()); await clickButton('나중에')
        await act(async () => { button('재시작하여 적용').click(); button('재시작하여 적용').click() })
        expect(document.querySelectorAll('[role="alertdialog"]')).toHaveLength(1)
        await clickButton('나중에')
        expect(dialog()).toBeNull(); expect(api.restart).not.toHaveBeenCalled()
    })

    it('reports guard failures and keeps the app running', async () => {
        api.status.mockResolvedValue(ok(ready()))
        const guard = vi.fn().mockRejectedValue(new Error('guard failed'))
        await mount(screen(vi.fn(), { current: guard }))
        await clickButton('지금 재시작')
        expect(dialog()?.textContent).toContain('작업을 저장한 뒤 다시 시도하세요')
        expect(api.restart).not.toHaveBeenCalled()
        await clickButton('확인')
        expect(dialog()).toBeNull()
    })

    it('shows live download progress, coalesces clicks and offers restart after completion', async () => {
        vi.useFakeTimers()
        const pending = deferred<UpdateResult>()
        api.status.mockResolvedValue(ok(available()))
        api.download.mockReturnValue(pending.promise)
        await mount(screen()); await clickButton('나중에')
        await act(async () => { button('업데이트 다운로드').click(); button('업데이트 다운로드').click() })
        expect(api.download).toHaveBeenCalledOnce()
        api.status.mockResolvedValue(ok(state({ ...available(), state: 'downloading', written: 500 })))
        await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
        expect(document.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow')).toBe('50')
        await act(async () => { pending.resolve(ok(ready())) })
        expect(dialog()?.textContent).toContain('업데이트 준비 완료')
        await clickButton('나중에')
        expect(button('재시작하여 적용')).toBeTruthy()
        expect(api.restart).not.toHaveBeenCalled()
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

    it('does not duplicate a startup prompt under StrictMode', async () => {
        vi.useFakeTimers()
        api.status.mockResolvedValue(ok(available()))
        await mount(h(StrictMode, null, screen()))
        expect(document.querySelectorAll('[role="alertdialog"]')).toHaveLength(1)
        await clickButton('나중에')
        await act(async () => { await vi.advanceTimersByTimeAsync(10000) })
        expect(dialog()).toBeNull()
    })

    it('keeps startup failures quiet and offers retries for manual checks and failed verification', async () => {
        api.status.mockResolvedValue(ok(state({ state: 'error', error: '네트워크 오류' })))
        api.check.mockRejectedValueOnce(new Error('offline')).mockResolvedValue(ok(available()))
        await mount(screen())
        expect(dialog()).toBeNull()
        await clickButton('업데이트 확인')
        expect(dialog()?.textContent).toContain('요청 결과')
        await clickButton('다시 시도')
        expect(dialog()?.textContent).toContain('새 버전 업데이트')
        api.download.mockResolvedValueOnce(ok(state({ ...available(), state: 'error', error: 'SHA256 검증 실패' }))).mockResolvedValue(ok(ready()))
        await clickButton('업데이트')
        expect(dialog()?.textContent).toContain('SHA256 검증 실패')
        expect(button('재시작하여 적용')).toBeUndefined()
        await clickButton('다시 시도')
        expect(dialog()?.textContent).toContain('업데이트 준비 완료')
        expect(api.download).toHaveBeenCalledTimes(2)
    })

    it('waits for engine status and initial setup before presenting the startup update', async () => {
        const engines = deferred<Record<string, { installed: boolean }>>()
        api.getEngineStatus.mockReturnValue(engines.promise)
        api.status.mockResolvedValue(ok(available()))
        function Startup() {
            const { showSetup, checkingSetup, closeSetup } = useEngineSetup()
            return h(AppUpdateProvider, { blocked: checkingSetup || showSetup }, h(Fragment, null,
                content(), showSetup && h('button', { onClick: closeSetup }, '엔진 설정 완료')))
        }
        await mount(h(ConfirmProvider, null, h(Startup)))
        expect(dialog()).toBeNull()
        await act(async () => { engines.resolve({ engine: { installed: false } }) })
        expect(button('엔진 설정 완료')).toBeTruthy()
        expect(dialog()).toBeNull()
        await clickButton('엔진 설정 완료')
        expect(dialog()?.textContent).toContain('새 버전 업데이트')
    })

    it('releases startup updates if checking engine status fails', async () => {
        api.getEngineStatus.mockRejectedValue(new Error('engine status failed'))
        api.status.mockResolvedValue(ok(available()))
        const logged = vi.spyOn(console, 'error').mockImplementation(() => {})
        function Startup() {
            const { showSetup, checkingSetup } = useEngineSetup()
            return h(AppUpdateProvider, { blocked: checkingSetup || showSetup }, content())
        }
        try {
            await mount(h(ConfirmProvider, null, h(Startup)))
            expect(api.showToast).toHaveBeenCalledWith('엔진 상태를 확인하지 못했습니다.', 'error')
            expect(dialog()?.textContent).toContain('새 버전 업데이트')
        } finally { logged.mockRestore() }
    })

    it('waits for an existing confirmation without consuming the startup update choice', async () => {
        vi.useFakeTimers()
        function ExistingConfirmation() {
            const { confirm } = useConfirm()
            return h('button', { onClick: () => void confirm({ title: '기존 확인', message: '진행 중인 확인' }) }, '기존 확인 열기')
        }
        await mount(h(ConfirmProvider, null, h(AppUpdateProvider, null, h(Fragment, null, content(), h(ExistingConfirmation)))))
        await clickButton('기존 확인 열기')
        api.status.mockResolvedValue(ok(available()))
        await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
        expect(dialog()?.textContent).toContain('기존 확인')
        expect(document.querySelectorAll('[role="alertdialog"]')).toHaveLength(1)
        await clickButton('취소')
        expect(dialog()?.textContent).toContain('새 버전 업데이트')
        await clickButton('나중에')
        expect(api.download).not.toHaveBeenCalled()
    })
})
