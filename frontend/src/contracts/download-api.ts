import { translate } from '@renderer/shared/i18n'
import type { DownloadOptions } from './types'

export type DownloadEngineKind = 'yt-dlp' | 'gallery-dl'

export type DownloadJobOptions = Pick<
    DownloadOptions,
    'type' | 'quality' | 'playlist' | 'extension'
>

export type DownloadState =
    'pending' | 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted'

export type DownloadPhase =
    'discovering' | 'downloading' | 'verifying' | 'committing' | 'rolling_back' | 'finished'

export type ManualDownloadTrigger = 'manual_initial' | 'manual_retry' | 'manual_restart'

export type DownloadResponse<T> =
    | {
          ok: true
          data: T
      }
    | {
          ok: false
          error: {
              code: string
              message: string
              context: Record<string, number>
          }
      }

export type DownloadEnqueueRequest = {
    url: string
    options?: DownloadJobOptions
} & (
    | {
          mode: 'create'
      }
    | {
          mode: 'replace'
          mediaId: number
          expectedRevision: number
      }
)

export interface DownloadTransferProgress {
    downloaded: number
    total: number | null
    estimated: boolean
    speed: number | null
    eta: number | null
    percent: number | null
    fileIndex: number
    fileCount: number
    processing: boolean
    batch: boolean
}

export interface DownloadJobDto {
    historyOnly?: boolean
    id: string
    url: string
    thumbnailPath?: string | null
    title: string | null
    state: DownloadState
    phase: DownloadPhase | null
    progress: number
    waiting?: boolean
    transfer?: DownloadTransferProgress | null

    // null은 기존 옵션을 현재 실행기가 지원하지 않는 상태다.
    options: DownloadJobOptions | null

    cleanupRequired: boolean
    currentAttemptId: string | null
    errorCode: string | null
    errorMessage?: string | null
}

export interface DownloadPageDto {
    data: DownloadJobDto[]
    activeJobs?: DownloadJobDto[]
    limit: number
    offset: number
    hasNextPage: boolean
}

export type DownloadRunDto =
    | {
          status: 'committed'
          jobId: string
          attemptId: string
          mediaIdSnapshot: number
      }
    | {
          status: 'cleanup_required'
          jobId: string
          attemptId: string
          taskId: string
      }
    | {
          status: 'blocked'
          jobId: string
          reason: string
      }

export interface DownloadApi {
    remove(request: { jobId: string }): Promise<DownloadResponse<{ accepted: true }>>
    logs(request: { jobId: string; attemptId?: string; before?: number; limit?: number }): Promise<DownloadResponse<DownloadLogPage>>
    enqueue(request: DownloadEnqueueRequest): Promise<DownloadResponse<DownloadJobDto>>

    list(request?: { limit?: number; offset?: number }): Promise<DownloadResponse<DownloadPageDto>>

    get(request: { jobId: string }): Promise<DownloadResponse<DownloadJobDto>>

    run(request: {
        jobId: string
        trigger: ManualDownloadTrigger
    }): Promise<DownloadResponse<DownloadRunDto>>

    cancel(request: { jobId: string }): Promise<DownloadResponse<{ accepted: true }>>
    cleanup?(request: { jobId: string }): Promise<DownloadResponse<{ accepted: true }>>

    previewEngine(
    url: string,
    options?: DownloadJobOptions
): Promise<DownloadEngineKind>
}

export interface DownloadLogEntry {
    sequence: number
    attemptId: string
    recordedAt: number | null
    engine: string
    stream: 'stdout' | 'stderr' | 'system' | 'legacy'
    text: string
}
export interface DownloadLogPage {
    entries: DownloadLogEntry[]
    attempts: { id: string; number: number; outcome: string | null }[]
    currentAttemptId: string | null
    state: DownloadState
    hasOlder: boolean
    truncated: boolean
    warning: string | null
}

export interface DownloadServicePort {
    enqueue(request: unknown): ReturnType<DownloadApi['enqueue']>
    list(request?: unknown): ReturnType<DownloadApi['list']>
    get(request: unknown): ReturnType<DownloadApi['get']>
    run(request: unknown): ReturnType<DownloadApi['run']>
    cancel(request: unknown): ReturnType<DownloadApi['cancel']>
}

export function downloadFailure(code: string): DownloadResponse<never> {
    const messages: Record<string, string> = {
        NOT_READY: translate("다운로드 기능을 사용할 준비가 되지 않았습니다."),
        IMPORTED_HISTORY: translate("가져오거나 이관한 작업 이력입니다. 다시 다운로드하려면 URL을 새로 등록하세요."),
        INVALID_ARGUMENT: translate("다운로드 요청 값이나 옵션을 확인하세요."),
        UNSUPPORTED_INSTAGRAM_URL: translate("Instagram 게시물 또는 릴스 링크를 입력하세요."),
        UNSUPPORTED_INSTAGRAM_OPTIONS: translate("Instagram은 종류: 영상, 화질: 최고 화질, 형식: 자동으로 설정하세요."),
        AUTHENTICATION_REQUIRED: translate("로그인이 필요하거나 쿠키가 만료됐습니다. 설정에서 브라우저 또는 쿠키 파일을 지정하세요."),
        ACCESS_DENIED: translate("게시물 접근이 제한됐습니다. 계정의 열람 권한이나 추가 인증을 확인하세요."),
        MEDIA_UNAVAILABLE: translate("게시물이 삭제되었거나 현재 계정으로 찾을 수 없습니다."),
        RATE_LIMITED: translate("요청이 일시적으로 제한됐습니다. 잠시 후 다시 시도하세요."),
        NOT_FOUND: translate("다운로드 작업을 찾을 수 없습니다."),
        ALREADY_PROCESSING: translate("이미 처리 중인 작업이 있습니다."),
        REDOWNLOAD_CONFIRMATION_REQUIRED: translate("등록된 묶음의 교체 여부를 확인하세요."),
        REVISION_CONFLICT: translate("묶음 정보가 변경되었습니다. 새로고침 후 다시 확인하세요."),
        CLEANUP_REQUIRED: translate("이전 실행의 정리를 마친 후 다시 실행할 수 있습니다."),
        CANNOT_DELETE_REQUEST: translate("정리가 끝난 실패·취소 요청 기록만 삭제할 수 있습니다."),
        CANNOT_CANCEL_COMMITTING: translate("저장 중인 작업은 취소할 수 없습니다."),
        DATABASE_ERROR: translate("다운로드 DB 작업을 처리하지 못했습니다."),
        UNSUPPORTED_TARGET_DISCOVERY: translate("지원하지 않는 주소·옵션이거나 엔진 버전이 맞지 않습니다."),
        UNSUPPORTED_MEDIA_FORMAT: translate("요청한 종류·화질에 맞는 MP4·M4A 포맷이 없습니다. 다른 화질이나 옵션을 선택하세요."),
        DOWNLOAD_ENGINE_FAILED: translate("다운로드 엔진이 작업을 완료하지 못했습니다. 실제 오류와 실행 로그를 확인하세요."),
        ENGINE_STOP_UNCONFIRMED: translate("엔진 종료 상태를 확인할 수 없어 실행을 보류했습니다.")
    }

    return {
        ok: false,
        error: {
            code,
            message: messages[code] ?? translate("다운로드 요청을 처리하지 못했습니다."),
            context: {}
        }
    }
}
