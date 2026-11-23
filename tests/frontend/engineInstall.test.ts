import { describe, expect, it } from 'vitest'
import type { EngineInstallProgress } from '@contracts/types'
import { installProgressView, mergeInstallProgress, batchProgressView } from '@renderer/features/settings/engineInstall'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

const event = (patch: Partial<EngineInstallProgress> = {}): EngineInstallProgress => ({ installId: 'request', engine: 'ffmpeg', phase: 'downloading', downloaded: 25, total: 100, ...patch })
describe('engine download progress decisions', () => {
    it('starts a fresh attempt and resets a retried installation', () => {
        expect(mergeInstallProgress(undefined, event())).toEqual(event())
        expect(mergeInstallProgress(event(), event({ installId: 'retry', downloaded: 0, total: null }))).toEqual(event({ installId: 'retry', downloaded: 0, total: null }))
    })
    it('keeps downloaded bytes through extraction, verification and final commit', () => {
        let progress = event({ downloaded: 100 })
        for (const phase of ['extracting', 'verifying', 'committing', 'completed']) {
            progress = mergeInstallProgress(progress, event({ phase, downloaded: 0, total: null }))
            expect(progress.downloaded).toBe(100); expect(progress.total).toBe(100)
            expect(installProgressView(progress).percent).toBe(100)
        }
    })
    it('ignores delayed updates after a terminal result and never decreases downloaded bytes', () => {
        for (const phase of ['completed', 'failed']) {
            const previous = event({ phase })
            expect(mergeInstallProgress(previous, event())).toBe(previous)
        }
        expect(mergeInstallProgress(event(), event({ downloaded: 1, total: 200 })).downloaded).toBe(25)
    })
    it('displays every stage, including an unknown backend stage', () => {
        const expected = { preparing: '설치 준비 중', downloading: '다운로드 중', extracting: '압축 해제 중', verifying: '실행 파일 확인 중', committing: '설치 파일 반영 중', completed: '설치 완료', failed: '설치 실패', unknown: '설치 중' }
        for (const [phase, label] of Object.entries(expected)) expect(installProgressView(event({ phase })).label).toBe(label)
    })
    it.each([null, 0, -1, NaN, Infinity])('leaves an unmeasurable total %s indeterminate', total => {
        const view = installProgressView(event({ total }))
        expect(view.percent).toBeUndefined(); expect(view.detail).toBe('25 B 다운로드')
    })
    it('clamps numeric edge cases and distinguishes failure from an unknown live total', () => {
        expect(installProgressView(event()).percent).toBe(25)
        expect(installProgressView(event()).detail).toBe('25 B / 100 B')
        for (const downloaded of [-2, NaN, Infinity]) expect(installProgressView(event({ downloaded })).percent).toBe(0)
        expect(installProgressView(event({ downloaded: 200 })).percent).toBe(100)
        expect(installProgressView(event({ phase: 'completed', total: null })).percent).toBe(100)
        expect(installProgressView(event({ phase: 'failed', total: null })).percent).toBe(0)
        expect(installProgressView(event({ phase: 'failed' })).percent).toBe(25)
    })
})
describe('shared byte formatting', () => {
    it.each([-1, NaN, Infinity])('rejects invalid size %s', value => { expect(formatBytes(value)).toBe('0 B') })
    it('supports bytes, rounding, scaling, and a bounded largest unit', () => {
        expect(formatBytes(12.6)).toBe('13 B')
        expect(formatBytes(1024)).toBe('1.0 KB')
        expect(formatBytes(1024 ** 3)).toBe('1.0 GB')
        expect(formatBytes(1024 ** 5)).toBe('1024.0 TB')
    })
})

describe('batch installation progress', () => {
    it('counts completed attempts separately from successful installations', () => {
        const view = batchProgressView({ names: ['a', 'b', 'c', 'd'], processed: 2, succeeded: 1, failed: ['a'] })
        expect(view.percent).toBe(50)
        expect(view.label).toBe('처리 2/4개 · 완료 1개 · 실패 1개')
        expect(batchProgressView({ names: [], processed: 0, succeeded: 0, failed: [] }).percent).toBe(0)
    })
})
