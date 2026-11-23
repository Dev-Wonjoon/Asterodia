import { translate } from '@renderer/shared/i18n'
import type { DownloadJobDto, ManualDownloadTrigger } from '@contracts/download-api'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

export const downloadStates = {
    get pending() { return translate("대기") },
    get running() { return translate("처리 중") },
    get completed() { return translate("완료") },
    get failed() { return translate("실패") },
    get cancelled() { return translate("취소됨") },
    get interrupted() { return translate("중단됨") }
}
export const downloadPhases = {
    get discovering() { return translate("대상 확인 중") },
    get downloading() { return translate("다운로드 중") },
    get verifying() { return translate("파일 확인 중") },
    get committing() { return translate("저장 중") },
    get rolling_back() { return translate("정리 중") },
    get finished() { return translate("종료") }
}
export function downloadPlatform(url: string): string {
    try {
        const host = new URL(url).hostname.toLowerCase()
        if (host === 'youtu.be' || host === 'youtube.com' || host.endsWith('.youtube.com'))
            return 'YouTube'
        if (host === 'pin.it' || /(^|\.)pinterest\.(com|co\.kr|kr|co\.uk|jp|ca|fr|de|com\.au)$/.test(host)) return 'Pinterest'
        if (host === 'weverse.io' || host.endsWith('.weverse.io')) return 'Weverse'
        return host || translate("플랫폼 미확인")
    } catch {
        return translate("플랫폼 미확인")
    }
}
export function downloadStatus(job: DownloadJobDto): string {
    if (job.state === 'running' && job.phase === 'downloading' && job.transfer?.processing)
        return translate("변환·병합 중")
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
        if (transfer.fileCount > 1) details.push(translate("파일 {0}/{1}", { "0": transfer.fileIndex, "1": transfer.fileCount }))
        const total = transfer.total !== null && Number.isFinite(transfer.total) && transfer.total > 0 ? transfer.total : null
        details.push(total === null
            ? translate("{0}{1} 다운로드", { "0": transfer.batch ? translate("누적 ") : '', "1": formatBytes(transfer.downloaded) })
            : `${formatBytes(transfer.downloaded)} / ${transfer.estimated ? translate("약 ") : ''}${formatBytes(total)}`)
        if (job.phase === 'downloading' && !transfer.processing) {
            if (transfer.speed !== null && Number.isFinite(transfer.speed) && transfer.speed > 0)
                details.push(`${transfer.batch ? translate("약 ") : ''}${formatBytes(transfer.speed)}/s`)
            if (transfer.eta !== null && Number.isFinite(transfer.eta) && transfer.eta > 0) {
                const seconds = Math.ceil(transfer.eta)
                const minutes = Math.floor(seconds / 60)
                const hours = Math.floor(minutes / 60)
                const duration = hours > 0 ? translate("{0}시간 {1}분", { "0": hours, "1": minutes % 60 }) : minutes > 0 ? translate("{0}분 {1}초", { "0": minutes, "1": seconds % 60 }) : translate("{0}초", { "0": seconds })
                details.push(translate("약 {0} 남음", { "0": duration }))
            }
        }
    }
    return {
        percent: progress ?? undefined,
        percentLabel: progress === null ? '' : `${transfer?.estimated ? translate("약 ") : ''}${progress.toFixed(1)}%`,
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
