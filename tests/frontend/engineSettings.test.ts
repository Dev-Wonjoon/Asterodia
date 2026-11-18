// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { EngineInstallProgress, EngineStatus } from '@contracts/types'
import { ToastContext } from '@renderer/shared/context/toastContext'
import { useEngineSettingsViewModel } from '@renderer/features/settings/useEngineSettingsViewModel'
import { EngineSetupModal } from '@renderer/features/settings/components/EngineSetupModal'
import { EngineSettings } from '@renderer/features/settings/EngineSettings'
import { EngineInstallProgress as Progress } from '@renderer/features/settings/components/EngineInstallProgress'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ getEngineStatus: vi.fn(), installEngine: vi.fn(), onEngineInstallProgress: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => api }))
let listener: (event: EngineInstallProgress) => void
let unsubscribe: ReturnType<typeof vi.fn>
const toast = vi.fn()
let model!: ReturnType<typeof useEngineSettingsViewModel>
function Probe() { model = useEngineSettingsViewModel(); return null }
const render = (node = h(Probe)) => mount(h(ToastContext.Provider, { value: { showToast: toast } }, node))
const status = (installed = false): Record<string, EngineStatus> => ({ ffmpeg: { installed, version: installed ? '1.0' : null }, 'yt-dlp': { installed: true, version: '2.0' } })
async function emit(installId: string, patch: Partial<EngineInstallProgress> = {}) {
    await act(async () => { listener({ installId, engine: 'ffmpeg', phase: 'downloading', downloaded: 40, total: 100, ...patch }) })
}
beforeEach(() => {
    vi.resetAllMocks(); unsubscribe = vi.fn()
    api.getEngineStatus.mockResolvedValue(status())
    api.onEngineInstallProgress.mockImplementation(callback => { listener = callback; return unsubscribe })
})
afterEach(async () => { await cleanupReact(); vi.restoreAllMocks() })

describe('shared engine installation hook', () => {
    it('correlates both engine and attempt, preserving byte counts through completion', async () => {
        const pending = deferred<boolean>(); api.installEngine.mockReturnValue(pending.promise)
        await render(); await emit('no-active-attempt'); expect(model.progress).toEqual({})
        let work!: Promise<void>
        await act(async () => { work = model.install('ffmpeg') })
        const id = api.installEngine.mock.calls[0][1]
        await emit('old-attempt'); expect(model.progress.ffmpeg.phase).toBe('preparing')
        await emit(id, { engine: 'yt-dlp' }); expect(model.progress['yt-dlp']).toBeUndefined()
        await emit(id); expect(model.progress.ffmpeg.downloaded).toBe(40)
        await emit(id, { phase: 'extracting', downloaded: 0, total: null }); expect(model.progress.ffmpeg.downloaded).toBe(40)
        api.getEngineStatus.mockResolvedValue(status(true))
        await act(async () => { pending.resolve(true); await work })
        expect(model.progress.ffmpeg.phase).toBe('completed'); expect(model.busy).toBe(false); expect(model.allInstalled).toBe(true)
        await emit(id, { downloaded: 1 }); expect(model.progress.ffmpeg.phase).toBe('completed')
    })
    it('serializes install-all and rejects a second click while the queue runs', async () => {
        api.getEngineStatus.mockResolvedValue({ a: { installed: false, version: null }, b: { installed: false, version: null } })
        const first = deferred<boolean>(); const second = deferred<boolean>()
        api.installEngine.mockImplementation(name => name === 'a' ? first.promise : second.promise)
        await render(); let work!: Promise<void>
        await act(async () => { work = model.installAll(); void model.install('b') })
        expect(api.installEngine).toHaveBeenCalledTimes(1); expect(model.busy).toBe(true)
        await act(async () => { first.resolve(true) })
        expect(api.installEngine).toHaveBeenCalledTimes(2); expect(model.installing).toBe('b'); expect(model.busy).toBe(true)
        await act(async () => { second.resolve(true); await work })
        expect(model.busy).toBe(false); expect(model.progress.a.phase).toBe('completed'); expect(model.progress.b.phase).toBe('completed')
    })
    it.each([false, new Error('다운로드 오류'), 'unknown error'])('shows failed install outcome %s and allows retry', async outcome => {
        if (outcome === false) api.installEngine.mockResolvedValue(false); else api.installEngine.mockRejectedValue(outcome)
        await render(); await act(async () => { await model.install('ffmpeg') })
        expect(model.progress.ffmpeg.phase).toBe('failed'); expect(toast).toHaveBeenCalledWith(expect.stringContaining('설치에 실패'), 'error')
        const previous = model.progress.ffmpeg.installId
        api.installEngine.mockResolvedValue(true); await act(async () => { await model.install('ffmpeg') })
        expect(model.progress.ffmpeg.installId).not.toBe(previous); expect(model.progress.ffmpeg.phase).toBe('completed')
    })
    it('reports a status error without turning an already successful install into a failure', async () => {
        await render(); api.installEngine.mockResolvedValue(true); api.getEngineStatus.mockRejectedValue(new Error('상태 확인 실패'))
        await act(async () => { await model.install('ffmpeg') })
        expect(model.progress.ffmpeg.phase).toBe('completed'); expect(model.statusError).not.toBeNull(); expect(model.allInstalled).toBe(false)
        api.getEngineStatus.mockResolvedValue(status(true)); await act(async () => { await model.refreshStatus() })
        expect(model.statusError).toBeNull(); expect(model.allInstalled).toBe(true)
    })
    it('ignores an older status response arriving last', async () => {
        const old = deferred<Record<string, EngineStatus>>(); api.getEngineStatus.mockReturnValueOnce(old.promise)
        await render(); api.getEngineStatus.mockResolvedValue(status(true)); await act(async () => { await model.refreshStatus() })
        await act(async () => { old.resolve(status()) }); expect(model.allInstalled).toBe(true)
    })
    it('unsubscribes and does not launch queued engines after closing the view', async () => {
        api.getEngineStatus.mockResolvedValue({ a: { installed: false, version: null }, b: { installed: false, version: null } })
        const pending = deferred<boolean>(); api.installEngine.mockReturnValue(pending.promise)
        const unmount = await render(); let work!: Promise<void>; await act(async () => { work = model.installAll() })
        await unmount(); expect(unsubscribe).toHaveBeenCalledOnce()
        await act(async () => { pending.resolve(true); await work }); expect(api.installEngine).toHaveBeenCalledTimes(1)
    })
    it('does not update an unmounted view on a rejected installation or status fetch', async () => {
        const pending = deferred<boolean>(); api.installEngine.mockReturnValue(pending.promise)
        const unmount = await render(); let work!: Promise<void>; await act(async () => { work = model.install('ffmpeg') })
        await unmount(); await act(async () => { pending.reject(new Error('closed')); await work }); expect(toast).not.toHaveBeenCalled()
        const statusPending = deferred<Record<string, EngineStatus>>(); api.getEngineStatus.mockReturnValue(statusPending.promise)
        const close = await render(); await close(); await act(async () => { statusPending.reject(new Error('closed')) })
    })
    it('skips an empty install-all queue', async () => {
        api.getEngineStatus.mockResolvedValue(status(true)); await render(); await act(async () => { await model.installAll() }); expect(api.installEngine).not.toHaveBeenCalled()
    })
})

describe('engine progress UI', () => {
    it('renders settings loading, status retry and an installed engine with an unknown label/version', async () => {
        const initial = deferred<Record<string, EngineStatus>>(); api.getEngineStatus.mockReturnValueOnce(initial.promise)
        await render(h(EngineSettings)); expect(document.body.textContent).toContain('엔진 상태 확인 중')
        await act(async () => { initial.reject(new Error('offline')) }); expect(document.querySelector('[role="alert"]')).not.toBeNull()
        api.getEngineStatus.mockResolvedValue({ custom: { installed: true, version: null } }); await clickButton('다시 확인')
        expect(document.body.textContent).toContain('custom'); expect(document.body.textContent).toContain('버전 확인 불가')
        api.installEngine.mockResolvedValue(true); await clickButton('업데이트')
        expect(document.querySelector('[aria-label="custom 다운로드 진행률"]')!.getAttribute('aria-valuenow')).toBe('100')
    })
    it('connects install-all and the later button, including a missing version label', async () => {
        api.getEngineStatus.mockResolvedValue({ ffmpeg: { installed: false, version: null }, 'yt-dlp': { installed: true, version: null } })
        const pending = deferred<boolean>(); api.installEngine.mockReturnValue(pending.promise)
        const complete = vi.fn(); await render(h(EngineSetupModal, { onComplete: complete }))
        expect(document.body.textContent).toContain('설치됨'); await clickButton('모두 설치')
        expect(api.installEngine).toHaveBeenCalledTimes(1); await clickButton('나중에'); expect(complete).toHaveBeenCalledOnce()
        await act(async () => { pending.resolve(true) })
    })
    it('shows a per-engine bar in setup and switches to start after completion', async () => {
        const pending = deferred<boolean>(); api.installEngine.mockReturnValue(pending.promise)
        const complete = vi.fn(); await render(h(EngineSetupModal, { onComplete: complete })); await clickButton('설치')
        const id = api.installEngine.mock.calls[0][1]; await emit(id)
        const bar = document.querySelector('[aria-label="ffmpeg 다운로드 진행률"]')!
        expect(bar.getAttribute('aria-valuenow')).toBe('40'); expect(document.body.textContent).toContain('40 B / 100 B')
        expect(document.querySelector('[aria-label="yt-dlp 다운로드 진행률"]')).toBeNull()
        await emit(id, { phase: 'verifying', downloaded: 0, total: null }); expect(document.body.textContent).toContain('실행 파일 확인 중')
        api.getEngineStatus.mockResolvedValue(status(true)); await act(async () => { pending.resolve(true) })
        await clickButton('시작하기'); expect(complete).toHaveBeenCalledOnce()
    })
    it('uses the same progress component in settings and renders failures', async () => {
        const pending = deferred<boolean>(); api.installEngine.mockReturnValue(pending.promise)
        await render(h(EngineSettings)); await clickButton('설치')
        await emit(api.installEngine.mock.calls[0][1], { total: null })
        expect(document.querySelector('[role="progressbar"]')!.hasAttribute('aria-valuenow')).toBe(false)
        await act(async () => { pending.reject(new Error('네트워크 오류')) })
        expect(document.body.textContent).toContain('네트워크 오류'); expect(document.querySelector('[role="alert"]')).not.toBeNull()
    })
    it('recovers from an initial status error in the setup modal', async () => {
        api.getEngineStatus.mockRejectedValue(new Error('offline')); await render(h(EngineSetupModal, { onComplete: vi.fn() }))
        expect(document.body.textContent).toContain('상태를 확인하지 못했습니다')
        api.getEngineStatus.mockResolvedValue(status()); await clickButton('다시 확인'); expect(document.body.textContent).toContain('ffmpeg')
    })
    it('renders no progress without an attempt, and clamps an accessible bar', async () => {
        await mount(h(Progress, { name: 'empty' })); expect(document.querySelector('[role="progressbar"]')).toBeNull()
        await mount(h(ProgressBar, { label: 'clamp', value: 200 })); expect(document.querySelector('[aria-label="clamp"]')!.getAttribute('aria-valuenow')).toBe('100')
    })
})
