import type { LocalImportJob } from '@contracts/local-import-api'

const key = 'storagebucket:closed-import-result'
export function canDismissImport(job: LocalImportJob | null | undefined, requesting: boolean): boolean {
    return !requesting && !!job && (job.state === 'completed' || job.state === 'completed_with_errors')
}
export function readDismissedImport(): string | null {
    try { return localStorage.getItem(key) } catch { return null }
}
export function writeDismissedImport(jobId: string | null): void {
    try {
        if (jobId === null) localStorage.removeItem(key)
        else localStorage.setItem(key, jobId)
    } catch { /* The current view still works when browser storage is unavailable. */ }
}
