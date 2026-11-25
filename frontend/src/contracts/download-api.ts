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
        NOT_READY: translate("common.downloadsAreNotReadyYet"),
        IMPORTED_HISTORY: translate("common.thisIsImportedOrMigratedHistoryAddTheUrl"),
        INVALID_ARGUMENT: translate("common.checkTheDownloadUrlAndOptions"),
        UNSUPPORTED_INSTAGRAM_URL: translate("common.enterAnInstagramPostOrReelLink"),
        UNSUPPORTED_INSTAGRAM_OPTIONS: translate("common.forInstagramSelectVideoBestQualityAndAutoFormat"),
        AUTHENTICATION_REQUIRED: translate("common.loginIsRequiredOrYourCookiesHaveExpiredSelect"),
        PUBLIC_MEDIA_EXTRACTION_FAILED: translate("common.publicInstagramMediaCouldNotBeExtracted"),
        ACCESS_DENIED: translate("common.accessToThisPostIsRestrictedCheckYourAccount"),
        MEDIA_UNAVAILABLE: translate("common.thePostWasDeletedOrCannotBeFoundWith"),
        RATE_LIMITED: translate("common.requestsAreTemporarilyLimitedTryAgainLater"),
        NOT_FOUND: translate("common.downloadJobNotFound"),
        ALREADY_PROCESSING: translate("common.aJobIsAlreadyRunning"),
        REDOWNLOAD_CONFIRMATION_REQUIRED: translate("common.confirmWhetherToReplaceTheExistingCollection"),
        REVISION_CONFLICT: translate("common.theCollectionHasChangedRefreshAndCheckAgain"),
        CLEANUP_REQUIRED: translate("common.cleanUpThePreviousRunBeforeRestarting"),
        CANNOT_DELETE_REQUEST: translate("common.onlyFailedOrCancelledRequestsWithCompletedCleanupCan"),
        CANNOT_CANCEL_COMMITTING: translate("common.aJobCannotBeCancelledWhileSaving"),
        DATABASE_ERROR: translate("common.couldNotUpdateTheDownloadDatabase"),
        UNSUPPORTED_TARGET_DISCOVERY: translate("common.theUrlOrOptionsAreUnsupportedOrTheEngine"),
        UNSUPPORTED_MEDIA_FORMAT: translate("common.noMp4OrM4aFormatMatchesTheRequestedType"),
        DOWNLOAD_ENGINE_FAILED: translate("common.theDownloadEngineCouldNotFinishTheJobCheck"),
        ENGINE_STOP_UNCONFIRMED: translate("common.executionIsOnHoldBecauseTheEngineExitStatus")
    }

    return {
        ok: false,
        error: {
            code,
            message: messages[code] ?? translate("common.couldNotProcessTheDownloadRequest"),
            context: {}
        }
    }
}
