export type WebServerResult<T> = { ok: true; data: T } | { ok: false; error: { code: string; message: string } }

export interface WebServerStatus {
    requireCode?: boolean
    pairCode?: string
    codeExpires?: number
    connected?: number
    running: boolean
    url: string
    error: string | null
    editing?: boolean
    downloading?: boolean
}

export interface WebServerApi {
    setRequireCode(required: boolean): Promise<WebServerResult<WebServerStatus>>
    revokeAccess(): Promise<WebServerResult<WebServerStatus>>
    setEditing(enabled: boolean): Promise<WebServerResult<WebServerStatus>>
    setDownloading(enabled: boolean): Promise<WebServerResult<WebServerStatus>>
    status(): Promise<WebServerResult<WebServerStatus>>
    start(): Promise<WebServerResult<WebServerStatus>>
    stop(): Promise<WebServerResult<WebServerStatus>>
}
