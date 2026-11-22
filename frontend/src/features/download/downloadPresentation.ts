import type { DownloadJobDto, ManualDownloadTrigger } from '@contracts/download-api'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

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
        if (host === 'pin.it' || /(^|\.)pinterest\.(com|co\.kr|kr|co\.uk|jp|ca|fr|de|com\.au)$/.test(host)) return 'Pinterest'
        if (host === 'weverse.io' || host.endsWith('.weverse.io')) return 'Weverse'
        return host || '플랫폼 미확인'
    } catch {
        return '플랫폼 미확인'
    }
}
export function downloadStatus(job: DownloadJobDto): string {
    if (job.state === 'running' && job.phase === 'downloading' && job.transfer?.processing)
        return '변환·병합 중'
    if (job.state === 'running' && job.phase && job.phase !== 'finished')
        return downloadPhases[job.phase]
    return downloadStates[job.state]
}
export function visibleProgress(job: DownloadJobDto): number | null {
    if (job.state === 'running' && job.phase === 'downloading' && job.transfer) {
        const percent = job.transfer.percent
        return percent !== null && Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : null
    }
    // A completed transfer must not imply a committed job.
    return job.state === 'running' &&
        job.phase === 'downloading' &&
        Number.isFinite(job.progress) &&
        job.progress > 0 &&
        job.progress < 100
        ? job.progress
        : null
}

export function downloadProgressView(job: DownloadJobDto) {
    const progress = visibleProgress(job)
    const transfer = job.transfer
    const details: string[] = []
    if (transfer && job.state === 'running') {
        if (transfer.fileCount > 1) details.push(`파일 ${transfer.fileIndex}/${transfer.fileCount}`)
        const total = transfer.total !== null && Number.isFinite(transfer.total) && transfer.total > 0 ? transfer.total : null
        details.push(total === null
            ? `${transfer.batch ? '누적 ' : ''}${formatBytes(transfer.downloaded)} 다운로드`
            : `${formatBytes(transfer.downloaded)} / ${transfer.estimated ? '약 ' : ''}${formatBytes(total)}`)
        if (job.phase === 'downloading' && !transfer.processing) {
            if (transfer.speed !== null && Number.isFinite(transfer.speed) && transfer.speed > 0)
                details.push(`${transfer.batch ? '약 ' : ''}${formatBytes(transfer.speed)}/s`)
            if (transfer.eta !== null && Number.isFinite(transfer.eta) && transfer.eta > 0) {
                const seconds = Math.ceil(transfer.eta)
                const minutes = Math.floor(seconds / 60)
                const hours = Math.floor(minutes / 60)
                const duration = hours > 0 ? `${hours}시간 ${minutes % 60}분` : minutes > 0 ? `${minutes}분 ${seconds % 60}초` : `${seconds}초`
                details.push(`약 ${duration} 남음`)
            }
        }
    }
    return {
        percent: progress ?? undefined,
        percentLabel: progress === null ? '' : `${transfer?.estimated ? '약 ' : ''}${progress.toFixed(1)}%`,
        detail: details.join(' · '),
    }
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
