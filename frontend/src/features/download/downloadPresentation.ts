import type { DownloadJobDto, ManualDownloadTrigger } from '@contracts/download-api'

export const downloadStates = {
    pending: '대기',
    running: '처리 중',
    completed: '완료',
    failed: '실패',
    cancelled: '취소됨',
    interrupted: '중단됨'
}
export const downloadPhases = {
    discovering: '대상 확인 중',
    downloading: '다운로드 중',
    verifying: '파일 확인 중',
    committing: '저장 중',
    rolling_back: '정리 중',
    finished: '종료'
}
export function downloadPlatform(url: string): string {
    try {
        const host = new URL(url).hostname.toLowerCase()
        if (host === 'youtu.be' || host === 'youtube.com' || host.endsWith('.youtube.com'))
            return 'YouTube'
        return host || '플랫폼 미확인'
    } catch {
        return '플랫폼 미확인'
    }
}
export function downloadStatus(job: DownloadJobDto): string {
    if (job.state === 'running' && job.phase && job.phase !== 'finished')
        return downloadPhases[job.phase]
    return downloadStates[job.state]
}
export function visibleProgress(job: DownloadJobDto): number | null {
    // A completed transfer must not imply a committed job.
    return job.state === 'running' &&
        job.phase === 'downloading' &&
        Number.isFinite(job.progress) &&
        job.progress > 0 &&
        job.progress < 100
        ? job.progress
        : null
}
export function isGalleryDownload(job: DownloadJobDto): boolean {
    return (job.state === 'pending' || job.state === 'running') &&
        !job.historyOnly && !job.cleanupRequired && job.phase !== 'rolling_back'
}
export function downloadTrigger(job: DownloadJobDto): ManualDownloadTrigger | null {
    if (job.state === 'pending') return 'manual_initial'
    if (job.state === 'failed') return 'manual_retry'
    if (job.state === 'cancelled' || job.state === 'interrupted') return 'manual_restart'
    return null
}
export function canCancelDownload(job: DownloadJobDto): boolean {
    return (
        (job.state === 'pending' || job.state === 'running') &&
        job.phase !== 'committing' &&
        job.phase !== 'rolling_back' &&
        !job.cleanupRequired
    )
}

export function canDeleteDownload(job: DownloadJobDto): boolean {
    return (job.state === 'failed' || job.state === 'cancelled') && !job.cleanupRequired
}

export function canRetryDownload(job: DownloadJobDto): boolean {
    return canDeleteDownload(job) && !job.historyOnly && job.options !== null
}
