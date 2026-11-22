export interface UpdateStatus {
    currentVersion: string
    state: 'idle' | 'unconfigured' | 'checking' | 'up-to-date' | 'available' | 'downloading' | 'verifying' | 'installing' | 'ready' | 'error' | 'restarting' | 'unsupported'
    version: string
    notes: string
    size: number
    written: number
    error: string
    lastChecked: number
    supported: boolean
}

export type UpdateResult = { ok: true; data: UpdateStatus } | { ok: false; error: { code: string; message: string } }

export interface UpdateApi {
    status(): Promise<UpdateResult>
    check(): Promise<UpdateResult>
    download(): Promise<UpdateResult>
    restart(): Promise<UpdateResult>
}
