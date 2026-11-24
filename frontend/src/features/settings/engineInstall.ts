import { translate } from '@renderer/shared/i18n'
import type { EngineInstallProgress } from '@contracts/types'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

const labels: Record<string, string> = {
    get preparing() { return translate("settings.preparingInstallation") }, get downloading() { return translate("download.downloading") }, get extracting() { return translate("settings.extracting") },
    get verifying() { return translate("settings.checkingExecutables") }, get committing() { return translate("settings.applyingInstallationFiles") }, get completed() { return translate("settings.installationCompleted") }, get failed() { return translate("settings.installationFailed") },
}

export interface EngineInstallBatch {
    names: string[]
    processed: number
    succeeded: number
    failed: string[]
}

export function batchProgressView(batch: EngineInstallBatch) {
    return {
        percent: batch.names.length ? batch.processed / batch.names.length * 100 : 0,
        label: translate("settings.processedCompletedFailed", {
            processed: batch.processed, total: batch.names.length, completed: batch.succeeded, failed: batch.failed.length,
        }),
    }
}
export function mergeInstallProgress(previous: EngineInstallProgress | undefined, next: EngineInstallProgress): EngineInstallProgress {
    if (!previous || previous.installId !== next.installId) return next
    if (previous.phase === 'completed' || previous.phase === 'failed') return previous
    return {
        ...next,
        downloaded: Math.max(previous.downloaded, next.downloaded),
        total: next.total ?? previous.total,
    }
}
export function installProgressView(progress: EngineInstallProgress) {
    const downloaded = Number.isFinite(progress.downloaded) ? Math.max(0, progress.downloaded) : 0
    const total = progress.total !== null && Number.isFinite(progress.total) && progress.total > 0 ? progress.total : null
    const percent = progress.phase === 'completed' ? 100 : total === null ? undefined : Math.min(100, downloaded / total * 100)
    return {
        label: labels[progress.phase] ?? translate("settings.engine.installing"),
        percent: progress.phase === 'failed' && percent === undefined ? 0 : percent,
        detail: total === null ? translate("settings.download", { engine: formatBytes(downloaded) }) : `${formatBytes(downloaded)} / ${formatBytes(total)}`,
    }
}
