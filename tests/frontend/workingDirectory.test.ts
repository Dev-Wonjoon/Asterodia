// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { WorkingDirectorySettings } from '@renderer/features/settings/WorkingDirectorySettings'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ getWorkingDirectory: vi.fn(), setWorkingDirectory: vi.fn(), clearWorkingDirectory: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => api }))
beforeEach(() => { vi.resetAllMocks(); api.getWorkingDirectory.mockResolvedValue(''); api.setWorkingDirectory.mockResolvedValue('D:/Working'); api.clearWorkingDirectory.mockResolvedValue(true) })
afterEach(cleanupReact)
it('loads the legacy fallback, changes the folder, and restores the download default', async () => {
    await mount(h(WorkingDirectorySettings)); expect(document.body.textContent).toContain('다운로드 폴더의 edits 사용')
    await clickButton('변경'); expect(document.body.textContent).toContain('D:/Working')
    await clickButton('기존 경로 사용'); expect(document.body.textContent).toContain('다운로드 폴더의 edits 사용')
    expect(api.clearWorkingDirectory).toHaveBeenCalledOnce()
})
it('preserves the displayed folder on cancellation and displays failure without changing it', async () => {
    api.getWorkingDirectory.mockResolvedValue('D:/Working'); api.setWorkingDirectory.mockResolvedValueOnce(null).mockRejectedValueOnce(Error())
    await mount(h(WorkingDirectorySettings)); await clickButton('변경'); expect(document.body.textContent).toContain('D:/Working')
    await clickButton('변경'); expect(document.querySelector('[role="alert"]')?.textContent).toContain('다시 시도')
    expect(document.body.textContent).toContain('D:/Working')
})
it('does not let a delayed initial read overwrite a newly chosen folder', async () => {
    const pending = deferred<string>(); api.getWorkingDirectory.mockReturnValue(pending.promise)
    await mount(h(WorkingDirectorySettings)); await clickButton('변경')
    await act(async () => pending.resolve('D:/Old'))
    expect(document.body.textContent).toContain('D:/Working'); expect(document.body.textContent).not.toContain('D:/Old')
})
