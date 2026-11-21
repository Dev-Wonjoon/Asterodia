export type WebServerResult<T> = { ok: true; data: T } | { ok: false; error: { code: string; message: string } }

export interface WebServerStatus {
    running: boolean
    url: string
    error: string | null
    editing?: boolean
}

export interface WebServerApi {
    setEditing(enabled: boolean): Promise<WebServerResult<WebServerStatus>>
    status(): Promise<WebServerResult<WebServerStatus>>
    start(): Promise<WebServerResult<WebServerStatus>>
    stop(): Promise<WebServerResult<WebServerStatus>>
}
