import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi
} from 'vitest'
import {
  createDownloadStore,
  DOWNLOAD_PAGE_SIZE
} from '@renderer/features/download/downloadStore'
import type {
  DownloadApi,
  DownloadJobDto,
  DownloadPageDto,
  DownloadResponse,
  DownloadRunDto
} from '@contracts/download-api'

const ok = <T>(data: T): DownloadResponse<T> => ({
  ok: true,
  data
})

const fail = (
  code: string
): DownloadResponse<never> => ({
  ok: false,
  error: {
    code,
    message: code,
    context: {}
  }
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void

  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })

  return { promise, resolve, reject }
}

function job(
  id: string,
  state: DownloadJobDto['state'] = 'pending'
): DownloadJobDto {
  return {
    id,
    url: 'https://example.invalid/' + id,
    title: null,
    state,
    phase: state === 'completed' ? 'finished' : null,
    progress: state === 'completed' ? 100 : 0,
    options: {},
    cleanupRequired: false,
    currentAttemptId:
      state === 'completed' ? 'attempt-' + id : null,
    errorCode: null
  }
}

function fixture() {
  const rows: DownloadJobDto[] = []
  let serial = 0

  const api = {
    remove: vi.fn<DownloadApi['remove']>(async ({ jobId }) => {
      const index = rows.findIndex(row => row.id === jobId)
      if (index < 0) return fail('NOT_FOUND')
      rows.splice(index, 1)
      return ok({ accepted: true })
    }),
    enqueue: vi.fn<DownloadApi['enqueue']>(
      async (request) => {
        const row = {
          ...job('new-' + ++serial),
          url: request.url,
          options: request.options ?? {}
        }

        rows.unshift(row)
        return ok(row)
      }
    ),

    list: vi.fn<DownloadApi['list']>(
      async ({
        limit = 50,
        offset = 0
      } = {}) =>
        ok({
          data: rows.slice(offset, offset + limit),
          activeJobs: rows.filter(row => row.state === 'pending' || row.state === 'running'),
          limit,
          offset,
          hasNextPage: rows.length > offset + limit
        })
    ),

    get: vi.fn<DownloadApi['get']>(
      async ({ jobId }) => {
        const row = rows.find(
          (item) => item.id === jobId
        )

        return row ? ok(row) : fail('NOT_FOUND')
      }
    ),

    run: vi.fn<DownloadApi['run']>(
      async ({ jobId }) => {
        const row = rows.find(
          (item) => item.id === jobId
        )!

        row.state = 'completed'
        row.phase = 'finished'
        row.currentAttemptId = 'attempt-' + jobId
        row.progress = 100

        return ok({
          status: 'committed',
          jobId,
          attemptId: row.currentAttemptId,
          mediaIdSnapshot: 1
        })
      }
    ),

    cancel: vi.fn<DownloadApi['cancel']>(
      async () => ok({ accepted: true })
    )
  }

  return { api, rows }
}

const stops: (() => void)[] = []

async function flush() {
  for (let i = 0; i < 16; i++) {
    await Promise.resolve()
  }
}

function open(f: ReturnType<typeof fixture>) {
  const notices: {
    message: string
    type: string
  }[] = []

  const store = createDownloadStore(f.api, 1000)
  const stop = store.connect(
    (notice) => notices.push(notice)
  )

  stops.push(stop)

  return { store, stop, notices }
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  stops.splice(0).forEach((stop) => stop())
  vi.clearAllTimers()
  vi.useRealTimers()
})

describe('download store', () => {
  it('keeps active gallery jobs across history pages and removes failures without deleting history', async () => {
    const f = fixture()
    f.rows.push(...Array.from({length: 55}, (_, i) => job('history-' + i, 'failed')), job('old', 'running'))
    const { store } = open(f); await flush()
    expect(store.getSnapshot().queue.some(row => row.id === 'old')).toBe(false)
    expect(store.getSnapshot().activeJobs.map(row => row.id)).toEqual(['old'])
    f.rows[55].state = 'failed'; await store.refresh()
    expect(store.getSnapshot().activeJobs).toEqual([])
    expect(store.getSnapshot().mediaRevision).toBe(0)
    expect(f.rows).toHaveLength(56)
    f.rows[55].state = 'pending'; await store.refresh()
    expect(store.getSnapshot().activeJobs.map(row => row.id)).toEqual(['old'])
    f.rows[55].state = 'completed'; f.rows[55].currentAttemptId = 'finished-old'; await store.refresh()
    expect(store.getSnapshot().activeJobs).toEqual([])
    expect(store.getSnapshot().mediaRevision).toBe(1)
    await store.refresh(); expect(store.getSnapshot().mediaRevision).toBe(1)
  })

  it('retries an off-page completion lookup after a transient read failure', async () => {
    const f = fixture()
    f.rows.push(...Array.from({length: 50}, (_, i) => job('history-' + i, 'failed')), job('old', 'running'))
    const { store } = open(f); await flush()
    f.rows[50].state = 'completed'; f.rows[50].currentAttemptId = 'new-attempt'
    f.api.get.mockRejectedValueOnce(new Error('temporary'))
    await store.refresh(); expect(store.getSnapshot().mediaRevision).toBe(0)
    await store.refresh(); expect(store.getSnapshot().mediaRevision).toBe(1)
  })
  it('한 provider의 조회 결과를 여러 소비자가 공유한다', async () => {
    const f = fixture()
    const { store } = open(f)
    const first = vi.fn()
    const second = vi.fn()

    store.subscribe(first)
    store.subscribe(second)

    await flush()

    expect(f.api.list).toHaveBeenCalledTimes(1)
    expect(first).toHaveBeenCalled()
    expect(second).toHaveBeenCalled()
    expect(store.getSnapshot().ready).toBe(true)
  })

  it('느린 조회 중에는 poll을 겹치지 않고 해제 후 추가 조회하지 않는다', async () => {
    const f = fixture()
    const pending =
      deferred<DownloadResponse<DownloadPageDto>>()

    f.api.list.mockReturnValueOnce(pending.promise)

    const { store, stop } = open(f)

    await vi.advanceTimersByTimeAsync(5000)

    expect(f.api.list).toHaveBeenCalledTimes(1)

    pending.resolve(
      ok({
        data: [],
        limit: 50,
        offset: 0,
        hasNextPage: false
      })
    )

    await flush()

    expect(store.getSnapshot().ready).toBe(true)

    await vi.advanceTimersByTimeAsync(1000)

    expect(f.api.list).toHaveBeenCalledTimes(2)

    stop()
    stop()

    await vi.advanceTimersByTimeAsync(5000)

    expect(f.api.list).toHaveBeenCalledTimes(2)
  })

  it('StrictMode 재연결 뒤 오래된 응답을 무시한다', async () => {
    const f = fixture()
    const old =
      deferred<DownloadResponse<DownloadPageDto>>()

    f.api.list.mockReturnValueOnce(old.promise)
    f.rows.push(job('current'))

    const { store, stop } = open(f)

    stop()
    stops.push(store.connect(() => {}))

    await flush()

    old.resolve(
      ok({
        data: [job('old')],
        limit: 50,
        offset: 0,
        hasNextPage: false
      })
    )

    await flush()

    expect(
      store.getSnapshot().queue.map((row) => row.id)
    ).toEqual(['current'])
  })

  it('조회한 기존 대기·중단·취소 작업을 자동 실행하지 않는다', async () => {
    const f = fixture()

    f.rows.push(
      job('pending'),
      job('interrupted', 'interrupted'),
      job('cancelled', 'cancelled')
    )

    const { store } = open(f)

    await flush()
    await store.refresh()
    await vi.advanceTimersByTimeAsync(1000)

    expect(f.api.run).not.toHaveBeenCalled()
  })

  it('새 등록에만 수동 최초 실행을 요청하고 완료를 갱신한다', async () => {
    const f = fixture()
    const { store, notices } = open(f)

    await flush()

    const result = await store.startDownload(
      '  https://example.invalid/post  '
    )

    expect(result.ok).toBe(true)

    await flush()

    expect(f.api.enqueue).toHaveBeenCalledWith({
      mode: 'create',
      url: 'https://example.invalid/post'
    })

    expect(f.api.run).toHaveBeenCalledWith({
      jobId: 'new-1',
      trigger: 'manual_initial'
    })

    expect(
      store.getSnapshot().mediaRevision
    ).toBe(1)

    expect(
      notices.filter(
        (notice) => notice.type === 'success'
      )
    ).toHaveLength(1)
  })

  it('여러 실행 요청의 순서는 main에 맡긴다', async () => {
    const f = fixture()
    const a =
      deferred<DownloadResponse<DownloadRunDto>>()
    const b =
      deferred<DownloadResponse<DownloadRunDto>>()

    f.api.run
      .mockReturnValueOnce(a.promise)
      .mockReturnValueOnce(b.promise)

    const { store } = open(f)

    await flush()

    await store.startDownload(
      'https://example.invalid/a'
    )
    await store.startDownload(
      'https://example.invalid/b'
    )

    expect(f.api.run).toHaveBeenCalledTimes(2)
    expect(
      store.getSnapshot().requestedIds
    ).toHaveLength(2)

    a.resolve(
      ok({
        status: 'committed',
        jobId: 'new-1',
        attemptId: 'a',
        mediaIdSnapshot: 1
      })
    )

    b.resolve(
      ok({
        status: 'committed',
        jobId: 'new-2',
        attemptId: 'b',
        mediaIdSnapshot: 2
      })
    )

    await flush()

    expect(
      store.getSnapshot().requestedIds
    ).toEqual([])
  })

  it('실행 통신 실패는 원문 노출·자동 재요청 없이 보류한다', async () => {
    const f = fixture()

    f.api.run.mockRejectedValueOnce(
      new Error('secret-path')
    )

    const { store, notices } = open(f)

    await flush()

    await store.startDownload(
      'https://example.invalid/post'
    )

    await flush()

    expect(
      store.getSnapshot().halted
    ).not.toBeNull()

    expect(
      JSON.stringify(notices)
    ).not.toContain('secret-path')

    expect(
      (
        await store.startDownload(
          'https://example.invalid/next'
        )
      ).ok
    ).toBe(false)

    await vi.advanceTimersByTimeAsync(3000)

    expect(f.api.run).toHaveBeenCalledTimes(1)
    expect(f.api.enqueue).toHaveBeenCalledTimes(1)
  })

  it('blocked 결과를 완료로 표시하지 않는다', async () => {
    const f = fixture()

    f.api.run.mockResolvedValueOnce(
      ok({
        status: 'blocked',
        jobId: 'new-1',
        reason: 'ENGINE_STATE_UNKNOWN'
      })
    )

    const { store, notices } = open(f)

    await flush()

    await store.startDownload(
      'https://example.invalid/post'
    )

    await flush()

    expect(
      store.getSnapshot().halted
    ).not.toBeNull()

    expect(
      store.getSnapshot().mediaRevision
    ).toBe(0)

    expect(
      notices.some(
        (notice) => notice.type === 'success'
      )
    ).toBe(false)
  })

  it('정리 필요 결과는 완료와 구분한다', async () => {
    const f = fixture()

    f.api.run.mockResolvedValueOnce(
      ok({
        status: 'cleanup_required',
        jobId: 'new-1',
        attemptId: 'a',
        taskId: 'cleanup'
      })
    )

    const { store, notices } = open(f)

    await flush()

    await store.startDownload(
      'https://example.invalid/post'
    )

    await flush()

    expect(
      store.getSnapshot().mediaRevision
    ).toBe(0)

    expect(
      store.getSnapshot().halted
    ).toBeNull()

    expect(
      notices.some(
        (notice) => notice.type === 'info'
      )
    ).toBe(true)
  })

  it('poll과 실행 응답이 같은 완료를 알려도 한 번만 갱신한다', async () => {
    const f = fixture()
    const result =
      deferred<DownloadResponse<DownloadRunDto>>()

    f.api.run.mockReturnValueOnce(result.promise)

    const { store } = open(f)

    await flush()

    await store.startDownload(
      'https://example.invalid/post'
    )

    Object.assign(f.rows[0], {
      state: 'completed',
      phase: 'finished',
      currentAttemptId: 'a'
    })

    await store.refresh()

    expect(
      store.getSnapshot().mediaRevision
    ).toBe(1)

    result.resolve(
      ok({
        status: 'committed',
        jobId: 'new-1',
        attemptId: 'a',
        mediaIdSnapshot: 1
      })
    )

    await flush()

    expect(
      store.getSnapshot().mediaRevision
    ).toBe(1)
  })

  it('다른 페이지에서 다시 본 과거 완료를 새 완료로 세지 않는다', async () => {
    const f = fixture()

    for (let i = 0; i < 60; i++) {
      f.rows.push(job(String(i), 'completed'))
    }

    const { store } = open(f)

    await flush()
    await store.refresh(50)
    await store.refresh(0)

    expect(
      store.getSnapshot().mediaRevision
    ).toBe(0)

    expect(f.api.run).not.toHaveBeenCalled()
  })

  it('입력 객체가 바뀌어도 이전 snapshot은 유지한다', async () => {
    const f = fixture()
    f.rows.push(job('old'))

    const { store } = open(f)

    await flush()

    const previous = store.getSnapshot()

    Object.assign(f.rows[0], {
      state: 'completed',
      phase: 'finished',
      currentAttemptId: 'a'
    })

    await store.refresh()

    expect(
      previous.queue[0].state
    ).toBe('pending')

    expect(
      store.getSnapshot().queue[0].state
    ).toBe('completed')

    expect(
      store.getSnapshot().mediaRevision
    ).toBe(1)
  })

  it('늦게 도착한 이전 페이지가 현재 페이지를 덮어쓰지 않는다', async () => {
    const f = fixture()

    for (let i = 0; i < 60; i++) {
      f.rows.push(job(String(i)))
    }

    const { store } = open(f)

    await flush()

    const pending =
      deferred<DownloadResponse<DownloadPageDto>>()

    f.api.list.mockReturnValueOnce(pending.promise)

    const old = store.refresh(0)

    await store.refresh(50)

    pending.resolve(
      ok({
        data: [job('stale')],
        limit: 50,
        offset: 0,
        hasNextPage: false
      })
    )

    await old

    expect(store.getSnapshot().offset).toBe(50)
    expect(
      store.getSnapshot().queue[0].id
    ).toBe('50')
  })

  it('NOT_READY 동안 실행하지 않고 이후 조회 성공으로 연결한다', async () => {
    const f = fixture()

    f.api.list.mockResolvedValueOnce(
      fail('NOT_READY')
    )

    const { store, notices } = open(f)

    await flush()

    expect(store.getSnapshot().ready).toBe(false)

    expect(
      (
        await store.startDownload(
          'https://example.invalid/post'
        )
      ).ok
    ).toBe(false)

    expect(f.api.enqueue).not.toHaveBeenCalled()
    expect(notices).toEqual([])

    await vi.advanceTimersByTimeAsync(1000)

    expect(store.getSnapshot().ready).toBe(true)
  })

  it('등록 도중 해제되면 늦은 응답으로 실행하지 않는다', async () => {
    const f = fixture()
    const pending =
      deferred<DownloadResponse<DownloadJobDto>>()

    f.api.enqueue.mockReturnValueOnce(
      pending.promise
    )

    const { store, stop } = open(f)

    await flush()

    const result = store.startDownload(
      'https://example.invalid/post'
    )

    stop()

    pending.resolve(ok(job('late')))

    await result
    await flush()

    expect(f.api.run).not.toHaveBeenCalled()
    expect(store.getSnapshot().ready).toBe(false)
  })

  it('취소 수락만으로 최종 취소 상태를 만들지 않는다', async () => {
    const f = fixture()

    f.rows.push({
      ...job('active', 'running'),
      phase: 'downloading',
      currentAttemptId: 'a'
    })

    const { store } = open(f)

    await flush()
    await store.cancelDownload('active')

    expect(
      store.getSnapshot().queue[0].state
    ).toBe('running')

    f.rows[0].state = 'cancelled'

    await store.refresh()

    expect(
      store.getSnapshot().queue[0].state
    ).toBe('cancelled')

    expect(
      store.getSnapshot().mediaRevision
    ).toBe(0)
  })

  it('committing 취소 거절을 화면 오류로 전달한다', async () => {
    const f = fixture()

    f.rows.push({
      ...job('active', 'running'),
      phase: 'committing',
      currentAttemptId: 'a'
    })

    f.api.cancel.mockResolvedValueOnce(
      fail('CANNOT_CANCEL_COMMITTING')
    )

    const { store, notices } = open(f)

    await flush()
    await store.cancelDownload('active')

    expect(
      store.getSnapshot().queue[0].state
    ).toBe('running')

    expect(notices).toContainEqual({
      message: 'CANNOT_CANCEL_COMMITTING',
      type: 'error'
    })
  })

  it('같은 실행 요청을 중복 전송하지 않고 수동 trigger를 보존한다', async () => {
    const f = fixture()
    f.rows.push(job('failed', 'failed'))

    const pending =
      deferred<DownloadResponse<DownloadRunDto>>()

    f.api.run.mockReturnValueOnce(pending.promise)

    const { store } = open(f)

    await flush()

    const first = store.runJob(
      'failed',
      'manual_retry'
    )

    await store.runJob(
      'failed',
      'manual_retry'
    )

    expect(f.api.run).toHaveBeenCalledTimes(1)

    expect(f.api.run).toHaveBeenCalledWith({
      jobId: 'failed',
      trigger: 'manual_retry'
    })

    pending.resolve(
      fail('REVISION_CONFLICT')
    )

    await first

    expect(
      store.getSnapshot().halted
    ).toBeNull()
  })

  it('취소 통신 실패의 원문을 노출하지 않는다', async () => {
    const f = fixture()
    f.rows.push(job('active', 'running'))

    f.api.cancel.mockRejectedValueOnce(
      new Error('secret-path')
    )

    const { store, notices } = open(f)

    await flush()
    await store.cancelDownload('active')

    expect(
      JSON.stringify(notices)
    ).not.toContain('secret-path')

    expect(
      store.getSnapshot().queue[0].state
    ).toBe('running')
  })

  it('목록은 페이지 크기로 제한하고 전체 작업을 순회하지 않는다', async () => {
    const f = fixture()

    for (let i = 0; i < 200; i++) {
      f.rows.push(job(String(i)))
    }

    const { store } = open(f)

    await flush()

    expect(
      store.getSnapshot().queue
    ).toHaveLength(DOWNLOAD_PAGE_SIZE)

    expect(f.api.list).toHaveBeenCalledTimes(1)

    expect(f.api.list).toHaveBeenCalledWith({
      limit: DOWNLOAD_PAGE_SIZE,
      offset: 0
    })

    expect(f.api.get).not.toHaveBeenCalled()
  })
})

describe('download detail selection', () => {
    it('이전 페이지 항목도 선택한 작업 ID로 계속 갱신한다', async () => {
        const f = fixture()
        for (let i = 0; i < 51; i++) f.rows.push(job('job-' + i))
        const { store } = open(f)
        await flush()
        await store.openDetail('job-0')
        await store.refresh(50)
        f.rows[0].state = 'cancelled'
        await vi.advanceTimersByTimeAsync(1000)
        expect(store.getSnapshot().queue).toHaveLength(1)
        expect(store.getSnapshot().detail.data?.id).toBe('job-0')
        expect(store.getSnapshot().detail.data?.state).toBe('cancelled')
        const revision = store.getSnapshot().mediaRevision
        f.rows[0].state = 'completed'
        f.rows[0].currentAttemptId = 'finished-off-page'
        await vi.advanceTimersByTimeAsync(1000)
        expect(store.getSnapshot().mediaRevision).toBe(revision + 1)
    })

    it('다른 작업 선택 뒤 도착한 응답은 현재 상세를 덮어쓰지 않는다', async () => {
        const f = fixture()
        f.rows.push(job('first'), job('second'))
        const { store } = open(f)
        await flush()
        const pending = deferred<DownloadResponse<DownloadJobDto>>()
        f.api.get.mockReturnValueOnce(pending.promise)
        const first = store.openDetail('first')
        await store.openDetail('second')
        pending.resolve(ok(job('first')))
        await first
        expect(store.getSnapshot().detail.data?.id).toBe('second')
    })

    it('닫은 뒤 늦은 응답이 도착해도 패널을 다시 열지 않는다', async () => {
        const f = fixture()
        const { store } = open(f)
        await flush()
        const pending = deferred<DownloadResponse<DownloadJobDto>>()
        f.api.get.mockReturnValueOnce(pending.promise)
        const read = store.openDetail('first')
        store.closeDetail()
        pending.resolve(ok(job('first')))
        await read
        expect(store.getSnapshot().detail).toEqual({
            jobId: null,
            data: null,
            loading: false,
            error: null
        })
    })

    it('상세 조회 실패는 오래된 조작 버튼 대신 오류를 표시하고 재조회할 수 있다', async () => {
        const f = fixture()
        f.rows.push(job('first'))
        const { store } = open(f)
        await flush()
        await store.openDetail('first')
        f.api.get.mockRejectedValueOnce(new Error('offline'))
        await store.refresh()
        expect(store.getSnapshot().detail.data).toBeNull()
        expect(store.getSnapshot().detail.error?.code).toBe('TRANSPORT_ERROR')
        await store.openDetail('first')
        expect(store.getSnapshot().detail.error).toBeNull()
        expect(store.getSnapshot().detail.data?.id).toBe('first')
    })

    it('구독 해제 후 도착한 상세 응답은 반영하지 않는다', async () => {
        const f = fixture()
        const { store, stop } = open(f)
        await flush()
        const pending = deferred<DownloadResponse<DownloadJobDto>>()
        f.api.get.mockReturnValueOnce(pending.promise)
        const read = store.openDetail('first')
        stop()
        const snapshot = store.getSnapshot()
        pending.resolve(ok(job('first')))
        await read
        expect(store.getSnapshot()).toBe(snapshot)
    })
})

describe('download history deletion and bulk retry', () => {
  it('deletes once, closes its detail and moves back from an empty last page', async () => {
    const f = fixture()
    f.rows.push(...Array.from({ length: 51 }, (_, i) => job(String(i), 'failed')))
    const { store } = open(f); await flush(); await store.refresh(50); await store.openDetail('50')
    const pending = deferred<DownloadResponse<{ accepted: true }>>()
    f.api.remove.mockImplementationOnce(() => pending.promise)
    const deleting = store.deleteJob('50'); await store.deleteJob('50'); await store.runJob('50', 'manual_retry')
    expect(f.api.remove).toHaveBeenCalledTimes(1); expect(f.api.run).not.toHaveBeenCalled()
    expect(store.getSnapshot().deletingIds).toEqual(['50'])
    f.rows.pop(); pending.resolve(ok({ accepted: true })); await deleting
    expect(store.getSnapshot().offset).toBe(0); expect(store.getSnapshot().queue).toHaveLength(50)
    expect(store.getSnapshot().detail.jobId).toBeNull(); expect(store.getSnapshot().deletingIds).toEqual([])
  })

  it('keeps history on a rejection/transport error and ignores a stale list after deletion', async () => {
    const f = fixture(); f.rows.push(job('one', 'cancelled'))
    const { store, notices } = open(f); await flush(); await store.openDetail('one')
    f.api.remove.mockResolvedValueOnce(fail('CLEANUP_REQUIRED')); await store.deleteJob('one')
    expect(store.getSnapshot().detail.jobId).toBe('one'); expect(notices.at(-1)?.message).toBe('CLEANUP_REQUIRED')
    f.api.remove.mockRejectedValueOnce(new Error('offline')); await store.deleteJob('one')
    expect(store.getSnapshot().queue).toHaveLength(1); expect(store.getSnapshot().deletingIds).toEqual([])
    const pending = deferred<DownloadResponse<DownloadPageDto>>(); f.api.list.mockReturnValueOnce(pending.promise)
    const stale = store.refresh(); await store.deleteJob('one')
    pending.resolve(ok({ data: [job('one', 'cancelled')], limit: 50, offset: 0, hasNextPage: false })); await stale
    expect(store.getSnapshot().queue).toEqual([]); expect(store.getSnapshot().detail.jobId).toBeNull()
  })

  it('retries failed/cancelled requests across pages with the correct trigger and skips ineligible history', async () => {
    const f = fixture()
    f.rows.push(...Array.from({ length: 205 }, (_, i) => job(String(i), i % 2 ? 'failed' : 'cancelled')))
    f.rows[0].cleanupRequired = true; f.rows[1].historyOnly = true; f.rows[2].options = null
    f.rows[3].state = 'interrupted'; f.rows[4].state = 'completed'
    const { store, notices } = open(f); await flush(); await store.retryAll()
    expect(f.api.list).toHaveBeenCalledWith({ limit: 200, offset: 200 })
    expect(f.api.run).toHaveBeenCalledTimes(200)
    expect(f.api.run).toHaveBeenCalledWith({ jobId: '5', trigger: 'manual_retry' })
    expect(f.api.run).toHaveBeenCalledWith({ jobId: '204', trigger: 'manual_restart' })
    expect(store.getSnapshot().batchRetry).toEqual({ running: false, total: 200, processed: 200, failed: 0, skipped: 3 })
    expect(notices.filter(n => n.type === 'success')).toHaveLength(0)
  })

  it('runs sequentially once per snapshot, blocks duplicate actions and continues after a normal download failure', async () => {
    const f = fixture(); f.rows.push(job('first', 'failed'), job('second', 'cancelled'))
    const pending = deferred<DownloadResponse<DownloadRunDto>>(); f.api.run.mockReturnValueOnce(pending.promise)
    const { store } = open(f); await flush()
    const batch = store.retryAll(); await flush()
    expect(store.getSnapshot().requestedIds).toEqual(expect.arrayContaining(['first', 'second']))
    await store.retryAll(); await store.runJob('second', 'manual_restart'); await store.deleteJob('first'); await store.deleteJob('second')
    expect(f.api.run).toHaveBeenCalledTimes(1); expect(f.api.remove).not.toHaveBeenCalled()
    pending.resolve(fail('DOWNLOAD_ENGINE_FAILED')); await batch
    expect(f.api.run).toHaveBeenCalledTimes(2)
    expect(store.getSnapshot().batchRetry).toEqual({ running: false, total: 2, processed: 2, failed: 1, skipped: 0 })
    expect(store.getSnapshot().requestedIds).toEqual([])
    expect(f.rows[0].state).toBe('failed')
  })

  it('stops on unknown execution state and releases the remaining reservations', async () => {
    const f = fixture(); f.rows.push(job('first', 'failed'), job('second', 'failed'))
    f.api.run.mockRejectedValueOnce(new Error('unknown response'))
    const { store } = open(f); await flush(); await store.retryAll()
    expect(f.api.run).toHaveBeenCalledTimes(1); expect(store.getSnapshot().halted).not.toBeNull()
    expect(store.getSnapshot().requestedIds).toEqual([]); expect(store.getSnapshot().batchRetry?.running).toBe(false)
    await store.retryAll(); expect(f.api.run).toHaveBeenCalledTimes(1)
  })

  it('does not execute a partial snapshot when paging fails and handles zero eligible requests', async () => {
    const f = fixture(); f.rows.push(job('first', 'failed'))
    const { store } = open(f); await flush()
    f.api.list.mockResolvedValueOnce(ok({ data: [job('first', 'failed')], limit: 200, offset: 0, hasNextPage: true })).mockResolvedValueOnce(fail('DATABASE_ERROR'))
    await store.retryAll(); expect(f.api.run).not.toHaveBeenCalled(); expect(store.getSnapshot().batchRetry?.running).toBe(false)
    f.api.list.mockRejectedValueOnce(new Error('offline')); await store.retryAll(); expect(f.api.run).not.toHaveBeenCalled()
    f.api.list.mockResolvedValueOnce(ok({ data: [], limit: 200, offset: 0, hasNextPage: true })); await store.retryAll(); expect(f.api.run).not.toHaveBeenCalled()
    f.rows[0].state = 'completed'; await store.retryAll()
    expect(store.getSnapshot().batchRetry).toEqual({ running: false, total: 0, processed: 0, failed: 0, skipped: 0 })
  })

  it('deduplicates overlapping pages and skips requests already being executed', async () => {
    const f = fixture(); f.rows.push(job('active', 'failed'), job('other', 'failed'))
    const pending = deferred<DownloadResponse<DownloadRunDto>>(); f.api.run.mockReturnValueOnce(pending.promise)
    const { store } = open(f); await flush(); const manual = store.runJob('active', 'manual_retry')
    f.api.list.mockResolvedValueOnce(ok({ data: [job('active', 'failed'), job('other', 'failed')], limit: 200, offset: 0, hasNextPage: true })).mockResolvedValueOnce(ok({ data: [job('other', 'failed')], limit: 200, offset: 2, hasNextPage: false }))
    await store.retryAll()
    expect(f.api.run).toHaveBeenCalledTimes(2); expect(store.getSnapshot().batchRetry?.skipped).toBe(1)
    pending.resolve(fail('DOWNLOAD_ENGINE_FAILED')); await manual
  })

  it('clears an interrupted collection on disconnect and permits a new batch after reconnect', async () => {
    const f = fixture(); f.rows.push(job('one', 'failed'))
    const { store, stop } = open(f); await flush()
    const pending = deferred<DownloadResponse<DownloadPageDto>>(); f.api.list.mockReturnValueOnce(pending.promise)
    const batch = store.retryAll(); stop()
    expect(store.getSnapshot().batchRetry?.running).toBe(false)
    const stopAgain = store.connect(() => {}); stops.push(stopAgain); await flush()
    await store.retryAll(); pending.resolve(ok({ data: [job('one', 'failed')], limit: 200, offset: 0, hasNextPage: false })); await batch
    expect(f.api.run).toHaveBeenCalledTimes(1); expect(store.getSnapshot().requestedIds).toEqual([])
  })

  it('does not start the next request when the provider disconnects during execution', async () => {
    const f = fixture(); f.rows.push(job('first', 'failed'), job('second', 'failed'))
    const pending = deferred<DownloadResponse<DownloadRunDto>>(); f.api.run.mockReturnValueOnce(pending.promise)
    const { store, stop } = open(f); await flush(); const batch = store.retryAll(); await flush()
    stop(); pending.resolve(ok({ status: 'committed', jobId: 'first', attemptId: 'new-first', mediaIdSnapshot: 1 })); await batch
    expect(f.api.run).toHaveBeenCalledTimes(1); expect(store.getSnapshot().requestedIds).toEqual([])
    expect(store.getSnapshot().batchRetry?.running).toBe(false)
    await store.deleteJob('second'); expect(f.api.remove).not.toHaveBeenCalled()
  })

  it('does not reserve a request while its deletion or cancellation response is pending', async () => {
    for (const operation of ['delete', 'cancel'] as const) {
      const f = fixture(); f.rows.push(job('busy', 'failed'), job('other', 'cancelled'))
      const pending = deferred<DownloadResponse<{ accepted: true }>>()
      if (operation === 'delete') f.api.remove.mockReturnValueOnce(pending.promise)
      else f.api.cancel.mockReturnValueOnce(pending.promise)
      const { store } = open(f); await flush()
      const request = operation === 'delete' ? store.deleteJob('busy') : store.cancelDownload('busy')
      await store.retryAll()
      expect(f.api.run).toHaveBeenCalledTimes(1); expect(f.api.run).toHaveBeenCalledWith({ jobId: 'other', trigger: 'manual_restart' })
      expect(store.getSnapshot().batchRetry?.skipped).toBe(1)
      pending.resolve(ok({ accepted: true })); await request
    }
  })
})
