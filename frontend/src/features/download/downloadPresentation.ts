import { translate } from '@renderer/shared/i18n'
import type { DownloadJobDto, ManualDownloadTrigger } from '@contracts/download-api'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

export const downloadStates = {
    get pending() { return translate("download.pending") },
    get running() { return translate("download.processing") },
    get completed() { return translate("download.completed") },
    get failed() { return translate("download.failed") },
    get cancelled() { return translate("download.cancelled") },
    get interrupted() { return translate("download.interrupted") }
}
export const downloadPhases = {
    get discovering() { return translate("download.checkingSources") },
    get downloading() { return translate("download.downloading") },
    get verifying() { return translate("download.verifyingFiles") },
    get committing() { return translate("download.saving") },
    get rolling_back() { return translate("download.state.cleaningUp") },
    get finished() { return translate("download.finished") }
}
export function downloadPlatform(url: string): string {
    try {
        const host = new URL(url).hostname.toLowerCase()
        if (host === 'youtu.be' || host === 'youtube.com' || host.endsWith('.youtube.com'))
            return 'YouTube'
        if (host === 'pin.it' || /(^|\.)pinterest\.(com|co\.kr|kr|co\.uk|jp|ca|fr|de|com\.au)$/.test(host)) return 'Pinterest'
        if (host === 'weverse.io' || host.endsWith('.weverse.io')) return 'Weverse'
        return host || translate("download.unknownPlatform")
    } catch {
        return translate("download.unknownPlatform")
    }
}
export function downloadStatus(job: DownloadJobDto): string {
    if (isDownloadWaiting(job)) return translate("download.waitingToDownload")
    if (job.state === 'running' && job.phase === 'downloading' && job.transfer?.processing)
        return translate("download.convertingAndMerging")
    if (job.state === 'running' && job.phase && job.phase !== 'finished')
        return downloadPhases[job.phase]
    return downloadStates[job.state]
}
export function visibleProgress(job: DownloadJobDto): number | null {
    if (isDownloadWaiting(job)) return null
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

export function isDownloadWaiting(job: DownloadJobDto): boolean {
    return !job.historyOnly && !job.cleanupRequired &&
        (job.state === 'pending' || job.state === 'running' && job.waiting === true)
}

export function downloadProgressView(job: DownloadJobDto) {
    const progress = visibleProgress(job)
    const transfer = job.transfer
    const details: string[] = []
    if (transfer && job.state === 'running') {
        if (transfer.fileCount > 1) details.push(translate("download.file", { current: transfer.fileIndex, total: transfer.fileCount }))
        const total = transfer.total !== null && Number.isFinite(transfer.total) && transfer.total > 0 ? transfer.total : null
        details.push(total === null
            ? translate("download.downloaded", { totalLabel: transfer.batch ? translate("download.total") : '', size: formatBytes(transfer.downloaded) })
            : `${formatBytes(transfer.downloaded)} / ${transfer.estimated ? translate("download.about") : ''}${formatBytes(total)}`)
        if (job.phase === 'downloading' && !transfer.processing) {
            if (transfer.speed !== null && Number.isFinite(transfer.speed) && transfer.speed > 0)
                details.push(`${transfer.batch ? translate("download.about") : ''}${formatBytes(transfer.speed)}/s`)
            if (transfer.eta !== null && Number.isFinite(transfer.eta) && transfer.eta > 0) {
                const seconds = Math.ceil(transfer.eta)
                const minutes = Math.floor(seconds / 60)
                const hours = Math.floor(minutes / 60)
                const duration = hours > 0 ? translate("download.hM", { hours: hours, minutes: minutes % 60 }) : minutes > 0 ? translate("download.mS", { minutes: minutes, seconds: seconds % 60 }) : translate("download.s", { seconds: seconds })
                details.push(translate("download.aboutRemaining", { duration: duration }))
            }
        }
    }
    return {
        percent: progress ?? undefined,
        percentLabel: progress === null ? '' : `${transfer?.estimated ? translate("download.about") : ''}${progress.toFixed(1)}%`,
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
