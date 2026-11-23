import { translateError, translate } from '@renderer/shared/i18n'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { LocalImportFileFilter, LocalImportFilePage, LocalImportPage, LocalImportResult } from '@contracts/local-import-api'
import { getApi } from '@renderer/shared/api'
import { canDismissImport, readDismissedImport, writeDismissedImport } from './panelVisibility'

export function useLocalImportViewModel(onComplete: () => void) {
    const [page, setPage] = useState<LocalImportPage | null>(null)
    const [error, setError] = useState<string | null>(null)
    const [requesting, setRequesting] = useState(false)
    const [offset, setOffset] = useState(0)
    const [dismissedJobId, setDismissedJobId] = useState(readDismissedImport)
    const latest = useRef({ onComplete })
    latest.current = { onComplete }
    const [showFiles, setShowFiles] = useState(false)
    const [fileState, setFileState] = useState<LocalImportFileFilter>('failed')
    const [fileOffset, setFileOffset] = useState(0)
    const [filePage, setFilePage] = useState<LocalImportFilePage | null>(null)
    const [fileError, setFileError] = useState<string | null>(null)
    const lock = useRef(false)
    const alive = useRef(true)
    const requestRevision = useRef(0)
    const notified = useRef('')
    const job = page?.job
    const busy = requesting || job?.state === 'scanning' || job?.state === 'running'
    useEffect(() => { alive.current = true; return () => { alive.current = false; requestRevision.current++ } }, [])
    const accept = useCallback((next: LocalImportPage) => {
        setPage(next)
        const job = next.job
        if (job && !['scanning', 'ready', 'running'].includes(job.state)) {
            const key = `${job.id}:${job.state}:${job.counts.processed}`
            if (notified.current !== key) { notified.current = key; latest.current.onComplete() }
        }
    }, [])
    const load = useCallback(async (jobId?: string, position = 0) => {
        const revision = ++requestRevision.current
        try {
            const result = await getApi().localImport.get({ jobId, offset: position, limit: 25 })
            if (!alive.current || revision !== requestRevision.current) return
            if (result.ok) { accept(result.data); setError(null) } else setError(translateError(result.error))
        } catch { if (alive.current && revision === requestRevision.current) setError(translate("가져오기 상태를 읽지 못했습니다. 새로고침해 주세요.")) }
    }, [accept])
    useEffect(() => { void load() }, [load])
    useEffect(() => {
        if (!job || !['scanning', 'running'].includes(job.state)) return
        let cancelled = false
        let timer: ReturnType<typeof setTimeout>
        const poll = async () => { await load(job.id, offset); if (!cancelled) timer = setTimeout(() => void poll(), 500) }
        timer = setTimeout(() => void poll(), 500)
        return () => { cancelled = true; clearTimeout(timer) }
    }, [job?.id, job?.state, load, offset])
    useEffect(() => {
        if (!showFiles || !job) return
        let stale = false
        setFilePage(null); setFileError(null)
        void getApi().localImport.files({ jobId: job.id, fileState, offset: fileOffset, limit: 25 }).then(result => {
            if (stale) return
            if (result.ok) setFilePage(result.data); else setFileError(translateError(result.error))
        }).catch(() => { if (!stale) setFileError(translate("파일 목록을 읽지 못했습니다.")) })
        return () => { stale = true }
    }, [showFiles, job?.id, job?.revision, fileState, fileOffset])
    const execute = useCallback(async (operation: () => Promise<LocalImportResult<LocalImportPage | boolean>>) => {
        if (lock.current) return
        lock.current = true; requestRevision.current++; setRequesting(true); setError(null)
        try {
            const result = await operation()
            if (!alive.current) return
            if (!result.ok) { setError(translateError(result.error)); return }
            if (typeof result.data === 'object') {
                if (result.data.job) { setOffset(0); setFileOffset(0); accept(result.data) }
            } else await load(job?.id, offset)
        } catch { if (alive.current) setError(translate("요청 결과를 확인하지 못했습니다. 상태를 새로고침해 주세요.")) }
        finally { lock.current = false; if (alive.current) setRequesting(false) }
    }, [accept, job?.id, load, offset])
    const goTo = (position: number) => { setOffset(position); void load(job?.id, position) }
    const canDismiss = canDismissImport(job, requesting)
    const dismiss = () => {
        if (!canDismiss || !job) return
        setShowFiles(false); setDismissedJobId(job.id); writeDismissedImport(job.id)
    }
    const reopen = () => { setDismissedJobId(null); writeDismissedImport(null) }
    return { page, error, busy, requesting, offset, showFiles, setShowFiles, fileState,
        canDismiss, dismissed: canDismissImport(job, false) && dismissedJobId === job?.id, dismiss, reopen,
        setFileState: (state: LocalImportFileFilter) => { setFileOffset(0); setFileState(state) },
        fileOffset, setFileOffset, filePage, fileError,
        choose: () => execute(() => getApi().localImport.choose()),
        run: () => job && execute(() => getApi().localImport.run({ jobId: job.id })),
        cancel: () => job && execute(() => getApi().localImport.cancel({ jobId: job.id })),
        pause: () => job && execute(() => getApi().localImport.pause({ jobId: job.id })),
        resume: () => job && execute(() => getApi().localImport.resume({ jobId: job.id })),
        exclude: (targetId?: string) => job && execute(() => getApi().localImport.exclude({ jobId: job.id, targetId, allFailed: !targetId })),
        retry: (targetId?: string) => job && execute(async () => {
            const result = await getApi().localImport.retry({ jobId: job.id, targetId, allFailed: !targetId })
            return result.ok ? (job.state === 'ready' ? getApi().localImport.run({ jobId: job.id }) : getApi().localImport.resume({ jobId: job.id })) : result
        }),
        rescan: () => job && execute(() => getApi().localImport.rescan({ jobId: job.id })),
        refresh: () => load(job?.id, offset), goTo }
}
