export interface ThumbnailRequest {
    mediaId: number
    fileId: number
}

export interface ThumbnailStatusDto {
    state: 'unsupported' | 'registered' | 'missing' | 'pending' | 'running' | 'failed' | 'blocked'
    taskId: string | null
    canRetry: boolean
}

export type ThumbnailErrorCode =
    | 'INVALID_ARGUMENT'
    | 'NOT_FOUND'
    | 'NOT_READY'
    | 'NOT_RETRYABLE'
    | 'SOURCE_BUSY'
    | 'SOURCE_CHANGED'
    | 'INTERNAL_ERROR'

export type ThumbnailResult<T> =
    | { ok: true; data: T }
    | {
          ok: false
          error: {
              code: ThumbnailErrorCode
              message: string
          }
      }

export interface ThumbnailCacheUsage { files:number; bytes:number; unusedFiles:number; unusedBytes:number; removedFiles:number; removedBytes:number }

export interface ThumbnailApi {
    cacheUsage?(): Promise<ThumbnailResult<ThumbnailCacheUsage>>
    clearUnusedCache?(): Promise<ThumbnailResult<ThumbnailCacheUsage>>
    get(request: ThumbnailRequest): Promise<ThumbnailResult<ThumbnailStatusDto>>

    retry(request: ThumbnailRequest): Promise<ThumbnailResult<ThumbnailStatusDto>>
}
