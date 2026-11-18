export interface LocalImportCounts {
    discovered: number; excluded: number; scanExcluded: number; userExcluded: number; scanErrors: number
    added: number; updated: number; unchanged: number
    completedFolders: number; failedFolders: number
    failed: number; processed: number; totalBytes: number; processedBytes: number
}
export interface LocalImportJob {
    id: string; root: string
    state: 'scanning' | 'ready' | 'running' | 'paused' | 'completed' | 'completed_with_errors' | 'failed' | 'cancelled' | 'interrupted'
    counts: LocalImportCounts; cancelRequested: boolean
    pauseRequested: boolean; scanComplete: boolean; canResume: boolean
    currentPath: string; error: string | null; elapsedMillis: number
    bytesPerSecond: number; filesPerSecond: number; largeFolders: number; revision: number
}
export interface LocalImportPage {
    job: LocalImportJob | null
    folders: { id: string; path: string; state: string; error: string | null; fileCount: number }[]
    total: number; hasNextPage: boolean
}
export type LocalImportResult<T> = { ok: true; data: T } | { ok: false; error: { code: string; message: string } }
export type LocalImportFileFilter = '' | 'pending' | 'failed' | 'excluded' | 'registered'
export interface LocalImportFilePage {
    files: { id: string; folderId: string; path: string; state: string; error: string | null }[]
    total: number; hasNextPage: boolean
}
export interface LocalImportTargetRequest { jobId: string; targetId?: string; allFailed?: boolean }
export interface LocalImportApi {
    choose(): Promise<LocalImportResult<LocalImportPage>>
    get(request?: { jobId?: string; offset?: number; limit?: number }): Promise<LocalImportResult<LocalImportPage>>
    run(request: { jobId: string }): Promise<LocalImportResult<boolean>>
    cancel(request: { jobId: string }): Promise<LocalImportResult<boolean>>
    rescan(request: { jobId: string }): Promise<LocalImportResult<LocalImportPage>>
    pause(request: { jobId: string }): Promise<LocalImportResult<boolean>>
    resume(request: { jobId: string }): Promise<LocalImportResult<boolean>>
    files(request: { jobId: string; folderId?: string; fileState?: LocalImportFileFilter; offset?: number; limit?: number }): Promise<LocalImportResult<LocalImportFilePage>>
    retry(request: LocalImportTargetRequest): Promise<LocalImportResult<boolean>>
    exclude(request: LocalImportTargetRequest): Promise<LocalImportResult<boolean>>
}
