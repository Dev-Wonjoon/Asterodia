// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { FolderMergeDialog } from '@renderer/features/gallery/FolderMergeDialog'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ mergeAlbum: vi.fn(), directories: vi.fn(), create: vi.fn(), run: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => ({ media: api, fileMove: api }) }))
const ok = (data: unknown) => ({ ok: true, data })
const targets = [{ mediaId: 1, revision: 4 }, { mediaId: 2, revision: 8, fileIds: [20] }]
const props = () => ({ selection: targets, onChanged: vi.fn(), onClose: vi.fn(), onStarted: vi.fn() })
beforeEach(() => {
    vi.resetAllMocks()
    api.mergeAlbum.mockResolvedValue(ok({ id: 9 }))
    api.directories.mockResolvedValue(ok({ data: [{ id: 7, directoryPath: 'C:/모음' }], hasNextPage: false }))
    api.create.mockResolvedValue(ok({ jobId: 'job', albumId: '9' })); api.run.mockResolvedValue(ok({ jobId: 'job' }))
})
afterEach(cleanupReact)
async function name(value = '여행 모음') {
    const input = document.querySelector<HTMLInputElement>('[aria-label="새 앨범 이름"]')!
    await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value); input.dispatchEvent(new Event('input', { bubbles: true })) })
}
async function mode(value: string) {
    await act(async () => document.querySelector<HTMLInputElement>(`input[value="${value}"]`)!.click())
}
async function destination(operation = 'copy') {
    await act(async () => {
        const select = document.querySelector<HTMLSelectElement>('[aria-label="병합 대상 폴더"]')!
        select.value = '7'; select.dispatchEvent(new Event('change', { bubbles: true }))
        const method = document.querySelector<HTMLSelectElement>('[aria-label="병합 파일 처리 방식"]')!
        method.value = operation; method.dispatchEvent(new Event('change', { bubbles: true }))
    })
}
it('creates a merged album without issuing any file operation', async () => {
    const p = props(); await mount(h(FolderMergeDialog, p)); await name(); await clickButton('병합 시작')
    expect(api.mergeAlbum).toHaveBeenCalledExactlyOnceWith({ title: '여행 모음', selection: targets })
    expect(api.create).not.toHaveBeenCalled(); expect(p.onChanged).toHaveBeenCalledOnce(); expect(p.onClose).toHaveBeenCalledOnce()
    expect(document.body.textContent).toContain('앨범만 합치기')
    expect(document.body.textContent).toContain('앨범으로 합치고 파일도 모으기')
})
it('gathers files without a new album and offers both copy and move', async () => {
    const p = props(); await mount(h(FolderMergeDialog, p)); await mode('files'); await destination('move'); await clickButton('병합 시작')
    expect(api.create).toHaveBeenCalledExactlyOnceWith({ selection: targets, directoryId: 7, operation: 'move' })
    expect(api.mergeAlbum).not.toHaveBeenCalled(); expect(p.onStarted).toHaveBeenCalledExactlyOnceWith('job', '')
})
it('creates the album and file job in one request, retaining copy semantics', async () => {
    const p = props(); await mount(h(FolderMergeDialog, p)); await mode('combined'); await name(); await destination()
    expect(document.body.textContent).toContain('원본 위치')
    await clickButton('병합 시작')
    expect(api.create).toHaveBeenCalledExactlyOnceWith({ selection: targets, directoryId: 7, operation: 'copy', mergeTitle: '여행 모음' })
    expect(api.mergeAlbum).not.toHaveBeenCalled(); expect(api.run).toHaveBeenCalledExactlyOnceWith({ jobId: 'job' })
})
it('retains the form and selection on a known error and prevents duplicate submissions', async () => {
    const pending = deferred<any>(); api.mergeAlbum.mockReturnValueOnce(pending.promise)
    const p = props(); await mount(h(FolderMergeDialog, p)); await name(); await clickButton('병합 시작')
    expect(document.querySelector<HTMLButtonElement>('footer button:last-child')!.disabled).toBe(true)
    await act(async () => pending.resolve({ ok: false, error: { message: '선택이 변경되었습니다.' } }))
    expect(document.querySelector('[role=alert]')?.textContent).toContain('선택이 변경되었습니다.')
    expect(p.onChanged).not.toHaveBeenCalled(); expect(p.onClose).not.toHaveBeenCalled(); expect(api.mergeAlbum).toHaveBeenCalledOnce()
})
it('shows the persistent file job after a start error instead of creating a second album', async () => {
    api.run.mockResolvedValueOnce({ ok: false, error: { message: '다른 작업 실행 중' } })
    const p = props(); await mount(h(FolderMergeDialog, p)); await mode('combined'); await name(); await destination(); await clickButton('병합 시작')
    expect(p.onStarted).toHaveBeenCalledExactlyOnceWith('job', '다른 작업 실행 중'); expect(p.onChanged).toHaveBeenCalledOnce()
    expect(api.create).toHaveBeenCalledOnce()
})
it('does not resubmit an ambiguous write or start a job after unmount', async () => {
    api.mergeAlbum.mockRejectedValueOnce(new Error('offline'))
    await mount(h(FolderMergeDialog, props())); await name(); await clickButton('병합 시작')
    expect(document.querySelector('[role=alert]')?.textContent).toContain('중복 생성')
    expect(document.querySelector<HTMLButtonElement>('footer button:last-child')!.disabled).toBe(true)
    await cleanupReact()
    const pending = deferred<any>(); api.create.mockReturnValueOnce(pending.promise)
    const p = props(), stop = await mount(h(FolderMergeDialog, p)); await mode('combined'); await name(); await destination(); await clickButton('병합 시작'); await stop()
    await act(async () => pending.resolve(ok({ jobId: 'late' }))); expect(api.run).not.toHaveBeenCalled(); expect(p.onChanged).not.toHaveBeenCalled()
})
