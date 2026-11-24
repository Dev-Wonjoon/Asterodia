import { getLocale, translate } from '@renderer/shared/i18n'
import type { DownloadLogEntry, DownloadLogPage } from '@contracts/download-api'

export function formatDownloadLog(entry: DownloadLogEntry): string {
    const time = entry.recordedAt === null ? translate("download.previousRecord") : new Date(entry.recordedAt).toLocaleString(getLocale(), { hour12: false })
    return `[${time}] [${entry.engine}/${entry.stream}] ${entry.text}`
}
export function pollDownloadLogs(page: DownloadLogPage, before: number, attempt: string): boolean {
    return before === 0 && (page.state === 'running' || page.state === 'pending') &&
        (attempt === '' || attempt === page.currentAttemptId)
}
