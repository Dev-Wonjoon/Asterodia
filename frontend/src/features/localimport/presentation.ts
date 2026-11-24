import { translate } from '@renderer/shared/i18n'
import type { LocalImportJob } from '@contracts/local-import-api'

export const states: Record<string, string> = {
    get scanning() { return translate("localImport.scanningFolders") }, get ready() { return translate("localImport.scanCompletedAwaitingRegistration") }, get running() { return translate("localImport.registering") },
    get completed() { return translate("localImport.completed") }, get completed_with_errors() { return translate("localImport.completedSomeFailures") }, get paused() { return translate("download.interrupted") },
    get failed() { return translate("localImport.stoppedDueToError") }, get cancelled() { return translate("localImport.finished") }, get interrupted() { return translate("localImport.interruptedWhenTheAppClosed") },
    get pending() { return translate("download.pending") }, get registered() { return translate("localImport.registered") }, get unchanged() { return translate("localImport.alreadyRegistered") }, get excluded() { return translate("localImport.excludeFromThisJob") },
}
export function importProgress(job: LocalImportJob): number | undefined {
    if (!job.scanComplete) return undefined
    if (job.counts.discovered === 0) return 0
    return Math.max(0, Math.min(100, job.counts.processed / job.counts.discovered * 100))
}
export function canResume(job: LocalImportJob): boolean {
    return job.canResume && ['paused', 'interrupted', 'cancelled', 'failed'].includes(job.state)
}
