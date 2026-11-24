import { translateError, translate } from '@renderer/shared/i18n'
import type { Confirm } from '@renderer/shared/hooks/confirmContext'
import type { Unsubscribe } from '@contracts/api'
import type {
    DownloadApi,
    DownloadJobDto,
    DownloadResponse,
    ManualDownloadTrigger
} from '@contracts/download-api'
import type { DownloadJobOptions } from '@contracts/download-api'
import { canRetryDownload, downloadTrigger, isGalleryDownload } from './downloadPresentation'

export const DOWNLOAD_PAGE_SIZE = 50

type DownloadError = Extract<DownloadResponse<never>, { ok: false }>['error']

export interface DownloadNotice {
    message: string
    type: 'success' | 'error' | 'info'
}

export interface DownloadSnapshot {
    queue: DownloadJobDto[]
    activeJobs: DownloadJobDto[]
    mediaRevision: number
    ready: boolean
    loading: boolean
    error: DownloadError | null
    halted: string | null
    offset: number
    hasNextPage: boolean
    requestedIds: string[]
    cancellingIds: string[]
    deletingIds: string[]
    batchRetry: { running: boolean; total: number; processed: number; failed: number; skipped: number } | null
    detail: {
        jobId: string | null
        data: DownloadJobDto | null
        loading: boolean
        error: DownloadError | null
    }
}

export interface DownloadStore {
    getSnapshot(): DownloadSnapshot
    subscribe(listener: () => void): Unsubscribe
    connect(notify: (notice: DownloadNotice) => void): Unsubscribe
    refresh(offset?: number): Promise<void>
    startDownload(url: string, options?: DownloadJobOptions): Promise<DownloadResponse<DownloadJobDto>>
    runJob(jobId: string, trigger: ManualDownloadTrigger): Promise<void>
    cancelDownload(jobId: string): Promise<void>
    deleteJob(jobId: string): Promise<void>
    retryAll(): Promise<void>
    openDetail(jobId: string): Promise<void>
    closeDetail(): void
}

function failure(code: string, message: string): DownloadResponse<never> {
    return {
        ok: false,
        error: { code, message, context: {} }
    }
}

export function createDownloadStore(api: DownloadApi, pollMs = 2000, confirm: Confirm = async () => false): DownloadStore {
    let snapshot: DownloadSnapshot = {
        queue: [],
        activeJobs: [],
        mediaRevision: 0,
        ready: false,
        loading: false,
        error: null,
        halted: null,
        offset: 0,
        hasNextPage: false,
        requestedIds: [],
        cancellingIds: [],
        deletingIds: [],
        batchRetry: null,
        detail: { jobId: null, data: null, loading: false, error: null }
    }

    const listeners = new Set<() => void>()
    const requested = new Set<string>()
    const cancelling = new Set<string>()
    const deleting = new Set<string>()
    const batchPending = new Set<string>()
    const completed = new Set<string>()

    const observed = new Map<
        string,
        {
            state: DownloadJobDto['state']
            attemptId: string | null
        }
    >()

    let connected = false
    let generation = 0
    let readSerial = 0
    let detailSerial = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    let notify: ((notice: DownloadNotice) => void) | undefined

    function publish(change: Partial<DownloadSnapshot>) {
        snapshot = { ...snapshot, ...change }

        for (const listener of listeners) {
            try {
                listener()
            } catch {
                // 다른 구독자에게 계속 전달한다.
            }
        }
    }

    function publishRequested() {
        publish({ requestedIds: [...new Set([...requested, ...batchPending])] })
    }

    function notice(message: string, type: DownloadNotice['type']) {
        if (!connected) return

        try {
            notify?.({ message, type })
        } catch {
            // 알림 실패는 작업 결과를 바꾸지 않는다.
        }
    }

    function commit(jobId: string, attemptId: string) {
        const key = JSON.stringify([jobId, attemptId])

        if (completed.has(key)) return

        completed.add(key)

        if (completed.size > 2000) {
            completed.delete(completed.values().next().value!)
        }

        publish({
            mediaRevision: snapshot.mediaRevision + 1
        })
    }

    function observe(job: DownloadJobDto) {
        const previous = observed.get(job.id)

        if (job.state === 'completed' && job.currentAttemptId) {
            if (
                previous &&
                (previous.state !== 'completed' || previous.attemptId !== job.currentAttemptId)
            ) {
                commit(job.id, job.currentAttemptId)
            } else if (!previous) {
                completed.add(JSON.stringify([job.id, job.currentAttemptId]))

                if (completed.size > 2000) {
                    completed.delete(completed.values().next().value!)
                }
            }
        }

        observed.set(job.id, {
            state: job.state,
            attemptId: job.currentAttemptId
        })
    }

    async function refreshDetail(): Promise<void> {
        const id = snapshot.detail.jobId
        if (!connected || id === null) return
        const ownGeneration = generation
        const serial = ++detailSerial
        try {
            const result = await api.get({ jobId: id })
            if (
                !connected ||
                generation !== ownGeneration ||
                serial !== detailSerial ||
                snapshot.detail.jobId !== id
            )
                return
            if (result.ok) {
                const job = structuredClone(result.data)
                observe(job)
                publish({ detail: { jobId: id, data: job, loading: false, error: null } })
            } else {
                publish({
                    detail: {
                        jobId: id,
                        data: null,
                        loading: false,
                        error: structuredClone(result.error)
                    }
                })
            }
        } catch {
            if (
                !connected ||
                generation !== ownGeneration ||
                serial !== detailSerial ||
                snapshot.detail.jobId !== id
            )
                return
            publish({
                detail: {
                    jobId: id,
                    data: null,
                    loading: false,
                    error: {
                        code: 'TRANSPORT_ERROR',
                        message: translate("download.couldNotLoadJobDetailsPleaseRefresh"),
                        context: {}
                    }
                }
            })
        }
    }

    async function openDetail(jobId: string): Promise<void> {
        ++detailSerial
        publish({ detail: { jobId, data: null, loading: true, error: null } })
        await refreshDetail()
    }

    function closeDetail(): void {
        ++detailSerial
        publish({ detail: { jobId: null, data: null, loading: false, error: null } })
    }

    async function refresh(offset = snapshot.offset) {
        if (!connected) return

        const ownGeneration = generation
        const serial = ++readSerial

        publish({ loading: true })

        try {
            const result = await api.list({
                limit: DOWNLOAD_PAGE_SIZE,
                offset
            })

            if (!connected || generation !== ownGeneration || serial !== readSerial) {
                return
            }

            if (!result.ok) {
                publish({
                    loading: false,
                    ready: false,
                    error: structuredClone(result.error)
                })
                return
            }

            const page = structuredClone(result.data)
            if (page.data.length === 0 && page.offset > 0) {
                await refresh(Math.max(0, page.offset - DOWNLOAD_PAGE_SIZE))
                return
            }

            const activeJobs = (page.activeJobs ?? page.data).filter(isGalleryDownload)
            const activeIds = new Set(activeJobs.map(job => job.id))
            const pageIds = new Set(page.data.map(job => job.id))
            const outsidePage = [...observed].filter(([id, job]) =>
                (job.state === 'pending' || job.state === 'running') && !activeIds.has(id) && !pageIds.has(id))
            const ended = await Promise.all(outsidePage.map(async ([jobId]) => {
                try { return await api.get({ jobId }) } catch { return null }
            }))
            if (!connected || generation !== ownGeneration || serial !== readSerial) return
            for (const result of ended) if (result?.ok) observe(result.data)
            for (const job of [...page.data, ...activeJobs]) {
                observe(job)
            }

            const keep = new Set([
                ...page.data.map((job) => job.id),
                ...activeIds,
                ...outsidePage.filter((_, index) => !ended[index]?.ok).map(([id]) => id),
                ...requested,
                ...batchPending,
                ...(snapshot.detail.jobId ? [snapshot.detail.jobId] : [])
            ])

            for (const id of observed.keys()) {
                if (!keep.has(id)) observed.delete(id)
            }

            publish({
                queue: page.data,
                activeJobs,
                offset: page.offset,
                hasNextPage: page.hasNextPage,
                loading: false,
                ready: true,
                error: null
            })
            await refreshDetail()
        } catch {
            if (!connected || generation !== ownGeneration || serial !== readSerial) {
                return
            }

            const result = failure('TRANSPORT_ERROR', translate("download.couldNotLoadJobs"))

            if (!result.ok) {
                publish({
                    loading: false,
                    ready: false,
                    error: result.error
                })
            }
        }
    }

    function halt(message: string) {
        if (snapshot.halted) return

        publish({ halted: message })
        notice(message, 'error')
    }

    async function executeJob(jobId: string, trigger: ManualDownloadTrigger, quiet: boolean) {
        if (!connected || !snapshot.ready || snapshot.halted || requested.has(jobId) || batchPending.has(jobId) || deleting.has(jobId) || cancelling.has(jobId)) {
            return
        }

        requested.add(jobId)
        publishRequested()

        try {
            // 동시 실행 한도와 대기는 백엔드 DownloadService가 관리한다.
            const result = await api.run({ jobId, trigger })

            if (!result.ok) {
                if (
                    [
                        'DATABASE_ERROR',
                        'COMMIT_STATE_UNKNOWN',
                        'NOT_READY',
                        'INTERNAL_ERROR'
                    ].includes(result.error.code)
                ) {
                    halt(translate("download.furtherExecutionRequestsAreOnHoldUntilTheExecution"))
                } else if (!quiet) {
                    notice(translateError(result.error), 'error')
                }
            } else if (result.data.status === 'blocked') {
                halt(translate("download.jobStatusNeedsCheckingFurtherExecutionRequestsAreOn"))
            } else if (result.data.status === 'committed') {
                commit(result.data.jobId, result.data.attemptId)

                if (!quiet) notice(translate("download.downloadAdded"), 'success')
            } else if (!quiet) {
                notice(translate("download.thisRunRequiresCleanupCheckTheJobList"), 'info')
            }
            return result.ok && result.data.status === 'committed'
        } catch {
            halt(translate("download.noExecutionResponseReceivedFurtherRequestsAreOnHold"))
            return false
        } finally {
            requested.delete(jobId)
            publishRequested()
            await refresh()
        }
    }

    async function runJob(jobId: string, trigger: ManualDownloadTrigger) {
        await executeJob(jobId, trigger, false)
    }

    async function retryAll() {
        if (!connected || !snapshot.ready || snapshot.halted || snapshot.batchRetry?.running) return
        const ownGeneration = generation
        const targets: { id: string; trigger: ManualDownloadTrigger }[] = []
        const seen = new Set<string>()
        const counts = { total: 0, processed: 0, failed: 0, skipped: 0 }
        publish({ batchRetry: { running: true, ...counts } })
        try {
            // Take one snapshot before running: failures are never added back to this batch.
            for (let offset = 0; ; ) {
                const result = await api.list({ limit: 200, offset })
                if (!connected || generation !== ownGeneration) return
                if (!result.ok) { notice(translateError(result.error), 'error'); return }
                for (const job of result.data.data) {
                    if (seen.has(job.id)) continue
                    seen.add(job.id)
                    if (job.state !== 'failed' && job.state !== 'cancelled') continue
                    if (!canRetryDownload(job) || requested.has(job.id) || cancelling.has(job.id) || deleting.has(job.id)) {
                        counts.skipped++
                        continue
                    }
                    targets.push({ id: job.id, trigger: downloadTrigger(job)! })
                }
                if (!result.data.hasNextPage) break
                if (result.data.data.length === 0) throw new Error('empty retry page')
                offset += result.data.data.length
            }
            counts.total = targets.length
            for (const target of targets) batchPending.add(target.id)
            publishRequested()
            publish({ batchRetry: { running: true, ...counts } })
            for (const target of targets) {
                if (!connected || generation !== ownGeneration || !snapshot.ready || snapshot.halted) break
                batchPending.delete(target.id)
                const success = await executeJob(target.id, target.trigger, true)
                if (!connected || generation !== ownGeneration) return
                counts.processed++
                if (success === false) counts.failed++
                if (success === undefined) counts.skipped++
                publish({ batchRetry: { running: true, ...counts } })
            }
            notice(translate("download.retry.summary", { processed: counts.processed, total: counts.total, failed: counts.failed, skipped: counts.skipped }), counts.failed || snapshot.halted ? 'error' : 'info')
        } catch {
            if (connected && generation === ownGeneration) notice(translate("download.couldNotCheckRetryCandidatesRefreshTheJobList"), 'error')
        } finally {
            if (generation === ownGeneration) {
                batchPending.clear()
                publishRequested()
                publish({ batchRetry: { running: false, ...counts } })
            }
        }
    }

    async function deleteJob(jobId: string) {
        if (!connected || deleting.has(jobId) || requested.has(jobId) || cancelling.has(jobId) || snapshot.batchRetry?.running) return
        deleting.add(jobId)
        ++readSerial
        publish({ deletingIds: [...deleting] })
        try {
            const result = await api.remove({ jobId })
            if (!result.ok) { notice(translateError(result.error), 'error'); return }
            ++readSerial
            if (snapshot.detail.jobId === jobId) closeDetail()
            observed.delete(jobId)
            publish({ queue: snapshot.queue.filter(job => job.id !== jobId) })
            notice(translate("download.requestHistoryDeleted"), 'info')
        } catch {
            notice(translate("download.noDeletionResponseReceivedCheckTheJobListAgain"), 'error')
        } finally {
            deleting.delete(jobId)
            publish({ deletingIds: [...deleting] })
            await refresh()
        }
    }

    async function startDownload(url: string, options?: DownloadJobOptions): Promise<DownloadResponse<DownloadJobDto>> {
        if (!connected || !snapshot.ready) {
            return failure('NOT_READY', translate("download.preparingTheDownloadConnection"))
        }

        if (snapshot.halted) {
            return failure('RUNS_PAUSED', snapshot.halted)
        }

        const ownGeneration = generation
        ++readSerial

        try {
            let result = await api.enqueue({
                mode: 'create',
                url: url.trim(),
                options
            })
            if (!result.ok && result.error.code === 'REDOWNLOAD_CONFIRMATION_REQUIRED') {
                const { mediaId, expectedRevision } = result.error.context
                if (Number.isSafeInteger(mediaId) && mediaId > 0 && Number.isSafeInteger(expectedRevision) &&
                    await confirm({ title: translate("download.replaceFiles"), message: translate("download.thisCollectionIsAlreadyRegisteredDownloadAgainAndReplace"), confirmLabel: translate("download.downloadAgain"), danger: true }) && connected && generation === ownGeneration) {
                    result = await api.enqueue({ mode: 'replace', url: url.trim(), options, mediaId, expectedRevision })
                }
            }

            if (result.ok && connected && generation === ownGeneration) {
                const job = structuredClone(result.data)

                observe(job)

                publish({
                    activeJobs: isGalleryDownload(job)
                        ? [job, ...snapshot.activeJobs.filter(item => item.id !== job.id)]
                        : snapshot.activeJobs,
                    queue: [job, ...snapshot.queue.filter((item) => item.id !== job.id)].slice(
                        0,
                        DOWNLOAD_PAGE_SIZE
                    ),
                    offset: 0
                })

                void runJob(job.id, 'manual_initial')
            }

            return result
        } catch {
            return failure(
                'TRANSPORT_ERROR',
                translate("download.noRegistrationResponseReceivedCheckTheJobList")
            )
        } finally {
            if (connected && generation === ownGeneration) {
                void refresh(0)
            }
        }
    }

    async function cancelDownload(jobId: string) {
        if (!connected || cancelling.has(jobId)) {
            return
        }

        cancelling.add(jobId)
        publish({ cancellingIds: [...cancelling] })

        try {
            const result = await api.cancel({ jobId })

            notice(
                result.ok
                    ? translate("download.cancellationRequestedCheckTheFinalStatus")
                    : translateError(result.error),
                result.ok ? 'info' : 'error'
            )
        } catch {
            notice(translate("download.noCancellationResponseReceivedCheckJobStatusAgain"), 'error')
        } finally {
            cancelling.delete(jobId)
            publish({ cancellingIds: [...cancelling] })
            await refresh()
        }
    }

    return {
        getSnapshot: () => snapshot,

        subscribe(listener) {
            listeners.add(listener)

            return () => {
                listeners.delete(listener)
            }
        },

        connect(callback) {
            if (connected) {
                throw new Error('Download store is already connected')
            }

            connected = true
            notify = callback

            const ownGeneration = ++generation

            async function tick() {
                await refresh()

                if (connected && generation === ownGeneration) {
                    timer = setTimeout(() => {
                        void tick()
                    }, pollMs)
                }
            }

            void tick()

            return () => {
                if (!connected || generation !== ownGeneration) {
                    return
                }

                connected = false
                notify = undefined
                ++generation
                ++readSerial
                ++detailSerial

                if (timer !== undefined) {
                    clearTimeout(timer)
                }

                timer = undefined
                batchPending.clear()
                if (snapshot.batchRetry) publish({ batchRetry: { ...snapshot.batchRetry, running: false } })
                publishRequested()

                publish({
                    ready: false,
                    loading: false
                })
            }
        },

        refresh,
        startDownload,
        runJob,
        cancelDownload,
        deleteJob,
        retryAll,
        openDetail,
        closeDetail
    }
}
