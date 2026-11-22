import { describe, expect, it } from 'vitest'
import { downloadFailure, type DownloadJobDto } from '@contracts/download-api'
import {
    canCancelDownload,
    canDeleteDownload,
    canRetryDownload,
    downloadPlatform,
    downloadStatus,
    downloadTrigger,
    visibleProgress,
    isGalleryDownload
} from '@renderer/features/download/downloadPresentation'
const job: DownloadJobDto = {
    id: 'job',
    url: 'https://youtu.be/example',
    title: null,
    state: 'running',
    phase: 'downloading',
    progress: 0,
    options: {},
    cleanupRequired: false,
    currentAttemptId: 'attempt',
    errorCode: null
}

describe('download presentation', () => {
    it('identifies Pinterest links and Weverse without matching lookalike hosts', () => {
        for (const url of ['https://pin.it/fixture','https://kr.pinterest.com/pin/123','https://www.pinterest.co.kr/pin/123']) expect(downloadPlatform(url)).toBe('Pinterest')
        expect(downloadPlatform('https://weverse.io/artist/media/123')).toBe('Weverse')
        expect(downloadPlatform('https://pinterest.com.evil.test/pin/123')).toBe('pinterest.com.evil.test')
    })
    it('shows only active, non-history jobs in the gallery', () => {
        for (const state of ['pending', 'running'] as const) expect(isGalleryDownload({ ...job, state })).toBe(true)
        for (const state of ['failed', 'cancelled', 'interrupted', 'completed'] as const) expect(isGalleryDownload({ ...job, state })).toBe(false)
        expect(isGalleryDownload({ ...job, phase: 'rolling_back' })).toBe(false)
        expect(isGalleryDownload({ ...job, cleanupRequired: true })).toBe(false)
        expect(isGalleryDownload({ ...job, historyOnly: true })).toBe(false)
    })
    it('only finished failed/cancelled requests can be deleted or included in bulk retry', () => {
        for (const state of ['failed', 'cancelled'] as const) {
            const target = { ...job, state }
            expect(canDeleteDownload(target)).toBe(true)
            expect(canRetryDownload(target)).toBe(true)
            expect(canDeleteDownload({ ...target, cleanupRequired: true })).toBe(false)
            expect(canRetryDownload({ ...target, cleanupRequired: true })).toBe(false)
            expect(canRetryDownload({ ...target, historyOnly: true })).toBe(false)
            expect(canRetryDownload({ ...target, options: null })).toBe(false)
        }
        for (const state of ['pending', 'running', 'completed', 'interrupted'] as const) {
            expect(canDeleteDownload({ ...job, state })).toBe(false)
            expect(canRetryDownload({ ...job, state })).toBe(false)
        }
    })
    it('미지원 다운로드 형식은 엔진 설치 오류와 구분해 안내한다', () => {
        const result = downloadFailure('UNSUPPORTED_MEDIA_FORMAT')
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.error.message).toContain('MP4·M4A')
            expect(result.error.message).not.toContain('아직 지원하지 않습니다')
        }
    })
    it('Instagram 인증·접근 제한·요청 제한의 안내를 구분한다', () => {
        for (const [code, text] of [['AUTHENTICATION_REQUIRED','쿠키'],['ACCESS_DENIED','열람 권한'],['MEDIA_UNAVAILABLE','삭제'],['RATE_LIMITED','잠시 후'],['UNSUPPORTED_INSTAGRAM_OPTIONS','최고 화질'],['UNSUPPORTED_INSTAGRAM_URL','릴스']]) {
            const result = downloadFailure(code)
            expect(result.ok).toBe(false)
            if (!result.ok) expect(result.error.message).toContain(text)
        }
    })
    it('실제 다운로드 수치만 표시하고 검증·커밋을 완료로 오인하지 않는다', () => {
        expect(visibleProgress(job)).toBeNull()
        expect(visibleProgress({ ...job, progress: 42 })).toBe(42)
        for (const phase of ['discovering', 'verifying', 'committing', 'rolling_back'] as const) {
            expect(visibleProgress({ ...job, phase, progress: 100 })).toBeNull()
        }
        expect(downloadStatus({ ...job, phase: 'committing', progress: 100 })).toBe('저장 중')
        expect(downloadStatus({ ...job, state: 'cancelled', phase: 'finished' })).toBe('취소됨')
    })
    it('커밋·롤백·정리 필요 작업은 취소 조작을 제공하지 않는다', () => {
        expect(canCancelDownload(job)).toBe(true)
        expect(canCancelDownload({ ...job, phase: 'committing' })).toBe(false)
        expect(canCancelDownload({ ...job, phase: 'rolling_back' })).toBe(false)
        expect(canCancelDownload({ ...job, cleanupRequired: true })).toBe(false)
    })
    it('실패와 취소·중단의 재실행 종류를 구분한다', () => {
        expect(downloadTrigger({ ...job, state: 'failed' })).toBe('manual_retry')
        expect(downloadTrigger({ ...job, state: 'cancelled' })).toBe('manual_restart')
        expect(downloadTrigger({ ...job, state: 'interrupted' })).toBe('manual_restart')
        expect(downloadTrigger({ ...job, state: 'completed' })).toBeNull()
    })
    it('YouTube 도메인을 정확히 구분하며 URL 파싱 실패를 허용한다', () => {
        expect(downloadPlatform('https://www.youtube.com/watch?v=id')).toBe('YouTube')
        expect(downloadPlatform('https://youtu.be/id')).toBe('YouTube')
        expect(downloadPlatform('https://notyoutube.com/id')).toBe('notyoutube.com')
        expect(downloadPlatform('broken')).toBe('플랫폼 미확인')
    })
})
