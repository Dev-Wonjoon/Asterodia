export interface MoveSelection { mediaId: number; revision: number; fileIds?: number[] }
export interface MoveRequest { jobId?: string; offset?: number }
export interface MoveItem { id: string; mediaId: number; fileId: number; source: string; target: string; state: string; error: string }
export interface MoveStatus {
 jobId: string; directory: string; operation?: 'copy' | 'move'; state: string; total: number; completed: number; failed: number; cleanupPending: number
 totalBytes: number; processedBytes: number; current: string; items: MoveItem[]; offset: number; hasMore: boolean
}
export type MoveResult<T> = { ok: true; data: T } | { ok: false; error: { code: string; message: string } }
export interface FileMoveApi {
 create(request: { selection: MoveSelection[]; directoryId: number; operation?: 'copy' | 'move' }): Promise<MoveResult<{ jobId: string }>>
 get(request?: MoveRequest): Promise<MoveResult<MoveStatus | null>>
 run(request: MoveRequest): Promise<MoveResult<{ jobId: string }>>
 pause(request: MoveRequest): Promise<MoveResult<{ jobId: string }>>
 retry(request: MoveRequest): Promise<MoveResult<{ jobId: string }>>
}
