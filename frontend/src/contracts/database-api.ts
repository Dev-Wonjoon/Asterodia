export interface DatabaseImportResult {
    cancelled: boolean
    sourceVersion: number
    addedMedia: number
    addedFiles: number
    skippedMedia: number
    skippedFiles: number
    addedHistory: number
    missingFiles: number
    backupPath: string
    sourceSnapshot: string
}
export type DatabaseResult<T> =
    | { ok: true; data: T }
    | { ok: false; error: { code: string; message: string; context: Record<string, number> } }

export interface DatabaseApi {
    import(): Promise<DatabaseResult<DatabaseImportResult>>
}
