import type { Unsubscribe } from '@contracts/api'
import type {
    DownloadApi,
    DownloadJobDto,
    DownloadResponse,
    ManualDownloadTrigger
} from '@contracts/download-api'
import type { DownloadJobOptions } from '@contracts/download-api'
import { canRetryDownload, downloadTrigger } from './downloadPresentation'

export const DOWNLOAD_PAGE_SIZE = 50

type DownloadError = Extract<DownloadResponse<never>, { ok: false }>['error']

export interface DownloadNotice {
    message: string
    type: 'success' | 'error' | 'info'
}

export interface DownloadSnapshot {
    queue: DownloadJobDto[]
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

export function createDownloadStore(api: DownloadApi, pollMs = 2000): DownloadStore {
    let snapshot: DownloadSnapshot = {
        queue: [],
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
                        message: '작업 상세를 불러오지 못했습니다. 새로고침하세요.',
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

            for (const job of page.data) {
                observe(job)
            }

            const keep = new Set([
                ...page.data.map((job) => job.id),
                ...requested,
                ...batchPending,
                ...(snapshot.detail.jobId ? [snapshot.detail.jobId] : [])
            ])

            for (const id of observed.keys()) {
                if (!keep.has(id)) observed.delete(id)
            }

            publish({
                queue: page.data,
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

            const result = failure('TRANSPORT_ERROR', '작업 목록을 불러오지 못했습니다.')

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
            // 순차 실행은 main의 DownloadService가 소유한다.
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
                    halt('실행 상태를 확인해야 하므로 추가 실행 요청을 보류했습니다.')
                } else if (!quiet) {
                    notice(result.error.message, 'error')
                }
            } else if (result.data.status === 'blocked') {
                halt('작업 상태 확인이 필요합니다. 추가 실행 요청을 보류했습니다.')
            } else if (result.data.status === 'committed') {
                commit(result.data.jobId, result.data.attemptId)

                if (!quiet) notice('다운로드 등록이 완료되었습니다.', 'success')
            } else if (!quiet) {
                notice('이번 실행은 정리가 필요합니다. 작업 목록을 확인하세요.', 'info')
            }
            return result.ok && result.data.status === 'committed'
        } catch {
            halt('실행 응답을 받지 못했습니다. 결과 확인 전까지 추가 실행 요청을 보류합니다.')
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
                if (!result.ok) { notice(result.error.message, 'error'); return }
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
            notice(`일괄 재시도 ${counts.processed}/${counts.total}건 처리 · 실패 ${counts.failed}건 · 제외 ${counts.skipped}건`, counts.failed || snapshot.halted ? 'error' : 'info')
        } catch {
            if (connected && generation === ownGeneration) notice('일괄 재시도 대상을 확인하지 못했습니다. 작업 목록을 새로고침해 주세요.', 'error')
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
            if (!result.ok) { notice(result.error.message, 'error'); return }
            ++readSerial
            if (snapshot.detail.jobId === jobId) closeDetail()
            observed.delete(jobId)
            publish({ queue: snapshot.queue.filter(job => job.id !== jobId) })
            notice('요청 기록을 삭제했습니다.', 'info')
        } catch {
            notice('삭제 응답을 받지 못했습니다. 작업 목록을 다시 확인하세요.', 'error')
        } finally {
            deleting.delete(jobId)
            publish({ deletingIds: [...deleting] })
            await refresh()
        }
    }

    async function startDownload(url: string, options?: DownloadJobOptions): Promise<DownloadResponse<DownloadJobDto>> {
        if (!connected || !snapshot.ready) {
            return failure('NOT_READY', '다운로드 연결을 준비 중입니다.')
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
                    window.confirm('이미 등록된 묶음입니다. 다시 다운로드해 파일을 교체할까요? 사용자 제목·태그·즐겨찾기는 유지됩니다.')) {
                    result = await api.enqueue({ mode: 'replace', url: url.trim(), options, mediaId, expectedRevision })
                }
            }

            if (result.ok && connected && generation === ownGeneration) {
                const job = structuredClone(result.data)

                observe(job)

                publish({
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
                '등록 응답을 받지 못했습니다. 작업 목록을 확인하세요.'
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
                    ? '취소 요청을 전달했습니다. 최종 상태를 확인하세요.'
                    : result.error.message,
                result.ok ? 'info' : 'error'
            )
        } catch {
            notice('취소 응답을 받지 못했습니다. 작업 상태를 다시 확인하세요.', 'error')
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
