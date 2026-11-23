// @vitest-environment happy-dom
import { act, createElement as h, useState } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import type { DownloadJobDto, DownloadTransferProgress } from '@contracts/download-api'
import { DownloadProgress } from '@renderer/features/download/components/DownloadProgress'
import { downloadProgressView, downloadStatus, visibleProgress } from '@renderer/features/download/downloadPresentation'
import { cleanupReact, mount } from './reactHarness'

afterEach(cleanupReact)
const transfer: DownloadTransferProgress = {
    downloaded: 1024 ** 2, total: 4 * 1024 ** 2, percent: 25, estimated: false,
    speed: 256 * 1024, eta: 12, fileIndex: 2, fileCount: 3, processing: false, batch: false,
}
const job: DownloadJobDto = {
    id: 'job', url: 'https://weverse.io/fixture', title: null, state: 'running', phase: 'downloading',
    progress: 0, options: {}, cleanupRequired: false, currentAttemptId: 'attempt', errorCode: null, transfer,
}

describe('live media download progress', () => {
    it.each(['pending', 'running'] as const)('shows only the waiting status for a %s job waiting for a slot', async state => {
        const waiting = { ...job, state, phase: 'discovering' as const, waiting: true }
        await mount(h('div', null, h('p', {role:'status'}, downloadStatus(waiting)), h(DownloadProgress, {job:waiting})))
        expect(document.body.textContent).toBe('다운로드 대기 중...')
        expect(document.querySelector('[role="progressbar"]')).toBeNull()
        expect(visibleProgress(waiting)).toBeNull()
    })
    it('shows progress after a waiting job obtains a download slot', async () => {
        let update!: (next: DownloadJobDto) => void
        function Host() {
            const [current, setCurrent] = useState<DownloadJobDto>({...job, phase:'discovering', waiting:true})
            update = setCurrent
            return h('div', null, h('p', null, downloadStatus(current)), h(DownloadProgress, {job:current}))
        }
        await mount(h(Host))
        expect(document.querySelector('[role="progressbar"]')).toBeNull()
        await act(async () => update({...job, waiting:false}))
        expect(document.body.textContent).not.toContain('다운로드 대기 중...')
        expect(document.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow')).toBe('25')
        expect(document.body.textContent).toContain('25.0%')
    })
    it('renders the same determinate bar and byte details used during engine installation', async () => {
        await mount(h(DownloadProgress, { job }))
        const bar = document.querySelector('[role="progressbar"]')!
        expect(bar.getAttribute('aria-valuenow')).toBe('25')
        expect(bar.getAttribute('aria-label')).toBe('현재 파일 다운로드 진행률')
        for (const text of ['25.0%', '파일 2/3', '1.0 MB / 4.0 MB', '256.0 KB/s', '약 12초 남음'])
            expect(document.body.textContent).toContain(text)
    })
    it('shows received bytes without inventing a total or percent', async () => {
        const unknown = { ...job, transfer: { ...transfer, total: null, percent: null, eta: null } }
        await mount(h(DownloadProgress, { job: unknown }))
        expect(document.querySelector('[role="progressbar"]')!.hasAttribute('aria-valuenow')).toBe(false)
        expect(document.body.textContent).toContain('1.0 MB 다운로드')
        expect(document.body.textContent).not.toContain('%')
        expect(document.body.textContent).not.toContain('남음')
    })
    it('updates unknown transfers and switches between moving and measured progress on the same bar', async () => {
        let update!: (next: DownloadJobDto) => void
        const unknown = { ...job, transfer: { ...transfer, total: null, percent: null, eta: null } }
        function Host() {
            const [current, setCurrent] = useState<DownloadJobDto>(unknown)
            update = setCurrent
            return h(DownloadProgress, { job: current })
        }
        await mount(h(Host))
        const bar = document.querySelector('[role="progressbar"]')!
        const fill = bar.firstElementChild as HTMLElement
        expect(fill.classList.contains('motion-progress-indeterminate')).toBe(true)
        expect(bar.hasAttribute('aria-valuenow')).toBe(false)
        await act(async () => update({ ...unknown, transfer: { ...unknown.transfer, downloaded: 2 * 1024 ** 2, speed: 512 * 1024 } }))
        expect(document.body.textContent).toContain('2.0 MB 다운로드')
        expect(document.body.textContent).toContain('512.0 KB/s')
        expect(document.body.textContent).not.toContain('%')
        expect(bar.firstElementChild).toBe(fill)
        await act(async () => update({ ...job, transfer: { ...transfer, percent: 50 } }))
        expect(bar.firstElementChild).toBe(fill)
        expect(fill.classList.contains('motion-progress-indeterminate')).toBe(false)
        expect(fill.style.width).toBe('50%')
        expect(bar.getAttribute('aria-valuenow')).toBe('50')
        expect(document.body.textContent).toContain('50.0%')
        await act(async () => update(unknown))
        expect(fill.classList.contains('motion-progress-indeterminate')).toBe(true)
        expect(fill.style.width).toBe('')
        expect(bar.hasAttribute('aria-valuenow')).toBe(false)
        expect(document.body.textContent).not.toContain('%')
    })
    it('labels estimated sizes and sampled gallery bytes explicitly', () => {
        expect(downloadProgressView({ ...job, transfer: { ...transfer, estimated: true } })).toMatchObject({
            percentLabel: '약 25.0%', detail: expect.stringContaining('1.0 MB / 약 4.0 MB'),
        })
        const view = downloadProgressView({ ...job, transfer: { ...transfer, batch: true, total: null, percent: null, eta: null } })
        expect(view.detail).toContain('누적 1.0 MB 다운로드')
        expect(view.detail).toContain('약 256.0 KB/s')
    })
    it('keeps transfer completion distinct from successful publication and hides stale speed', () => {
        const finishing = { ...job, transfer: { ...transfer, downloaded: transfer.total!, percent: 100, processing: true } }
        expect(visibleProgress(finishing)).toBe(100)
        expect(downloadStatus(finishing)).toBe('변환·병합 중')
        expect(downloadProgressView(finishing).detail).not.toContain('/s')
        for (const phase of ['verifying', 'committing', 'rolling_back'] as const) {
            const view = downloadProgressView({ ...finishing, phase })
            expect(view.percent).toBeUndefined()
            expect(view.detail).not.toContain('남음')
        }
    })
    it('never renders live progress for a terminal attempt', async () => {
        for (const state of ['completed', 'failed', 'cancelled', 'interrupted'] as const) {
            await mount(h(DownloadProgress, { job: { ...job, state } }))
            expect(document.querySelector('[role="progressbar"]')).toBeNull()
        }
    })
})
